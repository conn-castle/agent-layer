package antigravity

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
)

const modelListTimeout = 3 * time.Second

// ModelOptionsRequest configures Antigravity model option discovery.
type ModelOptionsRequest struct {
	Env      []string
	LookPath func(string) (string, error)
	// Timeout bounds the live `agy models` command. Zero or negative uses modelListTimeout.
	Timeout time.Duration
}

// ModelOptions returns Antigravity model display strings.
//
// It prefers the live `agy models` command and falls back to Agent Layer's
// field catalog only when live discovery is unavailable.
func ModelOptions(req ModelOptionsRequest) []string {
	models, err := liveModelOptions(req)
	if err == nil {
		return models
	}
	return config.FieldOptionValues(config.AntigravityModelFieldKey)
}

func liveModelOptions(req ModelOptionsRequest) ([]string, error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	output, err := runModelCommand(ctx, binary, req.Env)
	if err != nil {
		return nil, err
	}
	models, err := parseModelOutput(output)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, errors.New("agy models returned no model options")
	}
	return models, nil
}

func runModelCommand(ctx context.Context, binary string, env []string) ([]byte, error) {
	if env == nil {
		env = os.Environ()
	}
	env = clients.SetEnv(env, disableAutoUpdateEnv, "1")
	// #nosec G204 -- binary is resolved by LookPath; the command arguments are fixed by Agent Layer.
	cmd := exec.CommandContext(ctx, binary, "models")
	cmd.Env = env
	return cmd.Output()
}

func parseModelOutput(output []byte) ([]string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	models := make([]string, 0)
	for scanner.Scan() {
		model := strings.TrimSpace(scanner.Text())
		if model == "" {
			continue
		}
		models = append(models, model)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return models, nil
}
