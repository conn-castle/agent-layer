package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/fatih/color"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/wizard"
)

type stubUpgradeKeepListUI struct {
	selected []string
	title    string
	options  []string
	err      error
}

func (ui *stubUpgradeKeepListUI) MultiSelect(title string, options []string, selected *[]string) error {
	ui.title = title
	ui.options = append([]string(nil), options...)
	if ui.err != nil {
		return ui.err
	}
	*selected = append([]string(nil), ui.selected...)
	return nil
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
	// --apply-deletions --yes: auto-approve non-tmp deletions.
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))
	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{
		explicitCategory: true,
		applyDeletions:   true,
		yes:              true,
	})
	if deleteAll, err := p.DeleteUnknownAllFunc([]string{"/tmp/a"}); err != nil || !deleteAll {
		t.Fatalf("DeleteUnknownAll(yes+applyDeletions) = (%v, %v), want (true, nil)", deleteAll, err)
	}
	if deleteSingle, err := p.DeleteUnknownFunc("/tmp/a"); err != nil || !deleteSingle {
		t.Fatalf("DeleteUnknown(yes+applyDeletions) = (%v, %v), want (true, nil)", deleteSingle, err)
	}

	// --apply-deletions alone (no --apply-tmp-deletions) must NOT delete tmp,
	// because tmp deletion is destructive and explicitly gated behind its own
	// flag. This is the headline guarantee callers rely on.
	if deleteTmp, err := p.DeleteUnknownTmpAllFunc([]string{"/tmp/x"}); err != nil || deleteTmp {
		t.Fatalf("DeleteUnknownTmpAll(yes+applyDeletions, no tmp flag) = (%v, %v), want (false, nil)", deleteTmp, err)
	}

	// !applyDeletions (still explicit because another category flag is set):
	// skip non-tmp deletions.
	p2 := buildUpgradePrompter(cmd, upgradeApplyPolicy{
		explicitCategory: true,
		applyDeletions:   false,
		yes:              true,
	})
	if deleteAll, err := p2.DeleteUnknownAllFunc([]string{"/tmp/a"}); err != nil || deleteAll {
		t.Fatalf("DeleteUnknownAll(!applyDeletions) = (%v, %v), want (false, nil)", deleteAll, err)
	}
	if deleteSingle, err := p2.DeleteUnknownFunc("/tmp/a"); err != nil || deleteSingle {
		t.Fatalf("DeleteUnknown(!applyDeletions) = (%v, %v), want (false, nil)", deleteSingle, err)
	}
	if deleteTmp, err := p2.DeleteUnknownTmpAllFunc([]string{"/tmp/x"}); err != nil || deleteTmp {
		t.Fatalf("DeleteUnknownTmpAll(!applyTmpDeletions) = (%v, %v), want (false, nil)", deleteTmp, err)
	}

	// Both flags + --yes: auto-approve tmp deletion non-interactively.
	p3 := buildUpgradePrompter(cmd, upgradeApplyPolicy{
		explicitCategory:  true,
		applyDeletions:    true,
		applyTmpDeletions: true,
		yes:               true,
	})
	if deleteTmp, err := p3.DeleteUnknownTmpAllFunc([]string{"/tmp/x"}); err != nil || !deleteTmp {
		t.Fatalf("DeleteUnknownTmpAll(yes+applyTmpDeletions) = (%v, %v), want (true, nil)", deleteTmp, err)
	}
}

