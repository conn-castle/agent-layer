package install

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestLoadUpgradeMigrationManifestByVersion(t *testing.T) {
	manifest, manifestPath, err := loadUpgradeMigrationManifestByVersion("0.7.0")
	if err != nil {
		t.Fatalf("load migration manifest: %v", err)
	}
	if manifestPath != "migrations/0.7.0.json" {
		t.Fatalf("manifest path = %q, want %q", manifestPath, "migrations/0.7.0.json")
	}
	if manifest.TargetVersion != "0.7.0" {
		t.Fatalf("target_version = %q, want %q", manifest.TargetVersion, "0.7.0")
	}
	if manifest.MinPriorVersion == "" {
		t.Fatal("expected min_prior_version")
	}
}

func TestLoadUpgradeMigrationManifestByVersion_Missing(t *testing.T) {
	_, _, err := loadUpgradeMigrationManifestByVersion("9.9.9")
	if err == nil {
		t.Fatal("expected missing manifest error")
	}
	if got := err.Error(); !containsAll(got, "missing migration manifest", "9.9.9", "migrations/9.9.9.json") {
		t.Fatalf("unexpected missing manifest error: %v", err)
	}
}

func TestPlanUpgradeMigrations_UnknownSourceSkipsSourceDependent(t *testing.T) {
	root := t.TempDir()
	// Create the target file so the agnostic delete migration covers it.
	legacyJSON := filepath.Join(root, ".vscode", "legacy.json")
	if err := os.MkdirAll(filepath.Dir(legacyJSON), 0o755); err != nil {
		t.Fatalf("mkdir .vscode: %v", err)
	}
	if err := os.WriteFile(legacyJSON, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write legacy.json: %v", err)
	}
	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "dep_rename",
      "kind": "rename_file",
      "rationale": "Rename managed file",
      "from": "docs/agent-layer/OLD.md",
      "to": "docs/agent-layer/NEW.md"
    },
    {
      "id": "agnostic_delete",
      "kind": "delete_generated_artifact",
      "rationale": "Delete stale generated output",
      "source_agnostic": true,
      "path": ".vscode/legacy.json"
    }
  ]
}`)

	inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}
	if plan.report.SourceVersion != string(UpgradeMigrationSourceUnknown) {
		t.Fatalf("source version = %q, want unknown", plan.report.SourceVersion)
	}
	if len(plan.executable) != 1 || plan.executable[0].ID != "agnostic_delete" {
		t.Fatalf("executable migrations = %#v, want only agnostic_delete", plan.executable)
	}

	statuses := map[string]UpgradeMigrationStatus{}
	for _, entry := range plan.report.Entries {
		statuses[entry.ID] = entry.Status
	}
	if statuses["dep_rename"] != UpgradeMigrationStatusSkippedUnknownSource {
		t.Fatalf("dep_rename status = %q, want %q", statuses["dep_rename"], UpgradeMigrationStatusSkippedUnknownSource)
	}
	if statuses["agnostic_delete"] != UpgradeMigrationStatusPlanned {
		t.Fatalf("agnostic_delete status = %q, want %q", statuses["agnostic_delete"], UpgradeMigrationStatusPlanned)
	}
	if _, ok := plan.coveredPaths[".vscode/legacy.json"]; !ok {
		t.Fatalf("expected covered path for agnostic delete, got %#v", plan.coveredPaths)
	}
	if _, ok := plan.coveredPaths["docs/agent-layer/OLD.md"]; ok {
		t.Fatalf("did not expect skipped migration path to be covered, got %#v", plan.coveredPaths)
	}
}

func TestPlanUpgradeMigrations_SourceTooOldSkipsSourceDependent(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.5.0\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "dep_delete",
      "kind": "delete_file",
      "rationale": "Delete removed managed file",
      "path": "docs/agent-layer/LEGACY.md"
    }
  ]
}`)

	inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}
	if plan.report.SourceVersion != "0.5.0" {
		t.Fatalf("source version = %q, want %q", plan.report.SourceVersion, "0.5.0")
	}
	if plan.report.SourceVersionOrigin != UpgradeMigrationSourcePin {
		t.Fatalf("source origin = %q, want %q", plan.report.SourceVersionOrigin, UpgradeMigrationSourcePin)
	}
	if len(plan.report.Entries) != 1 {
		t.Fatalf("expected 1 report entry, got %d", len(plan.report.Entries))
	}
	if plan.report.Entries[0].Status != UpgradeMigrationStatusSkippedSourceTooOld {
		t.Fatalf("status = %q, want %q", plan.report.Entries[0].Status, UpgradeMigrationStatusSkippedSourceTooOld)
	}
	if len(plan.executable) != 0 {
		t.Fatalf("expected no executable migrations, got %#v", plan.executable)
	}
}

