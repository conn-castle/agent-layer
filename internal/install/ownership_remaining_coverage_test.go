package install

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestOwnershipLabelDisplay_NonEmptyTrimmed(t *testing.T) {
	if got := OwnershipLabel("  custom ownership  ").Display(); got != "custom ownership" {
		t.Fatalf("Display() = %q, want %q", got, "custom ownership")
	}
}

func TestResolveBaselineComparable_LegacyReadErrorBranch(t *testing.T) {
	root := t.TempDir()
	relPath := "docs/agent-layer/ROADMAP.md"
	localComp, err := buildOwnershipComparable(relPath, []byte("# Roadmap\n\n"+ownershipMarkerPhasesStart+"\n"))
	if err != nil {
		t.Fatalf("buildOwnershipComparable: %v", err)
	}

	legacyPath := filepath.Join(root, ".agent-layer", "templates", "docs", "ROADMAP.md")
	fault := newFaultSystem(RealSystem{})
	fault.readErrs[normalizePath(legacyPath)] = errors.New("legacy read boom")

	inst := &installer{root: root, sys: fault}
	if _, _, err := inst.ownership().resolveBaselineComparable(relPath, localComp); err == nil || err.Error() != "legacy read boom" {
		t.Fatalf("expected legacy read error, got %v", err)
	}
}
