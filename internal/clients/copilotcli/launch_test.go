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

type execCall struct {
	called bool
	path   string
	argv   []string
	env    []string
}

func captureExec(t *testing.T, err error) *execCall {
	t.Helper()
	original := execFunc
	call := &execCall{}
	execFunc = func(path string, argv []string, env []string) error {
		if call.called {
			t.Fatal("execFunc called more than once")
		}
		call.called = true
		call.path = path
		call.argv = append([]string(nil), argv...)
		call.env = append([]string(nil), env...)
		return err
	}
	t.Cleanup(func() { execFunc = original })
	return call
}

func forbidExec(t *testing.T) {
	t.Helper()
	original := execFunc
	execFunc = func(string, []string, []string) error {
		t.Fatal("execFunc should not be called")
		return nil
	}
	t.Cleanup(func() { execFunc = original })
}

func writeResolvableCopilot(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "copilot")
	t.Setenv("PATH", binDir)
	return filepath.Join(binDir, "copilot")
}

func assertExecCalled(t *testing.T, call *execCall, wantPath string, wantArgv []string) {
	t.Helper()
	if !call.called {
		t.Fatal("expected execFunc to be called")
	}
	if call.path != wantPath {
		t.Fatalf("expected exec path %q, got %q", wantPath, call.path)
	}
	if !reflect.DeepEqual(call.argv, wantArgv) {
		t.Fatalf("unexpected argv: got %#v want %#v", call.argv, wantArgv)
	}
}

func TestLaunchCopilotCLIExecHandoff(t *testing.T) {
	root := t.TempDir()
	copilotPath := writeResolvableCopilot(t)
	call := captureExec(t, nil)
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

	assertExecCalled(t, call, copilotPath, []string{"copilot", "--model", "test-model", "--prompt", "hello"})
	if !reflect.DeepEqual(call.env, env) {
		t.Fatalf("expected env to pass through unchanged, got %#v want %#v", call.env, env)
	}
}

func TestLaunchCopilotCLIExecError(t *testing.T) {
	root := t.TempDir()
	writeResolvableCopilot(t)
	wantErr := errors.New("exec failed")
	captureExec(t, wantErr)

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
	forbidExec(t)

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
	call := captureExec(t, nil)

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

	assertExecCalled(t, call, copilotPath, []string{"copilot", "--model", "test-model", "--yolo"})
}

func TestLaunchCopilotCLIAllowAllTools(t *testing.T) {
	root := t.TempDir()
	copilotPath := writeResolvableCopilot(t)
	call := captureExec(t, nil)

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

	assertExecCalled(t, call, copilotPath, []string{"copilot", "--model", "test-model", "--allow-all-tools"})
}
