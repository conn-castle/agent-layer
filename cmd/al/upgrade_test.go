package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestNewUpgradeCmd_RegistersPlanSubcommand(t *testing.T) {
	cmd := newUpgradeCmd()
	if cmd.Use != "upgrade" {
		t.Fatalf("unexpected use: %s", cmd.Use)
	}
	foundPlan := false
	foundRollback := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "plan" {
			foundPlan = true
		}
		if strings.HasPrefix(sub.Use, "rollback") {
			foundRollback = true
		}
	}
	if !foundPlan {
		t.Fatal("expected upgrade plan subcommand")
	}
	if !foundRollback {
		t.Fatal("expected upgrade rollback subcommand")
	}
}

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

	withWorkingDir(t, root, func() {
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

	withWorkingDir(t, root, func() {
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

	withWorkingDir(t, root, func() {
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

	withWorkingDir(t, root, func() {
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
	if captured.Force {
		t.Fatalf("captured opts.Force = true, want false")
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

	withWorkingDir(t, root, func() {
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
	if captured.Force {
		t.Fatalf("captured opts.Force = true, want false")
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

	withWorkingDir(t, root, func() {
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

	withWorkingDir(t, root, func() {
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

	withWorkingDir(t, root, func() {
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

func TestUpgradePlanCmd_JSONOutput(t *testing.T) {
	root := prepareUpgradeTestRepo(t)
	withWorkingDir(t, root, func() {
		diffLines := install.DefaultDiffMaxLines
		cmd := newUpgradePlanCmd(&diffLines)
		cmd.SetArgs([]string{"--json"})
		var out bytes.Buffer
		var errOut bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade plan --json: %v", err)
		}
		if !strings.Contains(errOut.String(), "deprecated") {
			t.Fatalf("expected deprecation warning on stderr for --json, got stderr=%q", errOut.String())
		}
		jsonPayload := bytes.TrimSpace(out.Bytes())
		if len(jsonPayload) == 0 || jsonPayload[0] != '{' {
			t.Fatalf("expected pure json object in stdout, got: %q", out.String())
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(jsonPayload, &raw); err != nil {
			t.Fatalf("decode raw json: %v\noutput: %s", err, out.String())
		}
		readinessJSON, ok := raw["readiness_checks"]
		if !ok {
			t.Fatalf("expected readiness_checks in json output\noutput: %s", out.String())
		}
		var readinessChecks []install.UpgradeReadinessCheck
		if err := json.Unmarshal(readinessJSON, &readinessChecks); err != nil {
			t.Fatalf("decode readiness_checks: %v\njson: %s", err, string(readinessJSON))
		}

		var plan install.UpgradePlan
		if err := json.Unmarshal(jsonPayload, &plan); err != nil {
			t.Fatalf("decode json: %v\noutput: %s", err, out.String())
		}
		if !plan.DryRun {
			t.Fatalf("expected dry-run plan")
		}
		if plan.SchemaVersion != install.UpgradePlanSchemaVersion {
			t.Fatalf("expected schema version %d, got %d", install.UpgradePlanSchemaVersion, plan.SchemaVersion)
		}
		if len(plan.TemplateRenames) == 0 {
			t.Fatalf("expected rename detection in plan output")
		}
		if plan.PinVersionChange.Action != install.UpgradePinActionRemove {
			t.Fatalf("expected pin removal action for dev target, got %s", plan.PinVersionChange.Action)
		}
		for _, change := range plan.TemplateUpdates {
			if change.OwnershipState == "" {
				t.Fatalf("expected ownership_state for update %s", change.Path)
			}
		}
		for _, change := range plan.SectionAwareUpdates {
			if change.OwnershipState == "" {
				t.Fatalf("expected ownership_state for section-aware update %s", change.Path)
			}
		}
		for _, rename := range plan.TemplateRenames {
			if rename.OwnershipState == "" {
				t.Fatalf("expected ownership_state for rename %s -> %s", rename.From, rename.To)
			}
		}
	})
}

func TestUpgradePlanCmd_TextOutputIncludesPlainSections(t *testing.T) {
	root := prepareUpgradeTestRepo(t)
	withWorkingDir(t, root, func() {
		diffLines := install.DefaultDiffMaxLines
		cmd := newUpgradePlanCmd(&diffLines)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade plan: %v", err)
		}

		output := out.String()
		expectedSnippets := []string{
			"Upgrade plan (dry-run): no files were written.",
			"Summary:",
			"Files to add:",
			"Files to update:",
			"Files to rename:",
			"Files to review for removal:",
			"Config updates:",
			"Pin version change:",
			"Readiness checks:",
			"action:",
			"needs review before apply:",
		}
		for _, snippet := range expectedSnippets {
			if !strings.Contains(output, snippet) {
				t.Fatalf("expected output to contain %q\noutput:\n%s", snippet, output)
			}
		}
	})
}

func TestUpgradePlanCmd_TextOutputHidesOwnershipDiagnostics(t *testing.T) {
	root := t.TempDir()
	if err := install.Run(root, install.Options{System: install.RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".agent-layer", "state", "managed-baseline.json")); err != nil {
		t.Fatalf("remove canonical baseline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "commands.allow"), []byte("# custom allowlist\n"), 0o644); err != nil {
		t.Fatalf("write custom allowlist: %v", err)
	}

	withWorkingDir(t, root, func() {
		diffLines := install.DefaultDiffMaxLines
		cmd := newUpgradePlanCmd(&diffLines)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade plan: %v", err)
		}
		output := out.String()
		notExpected := []string{
			"Ownership warnings:",
			"unknown no baseline",
			"confidence=high",
			"detection=",
			"reasons=",
		}
		for _, snippet := range notExpected {
			if strings.Contains(output, snippet) {
				t.Fatalf("expected output not to contain %q\noutput:\n%s", snippet, output)
			}
		}
	})
}

func TestUpgradePlanCmd_HelpHidesJSONFlag(t *testing.T) {
	diffLines := install.DefaultDiffMaxLines
	cmd := newUpgradePlanCmd(&diffLines)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute help: %v", err)
	}
	if strings.Contains(out.String(), "--json") {
		t.Fatalf("expected --json to be hidden from help output:\n%s", out.String())
	}
}

func TestUpgradePlanCmd_TextOutputIncludesDiffPreviewAndTruncation(t *testing.T) {
	root := prepareUpgradeTestRepo(t)
	withWorkingDir(t, root, func() {
		diffLines := 1
		cmd := newUpgradePlanCmd(&diffLines)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade plan: %v", err)
		}
		output := out.String()
		if !strings.Contains(output, "diff:") {
			t.Fatalf("expected diff blocks in plan output:\n%s", output)
		}
		if !strings.Contains(output, "--diff-lines") {
			t.Fatalf("expected truncation hint to mention --diff-lines:\n%s", output)
		}
	})
}

func TestUpgradePlanCmd_InvalidDiffLines(t *testing.T) {
	root := prepareUpgradeTestRepo(t)
	withWorkingDir(t, root, func() {
		diffLines := 0
		cmd := newUpgradePlanCmd(&diffLines)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error for invalid --diff-lines")
		}
		if !strings.Contains(err.Error(), "invalid value for --diff-lines") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestUpgradeCmd_InvalidDiffLines(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	withWorkingDir(t, root, func() {
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

func TestUpgradeCmd_HelpShowsApplyFlagsWithoutForce(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetArgs([]string{"--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute upgrade --help: %v", err)
	}
	help := out.String()
	if strings.Contains(help, "--force") {
		t.Fatalf("expected --force to be removed from help output:\n%s", help)
	}
	for _, flag := range []string{
		"--yes",
		"--apply-managed-updates",
		"--apply-memory-updates",
		"--apply-deletions",
	} {
		if !strings.Contains(help, flag) {
			t.Fatalf("expected help output to include %s:\n%s", flag, help)
		}
	}
}

func TestUpgradeRollbackCmd_RequiresSnapshotID(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	withWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{"rollback"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Error() != messages.UpgradeRollbackRequiresSnapshotID {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestUpgradeRollbackCmd_InvokesInstallRollback(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	origRollback := installRollbackUpgradeSnapshot
	called := false
	installRollbackUpgradeSnapshot = func(gotRoot string, snapshotID string, opts install.RollbackUpgradeSnapshotOptions) error {
		called = true
		if canonicalPath(gotRoot) != canonicalPath(root) {
			t.Fatalf("rollback root = %q, want %q", gotRoot, root)
		}
		if snapshotID != "snapshot-123" {
			t.Fatalf("snapshot id = %q, want snapshot-123", snapshotID)
		}
		if opts.System == nil {
			t.Fatal("opts.System = nil, want non-nil")
		}
		return nil
	}
	t.Cleanup(func() { installRollbackUpgradeSnapshot = origRollback })

	withWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		var out bytes.Buffer
		cmd.SetArgs([]string{"rollback", "snapshot-123"})
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade rollback: %v", err)
		}
		if !called {
			t.Fatal("expected installRollbackUpgradeSnapshot to be called")
		}
		if !strings.Contains(out.String(), "snapshot-123") {
			t.Fatalf("expected success output with snapshot id, got %q", out.String())
		}
	})
}

func TestUpgradeRollbackCmd_PropagatesInstallErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	sentinel := errors.New("rollback failed")
	origRollback := installRollbackUpgradeSnapshot
	installRollbackUpgradeSnapshot = func(string, string, install.RollbackUpgradeSnapshotOptions) error {
		return sentinel
	}
	t.Cleanup(func() { installRollbackUpgradeSnapshot = origRollback })

	withWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		cmd.SetArgs([]string{"rollback", "snapshot-123"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetIn(bytes.NewBufferString(""))

		err := cmd.Execute()
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel error, got %v", err)
		}
	})
}

func prepareUpgradeTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := install.Run(root, install.Options{System: install.RealSystem{}, PinVersion: "1.2.3"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".agent-layer", "state", "managed-baseline.json")); err != nil {
		t.Fatalf("remove canonical baseline: %v", err)
	}

	oldRoadmap := []byte("# ROADMAP\n\nLegacy header\n\n<!-- PHASES START -->\n")
	roadmapPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	baselineRoadmapPath := filepath.Join(root, ".agent-layer", "templates", "docs", "ROADMAP.md")
	if err := os.WriteFile(roadmapPath, oldRoadmap, 0o644); err != nil {
		t.Fatalf("write roadmap: %v", err)
	}
	if err := os.WriteFile(baselineRoadmapPath, oldRoadmap, 0o644); err != nil {
		t.Fatalf("write baseline roadmap: %v", err)
	}
	issuesTemplate, err := templates.Read("docs/agent-layer/ISSUES.md")
	if err != nil {
		t.Fatalf("read issues template: %v", err)
	}
	customIssues := strings.Replace(string(issuesTemplate), "<!-- ENTRIES START -->\n", "<!-- ENTRIES START -->\n\n- issue from repo\n", 1)
	if err := os.WriteFile(filepath.Join(root, "docs", "agent-layer", "ISSUES.md"), []byte(customIssues), 0o644); err != nil {
		t.Fatalf("write issues: %v", err)
	}

	findIssuesPath := filepath.Join(root, ".agent-layer", "slash-commands", "find-issues.md")
	if err := os.Remove(findIssuesPath); err != nil {
		t.Fatalf("remove find-issues slash command: %v", err)
	}
	findIssuesTemplate, err := templates.Read("slash-commands/find-issues.md")
	if err != nil {
		t.Fatalf("read find-issues template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "slash-commands", "find-issues-legacy.md"), findIssuesTemplate, 0o644); err != nil {
		t.Fatalf("write orphan rename file: %v", err)
	}
	return root
}

func TestIsMemoryPreviewPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"docs/agent-layer", true},
		{"docs/agent-layer/ROADMAP.md", true},
		{"docs/agent-layer/ISSUES.md", true},
		{".agent-layer/commands.allow", false},
		{"", false},
		{"docs/other/file.md", false},
	}
	for _, tt := range tests {
		if got := isMemoryPreviewPath(tt.path); got != tt.want {
			t.Errorf("isMemoryPreviewPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestBuildUpgradePrompter_DeletionPolicyPaths(t *testing.T) {
	// Test --apply-deletions --yes: auto-approve deletions.
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))
	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{
		explicitCategory: true,
		applyDeletions:   true,
		yes:              true,
	})
	if deleteAll, err := p.DeleteUnknownAll([]string{"/tmp/a"}); err != nil || !deleteAll {
		t.Fatalf("DeleteUnknownAll(yes+applyDeletions) = (%v, %v), want (true, nil)", deleteAll, err)
	}
	if deleteSingle, err := p.DeleteUnknown("/tmp/a"); err != nil || !deleteSingle {
		t.Fatalf("DeleteUnknown(yes+applyDeletions) = (%v, %v), want (true, nil)", deleteSingle, err)
	}

	// Test --apply-deletions without --yes (explicit category, not auto): skips deletions.
	// (In real usage this would still prompt, but with no stdin it would fail.
	// Instead, test the !applyDeletions path which is the skip-all case.)
	p2 := buildUpgradePrompter(cmd, upgradeApplyPolicy{
		explicitCategory: true,
		applyDeletions:   false,
		yes:              true,
	})
	if deleteAll, err := p2.DeleteUnknownAll([]string{"/tmp/a"}); err != nil || deleteAll {
		t.Fatalf("DeleteUnknownAll(!applyDeletions) = (%v, %v), want (false, nil)", deleteAll, err)
	}
	if deleteSingle, err := p2.DeleteUnknown("/tmp/a"); err != nil || deleteSingle {
		t.Fatalf("DeleteUnknown(!applyDeletions) = (%v, %v), want (false, nil)", deleteSingle, err)
	}
}

func TestBuildUpgradePrompter_OverwritePreviewMemoryPath(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))

	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{
		explicitCategory: true,
		applyManaged:     true,
		applyMemory:      false,
	})
	// Memory path should use applyMemory (false).
	memResult, err := p.Overwrite(install.DiffPreview{Path: "docs/agent-layer/ROADMAP.md"})
	if err != nil {
		t.Fatalf("Overwrite memory path: %v", err)
	}
	if memResult {
		t.Fatal("expected Overwrite for memory path to return false when applyMemory=false")
	}
	// Managed path should use applyManaged (true).
	managedResult, err := p.Overwrite(install.DiffPreview{Path: ".agent-layer/commands.allow"})
	if err != nil {
		t.Fatalf("Overwrite managed path: %v", err)
	}
	if !managedResult {
		t.Fatal("expected Overwrite for managed path to return true when applyManaged=true")
	}
}

func TestPrintDiffPreviews(t *testing.T) {
	var buf bytes.Buffer
	previews := []install.DiffPreview{
		{Path: "file-a.txt", UnifiedDiff: "--- a\n+++ b\n-old\n+new\n"},
		{Path: "file-b.txt", UnifiedDiff: ""},
	}
	if err := printDiffPreviews(&buf, "Test Header", previews); err != nil {
		t.Fatalf("printDiffPreviews: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Test Header") {
		t.Fatalf("expected header in output:\n%s", output)
	}
	if !strings.Contains(output, "file-a.txt") {
		t.Fatalf("expected file-a.txt in output:\n%s", output)
	}
	if !strings.Contains(output, "Diff for file-a.txt:") {
		t.Fatalf("expected diff block for file-a.txt in output:\n%s", output)
	}
	// file-b has empty diff, so should not have a diff block.
	if strings.Contains(output, "Diff for file-b.txt:") {
		t.Fatalf("expected no diff block for file-b.txt (empty diff):\n%s", output)
	}

	// Empty previews should be a no-op.
	var empty bytes.Buffer
	if err := printDiffPreviews(&empty, "Header", nil); err != nil {
		t.Fatalf("printDiffPreviews empty: %v", err)
	}
	if empty.Len() != 0 {
		t.Fatalf("expected no output for empty previews, got %q", empty.String())
	}

	// No header.
	var noHeader bytes.Buffer
	if err := printDiffPreviews(&noHeader, "", previews[:1]); err != nil {
		t.Fatalf("printDiffPreviews no header: %v", err)
	}
	if strings.Contains(noHeader.String(), "Test Header") {
		t.Fatalf("expected no header in output:\n%s", noHeader.String())
	}
}

func TestReadinessSummaryAndAction(t *testing.T) {
	ids := []string{
		"unrecognized_config_keys",
		"vscode_no_sync_outputs_stale",
		"floating_external_dependency_specs",
		"stale_disabled_agent_artifacts",
		"unknown_id",
	}
	for _, id := range ids {
		check := install.UpgradeReadinessCheck{ID: id, Summary: "fallback summary"}
		summary := readinessSummary(check)
		if summary == "" {
			t.Fatalf("readinessSummary(%q) returned empty", id)
		}
		action := readinessAction(id)
		if id == "unknown_id" {
			if action != "" {
				t.Fatalf("readinessAction(%q) = %q, want empty", id, action)
			}
			if summary != "fallback summary" {
				t.Fatalf("readinessSummary(%q) = %q, want %q", id, summary, "fallback summary")
			}
		} else if action == "" {
			t.Fatalf("readinessAction(%q) returned empty", id)
		}
	}
}

func TestWriteConfigMigrationSection_WithEntries(t *testing.T) {
	var buf bytes.Buffer
	migrations := []install.ConfigKeyMigration{
		{Key: "mcp.timeout", From: "30s", To: "60s"},
	}
	if err := writeConfigMigrationSection(&buf, "Config updates", migrations); err != nil {
		t.Fatalf("writeConfigMigrationSection: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "mcp.timeout") {
		t.Fatalf("expected migration key in output:\n%s", output)
	}
	if !strings.Contains(output, "30s") || !strings.Contains(output, "60s") {
		t.Fatalf("expected from/to values in output:\n%s", output)
	}
}

func TestWriteMigrationReportSection_WithEntries(t *testing.T) {
	var buf bytes.Buffer
	report := install.UpgradeMigrationReport{
		TargetVersion:       "0.7.0",
		SourceVersion:       "unknown",
		SourceVersionOrigin: install.UpgradeMigrationSourceUnknown,
		Entries: []install.UpgradeMigrationEntry{
			{
				ID:         "rename_find_issues",
				Kind:       "rename_file",
				Rationale:  "Move legacy slash command path",
				Status:     install.UpgradeMigrationStatusSkippedUnknownSource,
				SkipReason: "source version is unknown",
			},
		},
	}
	if err := writeMigrationReportSection(&buf, "Migrations", report); err != nil {
		t.Fatalf("writeMigrationReportSection: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Migrations:") {
		t.Fatalf("expected section title in output:\n%s", output)
	}
	if !strings.Contains(output, "source version: unknown") {
		t.Fatalf("expected source version in output:\n%s", output)
	}
	if !strings.Contains(output, "[skipped_unknown_source] rename_find_issues") {
		t.Fatalf("expected migration status line in output:\n%s", output)
	}
	if !strings.Contains(output, "reason: source version is unknown") {
		t.Fatalf("expected skip reason in output:\n%s", output)
	}
}

func TestWriteUpgradeSkippedCategoryNotes_AllSkipped(t *testing.T) {
	var buf bytes.Buffer
	policy := upgradeApplyPolicy{
		explicitCategory: true,
		applyManaged:     false,
		applyMemory:      false,
		applyDeletions:   false,
	}
	if err := writeUpgradeSkippedCategoryNotes(&buf, policy); err != nil {
		t.Fatalf("writeUpgradeSkippedCategoryNotes: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, messages.UpgradeSkipManagedUpdatesInfo) {
		t.Fatalf("expected managed skip note:\n%s", output)
	}
	if !strings.Contains(output, messages.UpgradeSkipMemoryUpdatesInfo) {
		t.Fatalf("expected memory skip note:\n%s", output)
	}
	if !strings.Contains(output, messages.UpgradeSkipDeletionsInfo) {
		t.Fatalf("expected deletions skip note:\n%s", output)
	}
}

func TestWriteUpgradeSkippedCategoryNotes_NoneSkipped(t *testing.T) {
	var buf bytes.Buffer
	policy := upgradeApplyPolicy{
		explicitCategory: true,
		applyManaged:     true,
		applyMemory:      true,
		applyDeletions:   true,
	}
	if err := writeUpgradeSkippedCategoryNotes(&buf, policy); err != nil {
		t.Fatalf("writeUpgradeSkippedCategoryNotes: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output when all categories applied, got %q", buf.String())
	}
}

func TestWriteUpgradeSkippedCategoryNotes_NotExplicit(t *testing.T) {
	var buf bytes.Buffer
	if err := writeUpgradeSkippedCategoryNotes(&buf, upgradeApplyPolicy{}); err != nil {
		t.Fatalf("writeUpgradeSkippedCategoryNotes: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output for non-explicit policy, got %q", buf.String())
	}
}

func canonicalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
