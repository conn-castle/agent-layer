package install

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestBuildUpgradePlan_DetectsCategoriesOwnershipAndRename(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	// Simulate an unchanged local docs file relative to the prior managed baseline,
	// while the embedded template has since changed.
	oldRoadmap := []byte("old roadmap baseline\n")
	roadmapPath := filepath.Join(root, "docs", "agent-layer", "ROADMAP.md")
	baselineRoadmapPath := filepath.Join(root, ".agent-layer", "templates", "docs", "ROADMAP.md")
	if err := os.WriteFile(roadmapPath, oldRoadmap, 0o644); err != nil {
		t.Fatalf("write roadmap: %v", err)
	}
	if err := os.WriteFile(baselineRoadmapPath, oldRoadmap, 0o644); err != nil {
		t.Fatalf("write baseline roadmap: %v", err)
	}

	// Simulate a local customization in memory docs.
	issuesPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	if err := os.WriteFile(issuesPath, []byte("custom issue text\n"), 0o644); err != nil {
		t.Fatalf("write issues: %v", err)
	}

	// Simulate a rename candidate by moving one managed template file to an orphan path.
	findIssuesPath := filepath.Join(root, ".agent-layer", "slash-commands", "find-issues.md")
	if err := os.Remove(findIssuesPath); err != nil {
		t.Fatalf("remove find-issues slash command: %v", err)
	}
	findIssuesTemplate, err := templates.Read("slash-commands/find-issues.md")
	if err != nil {
		t.Fatalf("read template slash command: %v", err)
	}
	orphanRenamePath := filepath.Join(root, ".agent-layer", "slash-commands", "find-issues-legacy.md")
	if err := os.WriteFile(orphanRenamePath, findIssuesTemplate, 0o644); err != nil {
		t.Fatalf("write orphan rename path: %v", err)
	}

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{
		TargetPinVersion: "2.0.0",
		System:           RealSystem{},
	})
	if err != nil {
		t.Fatalf("build upgrade plan: %v", err)
	}

	if !plan.DryRun {
		t.Fatalf("expected dry-run plan")
	}
	if len(plan.ConfigKeyMigrations) != 0 {
		t.Fatalf("expected empty config migrations, got %d", len(plan.ConfigKeyMigrations))
	}
	if plan.PinVersionChange.Action != UpgradePinActionUpdate {
		t.Fatalf("expected pin update action, got %s", plan.PinVersionChange.Action)
	}
	if plan.PinVersionChange.Current != "1.2.3" || plan.PinVersionChange.Target != "2.0.0" {
		t.Fatalf("unexpected pin transition: %#v", plan.PinVersionChange)
	}

	roadmapUpdate := findUpgradeChange(plan.TemplateUpdates, "docs/agent-layer/ROADMAP.md")
	if roadmapUpdate == nil {
		t.Fatalf("expected roadmap update in template updates")
	}
	if roadmapUpdate.Ownership != OwnershipUpstreamTemplateDelta {
		t.Fatalf("expected upstream ownership for roadmap, got %s", roadmapUpdate.Ownership)
	}

	issuesUpdate := findUpgradeChange(plan.TemplateUpdates, "docs/agent-layer/ISSUES.md")
	if issuesUpdate == nil {
		t.Fatalf("expected issues update in template updates")
	}
	if issuesUpdate.Ownership != OwnershipLocalCustomization {
		t.Fatalf("expected local ownership for issues, got %s", issuesUpdate.Ownership)
	}

	if len(plan.TemplateRenames) == 0 {
		t.Fatalf("expected at least one rename")
	}
	rename := plan.TemplateRenames[0]
	if rename.From != ".agent-layer/slash-commands/find-issues-legacy.md" {
		t.Fatalf("unexpected rename from path: %s", rename.From)
	}
	if rename.To != ".agent-layer/slash-commands/find-issues.md" {
		t.Fatalf("unexpected rename to path: %s", rename.To)
	}
	if rename.Confidence != UpgradeRenameConfidenceHigh {
		t.Fatalf("unexpected rename confidence: %s", rename.Confidence)
	}
	if rename.Detection != UpgradeRenameDetectionUniqueExactHash {
		t.Fatalf("unexpected rename detection: %s", rename.Detection)
	}
}

func findUpgradeChange(changes []UpgradeChange, path string) *UpgradeChange {
	for _, change := range changes {
		if change.Path == path {
			c := change
			return &c
		}
	}
	return nil
}

