// Package herdr implements Agent Layer's session-only HerdR recovery hook.
package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Environment names supplied by HerdR or Agent Layer's development launcher.
const (
	EnvEnabled            = "HERDR_ENV"
	EnvSocketPath         = "HERDR_SOCKET_PATH"
	EnvPaneID             = "HERDR_PANE_ID"
	EnvDispatch           = "AL_DISPATCH_ACTIVE"
	EnvDevExecutable      = "AL_DEV_EXECUTABLE"
	EnvDevBypass          = "AL_DEV_BYPASS_VERSION_DISPATCH" // #nosec G101 -- environment switch, not a credential.
	envRecoveryGeneration = "AL_HERDR_RECOVERY_GENERATION"

	providerClaude  = "claude"
	providerCodex   = "codex"
	providerAgy     = "agy"
	providerMuse    = "muse"
	providerGrok    = "grok"
	sourceMuseHerdR = "muse:herdr"
	sourceCodexAL   = "al:codex"

	flagResume               = "--resume"
	flagConversation         = "--conversation"
	eventSessionStart        = "SessionStart"
	eventMuseUserPrompt      = "UserPromptSubmit"
	resumeVerb               = "resume"
	flagDangerouslySkipPerms = "--dangerously-skip-permissions"
	commandEnv               = "env"
	requestResumeArgvKey     = "resume_argv"
	requestMethodKey         = "method"
	requestParamsKey         = "params"
	requestPaneIDKey         = "pane_id"
	requestSourceKey         = "source"
	requestAgentKey          = "agent"
	methodReportAgentSession = "pane.report_agent_session"
)

type providerSpec struct {
	name       string
	agent      string
	source     string
	event      string
	resumeArgs func(string) []string
}

var providers = map[string]providerSpec{
	providerClaude: {name: providerClaude, agent: providerClaude, source: "al:claude", event: eventSessionStart, resumeArgs: func(id string) []string { return []string{providerClaude, flagResume, id} }},
	providerCodex:  {name: providerCodex, agent: providerCodex, source: sourceCodexAL, event: eventSessionStart, resumeArgs: func(id string) []string { return []string{providerCodex, resumeVerb, id} }},
	providerAgy:    {name: providerAgy, agent: providerAgy, source: "al:agy", event: "PreInvocation", resumeArgs: func(id string) []string { return []string{providerAgy, flagConversation, id} }},
	// Muse's builtin lifecycle plugin is the holder of this source.  A
	// session-only report under it attaches only the resume argv; this package
	// never sends pane.report_agent for Muse.
	providerMuse: {name: providerMuse, agent: providerMuse, source: sourceMuseHerdR, event: eventSessionStart, resumeArgs: func(id string) []string { return []string{providerMuse, resumeVerb, id} }},
	providerGrok: {name: providerGrok, agent: providerGrok, source: "al:grok", event: eventSessionStart, resumeArgs: func(id string) []string { return []string{providerGrok, flagResume, id} }},
}

var lastSequence atomic.Int64

const (
	// HookTimeoutSeconds is shared with generated native hook configuration.
	HookTimeoutSeconds = 35
	hookWorkBudget     = (HookTimeoutSeconds - 5) * time.Second
	reportAttempts     = 4
)

var (
	persistPollEvery = 100 * time.Millisecond
	persistWait      = 7 * time.Second
)

// Handle consumes one native hook event. It is a strict no-op unless it is
// running in an actual HerdR pane and has a recognized main-session event.
func Handle(provider string, in io.Reader, out, errOut io.Writer, environ []string) error {
	return HandleForRoot(provider, "", in, out, errOut, environ)
}

