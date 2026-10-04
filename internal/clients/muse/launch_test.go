package muse

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestBaseArgsOnlyYoloDisablesSafety(t *testing.T) {
	for _, mode := range []string{config.ApprovalModeAll, config.ApprovalModeCommands, config.ApprovalModeMCP, config.ApprovalModeNone} {
		args := BaseArgs(t.TempDir(), config.Config{Approvals: config.ApprovalsConfig{Mode: mode}})
		if slices.Contains(args, "--yolo") || slices.Contains(args, "--disable-approval") {
			t.Fatalf("mode %s broadened safety: %v", mode, args)
		}
		if !slices.Contains(args, "untrusted") || !slices.Contains(args, "off") || !slices.Contains(args, "--approval-mode") || !slices.Contains(args, "--approval-judge") || !slices.Contains(args, "--trust-workspace") {
			t.Fatalf("mode %s missing policy/trust args: %v", mode, args)
		}
	}
	args := BaseArgs(t.TempDir(), config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO}})
	if !slices.Contains(args, "--yolo") {
		t.Fatalf("yolo args = %v", args)
	}
}

func TestLaunchPreservesNativeEnvironment(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "muse")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil { // #nosec G306 -- executable stub in a test-owned directory.
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	old := execFunc
	t.Cleanup(func() { execFunc = old })
	for _, env := range [][]string{{"HOME=/native/home"}, {"HOME=/native/home", "XDG_CONFIG_HOME=/native/config", "XDG_DATA_HOME=/native/data"}} {
		called := false
		execFunc = func(_ string, _ []string, got []string) error {
			called = true
			if !slices.Equal(got, env) {
				t.Fatalf("environment changed: %v", got)
			}
			return nil
		}
		if err := Launch(&config.ProjectConfig{Root: root}, nil, env, nil); err != nil {
			t.Fatal(err)
		}
		if !called {
			t.Fatal("Muse not launched")
		}
	}
	for _, name := range []string{".muse-config", ".muse-data"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected isolated root %s: %v", name, err)
		}
	}
}

func TestLaunchExplicitOptionsReplaceDefaults(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "muse")
	t.Setenv("PATH", binDir)
	call := testutil.CaptureExec(t, &execFunc, nil)
	cfg := &config.ProjectConfig{Root: root, Config: config.Config{
		Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
		Agents:    config.AgentsConfig{Muse: config.AgentConfig{Model: "default-model", ReasoningEffort: "high"}},
	}}
	if err := Launch(cfg, nil, nil, []string{"--workspace", "custom", "--trust-workspace", "--model", "chosen", "--reasoning-effort", "low", "--yolo"}); err != nil {
		t.Fatal(err)
	}
	call.AssertCalled(t, filepath.Join(binDir, "muse"), []string{"muse", "--workspace", "custom", "--trust-workspace", "--model", "chosen", "--reasoning-effort", "low", "--yolo"})
}

func TestLaunchRecordsMuseSourceIdentityWithoutFlags(t *testing.T) {
	oldExec := execFunc
	t.Cleanup(func() { execFunc = oldExec })
	execFunc = func(_ string, _ []string, _ []string) error { return nil }

	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "muse")
	t.Setenv("PATH", binDir)
	runInfo, err := run.Create(root)
	if err != nil {
		t.Fatal(err)
	}
	project := &config.ProjectConfig{Root: root, Config: config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Model: "repository-model"}}}}
	if err = Launch(project, runInfo, []string{"HERDR_ENV=1", "HERDR_SOCKET_PATH=/herdr.sock", "HERDR_PANE_ID=w1:p1"}, []string{"--prompt", "never-store-this"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(runInfo.Dir)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "herdr-launch-") {
		t.Fatalf("launch context entries=%v err=%v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(runInfo.Dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "never-store-this") || strings.Contains(string(data), "repository-model") || strings.Contains(string(data), "options") {
		t.Fatalf("launch context retained launch flags or configuration: %s", data)
	}
}