func TestBuildUpgradePrompter_SelectUnknownsToKeepInteractive(t *testing.T) {
	ui := &stubUpgradeKeepListUI{selected: []string{"docs/agent-layer/NOTES.md"}}
	original := newUpgradeKeepListUI
	newUpgradeKeepListUI = func() upgradeKeepListUI { return ui }
	t.Cleanup(func() { newUpgradeKeepListUI = original })

	cmd := newUpgradeCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("\n"))
	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{interactive: true})
	paths := []string{".agent-layer/local", "docs/agent-layer/NOTES.md"}
	selected, err := p.SelectUnknownsToKeepFunc(paths)
	if err != nil {
		t.Fatalf("SelectUnknownsToKeep: %v", err)
	}
	if !reflect.DeepEqual(selected, ui.selected) {
		t.Fatalf("selected = %v, want %v", selected, ui.selected)
	}
	if !reflect.DeepEqual(ui.options, paths) || ui.title != messages.UpgradeKeepListSelectTitle {
		t.Fatalf("UI received title %q and options %v", ui.title, ui.options)
	}
	printed := out.String()
	if !strings.Contains(printed, "Found 2 unknown paths eligible") {
		t.Fatalf("output did not describe keep-list candidates: %q", printed)
	}
	// The candidate paths must be listed before the yes/no question so the
	// user can decide with the actual list in view.
	promptIndex := strings.Index(printed, messages.UpgradeAddToKeepListPrompt)
	if promptIndex < 0 {
		t.Fatalf("keep-list question missing from output: %q", printed)
	}
	for _, path := range paths {
		pathIndex := strings.Index(printed, path)
		if pathIndex < 0 || pathIndex > promptIndex {
			t.Fatalf("path %q was not listed before the keep-list question: %q", path, printed)
		}
	}
}

func TestBuildUpgradePrompter_SelectUnknownsToKeepBackContinues(t *testing.T) {
	ui := &stubUpgradeKeepListUI{err: wizard.ErrBack}
	original := newUpgradeKeepListUI
	newUpgradeKeepListUI = func() upgradeKeepListUI { return ui }
	t.Cleanup(func() { newUpgradeKeepListUI = original })

	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("\n"))
	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{interactive: true})
	selected, err := p.SelectUnknownsToKeepFunc([]string{".agent-layer/local"})
	if err != nil || len(selected) != 0 {
		t.Fatalf("back selection = %v, err = %v; want no selection and no error", selected, err)
	}
}

func TestBuildUpgradePrompter_SelectUnknownsToKeepSkippedNonInteractive(t *testing.T) {
	called := false
	original := newUpgradeKeepListUI
	newUpgradeKeepListUI = func() upgradeKeepListUI {
		called = true
		return &stubUpgradeKeepListUI{}
	}
	t.Cleanup(func() { newUpgradeKeepListUI = original })

	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{yes: true, applyDeletions: true})
	selected, err := p.SelectUnknownsToKeepFunc([]string{".agent-layer/local"})
	if err != nil || len(selected) != 0 || called {
		t.Fatalf("non-interactive selection = %v, err = %v, UI called = %v", selected, err, called)
	}
}

func TestBuildUpgradePrompter_SelectUnknownsToKeepSkippedWhenDeletionsExcluded(t *testing.T) {
	called := false
	original := newUpgradeKeepListUI
	newUpgradeKeepListUI = func() upgradeKeepListUI {
		called = true
		return &stubUpgradeKeepListUI{}
	}
	t.Cleanup(func() { newUpgradeKeepListUI = original })

	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{interactive: true, explicitCategory: true, applyManaged: true})
	selected, err := p.SelectUnknownsToKeepFunc([]string{".agent-layer/local"})
	if err != nil || len(selected) != 0 || called {
		t.Fatalf("managed-only selection = %v, err = %v, UI called = %v", selected, err, called)
	}
}

