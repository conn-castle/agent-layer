package main

import (
	"errors"
	"io"
	"os"
	"testing"
)

func TestWizardCommandInteractiveRunsWizard(t *testing.T) {
	originalIsTerminal := isTerminal
	originalGetwd := getwd
	originalRunWizard := runWizard
	originalRunWizardProfile := runWizardProfile
	originalCleanup := cleanupWizardBackups
	t.Cleanup(func() {
		isTerminal = originalIsTerminal
		getwd = originalGetwd
		runWizard = originalRunWizard
		runWizardProfile = originalRunWizardProfile
		cleanupWizardBackups = originalCleanup
	})

	isTerminal = func() bool { return true }

	wantRoot := t.TempDir()
	getwd = func() (string, error) { return wantRoot, nil }

	wizardCalled := false
	runWizard = func(root string, _ string) error {
		wizardCalled = true
		if root != wantRoot {
			t.Fatalf("expected root %q, got %q", wantRoot, root)
		}
		return nil
	}

	cmd := newWizardCmd()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("wizard RunE error: %v", err)
	}
	if !wizardCalled {
		t.Fatal("expected wizard to run in interactive mode")
	}
}

func TestWizardCommandGetwdError(t *testing.T) {
	originalIsTerminal := isTerminal
	originalGetwd := getwd
	originalRunWizardProfile := runWizardProfile
	originalCleanup := cleanupWizardBackups
	t.Cleanup(func() {
		isTerminal = originalIsTerminal
		getwd = originalGetwd
		runWizardProfile = originalRunWizardProfile
		cleanupWizardBackups = originalCleanup
	})

	isTerminal = func() bool { return true }
	getwd = func() (string, error) { return "", errors.New("boom") }

	cmd := newWizardCmd()
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("expected error when getwd fails")
	}
}

func TestWizardCommandProfileModeNonInteractive(t *testing.T) {
	originalIsTerminal := isTerminal
	originalGetwd := getwd
	originalRunWizard := runWizard
	originalRunWizardProfile := runWizardProfile
	originalCleanup := cleanupWizardBackups
	t.Cleanup(func() {
		isTerminal = originalIsTerminal
		getwd = originalGetwd
		runWizard = originalRunWizard
		runWizardProfile = originalRunWizardProfile
		cleanupWizardBackups = originalCleanup
	})

	isTerminal = func() bool { return false }
	root := t.TempDir()
	getwd = func() (string, error) { return root, nil }

	profilePath := root + "/profile.toml"
	if err := os.WriteFile(profilePath, []byte(`[approvals]
mode = "all"
[agents.gemini]
enabled = true
[agents.claude]
enabled = true
[agents.codex]
enabled = true
[agents.vscode]
enabled = true
[agents.antigravity]
enabled = true
`), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	called := false
	runWizardProfile = func(gotRoot string, _ string, gotPath string, apply bool, _ io.Writer) error {
		called = true
		if gotRoot != root {
			t.Fatalf("expected root %q, got %q", root, gotRoot)
		}
		if gotPath != profilePath {
			t.Fatalf("expected profile %q, got %q", profilePath, gotPath)
		}
		if !apply {
			t.Fatal("expected --yes to set apply=true")
		}
		return nil
	}

	cmd := newWizardCmd()
	cmd.SetArgs([]string{"--profile", profilePath, "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("wizard profile execute error: %v", err)
	}
	if !called {
		t.Fatal("expected profile mode to run")
	}
}

func TestWizardCommandCleanupBackups(t *testing.T) {
	originalIsTerminal := isTerminal
	originalGetwd := getwd
	originalRunWizardProfile := runWizardProfile
	originalCleanup := cleanupWizardBackups
	t.Cleanup(func() {
		isTerminal = originalIsTerminal
		getwd = originalGetwd
		runWizardProfile = originalRunWizardProfile
		cleanupWizardBackups = originalCleanup
	})

	root := t.TempDir()
	getwd = func() (string, error) { return root, nil }
	cleanupWizardBackups = func(string) ([]string, error) {
		return []string{".agent-layer/config.toml.bak"}, nil
	}

	cmd := newWizardCmd()
	cmd.SetArgs([]string{"--cleanup-backups"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cleanup execute error: %v", err)
	}
}

func TestIsTerminalDefaultImplementation(t *testing.T) {
	originalIsTerminal := isTerminal
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	isTerminal = originalIsTerminal
	_ = isTerminal()
}