// HandleForRoot binds a generated project hook to its AL terminal launch.
func HandleForRoot(provider, root string, in io.Reader, out, errOut io.Writer, environ []string) error {
	deadline := time.Now().Add(hookWorkBudget)
	spec, ok := providers[provider]
	if !ok {
		return fmt.Errorf("unsupported HerdR provider %q", provider)
	}
	if spec.name == providerAgy && out != nil {
		defer func() { _, _ = io.WriteString(out, "{}\n") }()
	}
	env := environment(environ)
	if root != "" && provider == providerCodex {
		return handleCodexRooted(root, spec, in, errOut, env, deadline)
	}
	if env[EnvDispatch] != "" {
		return nil
	}
	if root != "" && (provider == providerClaude || provider == providerCodex || provider == providerGrok) && (env[EnvEnabled] != "1" || env[EnvSocketPath] == "" || env[EnvPaneID] == "") {
		return nil
	}
	var launch launchContext
	var err error
	if root != "" {
		var found bool
		launch, found, err = resolveLaunchContext(root, provider)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		{
			socketPath, err := resolveContextSocket(launch)
			if err != nil {
				return fmt.Errorf("resolve HerdR launch socket: %w", err)
			}
			env[EnvEnabled] = "1"
			env[EnvSocketPath] = socketPath
			env[EnvPaneID] = launch.PaneID
		}
		// The launch record is the sole source for a recovery executable. In
		// particular a release record must not inherit a caller's dev bypass.
		delete(env, EnvDevBypass)
		delete(env, EnvDevExecutable)
		if launch.DevBypass {
			env[EnvDevBypass] = "1"
			env[EnvDevExecutable] = launch.DevExecutable
		}
	}
	if env[EnvEnabled] != "1" || env[EnvSocketPath] == "" || env[EnvPaneID] == "" {
		return nil
	}
	payload, err := decodePayload(in)
	if err != nil {
		return fmt.Errorf("read %s HerdR hook event: %w", provider, err)
	}
	// Unrooted compatibility hooks cannot establish selected-child ownership.
	if provider == providerCodex && text(payload, "agent_id", "agentId") != "" {
		return nil
	}
	id, ok, err := sessionID(spec, payload, env)
	if err != nil {
		return fmt.Errorf("read %s HerdR hook event: %w", provider, err)
	}
	if !ok {
		return nil
	}
	argv, err := resumeArgv(spec, id, env)
	if err != nil {
		return fmt.Errorf("build %s HerdR resume command: %w", provider, err)
	}
	if provider == providerMuse && root != "" {
		// Do not trust an inbound value: the current, validated launch record
		// is the only authority for this inert resume-command annotation.
		argv = withRecoveryGeneration(argv, launchGeneration(launch.PID, launch.ProcessStart))
	}
	// Native Muse does not emit SessionStart when opening a retained native
	// conversation in a new pane. Its documented first prompt event carries
	// the real session ID. The generation above makes a command from an
	// earlier native launch non-identical, while later prompts retain the
	// ordinary exact-persisted-command fast path.
	canonicalPane := ""
	if provider == providerMuse && text(payload, "hook_event_name", "hookEventName") == eventMuseUserPrompt {
		var stored bool
		canonicalPane, stored, err = currentResumeStored(spec, argv, env, deadline)
		if err != nil {
			return fmt.Errorf("check %s HerdR stored resume command: %w", provider, err)
		}
		if stored {
			return nil
		}
	}
	if err := reportAndVerify(spec, id, argv, env, deadline, canonicalPane, nil); err != nil {
		if errOut != nil {
			_, _ = fmt.Fprintf(errOut, "agent-layer HerdR recovery (%s): %v\n", provider, err)
		}
		return err
	}
	return nil
}

