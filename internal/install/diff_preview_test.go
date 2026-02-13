package install

import (
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
