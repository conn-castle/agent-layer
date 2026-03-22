package install

import (
	"testing"
)

func TestFilterMigrationCoveredDiffs_EmptyCoverage(t *testing.T) {
	inst := &installer{}
	entries := []LabeledPath{{Path: "a.md", Ownership: OwnershipUpstreamTemplateDelta}}
	got := inst.filterMigrationCoveredDiffs(entries)
	if len(got) != 1 {
		t.Fatalf("expected 1 entry with empty coverage, got %d", len(got))
	}
}

func TestFilterMigrationCoveredDiffs_EmptyEntries(t *testing.T) {
	inst := &installer{
		migrationManifestCoverage: map[string]struct{}{"a.md": {}},
	}
	got := inst.filterMigrationCoveredDiffs(nil)
	if len(got) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(got))
	}
}

func TestFilterMigrationCoveredDiffs_FiltersCovered(t *testing.T) {
	inst := &installer{
		migrationManifestCoverage: map[string]struct{}{
			".agent-layer/skills": {},
		},
	}
	entries := []LabeledPath{
		{Path: ".agent-layer/skills/foo.md", Ownership: OwnershipUpstreamTemplateDelta},
		{Path: ".agent-layer/config.toml", Ownership: OwnershipUpstreamTemplateDelta},
	}
	got := inst.filterMigrationCoveredDiffs(entries)
	if len(got) != 1 {
		t.Fatalf("expected 1 uncovered entry, got %d", len(got))
	}
	if got[0].Path != ".agent-layer/config.toml" {
		t.Fatalf("expected config.toml to survive filter, got %s", got[0].Path)
	}
}

func TestIsCoveredByMigration_ExactMatch(t *testing.T) {
	covered := map[string]struct{}{"a/b.md": {}}
	if !isCoveredByMigration("a/b.md", covered) {
		t.Fatal("expected exact match to be covered")
	}
}

func TestIsCoveredByMigration_ParentDirectory(t *testing.T) {
	covered := map[string]struct{}{"a": {}}
	if !isCoveredByMigration("a/b/c.md", covered) {
		t.Fatal("expected parent dir match to be covered")
	}
}

func TestIsCoveredByMigration_NestedParent(t *testing.T) {
	covered := map[string]struct{}{"a/b": {}}
	if !isCoveredByMigration("a/b/c/d.md", covered) {
		t.Fatal("expected nested parent match to be covered")
	}
}

func TestIsCoveredByMigration_NoMatch(t *testing.T) {
	covered := map[string]struct{}{"x/y": {}}
	if isCoveredByMigration("a/b.md", covered) {
		t.Fatal("expected no match")
	}
}

func TestFilterCoveredUpgradeChanges_EmptyInputs(t *testing.T) {
	got := filterCoveredUpgradeChanges(nil, map[string]struct{}{"a": {}})
	if got != nil {
		t.Fatalf("expected nil for empty changes, got %v", got)
	}

	changes := []upgradeChangeWithTemplate{{path: "a.md"}}
	got2 := filterCoveredUpgradeChanges(changes, nil)
	if len(got2) != 1 {
		t.Fatalf("expected passthrough with empty coverage, got %d", len(got2))
	}
}

func TestFilterCoveredUpgradeChanges_FiltersCovered(t *testing.T) {
	covered := map[string]struct{}{".agent-layer/skills": {}}
	changes := []upgradeChangeWithTemplate{
		{path: ".agent-layer/skills/foo.md"},
		{path: ".agent-layer/config.toml"},
	}
	got := filterCoveredUpgradeChanges(changes, covered)
	if len(got) != 1 || got[0].path != ".agent-layer/config.toml" {
		t.Fatalf("expected only config.toml to survive, got %v", got)
	}
}