// handleCodexRooted resolves both the ordinary child-process launch and the
// native daemon case before honoring inherited HerdR/dispatch environment. A
// detached native app-server legitimately inherits stale values, while a
// payload thread ID plus the live title is current ownership evidence.
func handleCodexRooted(root string, spec providerSpec, in io.Reader, errOut io.Writer, env map[string]string, deadline time.Time) error {
	payload, err := decodePayload(in)
	if err != nil {
		return fmt.Errorf("read codex HerdR hook event: %w", err)
	}
	id, ok, err := sessionID(spec, payload, env)
	if err != nil || !ok {
		if err != nil {
			if strings.Contains(err.Error(), "missing its session identifier") {
				live, liveErr := hasLiveCodexDaemonLaunch(root)
				if liveErr != nil {
					return liveErr
				}
				if live {
					failure := errors.New("codex native hook event omitted its session identifier for a live Agent Layer launch")
					failure = recordCodexRecoveryFailure(root, failure)
					if errOut != nil {
						_, _ = fmt.Fprintf(errOut, "agent-layer HerdR recovery (codex): %v\n", failure)
					}
					return failure
				}
				return nil
			}
			return fmt.Errorf("read codex HerdR hook event: %w", err)
		}
		return nil
	}
	launch, found, err := resolveLaunchContext(root, providerCodex)
	if err != nil {
		return err
	}
	launches := []launchContext{launch}
	selectedChild := text(payload, "agent_id", "agentId") != ""
	daemonRecovery := !found
	if found {
		// The stock app server is a direct child of the first TUI that starts
		// it. Hooks for every later attached TUI are therefore also descendants
		// of that first launch. A launch record above this kernel-authenticated
		// server is provenance for the daemon, not authority for this hook.
		managed, managedErr := codexHookUsesManagedServer(launch, deadline)
		if managedErr != nil {
			managedErr = recordCodexRecoveryFailure(root, managedErr)
			if errOut != nil {
				_, _ = fmt.Fprintf(errOut, "agent-layer HerdR recovery (codex): %v\n", managedErr)
			}
			return managedErr
		}
		if managed {
			daemonRecovery = true
			found = false
		}
	}
	if !found {
		// A normal no-owner case remains a no-op. Unsafe matched ownership is a
		// failure and must never masquerade as a successful registration.
		launches, found, err = resolveCodexDaemonContexts(root, id, deadline, selectedChild)
		if err != nil {
			err = recordCodexRecoveryFailure(root, err)
			if errOut != nil {
				_, _ = fmt.Fprintf(errOut, "agent-layer HerdR recovery (codex): %v\n", err)
			}
			return err
		}
		if !found {
			return nil
		}
	}
	if !daemonRecovery {
		// Nested Codex launched from an embedded TUI inherits this marker. The
		// managed route has title/peer ownership instead, so it deliberately
		// ignores the inherited marker.
		if inherited := env["CODEX_THREAD_ID"]; inherited != "" && inherited != id {
			return nil
		}
	}
	titleDisabled := false
	if !daemonRecovery {
		titleDisabled, err = codexProjectTitleDisabled(root)
		if err != nil {
			err = recordCodexRecoveryFailure(root, err)
			if errOut != nil {
				_, _ = fmt.Fprintf(errOut, "agent-layer HerdR recovery (codex): %v\n", err)
			}
			return err
		}
	}
	seen := make(map[string]bool, len(launches))
	for _, launch := range launches {
		key := launch.SocketPath + "\x00" + launch.PaneID
		if seen[key] {
			continue
		}
		seen[key] = true
		socketPath, err := resolveContextSocket(launch)
		if err != nil {
			return fmt.Errorf("resolve HerdR launch socket: %w", err)
		}
		candidateEnv := maps.Clone(env)
		candidateEnv[EnvEnabled], candidateEnv[EnvSocketPath], candidateEnv[EnvPaneID] = "1", socketPath, launch.PaneID
		delete(candidateEnv, EnvDevBypass)
		delete(candidateEnv, EnvDevExecutable)
		if launch.DevBypass {
			candidateEnv[EnvDevBypass], candidateEnv[EnvDevExecutable] = "1", launch.DevExecutable
		}
		argv, err := resumeArgv(spec, id, candidateEnv)
		if selectedChild && !daemonRecovery {
			// An embedded Codex child has no managed-server peer that can prove
			// selected-child authority. Preserve the historical safe no-op rather
			// than replacing its parent recipe from an unverified child event.
			continue
		}
		if err == nil {
			firstReportAttempt := true
			// This callback runs immediately before attempt zero and before each
			// retry. A changed, gone, or still-unpainted title is a safe no-op;
			// ownership, API, and kernel-peer failures remain actionable.
			revalidate := func() (bool, error) {
				var matchErr error
				switch {
				case daemonRecovery || selectedChild:
					matchErr = codexContextMatchesPane(launch, id, deadline)
				case firstReportAttempt:
					firstReportAttempt = false
					matchErr = codexEmbeddedContextMatchesPaneWithPaintWait(launch, id, deadline, titleDisabled)
				default:
					matchErr = codexEmbeddedContextMatchesPane(launch, id, deadline)
				}
				if errors.Is(matchErr, errCodexPaneNoMatch) || errors.Is(matchErr, errCodexPaneGone) || errors.Is(matchErr, errCodexPaneTitlePending) {
					return false, nil
				}
				return matchErr == nil, matchErr
			}
			if !daemonRecovery {
				err = reportAndVerify(spec, id, argv, candidateEnv, deadline, "", revalidate)
			} else {
				canonicalPane, stored, storedErr := currentCodexResumeStored(launch, id, spec, argv, candidateEnv, deadline)
				if storedErr != nil {
					err = fmt.Errorf("check Codex HerdR stored resume command: %w", storedErr)
				} else if !stored {
					err = reportAndVerify(spec, id, argv, candidateEnv, deadline, canonicalPane, revalidate)
				}
			}
		}
		if err != nil {
			err = recordCodexRecoveryFailure(root, err)
			if errOut != nil {
				_, _ = fmt.Fprintf(errOut, "agent-layer HerdR recovery (codex): %v\n", err)
			}
			return err
		}
	}
	return nil
}

