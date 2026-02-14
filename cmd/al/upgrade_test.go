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
	for _, sub := range cmd.Commands() {
		if sub.Use == "plan" {
			foundPlan = true
			break
		}
	}
	if !foundPlan {
		t.Fatal("expected upgrade plan subcommand")
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
		deprecationOutput := errOut.String() + out.String()
		if !strings.Contains(deprecationOutput, "deprecated") {
			t.Fatalf("expected deprecation warning for --json, got: stderr=%q stdout=%q", errOut.String(), out.String())
		}
		jsonPayload := out.Bytes()
		jsonStart := bytes.IndexByte(jsonPayload, '{')
		if jsonStart == -1 {
			t.Fatalf("expected json object in output, got: %q", out.String())
		}
		jsonPayload = jsonPayload[jsonStart:]

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

func canonicalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
