package gemini

import (
	"os"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestLaunchGemini(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "gemini")

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Model: "test-model"},
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

func TestLaunchGeminiError(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStubWithExit(t, binDir, "gemini", 1)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestLaunchGeminiYOLO(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStubExpectArg(t, binDir, "gemini", "--approval-mode=yolo")

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "yolo"},
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Model: "test-model"},
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
