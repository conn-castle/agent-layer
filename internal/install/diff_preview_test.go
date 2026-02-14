package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestNormalizeDiffMaxLines_DefaultAndPositive(t *testing.T) {
	if got := normalizeDiffMaxLines(0); got != DefaultDiffMaxLines {
		t.Fatalf("normalizeDiffMaxLines(0) = %d, want %d", got, DefaultDiffMaxLines)
	}
	if got := normalizeDiffMaxLines(-1); got != DefaultDiffMaxLines {
		t.Fatalf("normalizeDiffMaxLines(-1) = %d, want %d", got, DefaultDiffMaxLines)
	}
	if got := normalizeDiffMaxLines(7); got != 7 {
		t.Fatalf("normalizeDiffMaxLines(7) = %d, want 7", got)
	}
}

func TestRenderTruncatedUnifiedDiff(t *testing.T) {
	from := "a\nb\nc\n"
	to := "a\nx\ny\nz\n"
	diff, truncated := renderTruncatedUnifiedDiff("from.txt", "to.txt", from, to, 2)
	if !truncated {
		t.Fatal("expected truncated diff")
	}
	if !strings.Contains(diff, "truncated to 2 lines") {
		t.Fatalf("expected truncation note in diff:\n%s", diff)
	}
	if !strings.Contains(diff, diffLineCapFlagName) {
		t.Fatalf("expected diff to mention %s:\n%s", diffLineCapFlagName, diff)
	}
}

func TestSplitSectionAwareContent(t *testing.T) {
	content := []byte("# Header\n\n" + ownershipMarkerEntriesStart + "\n- user entry\n")
	managed, user, err := splitSectionAwareContent("docs/agent-layer/ISSUES.md", ownershipMarkerEntriesStart, content)
	if err != nil {
		t.Fatalf("splitSectionAwareContent: %v", err)
	}
	if !strings.Contains(managed, ownershipMarkerEntriesStart) {
		t.Fatalf("managed section missing marker:\n%s", managed)
	}
	if !strings.Contains(user, "- user entry") {
		t.Fatalf("user section missing expected entry:\n%s", user)
	}
}

func TestSplitSectionAwareContent_MissingMarker(t *testing.T) {
	_, _, err := splitSectionAwareContent("docs/agent-layer/ISSUES.md", ownershipMarkerEntriesStart, []byte("# header\n"))
	if err == nil {
		t.Fatal("expected missing marker error")
	}
	if !strings.Contains(err.Error(), "missing in") {
		t.Fatalf("expected missing marker error, got: %v", err)
	}
}

func TestSplitSectionAwareContent_DuplicateMarker(t *testing.T) {
	content := []byte("# header\n" + ownershipMarkerEntriesStart + "\nentry\n" + ownershipMarkerEntriesStart + "\n")
	_, _, err := splitSectionAwareContent("docs/agent-layer/ISSUES.md", ownershipMarkerEntriesStart, content)
	if err == nil {
		t.Fatal("expected duplicate marker error")
	}
	if !strings.Contains(err.Error(), "appears multiple times") {
		t.Fatalf("expected duplicate marker error, got: %v", err)
	}
}

func TestSplitSectionAwareContent_PreservesRawUserSection(t *testing.T) {
	content := []byte("# Header\n\n" + ownershipMarkerEntriesStart + "\n- item\n\n\n")
	_, user, err := splitSectionAwareContent("docs/agent-layer/ISSUES.md", ownershipMarkerEntriesStart, content)
	if err != nil {
		t.Fatalf("splitSectionAwareContent: %v", err)
	}
	if user != "- item\n\n\n" {
		t.Fatalf("expected raw trailing newlines to be preserved, got %q", user)
	}
}

