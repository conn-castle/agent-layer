package quota

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/antigravity"
	"github.com/conn-castle/agent-layer/internal/ownedprocess"
)

type nativeCredential struct {
	lockDir string
	dir     string
	env     []string
}

const nativeRenewalGrace = time.Second

func (s credentialSource) native(lockDir, envKey, envValue string) *nativeCredential {
	if s.file != "" {
		return nil
	}
	env := s.env
	if envKey != "" {
		env = clients.SetUniqueEnv(env, envKey, envValue)
	}
	return &nativeCredential{lockDir: lockDir, dir: textFrom(s.workDir, s.root), env: env}
}

// Use an advisory lock on the directory inode, which stays stable across
// atomic credential-file replacements. Separate quota processes can share
// the same login and lock without creating anything in its store.
func (n *nativeCredential) lock(ctx context.Context) (func(), error) {
	dir, err := os.Open(n.lockDir) // #nosec G304 -- selected native credential directory.
	if err != nil {
		return nil, err
	}
	closeDir := func() { _ = dir.Close() }
	fd := int(dir.Fd()) // #nosec G115 -- Unix file descriptor.
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for ctx.Err() == nil {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return closeDir, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			closeDir()
			return nil, err
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
		}
	}
	closeDir()
	return nil, ctx.Err()
}

// updatedCredential only accepts a replacement from the same selected store.
func updatedCredential(ctx context.Context, req Request, source Source, prior credential, now func() time.Time, afterRenewal bool) (credential, failure) {
	next, fail := loadCredential(ctx, req, source)
	if ctx.Err() != nil {
		return credential{}, contextFailure(ctx)
	}
	if fail.reason == reasonMissingCredential && afterRenewal {
		return credential{}, failure{status: statusLoginExpired, reason: reasonExpiredCredentials, reauth: ptr(true)}
	}
	if fail.status == statusUnsupported || fail.status == statusNonSubscription || fail.status == statusMismatch {
		return credential{}, fail
	}
	if fail.status != "" {
		return credential{}, failure{}
	}
	if prior.identity != nil && next.identity != nil && prior.identity.ID != "" &&
		next.identity.ID != "" && prior.identity.ID != next.identity.ID {
		return credential{}, failure{status: statusMismatch, reason: "credential_scope_mismatch", reauth: ptr(false)}
	}
	if next.token == prior.token || next.expired(now()) {
		return credential{}, failure{}
	}
	return next, failure{}
}

func renewCredential(ctx context.Context, req Request, source Source, auth credential, now func() time.Time) (credential, failure) {
	expired := auth.authenticationFailure(reasonExpiredCredentials, now())
	if auth.native == nil || !auth.canRenew(now()) {
		if next, fail := updatedCredential(ctx, req, source, auth, now, false); next.token != "" || fail.status != "" {
			return next, fail
		}
		return auth, expired
	}
	release, err := auth.native.lock(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return auth, contextFailure(ctx)
		}
		return auth, expired
	}
	defer release()
	if next, fail := updatedCredential(ctx, req, source, auth, now, false); next.token != "" || fail.status != "" {
		return next, fail
	}
	// Leave part of the caller's deadline for the actual quota request.
	budget := 30 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		// Avoid starting a token exchange when there is little time for the
		// provider to commit its replacement. Leave time for cleanup and HTTP.
		if remaining < 10*time.Second {
			expired.retryable = true
			return auth, expired
		}
		budget = min(budget, remaining-3*nativeRenewalGrace)
	}
	renewalCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	err = auth.native.refresh(renewalCtx, source.Agent, req.Project.Root)
	if next, fail := updatedCredential(ctx, req, source, auth, now, true); next.token != "" || fail.status != "" {
		return next, fail
	}
	if errors.Is(err, errRefreshRevoked) {
		return auth, failure{status: statusLoginExpired, reason: credentialRevoked, reauth: ptr(true)}
	}
	if errors.Is(err, errRefreshExpired) {
		return auth, failure{status: statusLoginExpired, reason: reasonExpiredCredentials, reauth: ptr(true)}
	}
	expired.retryable = renewalCtx.Err() != nil
	return auth, expired
}

