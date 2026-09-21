package muse

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
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