func TestDiffPreviewErrorsUseMessageConstants(t *testing.T) {
	inst := &installer{root: "/tmp", sys: RealSystem{}}
	_, err := inst.buildSingleDiffPreview(LabeledPath{}, map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != messages.InstallDiffPreviewPathRequired {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSplitDiffLinesAndTrailingNewlineHelpers(t *testing.T) {
	if got := splitDiffLines(""); len(got) != 0 {
		t.Fatalf("splitDiffLines(\"\") = %v, want empty", got)
	}
	if got := splitDiffLines("a\nb\n"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("splitDiffLines unexpected output: %v", got)
	}
	if got := ensureTrailingNewline(""); got != "" {
		t.Fatalf("ensureTrailingNewline empty = %q, want empty", got)
	}
	if got := ensureTrailingNewline("x"); got != "x\n" {
		t.Fatalf("ensureTrailingNewline no newline = %q, want %q", got, "x\n")
	}
	if got := ensureTrailingNewline("x\n"); got != "x\n" {
		t.Fatalf("ensureTrailingNewline existing newline = %q, want %q", got, "x\n")
	}
}

func TestBuildManagedAndMemoryDiffPreviews(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	inst := &installer{
		root:         root,
		sys:          RealSystem{},
		diffMaxLines: 20,
	}

	managedEntries := []LabeledPath{{
		Path:      ".agent-layer/commands.allow",
		Ownership: OwnershipUpstreamTemplateDelta,
	}}
	memoryEntries := []LabeledPath{{
		Path:      "docs/agent-layer/ROADMAP.md",
		Ownership: OwnershipUpstreamTemplateDelta,
	}}

	managedPreviews, managedIndex, err := inst.buildManagedDiffPreviews(managedEntries)
	if err != nil {
		t.Fatalf("buildManagedDiffPreviews: %v", err)
	}
	if len(managedPreviews) != 1 || len(managedIndex) != 1 {
		t.Fatalf("unexpected managed preview sizes: previews=%d index=%d", len(managedPreviews), len(managedIndex))
	}

	memoryPreviews, memoryIndex, err := inst.buildMemoryDiffPreviews(memoryEntries)
	if err != nil {
		t.Fatalf("buildMemoryDiffPreviews: %v", err)
	}
	if len(memoryPreviews) != 1 || len(memoryIndex) != 1 {
		t.Fatalf("unexpected memory preview sizes: previews=%d index=%d", len(memoryPreviews), len(memoryIndex))
	}
}

func TestBuildManagedAndMemoryDiffPreviews_Errors(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	inst := &installer{
		root:         root,
		sys:          RealSystem{},
		diffMaxLines: 20,
	}

	_, _, err := inst.buildManagedDiffPreviews([]LabeledPath{{
		Path:      ".agent-layer/not-managed",
		Ownership: OwnershipUpstreamTemplateDelta,
	}})
	if err == nil || !strings.Contains(err.Error(), "missing template path mapping") {
		t.Fatalf("expected managed preview mapping error, got %v", err)
	}

	_, _, err = inst.buildMemoryDiffPreviews([]LabeledPath{{
		Path:      "docs/agent-layer/not-managed.md",
		Ownership: OwnershipUpstreamTemplateDelta,
	}})
	if err == nil || !strings.Contains(err.Error(), "missing template path mapping") {
		t.Fatalf("expected memory preview mapping error, got %v", err)
	}
}

func TestBuildDiffPreviews_PropagatesSinglePreviewError(t *testing.T) {
	inst := &installer{
		root:         t.TempDir(),
		sys:          RealSystem{},
		diffMaxLines: 20,
	}
	_, err := inst.buildDiffPreviews([]LabeledPath{{Path: ""}}, map[string]string{})
	if err == nil || err.Error() != messages.InstallDiffPreviewPathRequired {
		t.Fatalf("expected diff preview path required error, got %v", err)
	}
}

func TestBuildSingleDiffPreview_EdgeCases(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	inst := &installer{
		root:         root,
		sys:          RealSystem{},
		diffMaxLines: 20,
		pinVersion:   "1.1.0",
	}

	pinPreview, err := inst.buildSingleDiffPreview(LabeledPath{
		Path:      pinVersionRelPath,
		Ownership: OwnershipUpstreamTemplateDelta,
	}, map[string]string{})
	if err != nil {
		t.Fatalf("buildSingleDiffPreview pin path: %v", err)
	}
	if pinPreview.Path != pinVersionRelPath {
		t.Fatalf("pin preview path = %q, want %q", pinPreview.Path, pinVersionRelPath)
	}

	_, err = inst.buildSingleDiffPreview(LabeledPath{
		Path:      ".agent-layer/missing.file",
		Ownership: OwnershipUpstreamTemplateDelta,
	}, map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "missing template path mapping") {
		t.Fatalf("expected missing template mapping error, got %v", err)
	}
}

func TestBuildSingleDiffPreview_SectionAwareMarkerError(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.0.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	roadmapPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	if err := os.WriteFile(roadmapPath, []byte("# no marker here\n"), 0o644); err != nil {
		t.Fatalf("write roadmap without marker: %v", err)
	}

	inst := &installer{
		root:         root,
		sys:          RealSystem{},
		diffMaxLines: 20,
	}
	templatePathByRel, err := inst.memoryTemplatePathByRel()
	if err != nil {
		t.Fatalf("memoryTemplatePathByRel: %v", err)
	}

	_, err = inst.buildSingleDiffPreview(LabeledPath{
		Path:      "docs/agent-layer/ROADMAP.md",
		Ownership: OwnershipUpstreamTemplateDelta,
	}, templatePathByRel)
	if err == nil || !strings.Contains(err.Error(), "missing in") {
		t.Fatalf("expected section-aware marker missing error, got %v", err)
	}
}
