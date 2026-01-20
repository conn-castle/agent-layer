package main

import (
	"errors"
	"testing"
)

func TestWizardCommandInteractiveRunsWizard(t *testing.T) {
	originalIsTerminal := isTerminal
	originalGetwd := getwd
	originalRunWizard := runWizard
	t.Cleanup(func() {
		isTerminal = originalIsTerminal
		getwd = originalGetwd
		runWizard = originalRunWizard
	})

	isTerminal = func() bool { return true }

	wantRoot := t.TempDir()
	getwd = func() (string, error) { return wantRoot, nil }

	wizardCalled := false
	runWizard = func(root string) error {
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
	t.Cleanup(func() {
		isTerminal = originalIsTerminal
		getwd = originalGetwd
	})

	isTerminal = func() bool { return true }
	getwd = func() (string, error) { return "", errors.New("boom") }

	cmd := newWizardCmd()
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Fatal("expected error when getwd fails")
	}
}

func TestIsTerminalDefaultImplementation(t *testing.T) {
	originalIsTerminal := isTerminal
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	isTerminal = originalIsTerminal
	_ = isTerminal()
}
