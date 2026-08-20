package grok

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func writeResolvableGrok(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "grok")
	t.Setenv("PATH", binDir)
	return filepath.Join(binDir, "grok")
}

func TestLaunchGrokExecHandoff(t *testing.T) {
	root := t.TempDir()
	grokPath := writeResolvableGrok(t)
	call := testutil.CaptureExec(t, &execFunc, nil)
	env := []string{"PATH=" + filepath.Dir(grokPath), "CUSTOM=1"}

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Grok: config.GrokConfig{Model: "grok-4.6", ReasoningEffort: "high"},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, []string{"-p", "hello"}); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	wantArgs := []string{"grok", "--model", "grok-4.6", "--reasoning-effort", "high", "-p", "hello"}
	call.AssertCalled(t, grokPath, wantArgs)
	val, ok := clients.GetEnv(call.Env, EnvHome)
	if !ok || val != HomeDir(root) {
		t.Fatalf("expected GROK_HOME=%s, got %s (ok=%v)", HomeDir(root), val, ok)
	}
	homeInfo, err := os.Lstat(HomeDir(root))
	if err != nil {
		t.Fatalf("grok home: %v", err)
	}
	if gotMode := homeInfo.Mode().Perm(); gotMode != 0o700 {
		t.Fatalf("grok home mode = %04o, want 0700", gotMode)
	}
}

func TestLaunchGrokYOLO(t *testing.T) {
	root := t.TempDir()
	grokPath := writeResolvableGrok(t)
	call := testutil.CaptureExec(t, &execFunc, nil)
	env := []string{"PATH=" + filepath.Dir(grokPath)}

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
			Agents: config.AgentsConfig{
				Grok: config.GrokConfig{},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	call.AssertCalled(t, grokPath, []string{"grok", "--permission-mode", "bypassPermissions", "--always-approve"})
}

func TestLaunchGrokSandboxAndMemory(t *testing.T) {
	root := t.TempDir()
	grokPath := writeResolvableGrok(t)
	call := testutil.CaptureExec(t, &execFunc, nil)
	disable := true

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
			Agents: config.AgentsConfig{
				Grok: config.GrokConfig{DisableMemory: &disable},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{"PATH=" + filepath.Dir(grokPath)}, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	call.AssertCalled(t, grokPath, []string{"grok", "--no-memory"})
	if val, ok := clients.GetEnv(call.Env, EnvMemory); !ok || val != "0" {
		t.Fatalf("expected GROK_MEMORY=0, got %s (ok=%v)", val, ok)
	}
}

func TestLaunchGrokExecError(t *testing.T) {
	root := t.TempDir()
	writeResolvableGrok(t)
	wantErr := errors.New("exec failed")
	testutil.CaptureExec(t, &execFunc, wantErr)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Grok: config.GrokConfig{},
			},
		},
		Root: root,
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("expected error containing %v, got %v", wantErr, err)
	}
}

func TestLaunchGrokLookupError(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // Empty directory with no grok binary
	root := t.TempDir()
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Grok: config.GrokConfig{},
			},
		},
		Root: root,
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "grok launcher requires `grok` on PATH") {
		t.Fatalf("expected lookup error, got: %v", err)
	}
	if _, statErr := os.Lstat(HomeDir(root)); !os.IsNotExist(statErr) {
		t.Fatalf("lookup failure created grok home: %v", statErr)
	}
}

func TestEnsureHomeTightensBroadPermissions(t *testing.T) {
	root := t.TempDir()
	home := HomeDir(root)
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(home, 0o755); err != nil { // #nosec G302 -- fixture starts too open so production can tighten it.
		t.Fatal(err)
	}
	if err := EnsureHome(root); err != nil {
		t.Fatalf("EnsureHome: %v", err)
	}
	info, err := os.Lstat(home)
	if err != nil {
		t.Fatal(err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o700 {
		t.Fatalf("grok home mode = %04o, want 0700", gotMode)
	}
}

func TestEnsureHomeRejectsSymlinkWithoutDuplicatingPath(t *testing.T) {
	root := t.TempDir()
	home := HomeDir(root)
	if err := os.Symlink(t.TempDir(), home); err != nil {
		t.Fatal(err)
	}
	err := EnsureHome(root)
	if err == nil {
		t.Fatal("expected symlink home to fail")
	}
	if !strings.Contains(err.Error(), "grok home directory") {
		t.Fatalf("expected grok home prefix, got: %v", err)
	}
	if !strings.Contains(err.Error(), "must be a real directory") {
		t.Fatalf("expected inner directory error, got: %v", err)
	}
	if got, want := strings.Count(err.Error(), home), 1; got != want {
		t.Fatalf("path %q count = %d, want %d in %q", home, got, want, err)
	}
}

func TestConfigureEnvironmentAlwaysSetsHome(t *testing.T) {
	root := t.TempDir()
	expected := HomeDir(root)

	t.Run("sets GROK_HOME when unset", func(t *testing.T) {
		env := ConfigureEnvironment(root, []string{"A=1"}, config.GrokConfig{}, nil)
		val, ok := clients.GetEnv(env, EnvHome)
		if !ok || val != expected {
			t.Fatalf("expected GROK_HOME=%s, got %s (ok=%v)", expected, val, ok)
		}
	})

	t.Run("warns and replaces conflicting inherited home", func(t *testing.T) {
		var warn bytes.Buffer
		other := filepath.Join(t.TempDir(), "other-grok")
		env := ConfigureEnvironment(root, []string{EnvHome + "=" + other}, config.GrokConfig{}, &warn)
		val, ok := clients.GetEnv(env, EnvHome)
		if !ok || val != expected {
			t.Fatalf("expected GROK_HOME to be replaced with %s, got %s", expected, val)
		}
		if !strings.Contains(warn.String(), "Warning: overriding inherited GROK_HOME=") {
			t.Fatalf("expected warning message, got: %s", warn.String())
		}
	})

	t.Run("clears stale repo-local GROK_HOME when grok is unused", func(t *testing.T) {
		env := ClearStaleHome(root, []string{EnvHome + "=" + expected})
		if _, ok := clients.GetEnv(env, EnvHome); ok {
			t.Fatal("expected stale repo-local GROK_HOME to be cleared")
		}
	})

	t.Run("preserves external GROK_HOME when clearing stale home", func(t *testing.T) {
		other := filepath.Join(t.TempDir(), "user-grok")
		env := ClearStaleHome(root, []string{EnvHome + "=" + other})
		val, ok := clients.GetEnv(env, EnvHome)
		if !ok || val != other {
			t.Fatalf("expected external GROK_HOME to remain %s, got %s", other, val)
		}
	})
}

func TestGrokContractHelpers(t *testing.T) {
	if !IsSuccessfulStopReason("end_turn") {
		t.Fatal("documented successful stop reason rejected")
	}
	for _, reason := range []string{"EndTurn", "refusal"} {
		if IsSuccessfulStopReason(reason) {
			t.Fatalf("unsupported stop reason %q accepted", reason)
		}
	}

	commands := config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands}}
	if got := SandboxArgs(commands, []string{"go test"}); len(got) != 2 || got[1] != SandboxWorkspace {
		t.Fatalf("command-approved sandbox args = %v", got)
	}
	if got := SandboxArgs(config.Config{}, nil); len(got) != 2 || got[1] != SandboxReadOnly {
		t.Fatalf("read-only sandbox args = %v", got)
	}
	yolo := config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO}}
	if got := SandboxArgs(yolo, nil); got != nil {
		t.Fatalf("YOLO sandbox args = %v, want nil", got)
	}
}