func TestBuildUpgradePrompter_DeleteUnknownTmpAllInteractive(t *testing.T) {
	// Tmp deletion is destructive: the interactive prompt requires a
	// double-confirm. The first call answers "y" to the initial prompt and
	// "y" to the destructive confirm, so the call returns true. The second
	// call answers "n" to the initial prompt and skips the confirm. The
	// third call answers "y" then "n", verifying that declining the
	// destructive confirm aborts the deletion even after an initial "yes".
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("y\ny\nn\ny\nn\n"))

	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{})
	yes, err := p.DeleteUnknownTmpAllFunc([]string{".agent-layer/tmp/a", ".agent-layer/tmp/b"})
	if err != nil {
		t.Fatalf("DeleteUnknownTmpAll(yes): %v", err)
	}
	if !yes {
		t.Fatal("expected DeleteUnknownTmpAll to return true after 'y' + 'y' (initial + destructive confirm)")
	}
	no, err := p.DeleteUnknownTmpAllFunc([]string{".agent-layer/tmp/c"})
	if err != nil {
		t.Fatalf("DeleteUnknownTmpAll(no): %v", err)
	}
	if no {
		t.Fatal("expected DeleteUnknownTmpAll to return false on 'n' to the initial prompt")
	}
	declined, err := p.DeleteUnknownTmpAllFunc([]string{".agent-layer/tmp/d"})
	if err != nil {
		t.Fatalf("DeleteUnknownTmpAll(decline-confirm): %v", err)
	}
	if declined {
		t.Fatal("expected DeleteUnknownTmpAll to return false when destructive confirm is declined")
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
	memResult, err := p.OverwritePreviewFunc(install.DiffPreview{Path: "docs/agent-layer/ROADMAP.md"})
	if err != nil {
		t.Fatalf("Overwrite memory path: %v", err)
	}
	if memResult {
		t.Fatal("expected Overwrite for memory path to return false when applyMemory=false")
	}
	// Managed path should use applyManaged (true).
	managedResult, err := p.OverwritePreviewFunc(install.DiffPreview{Path: ".agent-layer/commands.allow"})
	if err != nil {
		t.Fatalf("Overwrite managed path: %v", err)
	}
	if !managedResult {
		t.Fatal("expected Overwrite for managed path to return true when applyManaged=true")
	}
}

func TestBuildUpgradePrompter_StatuslineSourceInteractiveReview(t *testing.T) {
	cmd := newUpgradeCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("y\n"))

	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{interactive: true})
	apply, err := p.StatuslineSourcePreviewFunc(install.DiffPreview{
		Path:         ".agent-layer/claude-statusline.sh",
		UnifiedDiff:  "--- current\n+++ template\n-old\n+new\n",
		LinesAdded:   1,
		LinesRemoved: 1,
	})
	if err != nil {
		t.Fatalf("StatuslineSource: %v", err)
	}
	if !apply {
		t.Fatal("expected interactive yes to approve statusline source replacement")
	}
	output := out.String()
	if !strings.Contains(output, "User-owned statusline source") {
		t.Fatalf("expected statusline source review header, got %q", output)
	}
	if !strings.Contains(output, ".agent-layer/claude-statusline.sh") {
		t.Fatalf("expected reviewed path in output, got %q", output)
	}
}