type faultSystem struct {
	base       System
	statErrs   map[string]error
	readErrs   map[string]error
	walkErrs   map[string]error
	mkdirErrs  map[string]error
	removeErrs map[string]error
	writeErrs  map[string]error
}

func newFaultSystem(base System) *faultSystem {
	return &faultSystem{
		base:       base,
		statErrs:   map[string]error{},
		readErrs:   map[string]error{},
		walkErrs:   map[string]error{},
		mkdirErrs:  map[string]error{},
		removeErrs: map[string]error{},
		writeErrs:  map[string]error{},
	}
}

func normalizePath(path string) string {
	return filepath.Clean(path)
}

func (f *faultSystem) Stat(name string) (os.FileInfo, error) {
	if err, ok := f.statErrs[normalizePath(name)]; ok {
		return nil, err
	}
	return f.base.Stat(name)
}

func (f *faultSystem) ReadFile(name string) ([]byte, error) {
	if err, ok := f.readErrs[normalizePath(name)]; ok {
		return nil, err
	}
	return f.base.ReadFile(name)
}

func (f *faultSystem) MkdirAll(path string, perm os.FileMode) error {
	if err, ok := f.mkdirErrs[normalizePath(path)]; ok {
		return err
	}
	return f.base.MkdirAll(path, perm)
}

func (f *faultSystem) RemoveAll(path string) error {
	if err, ok := f.removeErrs[normalizePath(path)]; ok {
		return err
	}
	return f.base.RemoveAll(path)
}

func (f *faultSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	if err, ok := f.walkErrs[normalizePath(root)]; ok {
		return err
	}
	return f.base.WalkDir(root, fn)
}

func (f *faultSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	if err, ok := f.writeErrs[normalizePath(filename)]; ok {
		return err
	}
	return f.base.WriteFileAtomic(filename, data, perm)
}

func TestBuildUpgradePlan_ValidationErrors(t *testing.T) {
	_, err := BuildUpgradePlan("", UpgradePlanOptions{System: RealSystem{}})
	if err == nil || !strings.Contains(err.Error(), "root path is required") {
		t.Fatalf("expected root validation error, got %v", err)
	}

	root := t.TempDir()
	_, err = BuildUpgradePlan(root, UpgradePlanOptions{})
	if err == nil || !strings.Contains(err.Error(), "install system is required") {
		t.Fatalf("expected system validation error, got %v", err)
	}

	_, err = BuildUpgradePlan(root, UpgradePlanOptions{
		System:           RealSystem{},
		TargetPinVersion: "bad-version",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid pin version") {
		t.Fatalf("expected invalid pin version error, got %v", err)
	}
}

func TestBuildUpgradePlan_StatErrorOnTemplateEntry(t *testing.T) {
	root := t.TempDir()
	sys := newFaultSystem(RealSystem{})
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	sys.statErrs[normalizePath(configPath)] = errors.New("stat boom")

	_, err := BuildUpgradePlan(root, UpgradePlanOptions{
		TargetPinVersion: "1.2.3",
		System:           sys,
	})
	if err == nil || !strings.Contains(err.Error(), "failed to stat") {
		t.Fatalf("expected stat error, got %v", err)
	}
}

func TestBuildUpgradePlan_WalkTemplateOrphansErrors(t *testing.T) {
	root := t.TempDir()
	sys := newFaultSystem(RealSystem{})

	instructionsRoot := filepath.Join(root, ".agent-layer", "instructions")
	sys.statErrs[normalizePath(instructionsRoot)] = errors.New("permission denied")
	_, err := BuildUpgradePlan(root, UpgradePlanOptions{System: sys, TargetPinVersion: "1.2.3"})
	if err == nil || !strings.Contains(err.Error(), "failed to stat") {
		t.Fatalf("expected stat error from orphan root, got %v", err)
	}

	delete(sys.statErrs, normalizePath(instructionsRoot))
	if err := os.MkdirAll(instructionsRoot, 0o755); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	sys.walkErrs[normalizePath(instructionsRoot)] = errors.New("walk failed")
	_, err = BuildUpgradePlan(root, UpgradePlanOptions{System: sys, TargetPinVersion: "1.2.3"})
	if err == nil || !strings.Contains(err.Error(), "walk failed") {
		t.Fatalf("expected walk error, got %v", err)
	}
}

func TestPinVersionDiff_EdgeCases(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}

	inst := &installer{root: root, pinVersion: "1.2.3", sys: RealSystem{}}
	diff, err := inst.pinVersionDiff()
	if err != nil {
		t.Fatalf("pinVersionDiff missing file: %v", err)
	}
	if diff.Action != UpgradePinActionSet {
		t.Fatalf("expected set action for missing file, got %s", diff.Action)
	}

	if err := os.WriteFile(path, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write empty pin: %v", err)
	}
	diff, err = inst.pinVersionDiff()
	if err != nil {
		t.Fatalf("pinVersionDiff empty file: %v", err)
	}
	if diff.Action != UpgradePinActionSet {
		t.Fatalf("expected set action for empty pin, got %s", diff.Action)
	}

	if err := os.WriteFile(path, []byte("not-semver\n"), 0o644); err != nil {
		t.Fatalf("write corrupt pin: %v", err)
	}
	diff, err = inst.pinVersionDiff()
	if err != nil {
		t.Fatalf("pinVersionDiff corrupt file: %v", err)
	}
	if diff.Action != UpgradePinActionUpdate {
		t.Fatalf("expected update action for corrupt pin, got %s", diff.Action)
	}
}

