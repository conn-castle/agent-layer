package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestNewUpgradeCmd_RegistersPlanSubcommand(t *testing.T) {
	cmd := newUpgradeCmd()
	if cmd.Use != "upgrade" {
		t.Fatalf("unexpected use: %s", cmd.Use)
	}
	foundPlan := false
	foundRollback := false
	foundPrefetch := false
	foundRepairGitignore := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "plan" {
			foundPlan = true
		}
		if strings.HasPrefix(sub.Use, "rollback") {
			foundRollback = true
		}
		if sub.Use == "prefetch" {
			foundPrefetch = true
		}
		if sub.Use == "repair-gitignore-block" {
			foundRepairGitignore = true
		}
	}
	if !foundPlan {
		t.Fatal("expected upgrade plan subcommand")
	}
	if !foundRollback {
		t.Fatal("expected upgrade rollback subcommand")
	}
	if !foundPrefetch {
		t.Fatal("expected upgrade prefetch subcommand")
	}
	if !foundRepairGitignore {
		t.Fatal("expected upgrade repair-gitignore-block subcommand")
	}
}

func TestUpgradePlanCmd_TextOutputIncludesPlainSections(t *testing.T) {
	root := prepareUpgradeTestRepo(t)
	testutil.WithWorkingDir(t, root, func() {
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
			"recommendation:",
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

	testutil.WithWorkingDir(t, root, func() {
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

func TestUpgradePlanCmd_JSONFlagRemoved(t *testing.T) {
	root := prepareUpgradeTestRepo(t)
	testutil.WithWorkingDir(t, root, func() {
		diffLines := install.DefaultDiffMaxLines
		cmd := newUpgradePlanCmd(&diffLines)
		cmd.SetArgs([]string{"--json"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected unknown flag error for removed --json")
		}
		if !strings.Contains(err.Error(), "unknown flag: --json") {
			t.Fatalf("unexpected error for removed --json flag: %v", err)
		}
	})
}

func TestUpgradePlanCmd_TextOutputIncludesDiffPreviewAndTruncation(t *testing.T) {
	root := prepareUpgradeTestRepo(t)
	testutil.WithWorkingDir(t, root, func() {
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
	testutil.WithWorkingDir(t, root, func() {
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

func TestUpgradePlanCmd_VersionFlagValidatesExplicitPin(t *testing.T) {
	root := prepareUpgradeTestRepo(t)

	origValidate := validatePinnedReleaseVersionFunc
	calledValidate := false
	validatePinnedReleaseVersionFunc = func(_ context.Context, version string) error {
		calledValidate = true
		if version != "0.8.4" {
			t.Fatalf("validated version = %q, want 0.8.4", version)
		}
		return nil
	}
	t.Cleanup(func() { validatePinnedReleaseVersionFunc = origValidate })

	testutil.WithWorkingDir(t, root, func() {
		diffLines := install.DefaultDiffMaxLines
		cmd := newUpgradePlanCmd(&diffLines)
		cmd.SetArgs([]string{"--version", "0.8.4"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade plan --version: %v", err)
		}
	})

	if !calledValidate {
		t.Fatal("expected explicit pin to be validated")
	}
}

func TestUpgradePlanCmd_VersionFlagValidationError(t *testing.T) {
	root := prepareUpgradeTestRepo(t)

	sentinel := errors.New("pin validation failed")
	origValidate := validatePinnedReleaseVersionFunc
	validatePinnedReleaseVersionFunc = func(context.Context, string) error {
		return sentinel
	}
	t.Cleanup(func() { validatePinnedReleaseVersionFunc = origValidate })

	testutil.WithWorkingDir(t, root, func() {
		diffLines := install.DefaultDiffMaxLines
		cmd := newUpgradePlanCmd(&diffLines)
		cmd.SetArgs([]string{"--version", "0.8.4"})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})

		err := cmd.Execute()
		if !errors.Is(err, sentinel) {
			t.Fatalf("expected sentinel error, got %v", err)
		}
	})
}

func TestUpgradePlanCmd_VersionLatestSkipsValidation(t *testing.T) {
	root := prepareUpgradeTestRepo(t)

	origResolveLatest := resolveLatestPinVersion
	resolveLatestPinVersion = func(context.Context, string) (string, error) {
		return "0.8.4", nil
	}
	t.Cleanup(func() { resolveLatestPinVersion = origResolveLatest })

	origValidate := validatePinnedReleaseVersionFunc
	validatePinnedReleaseVersionFunc = func(context.Context, string) error {
		return errors.New("validate should be skipped for latest")
	}
	t.Cleanup(func() { validatePinnedReleaseVersionFunc = origValidate })

	testutil.WithWorkingDir(t, root, func() {
		diffLines := install.DefaultDiffMaxLines
		cmd := newUpgradePlanCmd(&diffLines)
		cmd.SetArgs([]string{"--version", "latest"})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade plan --version latest: %v", err)
		}
		if !strings.Contains(out.String(), "Pin version change:") {
			t.Fatalf("expected plan output to include pin version section, got %q", out.String())
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