func TestPlanUpgradeMigrations_RollbackTargetsIncludeRenameDestination(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, ".agent-layer", "legacy.md")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy\n"), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "rename_managed",
      "kind": "rename_file",
      "rationale": "Move managed file",
      "source_agnostic": true,
      "from": ".agent-layer/legacy.md",
      "to": ".agent-layer/new.md"
    }
  ]
}`)

	inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}
	legacyAbs := filepath.Clean(filepath.Join(root, ".agent-layer", "legacy.md"))
	newAbs := filepath.Clean(filepath.Join(root, ".agent-layer", "new.md"))
	if !containsString(plan.rollbackTargets, legacyAbs) {
		t.Fatalf("rollback targets missing legacy path %q: %#v", legacyAbs, plan.rollbackTargets)
	}
	if !containsString(plan.rollbackTargets, newAbs) {
		t.Fatalf("rollback targets missing rename destination %q: %#v", newAbs, plan.rollbackTargets)
	}
}

func TestRunMigrations_AppliesAndReportsStatus(t *testing.T) {
	root := t.TempDir()
	legacyPath := filepath.Join(root, ".agent-layer", "legacy.md")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy\n"), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "rename_managed",
      "kind": "rename_file",
      "rationale": "Move managed file",
      "source_agnostic": true,
      "from": ".agent-layer/legacy.md",
      "to": ".agent-layer/new.md"
    }
  ]
}`)

	var warn bytes.Buffer
	inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}, warnWriter: &warn}
	if err := inst.prepareUpgradeMigrations(); err != nil {
		t.Fatalf("prepareUpgradeMigrations: %v", err)
	}
	if err := inst.runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, ".agent-layer", "new.md")); err != nil {
		t.Fatalf("expected renamed destination to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-layer", "legacy.md")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy source to be removed, stat err = %v", err)
	}
	if len(inst.migrationReport.Entries) != 1 {
		t.Fatalf("expected one migration report entry, got %d", len(inst.migrationReport.Entries))
	}
	if inst.migrationReport.Entries[0].Status != UpgradeMigrationStatusApplied {
		t.Fatalf("migration status = %q, want %q", inst.migrationReport.Entries[0].Status, UpgradeMigrationStatusApplied)
	}
	if !containsAll(warn.String(), "Migration report:", "rename_managed") {
		t.Fatalf("expected migration report output in warnings, got %q", warn.String())
	}
}