func TestDetectUpgradeRenames_ErrorAndAmbiguityPaths(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	renames, remainingAdditions, remainingOrphans, err := detectUpgradeRenames(inst,
		[]upgradeChangeWithTemplate{{path: ".agent-layer/config.toml", templatePath: "missing-template.md"}},
		[]upgradeChangeWithTemplate{{path: ".agent-layer/orphan.md", ownership: OwnershipLocalCustomization}},
	)
	if err == nil {
		t.Fatal("expected template read error")
	}
	if len(renames) != 0 || len(remainingAdditions) != 0 || len(remainingOrphans) != 0 {
		t.Fatal("expected empty results on early template read error")
	}

	validTemplateBytes, err := templates.Read("config.toml")
	if err != nil {
		t.Fatalf("read config template: %v", err)
	}
	orphanPath := filepath.Join(root, ".agent-layer", "orphan.md")
	if err := os.MkdirAll(filepath.Dir(orphanPath), 0o755); err != nil {
		t.Fatalf("mkdir orphan dir: %v", err)
	}
	if err := os.WriteFile(orphanPath, validTemplateBytes, 0o644); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	badReadSys := newFaultSystem(RealSystem{})
	badReadSys.readErrs[normalizePath(orphanPath)] = errors.New("read boom")
	inst.sys = badReadSys
	renames, remainingAdditions, remainingOrphans, err = detectUpgradeRenames(inst,
		[]upgradeChangeWithTemplate{{path: ".agent-layer/config.toml", templatePath: "config.toml"}},
		[]upgradeChangeWithTemplate{{path: ".agent-layer/orphan.md", ownership: OwnershipLocalCustomization}},
	)
	if err == nil || !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected orphan read error, got %v", err)
	}
	if len(renames) != 0 || len(remainingAdditions) != 0 || len(remainingOrphans) != 0 {
		t.Fatal("expected empty results on orphan read error")
	}

	inst.sys = RealSystem{}
	secondOrphanPath := filepath.Join(root, ".agent-layer", "orphan-2.md")
	if err := os.WriteFile(secondOrphanPath, validTemplateBytes, 0o644); err != nil {
		t.Fatalf("write second orphan: %v", err)
	}
	renames, additions, orphans, err := detectUpgradeRenames(inst,
		[]upgradeChangeWithTemplate{
			{path: ".agent-layer/config-a.toml", templatePath: "config.toml"},
			{path: ".agent-layer/config-b.toml", templatePath: "config.toml"},
		},
		[]upgradeChangeWithTemplate{
			{path: ".agent-layer/orphan.md", ownership: OwnershipLocalCustomization},
			{path: ".agent-layer/orphan-2.md", ownership: OwnershipLocalCustomization},
		},
	)
	if err != nil {
		t.Fatalf("detectUpgradeRenames ambiguity: %v", err)
	}
	if len(renames) != 0 {
		t.Fatalf("expected no rename for ambiguous hash matches, got %d", len(renames))
	}
	if len(additions) != 2 || len(orphans) != 2 {
		t.Fatalf("expected additions/orphans unchanged on ambiguity, got %d/%d", len(additions), len(orphans))
	}
}
