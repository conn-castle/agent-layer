package install

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
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

func TestExecuteConfigSetDefaultMigration_CallsPrompt(t *testing.T) {
	root := t.TempDir()

	// Write a config file missing the key.
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[agents]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var promptedKey string
	var promptedValue any
	var promptedRationale string
	prompter := PromptFuncs{
		OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return true, nil },
		OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return true, nil },
		OverwritePreviewFunc:          func(DiffPreview) (bool, error) { return true, nil },
		DeleteUnknownAllFunc:          func([]string) (bool, error) { return true, nil },
		DeleteUnknownFunc:             func(string) (bool, error) { return true, nil },
		ConfigSetDefaultFunc: func(key string, manifestValue any, rationale string, field *config.FieldDef) (any, error) {
			promptedKey = key
			promptedValue = manifestValue
			promptedRationale = rationale
			return true, nil // User chooses true instead of false
		},
	}

	inst := &installer{root: root, prompter: prompter, sys: RealSystem{}}
	op := upgradeMigrationOperation{
		ID:        "add-test-key",
		Kind:      upgradeMigrationKindConfigSetDefault,
		Key:       "agents.test-agent.enabled",
		Value:     []byte(`false`),
		Rationale: "New agent added for testing.",
	}
	changed, err := inst.executeConfigSetDefaultMigration(op)
	if err != nil {
		t.Fatalf("executeConfigSetDefaultMigration: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to report changed")
	}

	// Verify prompt was called with correct arguments.
	if promptedKey != "agents.test-agent.enabled" {
		t.Fatalf("prompted key = %q, want %q", promptedKey, "agents.test-agent.enabled")
	}
	if promptedValue != false {
		t.Fatalf("prompted manifest value = %v, want false", promptedValue)
	}
	if promptedRationale != "New agent added for testing." {
		t.Fatalf("prompted rationale = %q, want %q", promptedRationale, "New agent added for testing.")
	}

	// Verify the user's chosen value (true) was written, not the manifest default (false).
	data, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "enabled = true") {
		t.Fatalf("expected user-chosen value 'enabled = true' in config, got:\n%s", string(data))
	}
}

func TestExecuteConfigSetDefaultMigration_NoPromptUsesDefault(t *testing.T) {
	root := t.TempDir()

	// Write a config file missing the key.
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[agents]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// No ConfigSetDefaultFunc — should use the manifest's default value.
	prompter := autoApprovePrompter()
	inst := &installer{root: root, prompter: prompter, sys: RealSystem{}}
	op := upgradeMigrationOperation{
		ID:        "add-test-key",
		Kind:      upgradeMigrationKindConfigSetDefault,
		Key:       "agents.test-agent.enabled",
		Value:     []byte(`false`),
		Rationale: "New agent added for testing.",
	}
	changed, err := inst.executeConfigSetDefaultMigration(op)
	if err != nil {
		t.Fatalf("executeConfigSetDefaultMigration: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to report changed")
	}

	// Verify the manifest default (false) was written.
	data, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "enabled = false") {
		t.Fatalf("expected manifest default 'enabled = false' in config, got:\n%s", string(data))
	}
}

func TestLoadUpgradeMigrationManifest_0_8_1_IsEmpty(t *testing.T) {
	manifest, _, err := loadUpgradeMigrationManifestByVersion("0.8.1")
	if err != nil {
		t.Fatalf("load 0.8.1 manifest: %v", err)
	}
	if len(manifest.Operations) != 0 {
		t.Fatalf("expected 0 operations in 0.8.1 (shipped empty), got %d", len(manifest.Operations))
	}
}

func TestLoadUpgradeMigrationManifest_0_8_2_HasConfigSetDefault(t *testing.T) {
	manifest, _, err := loadUpgradeMigrationManifestByVersion("0.8.2")
	if err != nil {
		t.Fatalf("load 0.8.2 manifest: %v", err)
	}
	if len(manifest.Operations) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(manifest.Operations))
	}
	op := manifest.Operations[0]
	if op.ID != "add-claude-vscode-enabled" {
		t.Fatalf("op ID = %q, want %q", op.ID, "add-claude-vscode-enabled")
	}
	if op.Kind != upgradeMigrationKindConfigSetDefault {
		t.Fatalf("op kind = %q, want %q", op.Kind, upgradeMigrationKindConfigSetDefault)
	}
	if op.Key != "agents.claude-vscode.enabled" {
		t.Fatalf("op key = %q, want %q", op.Key, "agents.claude-vscode.enabled")
	}
	if !op.SourceAgnostic {
		t.Fatal("expected source_agnostic = true")
	}
	if manifest.MinPriorVersion != "0.8.0" {
		t.Fatalf("min_prior_version = %q, want %q", manifest.MinPriorVersion, "0.8.0")
	}
}

