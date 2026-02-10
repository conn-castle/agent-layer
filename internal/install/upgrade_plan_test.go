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
	if err := os.Remove(filepath.Join(root, ".agent-layer", "state", "managed-baseline.json")); err != nil {
		t.Fatalf("remove canonical baseline: %v", err)
	}

	// Simulate an unchanged local docs file relative to the prior managed baseline,
	// while the embedded template has since changed.
	oldRoadmap := []byte("# ROADMAP\n\nLegacy header\n\n<!-- PHASES START -->\n")
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
	issuesTemplate, err := templates.Read("docs/agent-layer/ISSUES.md")
	if err != nil {
		t.Fatalf("read issues template: %v", err)
	}
	customIssues := strings.Replace(string(issuesTemplate), "<!-- ENTRIES START -->\n", "<!-- ENTRIES START -->\n\n- issue from repo\n", 1)
	if err := os.WriteFile(issuesPath, []byte(customIssues), 0o644); err != nil {
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
	if plan.SchemaVersion != UpgradePlanSchemaVersion {
		t.Fatalf("expected schema version %d, got %d", UpgradePlanSchemaVersion, plan.SchemaVersion)
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

	roadmapUpdate := findUpgradeChange(plan.SectionAwareUpdates, "docs/agent-layer/ROADMAP.md")
	if roadmapUpdate == nil {
		t.Fatalf("expected roadmap update in section-aware updates")
	}
	if roadmapUpdate.Ownership != OwnershipUpstreamTemplateDelta {
		t.Fatalf("expected upstream ownership for roadmap, got %s", roadmapUpdate.Ownership)
	}
	if roadmapUpdate.OwnershipState != OwnershipStateUpstreamTemplateDelta {
		t.Fatalf("expected upstream ownership_state for roadmap, got %s", roadmapUpdate.OwnershipState)
	}
	if roadmapUpdate.OwnershipConfidence == nil || *roadmapUpdate.OwnershipConfidence != OwnershipConfidenceLow {
		t.Fatalf("expected low ownership_confidence for roadmap, got %#v", roadmapUpdate.OwnershipConfidence)
	}
	if roadmapUpdate.OwnershipBaselineSource == nil || *roadmapUpdate.OwnershipBaselineSource != BaselineStateSourceMigratedFromLegacyDocsSnapshot {
		t.Fatalf("expected migrated legacy baseline source for roadmap, got %#v", roadmapUpdate.OwnershipBaselineSource)
	}

	// ISSUES.md has a user entry below the marker but its managed section matches
	// the template. Section-aware matching should exclude it from the plan entirely.
	issuesInUpdates := findUpgradeChange(plan.TemplateUpdates, "docs/agent-layer/ISSUES.md")
	issuesInSection := findUpgradeChange(plan.SectionAwareUpdates, "docs/agent-layer/ISSUES.md")
	if issuesInUpdates != nil || issuesInSection != nil {
		t.Fatal("ISSUES.md should be excluded: managed section matches template, only user entries differ")
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
	if rename.OwnershipState != OwnershipStateUpstreamTemplateDelta {
		t.Fatalf("unexpected rename ownership_state: %s", rename.OwnershipState)
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

func TestBuildUpgradePlan_UnknownNoBaselineForManagedDiff(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".agent-layer", "state", "managed-baseline.json")); err != nil {
		t.Fatalf("remove canonical baseline: %v", err)
	}
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.WriteFile(allowPath, []byte("# custom allowlist\n"), 0o644); err != nil {
		t.Fatalf("write custom allowlist: %v", err)
	}

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	if err != nil {
		t.Fatalf("build upgrade plan: %v", err)
	}
	allowUpdate := findUpgradeChange(plan.TemplateUpdates, ".agent-layer/commands.allow")
	if allowUpdate == nil {
		t.Fatal("expected commands.allow update in plan")
	}
	if allowUpdate.Ownership != OwnershipUnknownNoBaseline {
		t.Fatalf("expected unknown ownership, got %s", allowUpdate.Ownership)
	}
	if allowUpdate.OwnershipState != OwnershipStateUnknownNoBaseline {
		t.Fatalf("expected unknown ownership_state, got %s", allowUpdate.OwnershipState)
	}
	if allowUpdate.OwnershipConfidence != nil {
		t.Fatalf("expected nil ownership confidence for unknown baseline, got %#v", allowUpdate.OwnershipConfidence)
	}
	foundBaselineMissing := false
	for _, reason := range allowUpdate.OwnershipReasonCodes {
		if reason == ownershipReasonBaselineMissing {
			foundBaselineMissing = true
			break
		}
	}
	if !foundBaselineMissing {
		t.Fatalf("expected baseline_missing reason, got %#v", allowUpdate.OwnershipReasonCodes)
	}
}

func TestBuildUpgradePlan_PinManifestCredibleInference(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.7.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".agent-layer", "state", "managed-baseline.json")); err != nil {
		t.Fatalf("remove canonical baseline: %v", err)
	}

	manifest, err := loadTemplateManifestByVersion("0.7.0")
	if err != nil {
		t.Fatalf("load 0.7.0 manifest: %v", err)
	}
	allowEntry, ok := manifestFileMap(manifest.Files)[".agent-layer/commands.allow"]
	if !ok {
		t.Fatal("0.7.0 manifest missing commands.allow")
	}
	payload, err := parseAllowlistPolicyPayload(allowEntry.PolicyPayload)
	if err != nil {
		t.Fatalf("parse allowlist payload: %v", err)
	}

	currentTemplate, err := templates.Read("commands.allow")
	if err != nil {
		t.Fatalf("read current commands.allow template: %v", err)
	}
	currentComp, err := buildOwnershipComparable(".agent-layer/commands.allow", currentTemplate)
	if err != nil {
		t.Fatalf("build current allowlist comparable: %v", err)
	}
	if currentComp.AllowHash == payload.UpstreamSetHash {
		t.Skip("current commands.allow matches 0.7.0 manifest allowlist; cannot exercise upstream delta inference")
	}

	localContent := strings.Join(payload.UpstreamSet, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "commands.allow"), []byte(localContent), 0o644); err != nil {
		t.Fatalf("write local commands.allow from manifest set: %v", err)
	}

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}, TargetPinVersion: "0.7.0"})
	if err != nil {
		t.Fatalf("build upgrade plan: %v", err)
	}
	change := findUpgradeChange(plan.TemplateUpdates, ".agent-layer/commands.allow")
	if change == nil {
		t.Fatal("expected commands.allow update in plan")
	}
	if change.Ownership != OwnershipUpstreamTemplateDelta {
		t.Fatalf("expected upstream ownership, got %s", change.Ownership)
	}
	if change.OwnershipConfidence == nil || *change.OwnershipConfidence != OwnershipConfidenceMedium {
		t.Fatalf("expected medium ownership_confidence, got %#v", change.OwnershipConfidence)
	}
	if change.OwnershipBaselineSource == nil || *change.OwnershipBaselineSource != BaselineStateSourceInferredFromPinManifest {
		t.Fatalf("expected inferred pin baseline source, got %#v", change.OwnershipBaselineSource)
	}
}

