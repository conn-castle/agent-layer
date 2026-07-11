package copilotcli

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func writeResolvableCopilot(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "copilot")
	t.Setenv("PATH", binDir)
	return filepath.Join(binDir, "copilot")
}

func TestLaunchCopilotCLIExecHandoff(t *testing.T) {
	root := t.TempDir()
	copilotPath := writeResolvableCopilot(t)
	call := testutil.CaptureExec(t, &execFunc, nil)
	env := []string{"PATH=" + filepath.Dir(copilotPath), "CUSTOM=1"}

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				CopilotCLI: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, []string{"--prompt", "hello"}); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	call.AssertCalled(t, copilotPath, []string{"copilot", "--model", "test-model", "--prompt", "hello"})
	if !reflect.DeepEqual(call.Env, env) {
		t.Fatalf("expected env to pass through unchanged, got %#v want %#v", call.Env, env)
	}
}

func TestLaunchCopilotCLIExecError(t *testing.T) {
	root := t.TempDir()
	writeResolvableCopilot(t)
	wantErr := errors.New("exec failed")
	testutil.CaptureExec(t, &execFunc, wantErr)

	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected exec error to wrap %v, got %v", wantErr, err)
	}
	if !strings.Contains(err.Error(), "copilot exec handoff failed") {
		t.Fatalf("expected exec handoff context, got %v", err)
	}
}

func TestLaunchCopilotCLIMissingBinary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	testutil.ForbidExec(t, &execFunc)

	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil)
	if err == nil {
		t.Fatal("expected missing binary error")
	}
	if !strings.Contains(err.Error(), "copilot launcher requires `copilot` on PATH") {
		t.Fatalf("expected lookup error to name copilot, got %v", err)
	}
}

func TestLaunchCopilotCLIYOLO(t *testing.T) {
	root := t.TempDir()
	copilotPath := writeResolvableCopilot(t)
	call := testutil.CaptureExec(t, &execFunc, nil)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
			Agents: config.AgentsConfig{
				CopilotCLI: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	call.AssertCalled(t, copilotPath, []string{"copilot", "--model", "test-model", "--yolo"})
}

func TestLaunchCopilotCLIAllowAllTools(t *testing.T) {
	root := t.TempDir()
	copilotPath := writeResolvableCopilot(t)
	call := testutil.CaptureExec(t, &execFunc, nil)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				CopilotCLI: config.AgentConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	call.AssertCalled(t, copilotPath, []string{"copilot", "--model", "test-model", "--allow-all-tools"})
}
