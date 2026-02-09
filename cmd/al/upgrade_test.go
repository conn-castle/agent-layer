package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/install"
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

func TestUpgradePlanCmd_JSONOutput(t *testing.T) {
	root := prepareUpgradeTestRepo(t)
	withWorkingDir(t, root, func() {
		cmd := newUpgradePlanCmd()
		cmd.SetArgs([]string{"--json"})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade plan --json: %v", err)
		}

		var plan install.UpgradePlan
		if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
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

func TestUpgradePlanCmd_TextOutputIncludesSectionsAndLabels(t *testing.T) {
	root := prepareUpgradeTestRepo(t)
	withWorkingDir(t, root, func() {
		cmd := newUpgradePlanCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade plan: %v", err)
		}

		output := out.String()
		expectedSnippets := []string{
			"Upgrade plan (dry-run): no files were written.",
			"Template additions:",
			"Template updates:",
			"Section-aware updates (managed header only, user entries preserved):",
			"Template renames:",
			"Template removals/orphans:",
			"Config key migrations:",
			"Pin version change:",
			"upstream template delta",
			"confidence=high",
		}
		for _, snippet := range expectedSnippets {
			if !strings.Contains(output, snippet) {
				t.Fatalf("expected output to contain %q\noutput:\n%s", snippet, output)
			}
		}
	})
}

func TestUpgradePlanCmd_TextOutputShowsUnknownBaselineWarning(t *testing.T) {
	root := t.TempDir()
	if err := install.Run(root, install.Options{System: install.RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".agent-layer", "state", "managed-baseline.json")); err != nil {
		t.Fatalf("remove canonical baseline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "config.toml"), []byte("custom config\n"), 0o644); err != nil {
		t.Fatalf("write custom config: %v", err)
	}

	withWorkingDir(t, root, func() {
		cmd := newUpgradePlanCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute upgrade plan: %v", err)
		}
		output := out.String()
		if !strings.Contains(output, "Ownership warnings:") {
			t.Fatalf("expected ownership warning block in output:\n%s", output)
		}
		if !strings.Contains(output, "unknown no baseline") {
			t.Fatalf("expected unknown ownership label in output:\n%s", output)
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
