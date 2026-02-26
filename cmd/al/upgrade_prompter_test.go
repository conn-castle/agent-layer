package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
)

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
	}, nil)
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
	}, nil)
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
	}, nil)
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

func TestBuildUpgradePrompter_UnifiedReviewStatePromptsOnce(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	// First answer applies managed updates, second declines memory updates.
	cmd.SetIn(bytes.NewBufferString("y\nn\n"))

	state := &upgradeReviewState{
		enabled: true,
		managedPreviews: []install.DiffPreview{
			{Path: ".agent-layer/config.toml"},
		},
		memoryPreviews: []install.DiffPreview{
			{Path: "docs/agent-layer/ROADMAP.md"},
		},
	}
	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, state)

	managed, err := p.OverwriteAll(nil)
	if err != nil {
		t.Fatalf("OverwriteAll: %v", err)
	}
	memory, err := p.OverwriteAllMemory(nil)
	if err != nil {
		t.Fatalf("OverwriteAllMemory: %v", err)
	}
	if !managed {
		t.Fatal("expected managed overwrite decision to be true")
	}
	if memory {
		t.Fatal("expected memory overwrite decision to be false")
	}
	if !state.prompted {
		t.Fatal("expected unified review state to be marked prompted")
	}
}

func TestBuildUpgradePrompter_UnifiedCallbackPromptsOnce(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	// First answer applies managed updates, second declines memory updates.
	cmd.SetIn(bytes.NewBufferString("y\nn\n"))

	state := &upgradeReviewState{enabled: true}
	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, state)

	managed, memory, err := p.OverwriteAllUnified(
		[]install.DiffPreview{{Path: ".agent-layer/config.toml"}},
		[]install.DiffPreview{{Path: "docs/agent-layer/ROADMAP.md"}},
	)
	if err != nil {
		t.Fatalf("OverwriteAllUnified: %v", err)
	}
	if !managed {
		t.Fatal("expected managed overwrite decision to be true")
	}
	if memory {
		t.Fatal("expected memory overwrite decision to be false")
	}
	if !state.prompted {
		t.Fatal("expected unified review state to be marked prompted")
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

// enableTestColorOutput configures the fatih/color library and isTerminal stub
// to force ANSI color output in tests. The fatih/color library reads NO_COLOR
// at init time and caches the result in color.NoColor, so we must both clear
// the env var and explicitly set the package-level bool. If fatih/color adds
// additional environment checks in future versions, this helper may need
// updating to account for them.
func enableTestColorOutput(t *testing.T) {
	t.Helper()
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	origIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	origNoColor := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = origNoColor })

	origAdded := diffColorAdded
	origRemoved := diffColorRemoved
	origHunk := diffColorHunk
	diffColorAdded = color.New(color.FgGreen)
	diffColorRemoved = color.New(color.FgRed)
	diffColorHunk = color.New(color.FgCyan)
	t.Cleanup(func() {
		diffColorAdded = origAdded
		diffColorRemoved = origRemoved
		diffColorHunk = origHunk
	})
}

// disableTestColorOutput configures the fatih/color library to suppress ANSI
// output. See enableTestColorOutput for context on the fatih/color coupling.
func disableTestColorOutput(t *testing.T) {
	t.Helper()
	t.Setenv("TERM", "xterm-256color")

	origIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	origNoColor := color.NoColor
	color.NoColor = true
	t.Cleanup(func() { color.NoColor = origNoColor })

	origAdded := diffColorAdded
	origRemoved := diffColorRemoved
	origHunk := diffColorHunk
	diffColorAdded = color.New(color.FgGreen)
	diffColorRemoved = color.New(color.FgRed)
	diffColorHunk = color.New(color.FgCyan)
	t.Cleanup(func() {
		diffColorAdded = origAdded
		diffColorRemoved = origRemoved
		diffColorHunk = origHunk
	})
}