func TestBuildUpgradePlan_StatErrorOnTemplateEntry(t *testing.T) {
	root := t.TempDir()
	sys := newFaultSystem(RealSystem{})
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	sys.statErrs[normalizePath(allowPath)] = errors.New("stat boom")

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
		[]upgradeChangeWithTemplate{{
			path: ".agent-layer/orphan.md",
			ownership: ownershipClassification{
				Label: OwnershipLocalCustomization,
				State: OwnershipStateLocalCustomization,
			},
		}},
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
		[]upgradeChangeWithTemplate{{
			path: ".agent-layer/orphan.md",
			ownership: ownershipClassification{
				Label: OwnershipLocalCustomization,
				State: OwnershipStateLocalCustomization,
			},
		}},
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
			{
				path: ".agent-layer/orphan.md",
				ownership: ownershipClassification{
					Label: OwnershipLocalCustomization,
					State: OwnershipStateLocalCustomization,
				},
			},
			{
				path: ".agent-layer/orphan-2.md",
				ownership: ownershipClassification{
					Label: OwnershipLocalCustomization,
					State: OwnershipStateLocalCustomization,
				},
			},
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

func TestDetectUpgradeRenames_NoCandidates(t *testing.T) {
	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	renames, additions, orphans, err := detectUpgradeRenames(inst, nil, nil)
	if err != nil {
		t.Fatalf("detectUpgradeRenames(nil,nil): %v", err)
	}
	if len(renames) != 0 || len(additions) != 0 || len(orphans) != 0 {
		t.Fatalf("expected all empty, got %d/%d/%d", len(renames), len(additions), len(orphans))
	}
}

func TestPinVersionDiff_RemoveNoneAndReadError(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("1.2.3\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	inst := &installer{root: root, pinVersion: "", sys: RealSystem{}}
	diff, err := inst.pinVersionDiff()
	if err != nil {
		t.Fatalf("pinVersionDiff remove: %v", err)
	}
	if diff.Action != UpgradePinActionRemove {
		t.Fatalf("expected remove action, got %s", diff.Action)
	}

	inst.pinVersion = "1.2.3"
	diff, err = inst.pinVersionDiff()
	if err != nil {
		t.Fatalf("pinVersionDiff none: %v", err)
	}
	if diff.Action != UpgradePinActionNone {
		t.Fatalf("expected none action, got %s", diff.Action)
	}

	readFault := newFaultSystem(RealSystem{})
	readFault.readErrs[normalizePath(pinPath)] = errors.New("read boom")
	inst.sys = readFault
	if _, err := inst.pinVersionDiff(); err == nil {
		t.Fatal("expected read error")
	}
}