func TestListMigrationManifestVersions(t *testing.T) {
	versions, err := listMigrationManifestVersions()
	if err != nil {
		t.Fatalf("listMigrationManifestVersions: %v", err)
	}
	if len(versions) == 0 {
		t.Fatal("expected at least one migration manifest version")
	}
	// Verify sorted ascending.
	for i := 1; i < len(versions); i++ {
		cmp, cmpErr := compareSemver(versions[i-1], versions[i])
		if cmpErr != nil {
			t.Fatalf("compareSemver(%q, %q): %v", versions[i-1], versions[i], cmpErr)
		}
		if cmp >= 0 {
			t.Fatalf("versions not sorted ascending: %q >= %q", versions[i-1], versions[i])
		}
	}
	// Verify known versions exist.
	if !containsString(versions, "0.7.0") {
		t.Fatalf("expected 0.7.0 in versions, got %v", versions)
	}
	if !containsString(versions, "0.8.2") {
		t.Fatalf("expected 0.8.2 in versions, got %v", versions)
	}
}

func TestCollectMigrationChain(t *testing.T) {
	withMigrationManifestChainOverride(t, map[string]string{
		"0.6.0": `{"schema_version":1,"target_version":"0.6.0","min_prior_version":"0.5.0","operations":[]}`,
		"0.6.1": `{"schema_version":1,"target_version":"0.6.1","min_prior_version":"0.6.0","operations":[
			{"id":"op-a","kind":"delete_file","rationale":"clean up","path":"old.txt","source_agnostic":true}
		]}`,
		"0.7.0": `{"schema_version":1,"target_version":"0.7.0","min_prior_version":"0.6.0","operations":[
			{"id":"op-b","kind":"delete_file","rationale":"more cleanup","path":"old2.txt","source_agnostic":true}
		]}`,
	})

	t.Run("source_exclusive_target_inclusive", func(t *testing.T) {
		chain, err := collectMigrationChain("0.6.0", "0.7.0")
		if err != nil {
			t.Fatalf("collectMigrationChain: %v", err)
		}
		if len(chain) != 2 {
			t.Fatalf("expected 2 manifests in chain, got %d", len(chain))
		}
		if chain[0].manifest.TargetVersion != "0.6.1" {
			t.Fatalf("first chain entry = %q, want 0.6.1", chain[0].manifest.TargetVersion)
		}
		if chain[1].manifest.TargetVersion != "0.7.0" {
			t.Fatalf("second chain entry = %q, want 0.7.0", chain[1].manifest.TargetVersion)
		}
	})

	t.Run("same_source_and_target_returns_empty", func(t *testing.T) {
		chain, err := collectMigrationChain("0.7.0", "0.7.0")
		if err != nil {
			t.Fatalf("collectMigrationChain: %v", err)
		}
		if len(chain) != 0 {
			t.Fatalf("expected empty chain, got %d", len(chain))
		}
	})

	t.Run("no_intermediate_manifests", func(t *testing.T) {
		chain, err := collectMigrationChain("0.6.1", "0.7.0")
		if err != nil {
			t.Fatalf("collectMigrationChain: %v", err)
		}
		if len(chain) != 1 {
			t.Fatalf("expected 1 manifest, got %d", len(chain))
		}
		if chain[0].manifest.TargetVersion != "0.7.0" {
			t.Fatalf("chain entry = %q, want 0.7.0", chain[0].manifest.TargetVersion)
		}
	})
}

