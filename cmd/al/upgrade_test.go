package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestUpgradeCmd_RequiresTerminalWithoutApplyFlags(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	origIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	origInstallRun := installRun
	installCalled := false
	installRun = func(string, install.Options) error {
		installCalled = true
		return nil
	}
	t.Cleanup(func() { installRun = origInstallRun })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Error() != messages.UpgradeRequiresTerminal {
			t.Fatalf("unexpected error: %v", err)
		}
		if installCalled {
			t.Fatal("expected installRun not to be called when terminal is required")
		}
	})
}

func TestUpgradeCmd_YesWithoutApplyFlagsErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	origIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	origInstallRun := installRun
	installCalled := false
	installRun = func(string, install.Options) error {
		installCalled = true
		return nil
	}
	t.Cleanup(func() { installRun = origInstallRun })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{"--yes"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Error() != messages.UpgradeYesRequiresApply {
			t.Fatalf("unexpected error: %v", err)
		}
		if installCalled {
			t.Fatal("expected installRun not to be called")
		}
	})
}

func TestUpgradeCmd_NonInteractiveApplyWithoutYesErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	origIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	origInstallRun := installRun
	installCalled := false
	installRun = func(string, install.Options) error {
		installCalled = true
		return nil
	}
	t.Cleanup(func() { installRun = origInstallRun })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{"--apply-managed-updates"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Error() != messages.UpgradeNonInteractiveRequiresYesApply {
			t.Fatalf("unexpected error: %v", err)
		}
		if installCalled {
			t.Fatal("expected installRun not to be called")
		}
	})
}

func TestUpgradeCmd_NonInteractiveYesApplyManagedRunsInstallWithPrompter(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	origIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	origInstallRun := installRun
	var captured install.Options
	installRun = func(gotRoot string, opts install.Options) error {
		if canonicalPath(gotRoot) != canonicalPath(root) {
			t.Fatalf("installRun root = %q, want %q", gotRoot, root)
		}
		captured = opts
		return nil
	}
	t.Cleanup(func() { installRun = origInstallRun })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.SetArgs([]string{"--yes", "--apply-managed-updates"})
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetIn(bytes.NewBufferString(""))

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade: %v", err)
		}
		errText := stderr.String()
		if !strings.Contains(errText, messages.UpgradeSkipMemoryUpdatesInfo) {
			t.Fatalf("expected skip-memory note, got %q", errText)
		}
		if !strings.Contains(errText, messages.UpgradeSkipDeletionsInfo) {
			t.Fatalf("expected skip-deletions note, got %q", errText)
		}
	})

	if !captured.Overwrite {
		t.Fatalf("captured opts.Overwrite = false, want true")
	}
	if captured.Prompter == nil {
		t.Fatal("captured opts.Prompter = nil, want non-nil")
	}
	promptFuncs, ok := captured.Prompter.(install.PromptFuncs)
	if !ok {
		t.Fatalf("captured opts.Prompter = %T, want install.PromptFuncs", captured.Prompter)
	}
	if promptFuncs.OverwriteAllPreviewFunc == nil ||
		promptFuncs.OverwriteAllMemoryPreviewFunc == nil ||
		promptFuncs.OverwriteAllUnifiedPreviewFunc == nil ||
		promptFuncs.OverwritePreviewFunc == nil ||
		promptFuncs.DeleteUnknownAllFunc == nil ||
		promptFuncs.DeleteUnknownFunc == nil {
		t.Fatalf("expected all prompt callbacks to be wired: %+v", promptFuncs)
	}

	if overwriteManaged, err := promptFuncs.OverwriteAll(nil); err != nil || !overwriteManaged {
		t.Fatalf("OverwriteAll = (%v, %v), want (true, nil)", overwriteManaged, err)
	}
	if overwriteMemory, err := promptFuncs.OverwriteAllMemory(nil); err != nil || overwriteMemory {
		t.Fatalf("OverwriteAllMemory = (%v, %v), want (false, nil)", overwriteMemory, err)
	}
	if deleteAll, err := promptFuncs.DeleteUnknownAll(nil); err != nil || deleteAll {
		t.Fatalf("DeleteUnknownAll = (%v, %v), want (false, nil)", deleteAll, err)
	}
}