// currentCodexResumeStored permits the daemon fast path only when HerdR's
// durable recipe and its live pane association still identify this exact hook
// thread. Session.json can lag a newer in-memory report by several seconds.
func currentCodexResumeStored(launch launchContext, hookID string, spec providerSpec, argv []string, env map[string]string, deadline time.Time) (string, bool, error) {
	canonicalPane, stored, err := currentResumeStored(spec, argv, env, deadline)
	if err != nil || !stored {
		return canonicalPane, stored, err
	}
	pane, _, err := codexPaneForLaunch(launch, deadline)
	if errors.Is(err, errCodexPaneGone) {
		return canonicalPane, false, nil
	}
	if err != nil {
		return "", false, err
	}
	return canonicalPane, codexLiveSessionMatches(pane, hookID), nil
}

// recordCodexRecoveryFailure leaves a small local receipt because native Codex
// presents hook failures generically and hook stdout must remain empty. It
// intentionally contains no payload, title, environment, or command argv.
func recordCodexRecoveryFailure(root string, failure error) error {
	directory := filepath.Join(root, ".agent-layer", "tmp", "herdr-hook-errors")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("%w; also create recovery error receipt: %v", failure, err)
	}
	name := fmt.Sprintf("codex-hook-error-%d.json", os.Getpid())
	data, err := json.Marshal(map[string]string{"provider": providerCodex, "error": failure.Error()})
	if err != nil {
		return fmt.Errorf("%w; also encode recovery error receipt: %v", failure, err)
	}
	if err := os.WriteFile(filepath.Join(directory, name), append(data, '\n'), 0o600); err != nil { // #nosec G306 -- private project-local hook receipt.
		return fmt.Errorf("%w; also write recovery error receipt: %v", failure, err)
	}
	return failure
}

func decodePayload(in io.Reader) (map[string]any, error) {
	if in == nil {
		return map[string]any{}, nil
	}
	decoder := json.NewDecoder(in)
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		if errors.Is(err, io.EOF) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if payload == nil {
		return nil, errors.New("payload must be a JSON object")
	}
	return payload, nil
}