func TestBuildUpgradePrompter_StatuslineSourceNonInteractiveSkipsOnce(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetIn(bytes.NewBufferString(""))

	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{yes: true})
	for i := 0; i < 2; i++ {
		apply, err := p.StatuslineSourcePreviewFunc(install.DiffPreview{Path: ".agent-layer/claude-statusline.sh"})
		if err != nil {
			t.Fatalf("StatuslineSource(%d): %v", i, err)
		}
		if apply {
			t.Fatalf("StatuslineSource(%d) applied in noninteractive mode", i)
		}
	}
	if got := strings.Count(stderr.String(), messages.UpgradeSkipStatuslineSourceUpdatesInfo); got != 1 {
		t.Fatalf("expected one skip note, got %d in %q", got, stderr.String())
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
	// Assert each known ID maps to its SPECIFIC summary/action constant. A bare
	// non-empty check would pass even if two cases were swapped or an ID mapped
	// to the wrong constant; the explicit expected values catch that.
	cases := []struct {
		id          string
		wantSummary string
		wantAction  string
	}{
		{"unrecognized_config_keys", messages.UpgradeReadinessUnrecognizedKeys, messages.UpgradeReadinessActionUnrecognizedKeys},
		{"unresolved_config_placeholders", messages.UpgradeReadinessUnresolvedPlaceholder, messages.UpgradeReadinessActionUnresolvedPlaceholder},
		{"process_env_overrides_dotenv", messages.UpgradeReadinessProcessEnvOverrides, messages.UpgradeReadinessActionProcessEnvOverrides},
		{"ignored_empty_dotenv_assignments", messages.UpgradeReadinessEmptyDotenv, messages.UpgradeReadinessActionEmptyDotenv},
		{"path_expansion_anomalies", messages.UpgradeReadinessPathExpansion, messages.UpgradeReadinessActionPathExpansion},
		{"vscode_no_sync_outputs_stale", messages.UpgradeReadinessVSCodeStale, messages.UpgradeReadinessActionVSCodeStale},
		{"floating_external_dependency_specs", messages.UpgradeReadinessFloatingDeps, messages.UpgradeReadinessActionFloatingDeps},
		{"stale_disabled_agent_artifacts", messages.UpgradeReadinessStaleDisabledAgents, messages.UpgradeReadinessActionStaleDisabledAgents},
		{"missing_required_config_fields", messages.UpgradeReadinessMissingRequiredFields, messages.UpgradeReadinessActionMissingRequiredFields},
	}
	for _, tc := range cases {
		check := install.UpgradeReadinessCheck{ID: tc.id, Summary: "fallback summary"}
		if got := readinessSummary(check); got != tc.wantSummary {
			t.Fatalf("readinessSummary(%q) = %q, want %q", tc.id, got, tc.wantSummary)
		}
		if got := readinessAction(tc.id); got != tc.wantAction {
			t.Fatalf("readinessAction(%q) = %q, want %q", tc.id, got, tc.wantAction)
		}
	}

	// Unknown IDs fall back to the check's own summary and produce no action.
	check := install.UpgradeReadinessCheck{ID: "unknown_id", Summary: "fallback summary"}
	if got := readinessSummary(check); got != "fallback summary" {
		t.Fatalf("readinessSummary(unknown) = %q, want fallback", got)
	}
	if got := readinessAction("unknown_id"); got != "" {
		t.Fatalf("readinessAction(unknown) = %q, want empty", got)
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
		explicitCategory:  true,
		applyManaged:      true,
		applyMemory:       true,
		applyDeletions:    true,
		applyTmpDeletions: true,
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

	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{})

	accepted, err := p.ConfigSetDefaultFunc("new.required", "alpha", "needed for test", nil)
	if err != nil {
		t.Fatalf("ConfigSetDefault accept: %v", err)
	}
	if accepted != "alpha" {
		t.Fatalf("accepted value = %v, want %q", accepted, "alpha")
	}

	_, err = p.ConfigSetDefaultFunc("new.required", "beta", "needed for test", nil)
	if err == nil || !strings.Contains(err.Error(), "user declined default value") {
		t.Fatalf("expected decline error, got %v", err)
	}
}

