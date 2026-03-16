package copilotcli

import (
	"os"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestLaunchCopilotCLI(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "copilot")

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				CopilotCLI: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}
}

func TestLaunchCopilotCLIError(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStubWithExit(t, binDir, "copilot", 1)

	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestLaunchCopilotCLIYOLO(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStubExpectArg(t, binDir, "copilot", "--yolo")

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
			Agents: config.AgentsConfig{
				CopilotCLI: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}
}

func TestLaunchCopilotCLIAllowAllTools(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStubExpectArg(t, binDir, "copilot", "--allow-all-tools")

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				CopilotCLI: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}
}