func sessionID(spec providerSpec, payload map[string]any, env map[string]string) (string, bool, error) {
	// Codex can select a child conversation in the foreground. Its live pane
	// title and managed-server ID are the authority; other providers have no
	// equivalent association signal and retain the conservative child no-op.
	if spec.name != providerCodex && (text(payload, "agent_id") != "" || text(payload, "agentId") != "") {
		return "", false, nil
	}
	event := text(payload, "hook_event_name", "hookEventName")
	var id string
	switch spec.name {
	case providerClaude:
		if event != spec.event {
			if event == "" {
				return "", false, fmt.Errorf("%s event is missing its hook event name", spec.name)
			}
			return "", false, nil
		}
		id = text(payload, "session_id", "sessionId")
	case providerMuse:
		if event != spec.event && event != eventMuseUserPrompt {
			if event == "" {
				return "", false, fmt.Errorf("%s event is missing its hook event name", spec.name)
			}
			return "", false, nil
		}
		id = text(payload, "session_id", "sessionId")
	case providerCodex:
		// Codex's native hook contract permits an omitted event discriminator,
		// but rejects an explicitly different event.
		if event != "" && event != spec.event && event != eventMuseUserPrompt {
			return "", false, nil
		}
		id = text(payload, "session_id", "sessionId")
		// Native child hooks keep the parent in session_id and identify their
		// own thread in agent_id. Title and server ownership still decide whether
		// that child is selected; a background child never claims the parent.
		if childID := text(payload, "agent_id", "agentId"); childID != "" {
			id = childID
		}
		if id != "" && !codexThreadID.MatchString(id) {
			return "", false, errors.New("codex event has an invalid session identifier")
		}
	case providerAgy:
		// Antigravity's documented PreInvocation payload supplies
		// conversationId and does not require a hook-event field.
		if event != "" && event != spec.event {
			return "", false, nil
		}
		id = text(payload, "conversationId")
	case providerGrok:
		if event != "" && event != "SessionStart" && event != "sessionStart" && event != "session_start" {
			return "", false, nil
		}
		id = env["GROK_SESSION_ID"]
		if id == "" {
			id = text(payload, "session_id", "sessionId")
		}
	}
	if id == "" {
		return "", false, fmt.Errorf("%s event is missing its session identifier", spec.name)
	}
	return id, true, nil
}