func TestPrintDiffPreviews_ColorizedWhenInteractive(t *testing.T) {
	enableTestColorOutput(t)

	var buf bytes.Buffer
	previews := []install.DiffPreview{
		{Path: "file-a.txt", UnifiedDiff: "--- a\n+++ b\n@@ -1 +1 @@\n-old\n+new\n"},
	}
	if err := printDiffPreviews(&buf, "Header", previews); err != nil {
		t.Fatalf("printDiffPreviews colorized: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("expected ANSI color sequences in output:\n%s", output)
	}
	if !strings.Contains(output, "Diff for file-a.txt:") {
		t.Fatalf("expected diff label in output:\n%s", output)
	}
}

func TestPrintDiffPreviews_NoColorFallback(t *testing.T) {
	disableTestColorOutput(t)

	var buf bytes.Buffer
	previews := []install.DiffPreview{
		{Path: "file-a.txt", UnifiedDiff: "--- a\n+++ b\n-old\n+new\n"},
	}
	if err := printDiffPreviews(&buf, "Header", previews); err != nil {
		t.Fatalf("printDiffPreviews no-color: %v", err)
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("expected plain output without ANSI color sequences:\n%s", buf.String())
	}
}

func TestWriteSinglePreviewBlock_ColorizedWhenInteractive(t *testing.T) {
	enableTestColorOutput(t)

	var buf bytes.Buffer
	preview := install.DiffPreview{Path: "file-a.txt", UnifiedDiff: "--- a\n+++ b\n-old\n+new\n"}
	if err := writeSinglePreviewBlock(&buf, preview); err != nil {
		t.Fatalf("writeSinglePreviewBlock colorized: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("expected ANSI color sequences in output:\n%s", output)
	}
	if !strings.Contains(output, "    diff:") {
		t.Fatalf("expected diff label in output:\n%s", output)
	}
}

func TestReadinessSummaryAndAction(t *testing.T) {
	ids := []string{
		"unrecognized_config_keys",
		"unresolved_config_placeholders",
		"process_env_overrides_dotenv",
		"ignored_empty_dotenv_assignments",
		"path_expansion_anomalies",
		"vscode_no_sync_outputs_stale",
		"floating_external_dependency_specs",
		"stale_disabled_agent_artifacts",
		"missing_required_config_fields",
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
				Rationale:  "Move legacy skill path",
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

func TestBuildUpgradePrompter_ConfigSetDefaultFallbackAcceptDecline(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("y\nn\n"))

	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)

	accepted, err := p.ConfigSetDefault("new.required", "alpha", "needed for test", nil)
	if err != nil {
		t.Fatalf("ConfigSetDefault accept: %v", err)
	}
	if accepted != "alpha" {
		t.Fatalf("accepted value = %v, want %q", accepted, "alpha")
	}

	_, err = p.ConfigSetDefault("new.required", "beta", "needed for test", nil)
	if err == nil || !strings.Contains(err.Error(), "user declined default value") {
		t.Fatalf("expected decline error, got %v", err)
	}
}

func TestBuildUpgradePrompter_ConfigSetDefaultBypassesPromptWhenYes(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))

	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{yes: true}, nil)
	value, err := p.ConfigSetDefault("new.required", true, "needed for test", &config.FieldDef{
		Key:  "new.required",
		Type: config.FieldBool,
	})
	if err != nil {
		t.Fatalf("ConfigSetDefault yes-mode: %v", err)
	}
	if value != true {
		t.Fatalf("value = %v, want true", value)
	}
}

func TestBuildUpgradePrompter_OverwriteAllUnifiedFallbackPrompts(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("y\nn\n"))

	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{}, nil)
	managed, memory, err := p.OverwriteAllUnified(
		[]install.DiffPreview{{Path: ".agent-layer/commands.allow"}},
		[]install.DiffPreview{{Path: "docs/agent-layer/ROADMAP.md"}},
	)
	if err != nil {
		t.Fatalf("OverwriteAllUnified fallback: %v", err)
	}
	if !managed {
		t.Fatal("expected managed overwrite approval")
	}
	if memory {
		t.Fatal("expected memory overwrite rejection")
	}
}

func TestPrintDiffPreviews_WriteError(t *testing.T) {
	out := &errorWriter{failAfter: 0}
	err := printDiffPreviews(out, "header", []install.DiffPreview{{Path: "a"}})
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected write failure, got %v", err)
	}
}

func TestWriteReadinessSection_TruncatesDetails(t *testing.T) {
	var buf bytes.Buffer
	checks := []install.UpgradeReadinessCheck{
		{
			ID:      "unrecognized_config_keys",
			Summary: "summary ignored for known IDs",
			Details: []string{"one", "two", "three", "four"},
		},
	}

	if err := writeReadinessSection(&buf, checks); err != nil {
		t.Fatalf("writeReadinessSection: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "recommendation:") {
		t.Fatalf("expected recommendation line, got:\n%s", output)
	}
	if !strings.Contains(output, "note: ... and 1 more") {
		t.Fatalf("expected detail truncation line, got:\n%s", output)
	}
}

func TestWriteUpgradeSummary_NoReadinessWarnings(t *testing.T) {
	var buf bytes.Buffer
	plan := install.UpgradePlan{
		MigrationReport: install.UpgradeMigrationReport{
			Entries: []install.UpgradeMigrationEntry{
				{Status: install.UpgradeMigrationStatusPlanned},
				{Status: install.UpgradeMigrationStatusNoop},
			},
		},
	}
	if err := writeUpgradeSummary(&buf, plan); err != nil {
		t.Fatalf("writeUpgradeSummary: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "migrations planned: 1") {
		t.Fatalf("expected planned migration count, got:\n%s", output)
	}
	if !strings.Contains(output, "needs review before apply: no") {
		t.Fatalf("expected no-review summary, got:\n%s", output)
	}
}