func (n nativeCredential) refresh(ctx context.Context, agent, root string) error {
	// Keep the native call on the selected store and first-party network path.
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy",
		"CODEX_REFRESH_TOKEN_URL_OVERRIDE", "CODEX_LOGIN_BASE_URL"} {
		n.env = clients.UnsetEnv(n.env, key)
	}
	if agent == agentClaude {
		return agentoptions.RefreshClaudeCredentials(ctx, n.dir, n.env, nativeRenewalGrace)
	}
	switch agent {
	case agentCodex:
		return refreshCodex(ctx, &n)
	case agentGrok:
		// Grok's model catalog command completes native sign-in renewal
		// before it exits, even when its catalog reports the old auth state.
		return n.run(ctx, agentGrok, []string{"--no-auto-update", "models"})
	case agentAntigravity:
		n.env = antigravity.ConfigureEnvironment(n.env)
		return n.run(ctx, "agy", []string{"--gemini_dir=" + filepath.Join(root, ".agy"), "models"})
	case agentMuse:
		return n.run(ctx, agentMuse, []string{"usage", "--json"})
	default:
		return nil
	}
}

func (n *nativeCredential) command(binary string, args []string) (*exec.Cmd, error) {
	path, err := exec.LookPath(binary)
	if err != nil {
		return nil, err
	}
	//nolint:noctx // ownedprocess owns cancellation and joins the process group.
	cmd := exec.Command(path, args...) // #nosec G204 -- native command with fixed non-turn arguments.
	cmd.Dir, cmd.Env = n.dir, n.env
	return cmd, nil
}

func (n *nativeCredential) run(ctx context.Context, binary string, args []string) error {
	cmd, err := n.command(binary, args)
	if err != nil {
		return err
	}
	wait, err := startNativeRenewal(ctx, cmd, nil)
	if err != nil {
		return err
	}
	return wait()
}

// Let a provider finish writing a rotated credential before the owned process
// group is forcibly stopped. The operation itself stops at ctx's deadline.
func startNativeRenewal(ctx context.Context, cmd *exec.Cmd, stop func()) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	child, cancel := context.WithCancel(context.WithoutCancel(ctx))
	wait, err := ownedprocess.Start(child, cmd)
	if err != nil {
		cancel()
		return nil, err
	}
	stopOnCancel := context.AfterFunc(ctx, func() {
		if stop != nil {
			stop()
		}
		time.AfterFunc(nativeRenewalGrace, cancel)
	})
	return func() error {
		defer cancel()
		defer stopOnCancel()
		return wait()
	}, nil
}

var (
	errRefreshRevoked = errors.New("native renewal rejected a revoked grant")
	errRefreshExpired = errors.New("native renewal rejected an expired grant")
)

func refreshCodex(ctx context.Context, n *nativeCredential) error {
	cmd, err := n.command(agentCodex, []string{"app-server"})
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer func() { _ = stdin.Close() }()
	stdout, childOutput, err := os.Pipe()
	if err != nil {
		return err
	}
	defer func() { _ = stdout.Close(); _ = childOutput.Close() }()
	cmd.Stdout = childOutput
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	wait, err := startNativeRenewal(callCtx, cmd, func() { _ = stdin.Close() })
	if err != nil {
		return err
	}
	_ = childOutput.Close()
	defer func() {
		_ = stdin.Close()
		cancel()
		_ = wait()
	}()
	stopClose := context.AfterFunc(ctx, func() { _ = stdout.Close(); _ = stdin.Close() })
	defer stopClose()
	const initialize = `{"id":1,"method":"initialize","params":{"clientInfo":{"name":"agent_layer_quota","version":"1"}}}`
	const accountRead = `{"method":"initialized"}` + "\n" + `{"id":2,"method":"account/read","params":{"refreshToken":true}}`
	decoder := json.NewDecoder(io.LimitReader(stdout, maxPayloadSize))
	for i, frame := range []string{initialize, accountRead} {
		if _, err := io.WriteString(stdin, frame+"\n"); err != nil {
			return err
		}
		for {
			var reply struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := decoder.Decode(&reply); err != nil {
				return err
			}
			if reply.Method != "" || len(reply.ID) != 1 || reply.ID[0] != byte('1'+i) {
				continue
			}
			if reply.Error != nil {
				if i == 1 {
					if rejection := codexRefreshRejection(reply.Error.Message); rejection != nil {
						return rejection
					}
				}
				return errors.New("native Codex rejected the renewal request")
			}
			break
		}
	}
	return nil
}

func codexRefreshRejection(message string) error {
	message = strings.ToLower(message)
	switch {
	case strings.Contains(message, "refresh token was revoked"), strings.Contains(message, "refresh token has been invalidated"):
		return errRefreshRevoked
	case strings.Contains(message, "refresh token has expired"), strings.Contains(message, "refresh token was already used"),
		strings.Contains(message, "you have since logged out or signed in to another account"):
		return errRefreshExpired
	default:
		return nil
	}
}
