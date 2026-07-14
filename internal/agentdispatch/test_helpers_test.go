package agentdispatch

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func requireDispatchExitCode(t *testing.T, err error, code int) {
	t.Helper()
	_ = requireDispatchExitError(t, err, code)
}

func requireDispatchExitError(t *testing.T, err error, code int) *ExitError {
	t.Helper()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != code {
		t.Fatalf("expected ExitError code %d, got %T: %v", code, err, err)
	}
	return exitErr
}

func dispatchTestConfig(enabledAgents ...string) config.Config {
	cfg := config.Config{}
	for _, agent := range enabledAgents {
		switch agent {
		case AgentCodex:
			cfg.Agents.Codex.Enabled = boolPtr(true)
		case AgentClaude:
			cfg.Agents.Claude.Enabled = boolPtr(true)
		case AgentAntigravity:
			cfg.Agents.Antigravity.Enabled = boolPtr(true)
		}
	}
	return cfg
}

func boolPtr(value bool) *bool { return &value }

func alwaysFound(name string) (string, error) { return "/mock/" + name, nil }

func chooseOnly(agent string) RandomChooser {
	return func([]string) (string, error) { return agent, nil }
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func disableAgentInDispatchConfig(t *testing.T, root string, agent string) {
	t.Helper()
	replaceDispatchConfigText(t, root, "[agents."+agent+"]\nenabled = true", "[agents."+agent+"]\nenabled = false")
}

func replaceDispatchConfigText(t *testing.T, root string, old string, replacement string) {
	t.Helper()
	configPath := config.DefaultPaths(root).ConfigPath
	data, err := os.ReadFile(configPath) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(data), old, replacement, 1)
	if updated == string(data) {
		t.Fatalf("config did not contain %q", old)
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil { // #nosec G306 G703 -- configPath is rooted in the test's temporary repository.
		t.Fatalf("write config: %v", err)
	}
}
