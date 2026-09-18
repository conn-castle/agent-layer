package antigravity

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
)

// Model listing authenticates and fetches the remote catalog. Allow headroom
// above observed 1–4 second startup times, while bounding an unavailable service.
const modelListTimeout = 10 * time.Second

// ModelOptionsRequest configures Antigravity model option discovery.
type ModelOptionsRequest struct {
	Context  context.Context
	Project  *config.ProjectConfig
	Env      []string
	LookPath func(string) (string, error)
	// Timeout bounds the live `agy models` command. Zero or negative uses modelListTimeout.
	Timeout time.Duration
}

// DiscoverModels lists models without syncing or starting a conversation.
// Discovery failures are returned to the caller, never replaced by a catalog.
func DiscoverModels(req ModelOptionsRequest) ([]string, error) {
	lookPath := req.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	binary, err := lookPath(executableName)
	if err != nil {
		return nil, err
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = modelListTimeout
	}
	parent := req.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	output, err := runModelCommand(ctx, binary, req)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	models, err := ParseModelOutput(output)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, errors.New("agy models returned no model options")
	}
	return models, nil
}

func runModelCommand(ctx context.Context, binary string, req ModelOptionsRequest) ([]byte, error) {
	env := req.Env
	if env == nil {
		env = os.Environ()
	}
	args := make([]string, 0, 1)
	if req.Project != nil {
		var err error
		args, err = BaseArgs(req.Project.Root, req.Project.Config)
		if err != nil {
			return nil, err
		}
		env = clients.BuildEnv(env, req.Project.Env, nil)
	}
	env = ConfigureEnvironment(env)
	args = append(args, "models")
	// #nosec G204 -- binary is resolved by LookPath; the command arguments are fixed by Agent Layer.
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	cmd.WaitDelay = time.Second
	if req.Project != nil {
		cmd.Dir = req.Project.Root
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = stdout.Close() })
	defer stopClose()
	const maxOutput = 2 * 1024 * 1024
	output, readErr := io.ReadAll(io.LimitReader(stdout, maxOutput+1))
	if readErr != nil || len(output) > maxOutput {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if readErr != nil {
			return nil, readErr
		}
		return nil, errors.New("agy models output exceeded size limit")
	}
	if err := cmd.Wait(); err != nil {
		return nil, err
	}
	return output, nil
}

// ParseModelOutput decodes native agy output, including remote benchmark output.
func ParseModelOutput(output []byte) ([]string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	models := make([]string, 0)
	for scanner.Scan() {
		model := strings.TrimSpace(scanner.Text())
		if model == "" {
			continue
		}
		// Require the native slug<TAB>display format. Arbitrary stdout (such
		// as an authentication message) must not become a model suggestion.
		slug, label, ok := strings.Cut(scanner.Text(), "\t")
		if !ok || strings.TrimSpace(slug) == "" || strings.TrimSpace(label) == "" || strings.Contains(label, "\t") {
			return nil, errors.New("agy models returned an invalid model row; expected slug<TAB>display name")
		}
		models = append(models, strings.TrimSpace(slug))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return models, nil
}