func TestPlanUpgradeMigrations_ChainsIntermediateManifests(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.6.0\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	withMigrationManifestChainOverride(t, map[string]string{
		"0.6.0": `{"schema_version":1,"target_version":"0.6.0","min_prior_version":"0.5.0","operations":[]}`,
		"0.6.1": `{"schema_version":1,"target_version":"0.6.1","min_prior_version":"0.6.0","operations":[
			{"id":"intermediate-op","kind":"delete_file","rationale":"cleanup intermediate","path":"stale.txt","source_agnostic":true}
		]}`,
		"0.7.0": `{"schema_version":1,"target_version":"0.7.0","min_prior_version":"0.6.0","operations":[
			{"id":"target-op","kind":"delete_file","rationale":"cleanup target","path":"old.txt","source_agnostic":true}
		]}`,
	})

	inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}
	if plan.report.TargetVersion != "0.7.0" {
		t.Fatalf("target version = %q, want 0.7.0", plan.report.TargetVersion)
	}
	if plan.report.MinPriorVersion != "0.6.0" {
		t.Fatalf("min prior version = %q, want 0.6.0", plan.report.MinPriorVersion)
	}

	// Verify both operations appear in order.
	if len(plan.report.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(plan.report.Entries))
	}
	ids := make([]string, 0, len(plan.report.Entries))
	for _, e := range plan.report.Entries {
		ids = append(ids, e.ID)
	}
	if !containsString(ids, "intermediate-op") {
		t.Fatalf("missing intermediate-op in entries: %v", ids)
	}
	if !containsString(ids, "target-op") {
		t.Fatalf("missing target-op in entries: %v", ids)
	}

	// Verify manifest path contains both paths.
	if !containsAll(plan.report.ManifestPath, "0.6.1.json", "0.7.0.json") {
		t.Fatalf("manifest path should contain both manifests, got %q", plan.report.ManifestPath)
	}
}

func TestPlanUpgradeMigrations_ChainDeduplicatesOperationIDs(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.6.0\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	withMigrationManifestChainOverride(t, map[string]string{
		"0.6.0": `{"schema_version":1,"target_version":"0.6.0","min_prior_version":"0.5.0","operations":[]}`,
		"0.6.1": `{"schema_version":1,"target_version":"0.6.1","min_prior_version":"0.6.0","operations":[
			{"id":"shared-op","kind":"delete_file","rationale":"from 0.6.1","path":"stale.txt","source_agnostic":true}
		]}`,
		"0.7.0": `{"schema_version":1,"target_version":"0.7.0","min_prior_version":"0.6.0","operations":[
			{"id":"shared-op","kind":"delete_file","rationale":"from 0.7.0","path":"stale.txt","source_agnostic":true}
		]}`,
	})

	inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}

	// Only one entry for the shared ID (from the first manifest in the chain).
	count := 0
	for _, e := range plan.report.Entries {
		if e.ID == "shared-op" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected shared-op to appear once (deduplicated), got %d", count)
	}
	// Verify the rationale is from the first manifest (0.6.1).
	for _, e := range plan.report.Entries {
		if e.ID == "shared-op" && e.Rationale != "from 0.6.1" {
			t.Fatalf("expected shared-op rationale from first manifest, got %q", e.Rationale)
		}
	}
}

func TestPlanUpgradeMigrations_UnknownSourceFallsBackToTargetOnly(t *testing.T) {
	root := t.TempDir()
	// No pin file → source is unknown.

	withMigrationManifestChainOverride(t, map[string]string{
		"0.6.0": `{"schema_version":1,"target_version":"0.6.0","min_prior_version":"0.5.0","operations":[
			{"id":"should-not-appear","kind":"delete_file","rationale":"from 0.6.0","path":"x.txt","source_agnostic":true}
		]}`,
		"0.6.1": `{"schema_version":1,"target_version":"0.6.1","min_prior_version":"0.6.0","operations":[
			{"id":"should-not-appear-either","kind":"delete_file","rationale":"from 0.6.1","path":"y.txt","source_agnostic":true}
		]}`,
		"0.7.0": `{"schema_version":1,"target_version":"0.7.0","min_prior_version":"0.6.0","operations":[
			{"id":"target-only","kind":"delete_file","rationale":"from target","path":"z.txt","source_agnostic":true}
		]}`,
	})

	inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}

	if plan.report.SourceVersionOrigin != UpgradeMigrationSourceUnknown {
		t.Fatalf("source origin = %q, want unknown", plan.report.SourceVersionOrigin)
	}

	// Only the target manifest's operations should appear.
	if len(plan.report.Entries) != 1 {
		t.Fatalf("expected 1 entry (target only), got %d", len(plan.report.Entries))
	}
	if plan.report.Entries[0].ID != "target-only" {
		t.Fatalf("entry ID = %q, want target-only", plan.report.Entries[0].ID)
	}
	// ManifestPath should be a single path (not comma-joined).
	if strings.Contains(plan.report.ManifestPath, ",") {
		t.Fatalf("expected single manifest path, got %q", plan.report.ManifestPath)
	}
}