func TestBuildUpgradePlan_ManifestCoverageSkipsHashRenameInference(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.6.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	findIssuesPath := filepath.Join(root, ".agent-layer", "slash-commands", "find-issues.md")
	if err := os.Remove(findIssuesPath); err != nil {
		t.Fatalf("remove find-issues: %v", err)
	}
	findIssuesTemplate, err := templates.Read("slash-commands/find-issues.md")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	legacyPath := filepath.Join(root, ".agent-layer", "slash-commands", "find-issues-legacy.md")
	if err := os.WriteFile(legacyPath, findIssuesTemplate, 0o644); err != nil {
		t.Fatalf("write legacy path: %v", err)
	}

	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "rename_find_issues",
      "kind": "rename_file",
      "rationale": "Move legacy slash command path",
      "from": ".agent-layer/slash-commands/find-issues-legacy.md",
      "to": ".agent-layer/slash-commands/find-issues.md"
    }
  ]
}`)

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{TargetPinVersion: "0.7.0", System: RealSystem{}})
	if err != nil {
		t.Fatalf("build upgrade plan: %v", err)
	}
	if len(plan.TemplateRenames) != 0 {
		t.Fatalf("expected hash-rename inference to be filtered by manifest coverage, got %#v", plan.TemplateRenames)
	}
	if findUpgradeChange(plan.TemplateAdditions, ".agent-layer/slash-commands/find-issues.md") != nil {
		t.Fatal("expected manifest-covered addition to be filtered")
	}
	if findUpgradeChange(plan.TemplateRemovalsOrOrphans, ".agent-layer/slash-commands/find-issues-legacy.md") != nil {
		t.Fatal("expected manifest-covered orphan to be filtered")
	}
	if len(plan.MigrationReport.Entries) != 1 {
		t.Fatalf("expected one migration report entry, got %d", len(plan.MigrationReport.Entries))
	}
	if plan.MigrationReport.Entries[0].Status != UpgradeMigrationStatusPlanned {
		t.Fatalf("migration status = %q, want %q", plan.MigrationReport.Entries[0].Status, UpgradeMigrationStatusPlanned)
	}
}

func TestBuildUpgradePlan_ManifestCoverageFiltersTemplateUpdates(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.6.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	commandsAllowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.WriteFile(commandsAllowPath, []byte("echo custom\n"), 0o644); err != nil {
		t.Fatalf("write custom commands.allow: %v", err)
	}

	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "cover_update_path",
      "kind": "delete_generated_artifact",
      "rationale": "Cover path to suppress template update diff",
      "source_agnostic": true,
      "path": ".agent-layer/commands.allow"
    }
  ]
}`)

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{TargetPinVersion: "0.7.0", System: RealSystem{}})
	if err != nil {
		t.Fatalf("build upgrade plan: %v", err)
	}
	if findUpgradeChange(plan.TemplateUpdates, ".agent-layer/commands.allow") != nil {
		t.Fatalf("expected manifest-covered update to be filtered from template updates: %#v", plan.TemplateUpdates)
	}
}

func TestBuildUpgradePlan_NoopMigrationDoesNotHideTemplateChange(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.6.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	// Remove find-issues.md so the template system would add it back.
	findIssuesPath := filepath.Join(root, ".agent-layer", "slash-commands", "find-issues.md")
	if err := os.Remove(findIssuesPath); err != nil {
		t.Fatalf("remove find-issues: %v", err)
	}
	// Do NOT create the legacy source file — the rename will no-op because
	// the source is absent. The plan must still show find-issues.md as an
	// addition so the user knows the template writer will create it.
	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "rename_find_issues",
      "kind": "rename_file",
      "rationale": "Move legacy slash command path",
      "source_agnostic": true,
      "from": ".agent-layer/slash-commands/find-issues-legacy.md",
      "to": ".agent-layer/slash-commands/find-issues.md"
    }
  ]
}`)

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{TargetPinVersion: "0.7.0", System: RealSystem{}})
	if err != nil {
		t.Fatalf("build upgrade plan: %v", err)
	}
	// The rename source doesn't exist, so the migration will no-op. The
	// destination path must NOT be filtered from additions.
	if findUpgradeChange(plan.TemplateAdditions, ".agent-layer/slash-commands/find-issues.md") == nil {
		t.Fatal("expected no-op migration destination to appear as template addition in plan")
	}
}

func TestRun_UpgradeRoundTripWithMigrationManifest(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.6.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	findIssuesPath := filepath.Join(root, ".agent-layer", "slash-commands", "find-issues.md")
	if err := os.Remove(findIssuesPath); err != nil {
		t.Fatalf("remove find-issues: %v", err)
	}
	findIssuesTemplate, err := templates.Read("slash-commands/find-issues.md")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	legacyPath := filepath.Join(root, ".agent-layer", "slash-commands", "find-issues-legacy.md")
	if err := os.WriteFile(legacyPath, findIssuesTemplate, 0o644); err != nil {
		t.Fatalf("write legacy path: %v", err)
	}

	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "rename_find_issues",
      "kind": "rename_file",
      "rationale": "Move legacy slash command path",
      "from": ".agent-layer/slash-commands/find-issues-legacy.md",
      "to": ".agent-layer/slash-commands/find-issues.md"
    }
  ]
}`)

	if err := Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "0.7.0"}); err != nil {
		t.Fatalf("upgrade run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-layer", "slash-commands", "find-issues.md")); err != nil {
		t.Fatalf("expected find-issues after migration+upgrade: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-layer", "slash-commands", "find-issues-legacy.md")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy file to be removed, stat err = %v", err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