func text(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func resumeArgv(spec providerSpec, id string, env map[string]string) ([]string, error) {
	args := spec.resumeArgs(id)
	argv := append([]string{"al"}, args...)
	devExecutable := env[EnvDevExecutable]
	if devExecutable == "" || env[EnvDevBypass] == "" {
		return argv, nil
	}
	if !filepath.IsAbs(devExecutable) {
		return nil, errors.New("AL_DEV_EXECUTABLE must be absolute for HerdR recovery")
	}
	argv = []string{commandEnv, EnvDevBypass + "=1", EnvDevExecutable + "=" + devExecutable}
	argv = append(argv, devExecutable)
	return append(argv, args...), nil
}

func withRecoveryGeneration(argv []string, generation string) []string {
	marker := envRecoveryGeneration + "=" + generation
	if len(argv) > 0 && argv[0] == commandEnv {
		withMarker := make([]string, 0, len(argv)+1)
		withMarker = append(withMarker, commandEnv, marker)
		return append(withMarker, argv[1:]...)
	}
	return append([]string{commandEnv, marker}, argv...)
}

func pathWithin(root, value string) bool {
	if !filepath.IsAbs(value) {
		return false
	}
	clean := filepath.Clean(value)
	return clean == root || strings.HasPrefix(clean, root+string(os.PathSeparator))
}

// reportAndVerify invokes revalidate immediately before every sequence-bearing
// report. A nil callback keeps the historical provider behavior. A false
// result is a safe no-op: the foreground selected a different conversation
// while a prior report was waiting for HerdR's persistent writer.
func reportAndVerify(spec providerSpec, id string, argv []string, env map[string]string, deadline time.Time, canonicalPane string, revalidate func() (bool, error)) error {
	if canonicalPane == "" {
		var err error
		canonicalPane, err = canonicalPaneID(env[EnvSocketPath], env[EnvPaneID], deadline)
		if err != nil {
			return err
		}
	}
	path, err := sessionPath(env[EnvSocketPath])
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < reportAttempts; attempt++ {
		if !time.Now().Before(deadline) {
			lastErr = errors.New("HerdR recovery verification deadline exceeded")
			break
		}
		if revalidate != nil {
			current, validationErr := revalidate()
			if validationErr != nil {
				return validationErr
			}
			if !current {
				return nil
			}
		}
		sequence := nextSequence()
		request := map[string]any{
			"id":             fmt.Sprintf("agent-layer:%s:%d", spec.name, sequence),
			requestMethodKey: methodReportAgentSession,
			requestParamsKey: map[string]any{
				requestPaneIDKey:     env[EnvPaneID],
				requestSourceKey:     spec.source,
				requestAgentKey:      spec.agent,
				"seq":                sequence,
				"agent_session_id":   id,
				requestResumeArgvKey: argv,
			},
		}
		response, err := socketRequest(env[EnvSocketPath], request, deadline)
		if err != nil {
			lastErr = err
		} else if code := responseError(response); code != "" {
			lastErr = fmt.Errorf("HerdR rejected session report: %s", code)
		} else if result, _ := response["result"].(map[string]any); result["applied"] == false {
			lastErr = errors.New("HerdR did not apply the session report")
		} else {
			if ok, checkErr := awaitStoredResume(path, canonicalPane, spec, argv, deadline); checkErr != nil {
				lastErr = checkErr
			} else if ok {
				return nil
			} else {
				lastErr = errors.New("HerdR accepted the report but did not persist its resume command before the bounded wait")
			}
		}
		time.Sleep(persistPollEvery)
	}
	return fmt.Errorf("resume command was not stored after retries: %w", lastErr)
}

// awaitStoredResume waits for HerdR's persisted session writer after an
// accepted report. It intentionally does not issue a newer sequence during
// this interval: HerdR's actual Ok response has no applied field and its
// session writer may be waiting for the native debounce interval.
func awaitStoredResume(path, paneID string, spec providerSpec, argv []string, workDeadline time.Time) (bool, error) {
	var lastErr error
	deadline := time.Now().Add(persistWait)
	if workDeadline.Before(deadline) {
		deadline = workDeadline
	}
	for {
		ok, err := storedResumeAt(path, paneID, spec, argv)
		if err == nil && ok {
			return true, nil
		}
		if err != nil {
			lastErr = err
		}
		if time.Now().Add(persistPollEvery).After(deadline) {
			return false, lastErr
		}
		time.Sleep(persistPollEvery)
	}
}

func nextSequence() int64 {
	for {
		now := time.Now().UnixNano()
		last := lastSequence.Load()
		next := now
		if next <= last {
			next = last + 1
		}
		if lastSequence.CompareAndSwap(last, next) {
			return next
		}
	}
}

func currentResumeStored(spec providerSpec, argv []string, env map[string]string, deadline time.Time) (string, bool, error) {
	canonicalPane, err := canonicalPaneID(env[EnvSocketPath], env[EnvPaneID], deadline)
	if err != nil {
		return "", false, err
	}
	path, err := sessionPath(env[EnvSocketPath])
	if err != nil {
		return "", false, err
	}
	stored, err := storedResumeAt(path, canonicalPane, spec, argv)
	if errors.Is(err, os.ErrNotExist) {
		return canonicalPane, false, nil
	}
	return canonicalPane, stored, err
}

func storedResumeAt(path, paneID string, spec providerSpec, argv []string) (bool, error) {
	file, err := os.Open(path) // #nosec G304 -- path is derived from a validated local HerdR socket directory.
	if err != nil {
		return false, fmt.Errorf("read HerdR persisted session state: %w", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("HerdR persisted session state is not a regular file")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return false, fmt.Errorf("read HerdR persisted session state: %w", err)
	}
	var session persistedSession
	if err := json.Unmarshal(data, &session); err != nil {
		return false, fmt.Errorf("decode HerdR persisted session state: %w", err)
	}
	return containsResume(session, paneID, spec, argv)
}

// persistedSession matches HerdR's session.json persistence contract, not its
// runtime session.snapshot API. public_pane_numbers maps an internal pane key
// to the public number used by pane.get's canonical w<workspace>:p<number> ID.
type persistedSession struct {
	Workspaces []persistedWorkspace `json:"workspaces"`
}

type persistedWorkspace struct {
	ID                string         `json:"id"`
	PublicPaneNumbers map[string]int `json:"public_pane_numbers"`
	Tabs              []persistedTab `json:"tabs"`
}

type persistedTab struct {
	Panes map[string]persistedPane `json:"panes"`
}

type persistedPane struct {
	AgentResume *persistedResume `json:"agent_resume"`
}

type persistedResume struct {
	Source string   `json:"source"`
	Agent  string   `json:"agent"`
	Argv   []string `json:"argv"`
}

func canonicalPaneID(socketPath, paneID string, deadline time.Time) (string, error) {
	response, err := socketRequest(socketPath, map[string]any{
		"id":             "agent-layer:canonical-pane",
		requestMethodKey: "pane.get",
		requestParamsKey: map[string]any{requestPaneIDKey: paneID},
	}, deadline)
	if err != nil {
		return "", fmt.Errorf("resolve HerdR pane: %w", err)
	}
	if code := responseError(response); code != "" {
		return "", fmt.Errorf("HerdR rejected pane lookup: %s", code)
	}
	result, _ := response["result"].(map[string]any)
	pane, _ := result["pane"].(map[string]any)
	canonical := stringValue(pane["pane_id"])
	if canonical == "" {
		return "", errors.New("HerdR pane lookup did not return a canonical pane_id")
	}
	return canonical, nil
}

func sessionPath(socketPath string) (string, error) {
	absSocket, err := filepath.Abs(socketPath)
	if err != nil {
		return "", fmt.Errorf("resolve HerdR socket path: %w", err)
	}
	// A validated launch may retain a short relative socket alias so Unix
	// dialing stays below sockaddr_un limits. Resolve the socket itself, not
	// merely its containing alias directory, before locating HerdR's canonical
	// session.json beside the physical socket.
	resolvedSocket, err := filepath.EvalSymlinks(absSocket)
	if err != nil {
		return "", fmt.Errorf("resolve HerdR socket: %w", err)
	}
	return filepath.Join(filepath.Dir(resolvedSocket), "session.json"), nil
}

func containsResume(session persistedSession, paneID string, spec providerSpec, argv []string) (bool, error) {
	workspaceID, publicPaneNumber, err := splitCanonicalPaneID(paneID)
	if err != nil {
		return false, err
	}
	for _, workspace := range session.Workspaces {
		if workspace.ID != workspaceID {
			continue
		}
		internalPaneID := ""
		for candidate, number := range workspace.PublicPaneNumbers {
			if number != publicPaneNumber {
				continue
			}
			if internalPaneID != "" {
				return false, errors.New("HerdR persisted session has duplicate public pane numbers")
			}
			internalPaneID = candidate
		}
		if internalPaneID == "" {
			return false, nil
		}
		var pane *persistedPane
		for _, tab := range workspace.Tabs {
			candidate, found := tab.Panes[internalPaneID]
			if !found {
				continue
			}
			if pane != nil {
				return false, errors.New("HerdR persisted session maps one pane to multiple tabs")
			}
			pane = &candidate
		}
		if pane == nil || pane.AgentResume == nil {
			return false, nil
		}
		resume := pane.AgentResume
		return resume.Source == spec.source && resume.Agent == spec.agent && equalArgv(resume.Argv, argv), nil
	}
	return false, nil
}

func splitCanonicalPaneID(paneID string) (string, int, error) {
	workspaceID, publicText, ok := strings.Cut(paneID, ":p")
	if !ok || workspaceID == "" || publicText == "" || strings.Contains(publicText, ":") {
		return "", 0, fmt.Errorf("invalid canonical HerdR pane ID %q", paneID)
	}
	publicPaneNumber, err := strconv.Atoi(publicText)
	if err != nil || publicPaneNumber < 1 {
		return "", 0, fmt.Errorf("invalid canonical HerdR pane ID %q", paneID)
	}
	return workspaceID, publicPaneNumber, nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func equalArgv(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func responseError(response map[string]any) string {
	if errValue, ok := response["error"].(map[string]any); ok {
		if code := stringValue(errValue["code"]); code != "" {
			return code
		}
		return "unknown_error"
	}
	return ""
}

func socketRequest(socketPath string, request map[string]any, deadline time.Time) (map[string]any, error) {
	dialDeadline := time.Now().Add(500 * time.Millisecond)
	if deadline.Before(dialDeadline) {
		dialDeadline = deadline
	}
	ctx, cancel := context.WithDeadline(context.Background(), dialDeadline)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("connect HerdR socket: %w", err)
	}
	defer func() { _ = conn.Close() }()
	ioDeadline := time.Now().Add(750 * time.Millisecond)
	if deadline.Before(ioDeadline) {
		ioDeadline = deadline
	}
	if err := conn.SetDeadline(ioDeadline); err != nil {
		return nil, err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write HerdR request: %w", err)
	}
	var response map[string]any
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		return nil, fmt.Errorf("read HerdR response: %w", err)
	}
	return response, nil
}

func environment(values []string) map[string]string {
	result := make(map[string]string, len(values))
	for _, entry := range values {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}