func TestBuildUpgradePrompter_ConfigSetDefaultBypassesPromptWhenYes(t *testing.T) {
	cmd := newUpgradeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))

	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{yes: true})
	value, err := p.ConfigSetDefaultFunc("new.required", true, "needed for test", &config.FieldDef{
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

func TestPrintDiffPreviewSummary_RendersStats(t *testing.T) {
	var buf bytes.Buffer
	previews := []install.DiffPreview{
		{Path: "file-a.txt", LinesAdded: 5, LinesRemoved: 2},
		{Path: "longer-name.md", LinesAdded: 0, LinesRemoved: 7},
	}
	if err := printDiffPreviewSummary(&buf, "Header", previews); err != nil {
		t.Fatalf("printDiffPreviewSummary: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Header") {
		t.Fatalf("expected header in output:\n%s", output)
	}
	if !strings.Contains(output, "file-a.txt") || !strings.Contains(output, "+5") || !strings.Contains(output, "-2") {
		t.Fatalf("expected file-a stats in output:\n%s", output)
	}
	if !strings.Contains(output, "longer-name.md") || !strings.Contains(output, "+0") || !strings.Contains(output, "-7") {
		t.Fatalf("expected longer-name stats in output:\n%s", output)
	}
	// "Diff for ..." block must NOT appear — summary only.
	if strings.Contains(output, "Diff for") {
		t.Fatalf("did not expect diff body in summary output:\n%s", output)
	}
}

func TestPrintDiffPreviewSummary_Colorizes(t *testing.T) {
	enableTestColorOutput(t)

	var buf bytes.Buffer
	previews := []install.DiffPreview{
		{Path: "a.txt", LinesAdded: 3, LinesRemoved: 1},
	}
	if err := printDiffPreviewSummary(&buf, "Header", previews); err != nil {
		t.Fatalf("printDiffPreviewSummary: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("expected ANSI color sequences in stats output:\n%s", output)
	}
}

func TestPrintDiffPreviewSummary_ZeroCountsPlainEvenWhenColorized(t *testing.T) {
	enableTestColorOutput(t)

	var buf bytes.Buffer
	previews := []install.DiffPreview{
		{Path: "a.txt", LinesAdded: 0, LinesRemoved: 0},
	}
	if err := printDiffPreviewSummary(&buf, "Header", previews); err != nil {
		t.Fatalf("printDiffPreviewSummary: %v", err)
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("expected no ANSI sequences for zero counts:\n%s", buf.String())
	}
}

func TestPromptUnifiedOverwriteSections_DeclinesViewDiffByDefaultThenApplies(t *testing.T) {
	var out bytes.Buffer
	// Enter declines view-diff, accepts managed updates, and declines memory updates.
	in := strings.NewReader("\n\n\n")
	previews := []install.DiffPreview{
		{Path: "a.txt", UnifiedDiff: "--- a\n+++ b\n-old\n+new\n", LinesAdded: 1, LinesRemoved: 1},
	}
	managed, memory, err := promptUnifiedOverwriteSections(in, &out, previews, []install.DiffPreview{
		{Path: "docs/agent-layer/ISSUES.md", UnifiedDiff: "--- a\n+++ b\n-old\n+new\n"},
	})
	if err != nil {
		t.Fatalf("promptUnifiedOverwriteSections: %v", err)
	}
	if !managed || memory {
		t.Fatalf("expected managed=true, memory=false, got %v, %v", managed, memory)
	}
	output := out.String()
	if !strings.Contains(output, "+1") || !strings.Contains(output, "-1") {
		t.Fatalf("expected stats in summary, got:\n%s", output)
	}
	if strings.Contains(output, "Diff for ") || strings.Contains(output, "-old") || strings.Contains(output, "+new") {
		t.Fatalf("did not expect diff body when view declined, got:\n%s", output)
	}
	if !strings.Contains(output, messages.UpgradeViewDiffPrompt) {
		t.Fatalf("expected view-diff prompt in output:\n%s", output)
	}
	view := strings.Index(output, messages.UpgradeViewDiffPrompt)
	managedPrompt := strings.Index(output, messages.UpgradeOverwriteAllPrompt)
	memoryPrompt := strings.Index(output, messages.UpgradeOverwriteMemoryAllPrompt)
	if view < 0 || managedPrompt <= view || memoryPrompt <= managedPrompt {
		t.Fatalf("expected view, managed, memory prompt order, got:\n%s", output)
	}
}

func TestPromptUnifiedOverwriteSections_AsksViewDiffOnce(t *testing.T) {
	var out bytes.Buffer
	// View=yes, then managed=yes, then memory=no.
	in := strings.NewReader("y\ny\nn\n")
	managed := []install.DiffPreview{
		{Path: "managed.txt", UnifiedDiff: "--- a\n+++ b\n-x\n+y\n", LinesAdded: 1, LinesRemoved: 1},
	}
	memory := []install.DiffPreview{
		{Path: "docs/agent-layer/ROADMAP.md", UnifiedDiff: "--- a\n+++ b\n-foo\n+bar\n", LinesAdded: 1, LinesRemoved: 1},
	}
	applyManaged, applyMemory, err := promptUnifiedOverwriteSections(in, &out, managed, memory)
	if err != nil {
		t.Fatalf("promptUnifiedOverwriteSections: %v", err)
	}
	if !applyManaged {
		t.Fatal("expected applyManaged=true")
	}
	if applyMemory {
		t.Fatal("expected applyMemory=false")
	}
	output := out.String()
	if !strings.Contains(output, "managed.txt") || !strings.Contains(output, "docs/agent-layer/ROADMAP.md") {
		t.Fatalf("expected both summaries in output:\n%s", output)
	}
	if !strings.Contains(output, "Diff for managed.txt:") || !strings.Contains(output, "Diff for docs/agent-layer/ROADMAP.md:") {
		t.Fatalf("expected diff bodies for both sections, got:\n%s", output)
	}
	// Exactly one view-diff prompt should appear, not two.
	if got := strings.Count(output, messages.UpgradeViewDiffPrompt); got != 1 {
		t.Fatalf("expected exactly 1 view-diff prompt, got %d in:\n%s", got, output)
	}
}

func TestPromptUnifiedOverwriteSections_NoDiffBodySkipsViewPrompt(t *testing.T) {
	var out bytes.Buffer
	// Only managed apply + memory apply are asked.
	in := strings.NewReader("y\nn\n")
	managed := []install.DiffPreview{{Path: "managed.txt"}}
	memory := []install.DiffPreview{{Path: "docs/agent-layer/ROADMAP.md"}}
	applyManaged, applyMemory, err := promptUnifiedOverwriteSections(in, &out, managed, memory)
	if err != nil {
		t.Fatalf("promptUnifiedOverwriteSections: %v", err)
	}
	if !applyManaged || applyMemory {
		t.Fatalf("unexpected results: applyManaged=%v applyMemory=%v", applyManaged, applyMemory)
	}
	if strings.Contains(out.String(), messages.UpgradeViewDiffPrompt) {
		t.Fatalf("did not expect view-diff prompt when no diff bodies, got:\n%s", out.String())
	}
}

func TestHasNonEmptyDiff(t *testing.T) {
	if hasNonEmptyDiff(nil) {
		t.Fatal("expected hasNonEmptyDiff(nil)=false")
	}
	if hasNonEmptyDiff([]install.DiffPreview{{UnifiedDiff: "  \n\t"}}) {
		t.Fatal("expected hasNonEmptyDiff(whitespace-only)=false")
	}
	if !hasNonEmptyDiff([]install.DiffPreview{{UnifiedDiff: "real"}}) {
		t.Fatal("expected hasNonEmptyDiff(real diff)=true")
	}
}

func TestBuildUpgradePrompter_UnifiedCallbackInteractive(t *testing.T) {
	cmd := newUpgradeCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader("n\nn\ny\n"))
	p := buildUpgradePrompter(cmd, upgradeApplyPolicy{interactive: true})
	managed, memory, err := p.OverwriteAllUnifiedPreviewFunc(
		[]install.DiffPreview{{Path: ".agent-layer/commands.allow", UnifiedDiff: "-old\n+new\n"}},
		[]install.DiffPreview{{Path: "docs/agent-layer/ISSUES.md", UnifiedDiff: "-old\n+new\n"}},
	)
	if err != nil || managed || !memory {
		t.Fatalf("unified callback = (%v, %v, %v), want (false, true, nil)", managed, memory, err)
	}
	for _, prompt := range []string{messages.UpgradeViewDiffPrompt, messages.UpgradeOverwriteAllPrompt, messages.UpgradeOverwriteMemoryAllPrompt} {
		if got := strings.Count(out.String(), prompt); got != 1 {
			t.Fatalf("expected prompt %q once, got %d in %q", prompt, got, out.String())
		}
	}
	if !strings.Contains(out.String(), messages.UpgradeOverwriteManagedHeader) || !strings.Contains(out.String(), messages.UpgradeOverwriteMemoryHeader) {
		t.Fatalf("expected unified summaries, got %q", out.String())
	}
}

func TestBuildUpgradePrompter_UnifiedCallbackExplicitCategory(t *testing.T) {
	for _, managed := range []bool{false, true} {
		for _, memory := range []bool{false, true} {
			cmd := newUpgradeCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			input := strings.NewReader("unused\n")
			cmd.SetIn(input)
			p := buildUpgradePrompter(cmd, upgradeApplyPolicy{explicitCategory: true, applyManaged: managed, applyMemory: memory})
			gotManaged, gotMemory, err := p.OverwriteAllUnifiedPreviewFunc(
				[]install.DiffPreview{{Path: "managed"}}, []install.DiffPreview{{Path: "memory"}},
			)
			if err != nil || gotManaged != managed || gotMemory != memory {
				t.Fatalf("unified flags = (%v, %v, %v), want (%v, %v, nil)", gotManaged, gotMemory, err, managed, memory)
			}
			if input.Len() != len("unused\n") || out.Len() != 0 {
				t.Fatal("explicit category callback must return flags without prompting or reading stdin")
			}
		}
	}
}