func TestPlanUpgradeMigrations_ChainSourceTooOldPerManifest(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	// Source is 0.5.0 — older than 0.6.1's min_prior_version (0.6.0) but
	// the agnostic op should still execute. The non-agnostic op in 0.6.1
	// should be skipped as source-too-old.
	if err := os.WriteFile(pinPath, []byte("0.5.0\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	withMigrationManifestChainOverride(t, map[string]string{
		"0.5.0": `{"schema_version":1,"target_version":"0.5.0","min_prior_version":"0.4.0","operations":[]}`,
		"0.6.0": `{"schema_version":1,"target_version":"0.6.0","min_prior_version":"0.5.0","operations":[
			{"id":"safe-op","kind":"delete_file","rationale":"agnostic cleanup","path":"safe.txt","source_agnostic":true}
		]}`,
		"0.6.1": `{"schema_version":1,"target_version":"0.6.1","min_prior_version":"0.6.0","operations":[
			{"id":"gated-op","kind":"delete_file","rationale":"needs 0.6.0+","path":"gated.txt"},
			{"id":"agnostic-later","kind":"delete_file","rationale":"agnostic in later manifest","path":"agnostic.txt","source_agnostic":true}
		]}`,
	})

	inst := &installer{root: root, pinVersion: "0.6.1", sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}

	statuses := map[string]UpgradeMigrationStatus{}
	for _, e := range plan.report.Entries {
		statuses[e.ID] = e.Status
	}

	// safe-op is in 0.6.0 which has min_prior 0.5.0 — source 0.5.0 >= 0.5.0 → planned.
	// But safe-op is source_agnostic, so it would be planned regardless.
	if statuses["safe-op"] != UpgradeMigrationStatusPlanned {
		t.Fatalf("safe-op status = %q, want planned", statuses["safe-op"])
	}

	// gated-op is in 0.6.1 which has min_prior 0.6.0 — source 0.5.0 < 0.6.0 → skipped.
	if statuses["gated-op"] != UpgradeMigrationStatusSkippedSourceTooOld {
		t.Fatalf("gated-op status = %q, want skipped_source_too_old", statuses["gated-op"])
	}

	// agnostic-later is source_agnostic so it should still be planned.
	if statuses["agnostic-later"] != UpgradeMigrationStatusPlanned {
		t.Fatalf("agnostic-later status = %q, want planned", statuses["agnostic-later"])
	}

	// Only the agnostic ops should be executable.
	execIDs := make([]string, 0, len(plan.executable))
	for _, op := range plan.executable {
		execIDs = append(execIDs, op.ID)
	}
	if !containsString(execIDs, "safe-op") {
		t.Fatalf("expected safe-op in executable, got %v", execIDs)
	}
	if containsString(execIDs, "gated-op") {
		t.Fatalf("did not expect gated-op in executable, got %v", execIDs)
	}
	if !containsString(execIDs, "agnostic-later") {
		t.Fatalf("expected agnostic-later in executable, got %v", execIDs)
	}
}

func TestPlanUpgradeMigrations_KnownSourceMissingTargetManifestFails(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.6.0\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	// Override walk to return only 0.6.0 and 0.6.1 — no 0.7.0 manifest.
	withMigrationManifestChainOverride(t, map[string]string{
		"0.6.0": `{"schema_version":1,"target_version":"0.6.0","min_prior_version":"0.5.0","operations":[]}`,
		"0.6.1": `{"schema_version":1,"target_version":"0.6.1","min_prior_version":"0.6.0","operations":[]}`,
	})

	inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
	_, err := inst.planUpgradeMigrations()
	if err == nil {
		t.Fatal("expected error for missing target manifest, got nil")
	}
	if !strings.Contains(err.Error(), "missing migration manifest") {
		t.Fatalf("expected 'missing migration manifest' error, got: %v", err)
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