func TestUpgradeCmd_InteractiveWiresPrompter(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	origIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	origInstallRun := installRun
	installRun = func(string, install.Options) error {
		return nil
	}
	t.Cleanup(func() { installRun = origInstallRun })

	var captured install.Options
	installRun = func(gotRoot string, opts install.Options) error {
		if canonicalPath(gotRoot) != canonicalPath(root) {
			t.Fatalf("installRun root = %q, want %q", gotRoot, root)
		}
		captured = opts
		return nil
	}

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade: %v", err)
		}
	})

	if !captured.Overwrite {
		t.Fatalf("captured opts.Overwrite = false, want true")
	}
	if captured.Prompter == nil {
		t.Fatal("captured opts.Prompter = nil, want non-nil")
	}
	promptFuncs, ok := captured.Prompter.(install.PromptFuncs)
	if !ok {
		t.Fatalf("captured opts.Prompter = %T, want install.PromptFuncs", captured.Prompter)
	}
	if promptFuncs.OverwriteAllPreviewFunc == nil ||
		promptFuncs.OverwriteAllMemoryPreviewFunc == nil ||
		promptFuncs.OverwriteAllUnifiedPreviewFunc == nil ||
		promptFuncs.OverwritePreviewFunc == nil ||
		promptFuncs.DeleteUnknownAllFunc == nil ||
		promptFuncs.DeleteUnknownFunc == nil {
		t.Fatalf("expected all prompt callbacks to be wired: %+v", promptFuncs)
	}
}

func TestUpgradeCmd_InteractiveApplyManagedAutoApprovesOnlyManaged(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	origIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	origInstallRun := installRun
	var captured install.Options
	installRun = func(gotRoot string, opts install.Options) error {
		if canonicalPath(gotRoot) != canonicalPath(root) {
			t.Fatalf("installRun root = %q, want %q", gotRoot, root)
		}
		captured = opts
		return nil
	}
	t.Cleanup(func() { installRun = origInstallRun })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.SetArgs([]string{"--apply-managed-updates"})
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetIn(bytes.NewBufferString(""))

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade: %v", err)
		}
		errText := stderr.String()
		if !strings.Contains(errText, messages.UpgradeSkipMemoryUpdatesInfo) {
			t.Fatalf("expected skip-memory note, got %q", errText)
		}
		if !strings.Contains(errText, messages.UpgradeSkipDeletionsInfo) {
			t.Fatalf("expected skip-deletions note, got %q", errText)
		}
	})

	promptFuncs, ok := captured.Prompter.(install.PromptFuncs)
	if !ok {
		t.Fatalf("captured opts.Prompter = %T, want install.PromptFuncs", captured.Prompter)
	}
	if overwriteManaged, err := promptFuncs.OverwriteAll(nil); err != nil || !overwriteManaged {
		t.Fatalf("OverwriteAll = (%v, %v), want (true, nil)", overwriteManaged, err)
	}
	if overwriteMemory, err := promptFuncs.OverwriteAllMemory(nil); err != nil || overwriteMemory {
		t.Fatalf("OverwriteAllMemory = (%v, %v), want (false, nil)", overwriteMemory, err)
	}
	if deleteAll, err := promptFuncs.DeleteUnknownAll(nil); err != nil || deleteAll {
		t.Fatalf("DeleteUnknownAll = (%v, %v), want (false, nil)", deleteAll, err)
	}
}

func TestUpgradeCmd_PropagatesInstallErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	origIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	sentinel := errors.New("boom")
	origInstallRun := installRun
	installRun = func(string, install.Options) error {
		return sentinel
	}
	t.Cleanup(func() { installRun = origInstallRun })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{"--yes", "--apply-managed-updates"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		err := cmd.Execute()
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel error, got %v", err)
		}
	})
}

func TestUpgradeCmd_MissingAgentLayerErrors(t *testing.T) {
	root := t.TempDir()

	origIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{"--yes", "--apply-managed-updates"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Error() != messages.RootMissingAgentLayer {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestUpgradeCmd_InvalidDiffLines(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{"--yes", "--apply-managed-updates", "--diff-lines=0"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error for invalid --diff-lines")
		}
		if !strings.Contains(err.Error(), "invalid value for --diff-lines") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
