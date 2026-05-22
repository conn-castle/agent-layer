package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
	if err := os.MkdirAll(filepath.Dir(legacyJSON), 0o700); err != nil {
		t.Fatalf("mkdir .vscode: %v", err)
	}
	if err := os.WriteFile(legacyJSON, []byte(`{}`), 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o700); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.5.0\n"), 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy\n"), 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy\n"), 0o600); err != nil {
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
	planWorkPath := filepath.Join(root, ".agent-layer", "skills", "plan-work", "SKILL.md")
	if err := os.Remove(planWorkPath); err != nil {
		t.Fatalf("remove plan-work: %v", err)
	}
	planWorkTemplate, err := templates.Read("skills/plan-work/SKILL.md")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	legacyPath := filepath.Join(root, ".agent-layer", "skills", "plan-work-legacy.md")
	if err := os.WriteFile(legacyPath, planWorkTemplate, 0o600); err != nil {
		t.Fatalf("write legacy path: %v", err)
	}

	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "rename_plan_work",
      "kind": "rename_file",
      "rationale": "Move legacy skill path",
      "from": ".agent-layer/skills/plan-work-legacy.md",
      "to": ".agent-layer/skills/plan-work/SKILL.md"
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
	if findUpgradeChange(plan.TemplateAdditions, ".agent-layer/skills/plan-work/SKILL.md") != nil {
		t.Fatal("expected manifest-covered addition to be filtered")
	}
	if findUpgradeChange(plan.TemplateRemovalsOrOrphans, ".agent-layer/skills/plan-work-legacy.md") != nil {
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
	if err := os.WriteFile(commandsAllowPath, []byte("echo custom\n"), 0o600); err != nil {
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

	// Remove plan-work/SKILL.md so the template system would add it back.
	planWorkPath := filepath.Join(root, ".agent-layer", "skills", "plan-work", "SKILL.md")
	if err := os.Remove(planWorkPath); err != nil {
		t.Fatalf("remove plan-work: %v", err)
	}
	// Do NOT create the legacy source file — the rename will no-op because
	// the source is absent. The plan must still show plan-work/SKILL.md as an
	// addition so the user knows the template writer will create it.
	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "rename_plan_work",
      "kind": "rename_file",
      "rationale": "Move legacy skill path",
      "source_agnostic": true,
      "from": ".agent-layer/skills/plan-work-legacy.md",
      "to": ".agent-layer/skills/plan-work/SKILL.md"
    }
  ]
}`)

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{TargetPinVersion: "0.7.0", System: RealSystem{}})
	if err != nil {
		t.Fatalf("build upgrade plan: %v", err)
	}
	// The rename source doesn't exist, so the migration will no-op. The
	// destination path must NOT be filtered from additions.
	if findUpgradeChange(plan.TemplateAdditions, ".agent-layer/skills/plan-work/SKILL.md") == nil {
		t.Fatal("expected no-op migration destination to appear as template addition in plan")
	}
}

func TestRun_UpgradeRoundTripWithMigrationManifest(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.6.0"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	planWorkPath := filepath.Join(root, ".agent-layer", "skills", "plan-work", "SKILL.md")
	if err := os.Remove(planWorkPath); err != nil {
		t.Fatalf("remove plan-work: %v", err)
	}
	planWorkTemplate, err := templates.Read("skills/plan-work/SKILL.md")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	legacyPath := filepath.Join(root, ".agent-layer", "skills", "plan-work-legacy.md")
	if err := os.WriteFile(legacyPath, planWorkTemplate, 0o600); err != nil {
		t.Fatalf("write legacy path: %v", err)
	}

	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "rename_plan_work",
      "kind": "rename_file",
      "rationale": "Move legacy skill path",
      "from": ".agent-layer/skills/plan-work-legacy.md",
      "to": ".agent-layer/skills/plan-work/SKILL.md"
    }
  ]
}`)

	if err := Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "0.7.0"}); err != nil {
		t.Fatalf("upgrade run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-layer", "skills", "plan-work", "SKILL.md")); err != nil {
		t.Fatalf("expected plan-work after migration+upgrade: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agent-layer", "skills", "plan-work-legacy.md")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy file to be removed, stat err = %v", err)
	}
}

func TestExecuteConfigSetDefaultMigration_CallsPrompt(t *testing.T) {
	root := t.TempDir()

	// Write a config file missing the key.
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[agents]\n"), 0o600); err != nil {
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
	data, err := os.ReadFile(filepath.Join(configDir, "config.toml")) // #nosec G304 -- path is constructed from test-controlled inputs.
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
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("[agents]\n"), 0o600); err != nil {
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
	data, err := os.ReadFile(filepath.Join(configDir, "config.toml")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "enabled = false") {
		t.Fatalf("expected manifest default 'enabled = false' in config, got:\n%s", string(data))
	}
}

func TestExecuteConfigDeleteKeyMigration_DeletesLeaf(t *testing.T) {
	root := t.TempDir()
	cfgPath := writeMigrationConfigForTest(t, root, strings.Join([]string{
		"[agents.gemini]",
		"enabled = true",
		`model = "custom"`,
	}, "\n"))

	inst := &installer{root: root, sys: RealSystem{}}
	changed, err := inst.executeConfigDeleteKeyMigration("agents.gemini.model")
	if err != nil {
		t.Fatalf("executeConfigDeleteKeyMigration: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to report changed")
	}

	data, err := os.ReadFile(cfgPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "model") {
		t.Fatalf("expected model key deleted, got:\n%s", got)
	}
	if !strings.Contains(got, "enabled = true") {
		t.Fatalf("expected enabled key preserved, got:\n%s", got)
	}
}

func TestExecuteConfigDeleteKeyMigration_DeletesTableAndPrunesParents(t *testing.T) {
	root := t.TempDir()
	cfgPath := writeMigrationConfigForTest(t, root, strings.Join([]string{
		"[agents.gemini]",
		"enabled = true",
		`model = "custom"`,
		"",
		"[warnings]",
		"instruction_token_threshold = 10000",
	}, "\n"))

	inst := &installer{root: root, sys: RealSystem{}}
	changed, err := inst.executeConfigDeleteKeyMigration("agents.gemini")
	if err != nil {
		t.Fatalf("executeConfigDeleteKeyMigration: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to report changed")
	}

	data, err := os.ReadFile(cfgPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "gemini") || strings.Contains(got, "[agents]") {
		t.Fatalf("expected gemini table and empty agents parent pruned, got:\n%s", got)
	}
	if !strings.Contains(got, "[warnings]") {
		t.Fatalf("expected unrelated table preserved, got:\n%s", got)
	}
}

func TestExecuteConfigDeleteKeyMigration_IdempotentWhenMissing(t *testing.T) {
	root := t.TempDir()
	writeMigrationConfigForTest(t, root, "[agents]\n")

	inst := &installer{root: root, sys: RealSystem{}}
	changed, err := inst.executeConfigDeleteKeyMigration("agents.gemini")
	if err != nil {
		t.Fatalf("executeConfigDeleteKeyMigration: %v", err)
	}
	if changed {
		t.Fatal("expected missing key deletion to be a no-op")
	}
}

func TestValidateUpgradeMigrationOperation_ConfigDeleteKeyRequiresValidKey(t *testing.T) {
	err := validateUpgradeMigrationOperation(upgradeMigrationOperation{
		ID:        "delete-bad-key",
		Kind:      upgradeMigrationKindConfigDeleteKey,
		Key:       "agents..gemini",
		Rationale: "Delete invalid key for test.",
	})
	if err == nil {
		t.Fatal("expected invalid config_delete_key operation to fail validation")
	}
	if !strings.Contains(err.Error(), "invalid key") {
		t.Fatalf("expected invalid-key validation error, got %v", err)
	}
}

func TestValidateUpgradeMigrationOperation_ConfigReplaceString(t *testing.T) {
	validOp := upgradeMigrationOperation{
		ID:        "replace-client",
		Kind:      upgradeMigrationKindConfigReplaceString,
		Key:       "mcp.servers[].clients[]",
		From:      "gemini",
		To:        "antigravity",
		Rationale: "Replace legacy client ID.",
	}
	if err := validateUpgradeMigrationOperation(validOp); err != nil {
		t.Fatalf("expected valid config_replace_string operation, got %v", err)
	}

	invalidPath := validOp
	invalidPath.Key = "mcp..clients[]"
	if err := validateUpgradeMigrationOperation(invalidPath); err == nil || !strings.Contains(err.Error(), "invalid key") {
		t.Fatalf("expected invalid path error, got %v", err)
	}

	missingValue := validOp
	missingValue.From = ""
	if err := validateUpgradeMigrationOperation(missingValue); err == nil || !strings.Contains(err.Error(), "requires from and to") {
		t.Fatalf("expected missing from/to error, got %v", err)
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

func TestLoadUpgradeMigrationManifest_0_8_8_RenamesAndBackfillsClaudeVSCodeKey(t *testing.T) {
	manifest, _, err := loadUpgradeMigrationManifestByVersion("0.8.8")
	if err != nil {
		t.Fatalf("load 0.8.8 manifest: %v", err)
	}
	if len(manifest.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(manifest.Operations))
	}

	byID := make(map[string]upgradeMigrationOperation, len(manifest.Operations))
	for _, op := range manifest.Operations {
		byID[op.ID] = op
	}

	renameOp, ok := byID["a-rename-claude-vscode-enabled-key"]
	if !ok {
		t.Fatalf("missing rename operation, got IDs: %v", mapKeys(byID))
	}
	if renameOp.Kind != upgradeMigrationKindConfigRenameKey {
		t.Fatalf("rename op kind = %q, want %q", renameOp.Kind, upgradeMigrationKindConfigRenameKey)
	}
	if renameOp.From != "agents.claude-vscode.enabled" {
		t.Fatalf("rename op from = %q, want %q", renameOp.From, "agents.claude-vscode.enabled")
	}
	if renameOp.To != "agents.claude_vscode.enabled" {
		t.Fatalf("rename op to = %q, want %q", renameOp.To, "agents.claude_vscode.enabled")
	}
	if !renameOp.SourceAgnostic {
		t.Fatal("expected rename op source_agnostic = true")
	}

	defaultOp, ok := byID["b-set-default-claude_vscode-enabled"]
	if !ok {
		t.Fatalf("missing set-default operation, got IDs: %v", mapKeys(byID))
	}
	if defaultOp.Kind != upgradeMigrationKindConfigSetDefault {
		t.Fatalf("set-default op kind = %q, want %q", defaultOp.Kind, upgradeMigrationKindConfigSetDefault)
	}
	if defaultOp.Key != "agents.claude_vscode.enabled" {
		t.Fatalf("set-default op key = %q, want %q", defaultOp.Key, "agents.claude_vscode.enabled")
	}
	if !defaultOp.SourceAgnostic {
		t.Fatal("expected set-default op source_agnostic = true")
	}
	if string(defaultOp.Value) != "false" {
		t.Fatalf("set-default op value = %q, want %q", string(defaultOp.Value), "false")
	}

	if manifest.MinPriorVersion != "0.8.0" {
		t.Fatalf("min_prior_version = %q, want %q", manifest.MinPriorVersion, "0.8.0")
	}
}

func TestLoadUpgradeMigrationManifest_0_9_0_IncludesMigrateSkillsFormat(t *testing.T) {
	manifest, _, err := loadUpgradeMigrationManifestByVersion("0.9.0")
	if err != nil {
		t.Fatalf("load 0.9.0 manifest: %v", err)
	}
	if len(manifest.Operations) != 4 {
		t.Fatalf("expected 4 operations, got %d", len(manifest.Operations))
	}

	byID := make(map[string]upgradeMigrationOperation, len(manifest.Operations))
	for _, op := range manifest.Operations {
		byID[op.ID] = op
	}

	renameDirOp, ok := byID["c-rename-slash-commands-dir-to-skills"]
	if !ok {
		t.Fatalf("missing slash-commands rename operation, got IDs: %v", mapKeys(byID))
	}
	if renameDirOp.Kind != upgradeMigrationKindRenameFile {
		t.Fatalf("rename-dir op kind = %q, want %q", renameDirOp.Kind, upgradeMigrationKindRenameFile)
	}
	if renameDirOp.From != ".agent-layer/slash-commands" {
		t.Fatalf("rename-dir op from = %q, want %q", renameDirOp.From, ".agent-layer/slash-commands")
	}
	if renameDirOp.To != ".agent-layer/skills" {
		t.Fatalf("rename-dir op to = %q, want %q", renameDirOp.To, ".agent-layer/skills")
	}
	if !renameDirOp.SourceAgnostic {
		t.Fatal("expected rename-dir op source_agnostic = true")
	}

	migrateOp, ok := byID["d-migrate-all-skills-to-directory-format"]
	if !ok {
		t.Fatalf("missing migrate_skills_format operation, got IDs: %v", mapKeys(byID))
	}
	if migrateOp.Kind != upgradeMigrationKindMigrateSkillsFormat {
		t.Fatalf("migrate op kind = %q, want %q", migrateOp.Kind, upgradeMigrationKindMigrateSkillsFormat)
	}
	if migrateOp.Path != ".agent-layer/skills" {
		t.Fatalf("migrate op path = %q, want %q", migrateOp.Path, ".agent-layer/skills")
	}
	if !migrateOp.SourceAgnostic {
		t.Fatal("expected migrate op source_agnostic = true")
	}
	if !migrateOp.Breaking {
		t.Fatal("expected migrate op breaking = true")
	}
	if migrateOp.BreakingNotice == "" {
		t.Fatal("expected migrate op breaking_notice to be set")
	}
	if len(migrateOp.BreakingDetails) == 0 {
		t.Fatal("expected migrate op breaking_details to be non-empty")
	}
}

func TestLoadUpgradeMigrationManifest_0_10_2_MigratesGeminiToAntigravity(t *testing.T) {
	manifest, _, err := loadUpgradeMigrationManifestByVersion("0.10.2")
	if err != nil {
		t.Fatalf("load 0.10.2 manifest: %v", err)
	}
	byID := make(map[string]upgradeMigrationOperation, len(manifest.Operations))
	for _, op := range manifest.Operations {
		byID[op.ID] = op
	}
	// Required ops for the v0.10.2 contract. Adding a patch op to the
	// manifest later does NOT need to update this list — only assert the
	// known ones exist, then reject any unknown IDs to catch silent slip-ins.
	requiredIDs := []string{
		"a-delete-old-agents-antigravity",
		"b-rename-agents-gemini-enabled",
		"c-delete-agents-gemini",
		"d-set-default-agents-antigravity-enabled",
		"e-replace-gemini-mcp-client",
		"f-delete-orphan-gemini-md",
	}
	for _, id := range requiredIDs {
		if _, ok := byID[id]; !ok {
			t.Fatalf("missing required op %s in 0.10.2 manifest", id)
		}
	}
	requiredSet := make(map[string]struct{}, len(requiredIDs))
	for _, id := range requiredIDs {
		requiredSet[id] = struct{}{}
	}
	for id := range byID {
		if _, ok := requiredSet[id]; !ok {
			t.Fatalf("unknown op %s in 0.10.2 manifest; update requiredIDs and document the new op", id)
		}
	}

	oldDeleteOp := byID["a-delete-old-agents-antigravity"]
	if oldDeleteOp.Kind != upgradeMigrationKindConfigDeleteKey {
		t.Fatalf("old antigravity delete op kind = %q, want %q", oldDeleteOp.Kind, upgradeMigrationKindConfigDeleteKey)
	}
	if oldDeleteOp.Key != "agents.antigravity" {
		t.Fatalf("old antigravity delete key = %q, want agents.antigravity", oldDeleteOp.Key)
	}

	renameOp := byID["b-rename-agents-gemini-enabled"]
	if renameOp.Kind != upgradeMigrationKindConfigRenameKey {
		t.Fatalf("rename op kind = %q, want %q", renameOp.Kind, upgradeMigrationKindConfigRenameKey)
	}
	if renameOp.From != "agents.gemini.enabled" || renameOp.To != "agents.antigravity.enabled" {
		t.Fatalf("rename op from/to = %q/%q", renameOp.From, renameOp.To)
	}

	deleteOp := byID["c-delete-agents-gemini"]
	if deleteOp.Kind != upgradeMigrationKindConfigDeleteKey {
		t.Fatalf("delete op kind = %q, want %q", deleteOp.Kind, upgradeMigrationKindConfigDeleteKey)
	}
	if deleteOp.Key != "agents.gemini" {
		t.Fatalf("delete op key = %q, want agents.gemini", deleteOp.Key)
	}

	defaultOp := byID["d-set-default-agents-antigravity-enabled"]
	if defaultOp.Kind != upgradeMigrationKindConfigSetDefault {
		t.Fatalf("default op kind = %q, want %q", defaultOp.Kind, upgradeMigrationKindConfigSetDefault)
	}
	if defaultOp.Key != "agents.antigravity.enabled" || string(defaultOp.Value) != "false" {
		t.Fatalf("default op key/value = %q/%q", defaultOp.Key, string(defaultOp.Value))
	}

	replaceOp := byID["e-replace-gemini-mcp-client"]
	if replaceOp.Kind != upgradeMigrationKindConfigReplaceString {
		t.Fatalf("replace op kind = %q, want %q", replaceOp.Kind, upgradeMigrationKindConfigReplaceString)
	}
	if replaceOp.Key != "mcp.servers[].clients[]" || replaceOp.From != "gemini" || replaceOp.To != "antigravity" {
		t.Fatalf("replace op key/from/to = %q/%q/%q", replaceOp.Key, replaceOp.From, replaceOp.To)
	}

	orphanOp := byID["f-delete-orphan-gemini-md"]
	if orphanOp.Kind != upgradeMigrationKindDeleteGeneratedArtifact {
		t.Fatalf("orphan delete kind = %q, want %q", orphanOp.Kind, upgradeMigrationKindDeleteGeneratedArtifact)
	}
	if orphanOp.Path != "GEMINI.md" {
		t.Fatalf("orphan delete path = %q, want GEMINI.md", orphanOp.Path)
	}

	// All 0.10.2 ops are source-agnostic so users without a resolvable
	// source version (no pin / baseline / snapshot / manifest match) still
	// get the full migration. Re-run safety is provided by al upgrade
	// bumping the pin to 0.10.2 on success; subsequent runs resolve source
	// as 0.10.2 and skip the 0.10.2 manifest entirely (Round 3 F-3-1).
	for _, op := range byID {
		if !op.SourceAgnostic {
			t.Fatalf("op %s expected source_agnostic, got false", op.ID)
		}
	}
	if manifest.MinPriorVersion != "0.10.1" {
		t.Fatalf("min_prior_version = %q, want 0.10.1", manifest.MinPriorVersion)
	}
}

func TestMigration_0_10_2_MigratesGeminiConfigToAntigravity(t *testing.T) {
	tests := []struct {
		name        string
		geminiBlock []string
		clients     string
		wantEnabled bool
		wantClients []string
	}{
		{
			name: "enabled true preserved and legacy keys deleted",
			geminiBlock: []string{
				"[agents.gemini]",
				"enabled = true",
				`model = "gemini-custom"`,
				`reasoning_effort = "high"`,
				"",
			},
			clients:     `["claude", "gemini", "antigravity"]`,
			wantEnabled: true,
			wantClients: []string{"claude", "antigravity"},
		},
		{
			name: "enabled false preserved",
			geminiBlock: []string{
				"[agents.gemini]",
				"enabled = false",
				"",
			},
			clients:     `["claude", "gemini", "antigravity"]`,
			wantEnabled: false,
			wantClients: []string{"claude", "antigravity"},
		},
		{
			name:        "missing gemini defaults false",
			geminiBlock: nil,
			clients:     `["claude", "antigravity"]`,
			wantEnabled: false,
			wantClients: []string{"claude", "antigravity"},
		},
		{
			// F-B-5 case (a): clients=["gemini"] alone — the migration must
			// rewrite to ["antigravity"], not delete the server.
			name:        "clients sole-gemini-element becomes antigravity",
			geminiBlock: nil,
			clients:     `["gemini"]`,
			wantEnabled: false,
			wantClients: []string{"antigravity"},
		},
		{
			// F-B-5 case (b): clients=["gemini","antigravity"] — dedupe
			// must collapse the duplicate-of-existing-antigravity to a
			// single entry.
			name:        "clients dedupes pre-existing antigravity after replace",
			geminiBlock: nil,
			clients:     `["gemini", "antigravity"]`,
			wantEnabled: false,
			wantClients: []string{"antigravity"},
		},
		{
			// F-B-5 case (c): clients with no gemini — the migration must be
			// a no-op on the array (not dedupe pre-existing duplicates).
			name:        "clients without gemini is a no-op",
			geminiBlock: nil,
			clients:     `["claude", "claude"]`,
			wantEnabled: false,
			wantClients: []string{"claude", "claude"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			cfgPath := writeMigrationConfigForTest(t, root, antigravityMigrationConfigWithClients(tt.geminiBlock, tt.clients))
			// Write a pre-0.10.2 pin so the source-gated ops (a, see Round 2
			// F-B2-1) run and clear the legacy desktop `[agents.antigravity]`
			// block before the rename. This simulates a real `al upgrade`
			// from 0.10.1 to 0.10.2.
			writePinForTest(t, root, "0.10.1")
			var warn bytes.Buffer
			inst := &installer{root: root, pinVersion: "0.10.2", sys: RealSystem{}, warnWriter: &warn, prompter: PromptFuncs{}}
			if err := inst.prepareUpgradeMigrations(); err != nil {
				t.Fatalf("prepareUpgradeMigrations: %v", err)
			}
			if err := inst.runMigrations(); err != nil {
				t.Fatalf("runMigrations: %v", err)
			}

			data, err := os.ReadFile(cfgPath) // #nosec G304 -- path is constructed from test-controlled inputs.
			if err != nil {
				t.Fatalf("read migrated config: %v", err)
			}
			if strings.Contains(string(data), "gemini") {
				t.Fatalf("expected Gemini table and keys removed, got:\n%s", string(data))
			}
			cfg, err := config.ParseConfig(data, cfgPath)
			if err != nil {
				t.Fatalf("strict config parse after migration: %v\n%s", err, string(data))
			}
			if len(cfg.MCP.Servers) != 1 {
				t.Fatalf("expected one MCP server, got %d", len(cfg.MCP.Servers))
			}
			// Compare clients as a multiset, not by slice order. The replace
			// + dedupe contract is "every gemini becomes antigravity, then
			// duplicates of antigravity collapse" — the resulting slice
			// order is an implementation detail.
			assertSameStringMultiset(t, cfg.MCP.Servers[0].Clients, tt.wantClients)
			if cfg.Agents.Antigravity.Enabled == nil {
				t.Fatal("expected agents.antigravity.enabled to be set")
			}
			if *cfg.Agents.Antigravity.Enabled != tt.wantEnabled {
				t.Fatalf("agents.antigravity.enabled = %v, want %v", *cfg.Agents.Antigravity.Enabled, tt.wantEnabled)
			}

			// Idempotency: bump the pin to 0.10.2 (as a real successful
			// upgrade would) and re-run. Every op must be a no-op or
			// source-gated skip, and the user's enable value must survive
			// unchanged (F-B2-1 regression guard).
			writePinForTest(t, root, "0.10.2")
			inst2 := &installer{root: root, pinVersion: "0.10.2", sys: RealSystem{}, warnWriter: &warn, prompter: PromptFuncs{}}
			if err := inst2.prepareUpgradeMigrations(); err != nil {
				t.Fatalf("prepareUpgradeMigrations (rerun): %v", err)
			}
			if err := inst2.runMigrations(); err != nil {
				t.Fatalf("runMigrations (rerun): %v", err)
			}
			// The enable value must survive the rerun unchanged.
			rerunData, err := os.ReadFile(cfgPath) // #nosec G304 -- test-owned path.
			if err != nil {
				t.Fatalf("re-read migrated config: %v", err)
			}
			rerunCfg, err := config.ParseConfig(rerunData, cfgPath)
			if err != nil {
				t.Fatalf("strict parse after rerun: %v\n%s", err, string(rerunData))
			}
			if rerunCfg.Agents.Antigravity.Enabled == nil || *rerunCfg.Agents.Antigravity.Enabled != tt.wantEnabled {
				t.Fatalf("rerun regressed Antigravity.Enabled: want %v, got %v", tt.wantEnabled, rerunCfg.Agents.Antigravity.Enabled)
			}
			for _, id := range []string{
				"b-rename-agents-gemini-enabled",
				"c-delete-agents-gemini",
				"e-replace-gemini-mcp-client",
			} {
				if entry, ok := migrationReportEntryByID(inst2.migrationReport.Entries, id); ok {
					if entry.Status != UpgradeMigrationStatusNoop {
						t.Fatalf("rerun: expected %s to be no-op, got status=%q", id, entry.Status)
					}
				}
			}
		})
	}
}

func assertSameStringMultiset(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("clients length = %d, want %d (got=%#v want=%#v)", len(got), len(want), got, want)
	}
	gotSorted := append([]string(nil), got...)
	wantSorted := append([]string(nil), want...)
	sort.Strings(gotSorted)
	sort.Strings(wantSorted)
	if !reflect.DeepEqual(gotSorted, wantSorted) {
		t.Fatalf("clients (sorted) = %#v, want %#v", gotSorted, wantSorted)
	}
}

// TestExecuteConfigReplaceStringMigration directly exercises the
// config_replace_string executor on a real temp config so the branches
// missing from the higher-level test (idempotency, no-op key path, wrong
// leaf type, []any vs []string) are covered. F-C-4 called this out as a
// ~150-line subsystem with only one indirect integration test today.
func TestExecuteConfigReplaceStringMigration(t *testing.T) {
	t.Run("replaces matching elements in []string and dedupes new duplicates", func(t *testing.T) {
		root := t.TempDir()
		cfgPath := writeMigrationConfigForTest(t, root, strings.Join([]string{
			"[approvals]",
			`mode = "all"`,
			"",
			"[[mcp.servers]]",
			`id = "fs"`,
			"enabled = true",
			`transport = "stdio"`,
			`command = "npx"`,
			`clients = ["claude", "gemini", "antigravity"]`,
		}, "\n"))
		inst := &installer{root: root, pinVersion: "0.10.2", sys: RealSystem{}}
		changed, err := inst.executeConfigReplaceStringMigration(upgradeMigrationOperation{
			ID:   "test",
			Kind: upgradeMigrationKindConfigReplaceString,
			Key:  "mcp.servers[].clients[]",
			From: "gemini",
			To:   "antigravity",
		})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if !changed {
			t.Fatal("expected changed=true")
		}
		data, err := os.ReadFile(cfgPath) // #nosec G304 -- test-owned path.
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		// Lenient-parse and assert the clients slice — strict ParseConfig
		// requires the full required-field set which is unrelated to what
		// this unit test is checking.
		cfg, err := config.ParseConfigLenient(data, cfgPath)
		if err != nil {
			t.Fatalf("parse after migration: %v\n%s", err, string(data))
		}
		if len(cfg.MCP.Servers) != 1 {
			t.Fatalf("expected one server, got %d", len(cfg.MCP.Servers))
		}
		assertSameStringMultiset(t, cfg.MCP.Servers[0].Clients, []string{"claude", "antigravity"})
	})

	t.Run("no-op when no element matches from", func(t *testing.T) {
		root := t.TempDir()
		cfgPath := writeMigrationConfigForTest(t, root, strings.Join([]string{
			"[approvals]",
			`mode = "all"`,
			"",
			"[[mcp.servers]]",
			`id = "fs"`,
			"enabled = true",
			`transport = "stdio"`,
			`command = "npx"`,
			`clients = ["claude", "claude"]`,
		}, "\n"))
		original, err := os.ReadFile(cfgPath) // #nosec G304 -- test-owned path.
		if err != nil {
			t.Fatalf("read original: %v", err)
		}
		inst := &installer{root: root, pinVersion: "0.10.2", sys: RealSystem{}}
		changed, err := inst.executeConfigReplaceStringMigration(upgradeMigrationOperation{
			ID:   "test",
			Kind: upgradeMigrationKindConfigReplaceString,
			Key:  "mcp.servers[].clients[]",
			From: "gemini",
			To:   "antigravity",
		})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if changed {
			t.Fatal("expected changed=false for no-match case")
		}
		// The unconditional dedupe bug (pre-fix) collapsed the user's
		// intentional ["claude","claude"] duplicate. After the fix the
		// duplicate must survive untouched on the no-match path.
		current, err := os.ReadFile(cfgPath) // #nosec G304 -- test-owned path.
		if err != nil {
			t.Fatalf("read after: %v", err)
		}
		if string(current) != string(original) {
			t.Fatalf("expected no rewrite on no-match path; got:\n%s\nwant:\n%s", string(current), string(original))
		}
	})

	t.Run("idempotent re-run is no-op", func(t *testing.T) {
		root := t.TempDir()
		writeMigrationConfigForTest(t, root, strings.Join([]string{
			"[approvals]",
			`mode = "all"`,
			"",
			"[[mcp.servers]]",
			`id = "fs"`,
			"enabled = true",
			`transport = "stdio"`,
			`command = "npx"`,
			`clients = ["antigravity"]`,
		}, "\n"))
		inst := &installer{root: root, pinVersion: "0.10.2", sys: RealSystem{}}
		changed, err := inst.executeConfigReplaceStringMigration(upgradeMigrationOperation{
			ID:   "test",
			Kind: upgradeMigrationKindConfigReplaceString,
			Key:  "mcp.servers[].clients[]",
			From: "gemini",
			To:   "antigravity",
		})
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if changed {
			t.Fatal("expected no-op on already-migrated config")
		}
	})
}

// TestMigration_0_10_2_UnknownSourceUpgradePath covers the F-3-1 regression
// path: a user upgrading from 0.10.1 → 0.10.2 with no resolvable source
// (no pin / baseline / snapshot) must still complete the migration even
// when the legacy `[agents.antigravity]` desktop block + `[agents.gemini]`
// block coexist. Op `a` being source-agnostic clears the desktop block
// before op `b` renames into the same key.
func TestMigration_0_10_2_UnknownSourceUpgradePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	geminiBlock := []string{"[agents.gemini]", "enabled = true", ""}
	cfgPath := writeMigrationConfigForTest(t, root, antigravityMigrationConfigWithClients(
		geminiBlock,
		`["claude", "gemini", "antigravity"]`,
	))
	// Deliberately no pin / baseline written: source resolution must fall
	// through to "unknown" so source-agnostic ops carry the migration.
	var warn bytes.Buffer
	inst := &installer{root: root, pinVersion: "0.10.2", sys: RealSystem{}, warnWriter: &warn, prompter: PromptFuncs{}}
	if err := inst.prepareUpgradeMigrations(); err != nil {
		t.Fatalf("prepareUpgradeMigrations (unknown source): %v", err)
	}
	if err := inst.runMigrations(); err != nil {
		t.Fatalf("runMigrations (unknown source) must succeed even with legacy [agents.antigravity] present: %v", err)
	}
	data, err := os.ReadFile(cfgPath) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	// Explicit assertion (F-4-4): strict ParseConfig below already rejects
	// surviving [agents.gemini], but pinning the absence directly makes a
	// future schema-relaxation that silently re-admits the table impossible
	// to land without flipping this test red.
	if strings.Contains(string(data), "[agents.gemini]") {
		t.Fatalf("expected [agents.gemini] table to be removed by migration; got:\n%s", string(data))
	}
	cfg, err := config.ParseConfig(data, cfgPath)
	if err != nil {
		t.Fatalf("strict parse after unknown-source migration: %v\n%s", err, string(data))
	}
	if cfg.Agents.Antigravity.Enabled == nil || !*cfg.Agents.Antigravity.Enabled {
		t.Fatalf("expected agents.antigravity.enabled = true after unknown-source migration, got %v", cfg.Agents.Antigravity.Enabled)
	}
}

func TestMigration_0_10_2_UnknownSourceKeepsCurrentAntigravity(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cfgPath := writeMigrationConfigForTest(t, root, strings.Join([]string{
		"[approvals]",
		`mode = "all"`,
		"",
		"[agents.claude]",
		"enabled = false",
		"",
		"[agents.claude_vscode]",
		"enabled = false",
		"",
		"[agents.codex]",
		"enabled = false",
		"",
		"[agents.vscode]",
		"enabled = false",
		"",
		"[agents.antigravity]",
		"enabled = true",
		"",
		"[agents.copilot_cli]",
		"enabled = false",
		"",
		"[[mcp.servers]]",
		`id = "example"`,
		"enabled = true",
		`transport = "stdio"`,
		`command = "npx"`,
		`clients = ["antigravity"]`,
	}, "\n"))

	var warn bytes.Buffer
	inst := &installer{root: root, pinVersion: "0.10.2", sys: RealSystem{}, warnWriter: &warn, prompter: PromptFuncs{}}
	if err := inst.prepareUpgradeMigrations(); err != nil {
		t.Fatalf("prepareUpgradeMigrations: %v", err)
	}
	if err := inst.runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	data, err := os.ReadFile(cfgPath) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	cfg, err := config.ParseConfig(data, cfgPath)
	if err != nil {
		t.Fatalf("strict parse after migration: %v\n%s", err, string(data))
	}
	if cfg.Agents.Antigravity.Enabled == nil || !*cfg.Agents.Antigravity.Enabled {
		t.Fatalf("expected current agents.antigravity.enabled = true to survive unknown-source migration, got %v", cfg.Agents.Antigravity.Enabled)
	}
	if entry, ok := migrationReportEntryByID(inst.migrationReport.Entries, "a-delete-old-agents-antigravity"); !ok || entry.Status != UpgradeMigrationStatusNoop {
		t.Fatalf("expected old antigravity delete to be no-op without legacy Gemini, got %#v ok=%v", entry, ok)
	}
}

func TestUpgradeTransactionRunsMigrationsBeforePinBump(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.10.1"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	cfgPath := filepath.Join(root, ".agent-layer", "config.toml")
	legacyConfig := antigravityMigrationConfigWithClients(
		[]string{"[agents.gemini]", "enabled = true", ""},
		`["gemini"]`,
	)
	if err := os.WriteFile(cfgPath, []byte(legacyConfig), 0o600); err != nil {
		t.Fatalf("seed legacy config: %v", err)
	}

	sys := &recordWriteSystem{base: RealSystem{}}
	if err := Run(root, Options{System: sys, Overwrite: true, Prompter: autoApprovePrompter(), PinVersion: "0.10.2"}); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	configWriteIndex := sys.firstWriteIndex(cfgPath)
	pinWriteIndex := sys.firstWriteIndex(filepath.Join(root, ".agent-layer", "al.version"))
	if configWriteIndex == -1 {
		t.Fatalf("expected config migration write, writes = %#v", sys.writes)
	}
	if pinWriteIndex == -1 {
		t.Fatalf("expected pin write, writes = %#v", sys.writes)
	}
	if configWriteIndex > pinWriteIndex {
		t.Fatalf("expected migrations to write config before pin bump, writes = %#v", sys.writes)
	}
}

func TestRun_DevUpgradeRunsLatestMigrationsWithoutWritingPin(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cfgPath := writeMigrationConfigForTest(t, root, antigravityMigrationConfigWithClients(
		[]string{"[agents.gemini]", "enabled = true", ""},
		`["gemini"]`,
	))

	if err := Run(root, Options{System: RealSystem{}, Overwrite: true, Prompter: autoApprovePrompter()}); err != nil {
		t.Fatalf("dev upgrade: %v", err)
	}

	data, err := os.ReadFile(cfgPath) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	if strings.Contains(string(data), "gemini") {
		t.Fatalf("expected dev upgrade to apply latest Gemini migration, got:\n%s", string(data))
	}
	cfg, err := config.ParseConfig(data, cfgPath)
	if err != nil {
		t.Fatalf("strict parse after dev upgrade migration: %v\n%s", err, string(data))
	}
	if cfg.Agents.Antigravity.Enabled == nil || !*cfg.Agents.Antigravity.Enabled {
		t.Fatalf("expected agents.antigravity.enabled = true after dev upgrade, got %v", cfg.Agents.Antigravity.Enabled)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".agent-layer", "al.version")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("dev upgrade should not write al.version, statErr=%v", statErr)
	}
}

func TestPlanUpgradeMigrations_UnpinnedMCPGeminiClientTriggersLatestManifest(t *testing.T) {
	root := t.TempDir()
	writeMigrationConfigForTest(t, root, antigravityMigrationConfigWithClients(
		nil,
		`["gemini"]`,
	))
	withMigrationManifestChainOverride(t, map[string]string{
		"0.7.0": `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "replace-gemini-client",
      "kind": "config_replace_string",
      "rationale": "Replace legacy Gemini MCP client name",
      "source_agnostic": true,
      "key": "mcp.servers[].clients[]",
      "from": "gemini",
      "to": "antigravity"
    }
  ]
}`,
	})

	inst := &installer{root: root, sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}
	if plan.report.TargetVersion != "0.7.0" {
		t.Fatalf("target version = %q, want 0.7.0", plan.report.TargetVersion)
	}
	if plan.report.SourceVersionOrigin != UpgradeMigrationSourceUnknown {
		t.Fatalf("source origin = %q, want unknown", plan.report.SourceVersionOrigin)
	}
	if len(plan.executable) != 1 || plan.executable[0].ID != "replace-gemini-client" {
		t.Fatalf("executable migrations = %#v, want replace-gemini-client", plan.executable)
	}
}

func TestPlanUpgradeMigrations_UnpinnedNoLegacyTriggerSkipsManifest(t *testing.T) {
	root := t.TempDir()
	writeMigrationConfigForTest(t, root, antigravityMigrationConfigWithClients(
		nil,
		`["antigravity"]`,
	))
	withMigrationManifestChainOverride(t, map[string]string{
		"0.7.0": `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "would-run-if-triggered",
      "kind": "delete_file",
      "rationale": "Should not run without source evidence or legacy config",
      "source_agnostic": true,
      "path": "legacy.txt"
    }
  ]
}`,
	})

	inst := &installer{root: root, sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}
	if plan.report.TargetVersion != "" {
		t.Fatalf("target version = %q, want empty", plan.report.TargetVersion)
	}
	if len(plan.report.Entries) != 0 || len(plan.executable) != 0 {
		t.Fatalf("expected no migration entries or executable ops, got entries=%#v executable=%#v", plan.report.Entries, plan.executable)
	}
}

func TestPlanUpgradeMigrations_UnpinnedTriggerReadError(t *testing.T) {
	root := t.TempDir()
	configPath := writeMigrationConfigForTest(t, root, "[agents]\n")
	fault := newFaultSystem(RealSystem{})
	fault.readErrs[normalizePath(configPath)] = errors.New("config read boom")

	inst := &installer{root: root, sys: fault}
	_, err := inst.planUpgradeMigrations()
	if err == nil || !strings.Contains(err.Error(), "config read boom") {
		t.Fatalf("expected config read error, got %v", err)
	}
}

func TestUpgradeMigrationTargetVersion_UnpinnedKnownSourceWithoutManifests(t *testing.T) {
	withMigrationManifestChainOverride(t, map[string]string{})
	inst := &installer{root: t.TempDir(), sys: RealSystem{}}

	got, err := inst.upgradeMigrationTargetVersion(sourceVersionResolution{
		version: "0.6.0",
		origin:  UpgradeMigrationSourcePin,
	})
	if err != nil {
		t.Fatalf("upgradeMigrationTargetVersion: %v", err)
	}
	if got != "" {
		t.Fatalf("target version = %q, want empty when no manifests exist", got)
	}
}

func TestUpgradeMigrationTargetVersion_ExplicitPinBypassesTriggerRead(t *testing.T) {
	root := t.TempDir()
	configPath := writeMigrationConfigForTest(t, root, "[agents]\n")
	fault := newFaultSystem(RealSystem{})
	fault.readErrs[normalizePath(configPath)] = errors.New("config read boom")
	inst := &installer{root: root, pinVersion: "0.7.0", sys: fault}

	got, err := inst.upgradeMigrationTargetVersion(sourceVersionResolution{origin: UpgradeMigrationSourceUnknown})
	if err != nil {
		t.Fatalf("upgradeMigrationTargetVersion: %v", err)
	}
	if got != "0.7.0" {
		t.Fatalf("target version = %q, want 0.7.0", got)
	}
}

func TestHasLegacyGeminiMCPClient(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{
			name: "legacy client present",
			data: "[[mcp.servers]]\nclients = [\"gemini\"]\n",
			want: true,
		},
		{
			name: "legacy client absent",
			data: "[[mcp.servers]]\nclients = [\"antigravity\"]\n",
			want: false,
		},
		{
			name: "invalid toml",
			data: "clients = [\n",
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasLegacyGeminiMCPClient([]byte(tc.data)); got != tc.want {
				t.Fatalf("hasLegacyGeminiMCPClient = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReplaceStringInMigrationArray_StringSliceDedupe(t *testing.T) {
	t.Run("replacement dedupes only new duplicates", func(t *testing.T) {
		updated, changed, err := replaceStringInMigrationArray(
			[]string{"claude", "gemini", "antigravity", "gemini"},
			nil,
			"gemini",
			"antigravity",
			"clients",
		)
		if err != nil {
			t.Fatalf("replaceStringInMigrationArray: %v", err)
		}
		if !changed {
			t.Fatal("expected changed=true")
		}
		got, ok := updated.([]string)
		if !ok {
			t.Fatalf("updated value type = %T, want []string", updated)
		}
		if want := []string{"claude", "antigravity"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("updated clients = %#v, want %#v", got, want)
		}
	})

	t.Run("no match preserves pre-existing duplicates", func(t *testing.T) {
		updated, changed, err := replaceStringInMigrationArray(
			[]string{"claude", "claude"},
			nil,
			"gemini",
			"antigravity",
			"clients",
		)
		if err != nil {
			t.Fatalf("replaceStringInMigrationArray: %v", err)
		}
		if changed {
			t.Fatal("expected changed=false")
		}
		got, ok := updated.([]string)
		if !ok {
			t.Fatalf("updated value type = %T, want []string", updated)
		}
		if want := []string{"claude", "claude"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("updated clients = %#v, want %#v", got, want)
		}
	})

	t.Run("string arrays cannot be traversed", func(t *testing.T) {
		_, _, err := replaceStringInMigrationArray(
			[]string{"gemini"},
			[]configValuePathSegment{{name: "nested"}},
			"gemini",
			"antigravity",
			"clients",
		)
		if err == nil || !strings.Contains(err.Error(), "traverses string array") {
			t.Fatalf("expected traversal error, got %v", err)
		}
	})
}

// TestDeleteGeneratedArtifact_PreservesHandAuthoredFile pins F-B2-2: the
// `delete_generated_artifact` migration must only delete files Agent Layer
// itself produced (carrying the `GENERATED FILE` watermark). A user's
// hand-authored file at the same path must survive `al upgrade`.
func TestDeleteGeneratedArtifact_PreservesHandAuthoredFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	handAuthored := filepath.Join(root, "GEMINI.md")
	if err := os.WriteFile(handAuthored, []byte("# my Gemini notes\nNot generated, do not delete.\n"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	inst := &installer{root: root, pinVersion: "0.10.2", sys: RealSystem{}}
	changed, err := inst.executeDeleteMigration("GEMINI.md", true)
	if err != nil {
		t.Fatalf("execute delete: %v", err)
	}
	if changed {
		t.Fatal("expected no deletion for hand-authored file lacking the GENERATED FILE marker")
	}
	if _, err := os.Stat(handAuthored); err != nil {
		t.Fatalf("hand-authored file must survive: %v", err)
	}
}

// TestDeleteGeneratedArtifact_PreservesDirectory pins that watermarked artifact
// deletion never recursively removes a directory without explicit generated
// ownership proof.
func TestDeleteGeneratedArtifact_PreservesDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dirPath := filepath.Join(root, "GEMINI.md")
	if err := os.MkdirAll(dirPath, 0o700); err != nil {
		t.Fatalf("seed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "notes.md"), []byte("user notes\n"), 0o600); err != nil {
		t.Fatalf("seed nested file: %v", err)
	}
	var warn bytes.Buffer
	inst := &installer{root: root, pinVersion: "0.10.2", sys: RealSystem{}, warnWriter: &warn}
	changed, err := inst.executeDeleteMigration("GEMINI.md", true)
	if err != nil {
		t.Fatalf("execute delete: %v", err)
	}
	if changed {
		t.Fatal("expected no deletion for directory lacking explicit generated ownership proof")
	}
	if _, err := os.Stat(filepath.Join(dirPath, "notes.md")); err != nil {
		t.Fatalf("directory contents must survive: %v", err)
	}
	if !strings.Contains(warn.String(), "refuses to remove directories") {
		t.Fatalf("expected directory-preservation warning, got %q", warn.String())
	}
}

// TestDeleteGeneratedArtifact_DeletesWatermarkedFile is the positive case:
// when the watermark is present, the file is deleted (this is the normal
// orphan-cleanup path).
func TestDeleteGeneratedArtifact_DeletesWatermarkedFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	generated := filepath.Join(root, "GEMINI.md")
	if err := os.WriteFile(generated, []byte("<!--\n  GENERATED FILE\n  Source: .agent-layer/instructions/*.md\n-->\n\nbody\n"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	inst := &installer{root: root, pinVersion: "0.10.2", sys: RealSystem{}}
	changed, err := inst.executeDeleteMigration("GEMINI.md", true)
	if err != nil {
		t.Fatalf("execute delete: %v", err)
	}
	if !changed {
		t.Fatal("expected watermarked file to be deleted")
	}
	if _, err := os.Stat(generated); !os.IsNotExist(err) {
		t.Fatalf("expected file removed, stat err = %v", err)
	}
}

// TestConfigMigrationFromOperation_SurfacesAllConfigKinds locks in F-A-1:
// every config-kind migration operation must produce a ConfigKeyMigration so
// the upgrade preview tells the user what the migration will change. Before
// this test, config_delete_key was silently omitted from the preview even
// though deletes are the most destructive config mutation.
func TestConfigMigrationFromOperation_SurfacesAllConfigKinds(t *testing.T) {
	cases := []struct {
		name    string
		op      upgradeMigrationOperation
		wantKey string
		wantTo  string
	}{
		{
			name:    "delete_key surfaces with (existing)→(removed)",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindConfigDeleteKey, Key: "agents.gemini"},
			wantKey: "agents.gemini",
			wantTo:  "(removed)",
		},
		{
			name:    "rename_key surfaces from→to",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindConfigRenameKey, From: "agents.gemini.enabled", To: "agents.antigravity.enabled"},
			wantKey: "agents.gemini.enabled",
			wantTo:  "agents.antigravity.enabled",
		},
		{
			name:    "replace_string surfaces from→to",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindConfigReplaceString, Key: "mcp.servers[].clients[]", From: "gemini", To: "antigravity"},
			wantKey: "mcp.servers[].clients[]",
			wantTo:  "antigravity",
		},
		{
			name:    "set_default surfaces (unset)→value",
			op:      upgradeMigrationOperation{Kind: upgradeMigrationKindConfigSetDefault, Key: "agents.antigravity.enabled", Value: json.RawMessage(`false`)},
			wantKey: "agents.antigravity.enabled",
			wantTo:  "false",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := configMigrationFromOperation(tc.op)
			if !ok {
				t.Fatalf("expected configMigrationFromOperation to surface %s", tc.op.Kind)
			}
			if got.Key != tc.wantKey {
				t.Fatalf("key = %q, want %q", got.Key, tc.wantKey)
			}
			if got.To != tc.wantTo {
				t.Fatalf("to = %q, want %q", got.To, tc.wantTo)
			}
		})
	}
}

// antigravityMigrationConfigWithClients builds a complete 0.10.2-compatible
// config fixture with a pluggable `[agents.gemini]` block and `clients` array
// so the table-driven migration tests can vary just those two surfaces.
func antigravityMigrationConfigWithClients(geminiBlock []string, clients string) string {
	lines := []string{
		"[approvals]",
		`mode = "all"`,
		"",
	}
	lines = append(lines, geminiBlock...)
	lines = append(lines,
		"[agents.claude]",
		"enabled = false",
		"",
		"[agents.claude_vscode]",
		"enabled = false",
		"",
		"[agents.codex]",
		"enabled = false",
		"",
		"[agents.vscode]",
		"enabled = false",
		"",
		"[agents.antigravity]",
		"enabled = false",
		"",
		"[agents.copilot_cli]",
		"enabled = false",
		"",
		"[[mcp.servers]]",
		`id = "filesystem"`,
		"enabled = true",
		`transport = "stdio"`,
		`command = "npx"`,
		`clients = `+clients,
	)
	return strings.Join(lines, "\n")
}

type recordWriteSystem struct {
	base   System
	writes []string
}

func (r *recordWriteSystem) Lstat(name string) (os.FileInfo, error) {
	return r.base.Lstat(name)
}

func (r *recordWriteSystem) Stat(name string) (os.FileInfo, error) {
	return r.base.Stat(name)
}

func (r *recordWriteSystem) ReadFile(name string) ([]byte, error) {
	return r.base.ReadFile(name)
}

func (r *recordWriteSystem) Readlink(name string) (string, error) {
	return r.base.Readlink(name)
}

func (r *recordWriteSystem) LookupEnv(key string) (string, bool) {
	return r.base.LookupEnv(key)
}

func (r *recordWriteSystem) MkdirAll(path string, perm os.FileMode) error {
	return r.base.MkdirAll(path, perm)
}

func (r *recordWriteSystem) RemoveAll(path string) error {
	return r.base.RemoveAll(path)
}

func (r *recordWriteSystem) Rename(oldpath string, newpath string) error {
	return r.base.Rename(oldpath, newpath)
}

func (r *recordWriteSystem) Symlink(oldname string, newname string) error {
	return r.base.Symlink(oldname, newname)
}

func (r *recordWriteSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return r.base.WalkDir(root, fn)
}

func (r *recordWriteSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	r.writes = append(r.writes, filepath.Clean(filename))
	return r.base.WriteFileAtomic(filename, data, perm)
}

func (r *recordWriteSystem) firstWriteIndex(filename string) int {
	clean := filepath.Clean(filename)
	for idx, write := range r.writes {
		if write == clean {
			return idx
		}
	}
	return -1
}

func TestMigration_0_9_0_RenamesSlashCommandsAndMigratesFlatSkills(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, ".agent-layer", "slash-commands")
	legacyFile := filepath.Join(legacyDir, "custom.md")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.WriteFile(legacyFile, []byte("custom skill\n"), 0o600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	var warn bytes.Buffer
	inst := &installer{root: root, pinVersion: "0.9.0", sys: RealSystem{}, warnWriter: &warn}
	if err := inst.prepareUpgradeMigrations(); err != nil {
		t.Fatalf("prepareUpgradeMigrations: %v", err)
	}
	if err := inst.runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	if _, err := os.Stat(legacyDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected legacy dir removed, stat err = %v", err)
	}
	// After rename (slash-commands -> skills) and skills format migration,
	// custom.md should be at custom/SKILL.md.
	migratedFile := filepath.Join(root, ".agent-layer", "skills", "custom", "SKILL.md")
	got, err := os.ReadFile(migratedFile) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read migrated file: %v", err)
	}
	if string(got) != "custom skill\n" {
		t.Fatalf("unexpected migrated content: %q", string(got))
	}
	// Flat file should no longer exist.
	flatFile := filepath.Join(root, ".agent-layer", "skills", "custom.md")
	if _, err := os.Stat(flatFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected flat file removed, stat err = %v", err)
	}
	if entry, ok := migrationReportEntryByID(inst.migrationReport.Entries, "c-rename-slash-commands-dir-to-skills"); !ok || entry.Status != UpgradeMigrationStatusApplied {
		t.Fatalf("expected applied rename-dir migration entry, got %#v ok=%v", entry, ok)
	}
	if entry, ok := migrationReportEntryByID(inst.migrationReport.Entries, "d-migrate-all-skills-to-directory-format"); !ok || entry.Status != UpgradeMigrationStatusApplied {
		t.Fatalf("expected applied migrate_skills_format entry, got %#v ok=%v", entry, ok)
	}

	// Re-running the same target migration should no-op without error.
	var warnSecond bytes.Buffer
	instSecond := &installer{root: root, pinVersion: "0.9.0", sys: RealSystem{}, warnWriter: &warnSecond}
	if err := instSecond.prepareUpgradeMigrations(); err != nil {
		t.Fatalf("prepareUpgradeMigrations (second): %v", err)
	}
	if err := instSecond.runMigrations(); err != nil {
		t.Fatalf("runMigrations (second): %v", err)
	}
	if entry, ok := migrationReportEntryByID(instSecond.migrationReport.Entries, "c-rename-slash-commands-dir-to-skills"); !ok || entry.Status != UpgradeMigrationStatusNoop {
		t.Fatalf("expected no-op rename-dir migration entry on second run, got %#v ok=%v", entry, ok)
	}
	if entry, ok := migrationReportEntryByID(instSecond.migrationReport.Entries, "d-migrate-all-skills-to-directory-format"); !ok || entry.Status != UpgradeMigrationStatusNoop {
		t.Fatalf("expected no-op migrate_skills_format entry on second run, got %#v ok=%v", entry, ok)
	}
}

func TestMigration_0_9_0_FailsWhenSlashCommandsAndSkillsBothExist(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, ".agent-layer", "slash-commands")
	newDir := filepath.Join(root, ".agent-layer", "skills")
	if err := os.MkdirAll(legacyDir, 0o700); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	if err := os.MkdirAll(newDir, 0o700); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "alpha.md"), []byte("legacy\n"), 0o600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "alpha.md"), []byte("new\n"), 0o600); err != nil {
		t.Fatalf("write new file: %v", err)
	}

	var warn bytes.Buffer
	inst := &installer{root: root, pinVersion: "0.9.0", sys: RealSystem{}, warnWriter: &warn}
	if err := inst.prepareUpgradeMigrations(); err != nil {
		t.Fatalf("prepareUpgradeMigrations: %v", err)
	}
	err := inst.runMigrations()
	if err == nil || !containsAll(err.Error(), "execute migration", "c-rename-slash-commands-dir-to-skills", "target already exists") {
		t.Fatalf("expected fail-loud rename collision error, got %v", err)
	}
}

func TestMigration_0_9_0_ProducesValidConfig(t *testing.T) {
	// Start with a realistic pre-migration config that uses the legacy
	// kebab-case key [agents.claude-vscode]. After rename,
	// the result must pass strict config parsing (no empty legacy table).
	legacyConfig := strings.Join([]string{
		"[approvals]",
		`mode = "all"`,
		"",
		"[agents.antigravity]",
		"enabled = false",
		"",
		"[agents.claude]",
		"enabled = true",
		"",
		"[agents.claude-vscode]",
		"enabled = true",
		"",
		"[agents.codex]",
		"enabled = false",
		"",
		"[agents.vscode]",
		"enabled = false",
		"",
		"[agents.copilot_cli]",
		"enabled = false",
		"",
		"[warnings]",
		"instruction_token_threshold = 10000",
		"mcp_server_threshold = 5",
		"mcp_tools_total_threshold = 40",
		"mcp_server_tools_threshold = 25",
		"mcp_schema_tokens_total_threshold = 25000",
		"mcp_schema_tokens_server_threshold = 10000",
	}, "\n")

	root := t.TempDir()
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfgPath := filepath.Join(alDir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(legacyConfig), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}

	// Execute the rename migration.
	changed, err := inst.executeConfigRenameKeyMigration(
		"agents.claude-vscode.enabled",
		"agents.claude_vscode.enabled",
	)
	if err != nil {
		t.Fatalf("rename migration: %v", err)
	}
	if !changed {
		t.Fatal("expected rename to apply")
	}

	// Read resulting config and verify it passes strict parsing.
	data, err := os.ReadFile(cfgPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg, err := config.ParseConfig(data, cfgPath)
	if err != nil {
		t.Fatalf("strict config parse failed after migration: %v", err)
	}
	if cfg.Agents.ClaudeVSCode.Enabled == nil || !*cfg.Agents.ClaudeVSCode.Enabled {
		t.Fatal("expected agents.claude_vscode.enabled = true after migration")
	}
}

func writeMigrationConfigForTest(t *testing.T, root string, content string) string {
	t.Helper()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	cfgPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func writePinForTest(t *testing.T, root string, version string) {
	t.Helper()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o700); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte(version+"\n"), 0o600); err != nil {
		t.Fatalf("write pin: %v", err)
	}
}

func mapKeys(m map[string]upgradeMigrationOperation) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func migrationReportEntryByID(entries []UpgradeMigrationEntry, id string) (UpgradeMigrationEntry, bool) {
	for _, entry := range entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return UpgradeMigrationEntry{}, false
}

func TestExecuteMigrateSkillsFormat_BasicMigration(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha.md"), []byte("alpha content\n"), 0o600); err != nil {
		t.Fatalf("write alpha: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "beta.md"), []byte("beta content\n"), 0o600); err != nil {
		t.Fatalf("write beta: %v", err)
	}

	var warn bytes.Buffer
	inst := &installer{root: root, sys: RealSystem{}, warnWriter: &warn, prompter: PromptFuncs{}}
	changed, err := inst.executeMigrateSkillsFormat(".agent-layer/skills")
	if err != nil {
		t.Fatalf("executeMigrateSkillsFormat: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to report changed")
	}
	// Verify flat files were moved to directory format.
	for _, name := range []string{"alpha", "beta"} {
		data, readErr := os.ReadFile(filepath.Join(skillsDir, name, "SKILL.md")) // #nosec G304 -- path is constructed from test-controlled inputs.
		if readErr != nil {
			t.Fatalf("read %s/SKILL.md: %v", name, readErr)
		}
		if string(data) != name+" content\n" {
			t.Fatalf("unexpected %s content: %q", name, string(data))
		}
		if _, statErr := os.Stat(filepath.Join(skillsDir, name+".md")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("expected flat file %s.md to be removed, stat err = %v", name, statErr)
		}
	}

	// Verify post-migration success message.
	warnOut := warn.String()
	if !strings.Contains(warnOut, "Migrated 2 skill(s) to directory format") {
		t.Errorf("expected success message with count, got:\n%s", warnOut)
	}
	if !strings.Contains(warnOut, "alpha.md  ->  alpha/SKILL.md") {
		t.Errorf("expected alpha listed in success message, got:\n%s", warnOut)
	}
	if !strings.Contains(warnOut, "beta.md  ->  beta/SKILL.md") {
		t.Errorf("expected beta listed in success message, got:\n%s", warnOut)
	}
	if !strings.Contains(warnOut, "Skills migration complete.") {
		t.Errorf("expected completion line in success message, got:\n%s", warnOut)
	}
}

func TestExecuteMigrateSkillsFormat_NoFlatFiles(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "alpha"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha", "SKILL.md"), []byte("content\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var warn bytes.Buffer
	inst := &installer{root: root, sys: RealSystem{}, warnWriter: &warn, prompter: PromptFuncs{}}
	changed, err := inst.executeMigrateSkillsFormat(".agent-layer/skills")
	if err != nil {
		t.Fatalf("executeMigrateSkillsFormat: %v", err)
	}
	if changed {
		t.Fatal("expected no-op when no flat files exist")
	}
}

func TestExecuteMigrateSkillsFormat_SkillsDirMissing(t *testing.T) {
	root := t.TempDir()
	var warn bytes.Buffer
	inst := &installer{root: root, sys: RealSystem{}, warnWriter: &warn, prompter: PromptFuncs{}}
	changed, err := inst.executeMigrateSkillsFormat(".agent-layer/skills")
	if err != nil {
		t.Fatalf("executeMigrateSkillsFormat: %v", err)
	}
	if changed {
		t.Fatal("expected no-op when skills dir is absent")
	}
}

func TestExecuteMigrateSkillsFormat_ConflictDifferentContent(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "alpha"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Flat file with one content
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha.md"), []byte("flat content\n"), 0o600); err != nil {
		t.Fatalf("write flat: %v", err)
	}
	// Directory file with different content
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha", "SKILL.md"), []byte("dir content\n"), 0o600); err != nil {
		t.Fatalf("write dir: %v", err)
	}

	var warn bytes.Buffer
	inst := &installer{root: root, sys: RealSystem{}, warnWriter: &warn, prompter: PromptFuncs{}}
	_, err := inst.executeMigrateSkillsFormat(".agent-layer/skills")
	if err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestExecuteMigrateSkillsFormat_DuplicateContentCleansUp(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "alpha"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "same content\n"
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write flat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha", "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write dir: %v", err)
	}

	var warn bytes.Buffer
	inst := &installer{root: root, sys: RealSystem{}, warnWriter: &warn, prompter: PromptFuncs{}}
	changed, err := inst.executeMigrateSkillsFormat(".agent-layer/skills")
	if err != nil {
		t.Fatalf("executeMigrateSkillsFormat: %v", err)
	}
	if !changed {
		t.Fatal("expected changed (flat file removed)")
	}
	// Flat file should be removed.
	if _, statErr := os.Stat(filepath.Join(skillsDir, "alpha.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected flat file removed, stat err = %v", statErr)
	}
	// Directory file should still exist.
	data, readErr := os.ReadFile(filepath.Join(skillsDir, "alpha", "SKILL.md")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if readErr != nil {
		t.Fatalf("read dir file: %v", readErr)
	}
	if string(data) != content {
		t.Fatalf("unexpected dir content: %q", string(data))
	}
	// Duplicate cleanup: success message should show completion without the
	// "Migrated N skill(s)" header (nothing was moved to a new location).
	warnOut := warn.String()
	if strings.Contains(warnOut, "Migrated") {
		t.Errorf("duplicate cleanup should not show 'Migrated' header, got:\n%s", warnOut)
	}
	if !strings.Contains(warnOut, "Skills migration complete.") {
		t.Errorf("expected completion line for duplicate cleanup, got:\n%s", warnOut)
	}
}

func TestExecuteMigrateSkillsFormat_UserCancels(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha.md"), []byte("content\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var warn bytes.Buffer
	prompter := PromptFuncs{
		ConfirmSkillsMigrationFunc: func(flatSkills []string, conflicts []SkillsMigrationConflict) (bool, error) {
			return false, nil // user declines
		},
	}
	inst := &installer{root: root, sys: RealSystem{}, warnWriter: &warn, prompter: prompter}
	_, err := inst.executeMigrateSkillsFormat(".agent-layer/skills")
	if err == nil || !strings.Contains(err.Error(), "declined by user") {
		t.Fatalf("expected decline error, got %v", err)
	}
	// Flat file should still exist.
	if _, statErr := os.Stat(filepath.Join(skillsDir, "alpha.md")); statErr != nil {
		t.Fatalf("flat file should still exist after decline: %v", statErr)
	}
	// Success message must NOT appear when migration is declined.
	if strings.Contains(warn.String(), "Skills migration complete.") {
		t.Errorf("success message should not appear when migration is declined")
	}
}

func TestPreflightSkillsMigration_DetectsConflicts(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "alpha"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha.md"), []byte("flat\n"), 0o600); err != nil {
		t.Fatalf("write flat: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "alpha", "SKILL.md"), []byte("dir\n"), 0o600); err != nil {
		t.Fatalf("write dir: %v", err)
	}

	count, conflicts, err := preflightSkillsMigration(RealSystem{}, skillsDir)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 flat file, got %d", count)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d", len(conflicts))
	}
	if conflicts[0].SkillName != "alpha" {
		t.Fatalf("expected conflict for alpha, got %q", conflicts[0].SkillName)
	}
}

func TestPreflightSkillsMigration_CountsFlatSkills(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, "skills")
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "a.md"), []byte("a\n"), 0o600); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "b.md"), []byte("b\n"), 0o600); err != nil {
		t.Fatalf("write b: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(skillsDir, "c"), 0o700); err != nil {
		t.Fatalf("mkdir c: %v", err)
	}

	count, conflicts, err := preflightSkillsMigration(RealSystem{}, skillsDir)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 flat files, got %d", count)
	}
	if len(conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %d", len(conflicts))
	}
}

func TestPreflightSkillsMigration_PathNotDirectory(t *testing.T) {
	root := t.TempDir()
	skillsPath := filepath.Join(root, "skills")
	if err := os.WriteFile(skillsPath, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("write skills path: %v", err)
	}

	_, _, err := preflightSkillsMigration(RealSystem{}, skillsPath)
	if err == nil {
		t.Fatal("expected error when skills path is not a directory")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected non-directory error, got %v", err)
	}
}

func TestExecuteMigrateSkillsFormat_PathNotDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	skillsPath := filepath.Join(root, ".agent-layer", "skills")
	if err := os.WriteFile(skillsPath, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatalf("write skills path: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}, prompter: PromptFuncs{}}
	_, err := inst.executeMigrateSkillsFormat(".agent-layer/skills")
	if err == nil {
		t.Fatal("expected error when skills path is not a directory")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected non-directory error, got %v", err)
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
	if !containsString(versions, "0.8.8") {
		t.Fatalf("expected 0.8.8 in versions, got %v", versions)
	}
	if !containsString(versions, "0.9.0") {
		t.Fatalf("expected 0.9.0 in versions, got %v", versions)
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
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o700); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.6.0\n"), 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o700); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.6.0\n"), 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o700); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	// Source is 0.5.0 — older than 0.6.1's min_prior_version (0.6.0) but
	// the agnostic op should still execute. The non-agnostic op in 0.6.1
	// should be skipped as source-too-old.
	if err := os.WriteFile(pinPath, []byte("0.5.0\n"), 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o700); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.6.0\n"), 0o600); err != nil {
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

// skillsMigrationManifest is the test manifest used by all skills-migration
// pre-flight tests. It includes a config rename (to verify it is NOT applied
// when the pre-flight aborts) and the migrate_skills_format operation.
const skillsMigrationManifest = `{
  "schema_version": 1,
  "target_version": "0.9.0",
  "min_prior_version": "0.8.0",
  "operations": [
    {
      "id": "a-rename-config-key",
      "kind": "config_rename_key",
      "rationale": "Normalize config key",
      "from": "agents.claude-vscode.enabled",
      "to": "agents.claude_vscode.enabled",
      "source_agnostic": true
    },
    {
      "id": "d-migrate-all-skills-to-directory-format",
      "kind": "migrate_skills_format",
      "path": ".agent-layer/skills",
      "rationale": "Migrate flat-format skills to directory format.",
      "source_agnostic": true
    }
  ]
}`

// TestRun_SkillsMigrationDeclinedBeforeMutations verifies that when the user
// declines the skills-format migration, Run() returns an error, the exact
// warning banner was printed, and no disk mutations have occurred. The
// pre-flight confirmation runs before the upgrade transaction, so declining
// must leave the repository completely untouched.
func TestRun_SkillsMigrationDeclinedBeforeMutations(t *testing.T) {
	root := t.TempDir()

	// Seed the repo at version 0.8.8.
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.8.8"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	// Add a flat-format user-authored skill (simulates a pre-upgrade state).
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	flatSkillPath := filepath.Join(skillsDir, "my-custom-skill.md")
	flatContent := []byte("---\ndescription: Custom skill\n---\nDo something custom.\n")
	if err := os.WriteFile(flatSkillPath, flatContent, 0o600); err != nil {
		t.Fatalf("write flat skill: %v", err)
	}

	// Capture pre-upgrade file state for mutation detection.
	versionPath := filepath.Join(root, ".agent-layer", "al.version")
	preVersionContent, err := os.ReadFile(versionPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read pre-upgrade version file: %v", err)
	}
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	preConfigContent, err := os.ReadFile(configPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read pre-upgrade config: %v", err)
	}

	withMigrationManifestOverride(t, "0.9.0", skillsMigrationManifest)

	// Track exactly what the prompter callback receives.
	var promptedSkills []string
	var promptedConflicts []SkillsMigrationConflict
	prompter := PromptFuncs{
		OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return true, nil },
		OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return true, nil },
		OverwritePreviewFunc:          func(DiffPreview) (bool, error) { return true, nil },
		DeleteUnknownAllFunc:          func([]string) (bool, error) { return true, nil },
		DeleteUnknownFunc:             func(string) (bool, error) { return true, nil },
		ConfirmSkillsMigrationFunc: func(flatSkills []string, conflicts []SkillsMigrationConflict) (bool, error) {
			promptedSkills = flatSkills
			promptedConflicts = conflicts
			return false, nil // user declines
		},
	}

	var warn bytes.Buffer
	upgradeErr := Run(root, Options{
		System:     RealSystem{},
		Overwrite:  true,
		Prompter:   prompter,
		PinVersion: "0.9.0",
		WarnWriter: &warn,
	})

	// ── Verify error ──
	if upgradeErr == nil {
		t.Fatal("expected Run() to return error when user declines skills migration")
	}
	if !strings.Contains(upgradeErr.Error(), "skills format migration declined by user") {
		t.Fatalf("expected exact decline error message, got: %v", upgradeErr)
	}

	// ── Verify prompter received correct arguments ──
	if len(promptedSkills) != 1 || promptedSkills[0] != "my-custom-skill" {
		t.Fatalf("expected prompter to receive [my-custom-skill], got %v", promptedSkills)
	}
	if len(promptedConflicts) != 0 {
		t.Fatalf("expected no conflicts, got %d", len(promptedConflicts))
	}

	// ── Verify exact warning banner output ──
	warnOutput := warn.String()
	expectedLines := []string{
		"=============================================================",
		"  BREAKING CHANGE: Slash-commands renamed to skills",
		"=============================================================",
		"  Slash-commands are being renamed to skills and converted to",
		"  directory format. The old flat file format (<name>.md) will",
		"  no longer work after this upgrade.",
		"  Found 1 flat-format skill(s) that must be migrated:",
		"    my-custom-skill.md  ->  my-custom-skill/SKILL.md",
		"  No conflicts detected — all skills can be migrated automatically.",
	}
	for _, line := range expectedLines {
		if !strings.Contains(warnOutput, line) {
			t.Errorf("warning output missing expected line: %q\n\nFull output:\n%s", line, warnOutput)
		}
	}
	// The warning output must NOT contain conflict-related text or success message.
	for _, forbidden := range []string{"MIGRATION BLOCKED", "DIFFERENT content", "To fix:", "Skills migration complete."} {
		if strings.Contains(warnOutput, forbidden) {
			t.Errorf("warning output should not contain %q for declined migration\n\nFull output:\n%s", forbidden, warnOutput)
		}
	}

	// ── Verify NO disk mutations ──
	// Version file: still 0.8.8, not 0.9.0.
	postVersionContent, err := os.ReadFile(versionPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read post-upgrade version file: %v", err)
	}
	if string(postVersionContent) != string(preVersionContent) {
		t.Fatalf("version file was mutated: pre=%q post=%q", string(preVersionContent), string(postVersionContent))
	}
	// Config: unchanged.
	postConfigContent, err := os.ReadFile(configPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read post-upgrade config: %v", err)
	}
	if string(postConfigContent) != string(preConfigContent) {
		t.Fatalf("config file was mutated: pre=%q post=%q", string(preConfigContent), string(postConfigContent))
	}
	// Flat skill file: still present with original content.
	postFlatContent, err := os.ReadFile(flatSkillPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("flat skill should still exist after declined migration: %v", err)
	}
	if string(postFlatContent) != string(flatContent) {
		t.Fatalf("flat skill content was mutated")
	}
	// Directory-format skill: must NOT have been created.
	dirSkillPath := filepath.Join(skillsDir, "my-custom-skill", "SKILL.md")
	if _, statErr := os.Stat(dirSkillPath); !os.IsNotExist(statErr) {
		t.Fatalf("directory-format skill should not exist after declined migration, stat err = %v", statErr)
	}
}

// TestRun_SkillsMigrationBlockedByConflict verifies that when a flat-format
// skill conflicts with an existing directory-format skill (different content),
// the pre-flight aborts with a clear error and actionable output, and no disk
// mutations have occurred.
func TestRun_SkillsMigrationBlockedByConflict(t *testing.T) {
	root := t.TempDir()

	// Seed the repo at version 0.8.8.
	if err := Run(root, Options{System: RealSystem{}, PinVersion: "0.8.8"}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	// Create a conflict: flat and directory versions with different content.
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	conflictDir := filepath.Join(skillsDir, "my-skill")
	if err := os.MkdirAll(conflictDir, 0o700); err != nil {
		t.Fatalf("mkdir conflict dir: %v", err)
	}
	flatPath := filepath.Join(skillsDir, "my-skill.md")
	if err := os.WriteFile(flatPath, []byte("flat version\n"), 0o600); err != nil {
		t.Fatalf("write flat: %v", err)
	}
	dirPath := filepath.Join(conflictDir, "SKILL.md")
	if err := os.WriteFile(dirPath, []byte("directory version\n"), 0o600); err != nil {
		t.Fatalf("write dir: %v", err)
	}

	// Capture pre-upgrade state.
	versionPath := filepath.Join(root, ".agent-layer", "al.version")
	preVersionContent, err := os.ReadFile(versionPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read pre-upgrade version: %v", err)
	}

	withMigrationManifestOverride(t, "0.9.0", skillsMigrationManifest)

	// The ConfirmSkillsMigrationFunc should NOT be called — conflicts abort
	// before reaching the confirmation prompt.
	promptCalled := false
	prompter := PromptFuncs{
		OverwriteAllPreviewFunc:       func([]DiffPreview) (bool, error) { return true, nil },
		OverwriteAllMemoryPreviewFunc: func([]DiffPreview) (bool, error) { return true, nil },
		OverwritePreviewFunc:          func(DiffPreview) (bool, error) { return true, nil },
		DeleteUnknownAllFunc:          func([]string) (bool, error) { return true, nil },
		DeleteUnknownFunc:             func(string) (bool, error) { return true, nil },
		ConfirmSkillsMigrationFunc: func(flatSkills []string, conflicts []SkillsMigrationConflict) (bool, error) {
			promptCalled = true
			return true, nil
		},
	}

	var warn bytes.Buffer
	upgradeErr := Run(root, Options{
		System:     RealSystem{},
		Overwrite:  true,
		Prompter:   prompter,
		PinVersion: "0.9.0",
		WarnWriter: &warn,
	})

	// ── Verify error ──
	if upgradeErr == nil {
		t.Fatal("expected Run() to return error when conflicts exist")
	}
	if !strings.Contains(upgradeErr.Error(), "conflict") {
		t.Fatalf("expected error to mention 'conflict', got: %v", upgradeErr)
	}

	// ── Verify prompt was NOT called (conflicts block before confirmation) ──
	if promptCalled {
		t.Fatal("ConfirmSkillsMigration should not be called when conflicts exist")
	}

	// ── Verify exact warning output for conflict path ──
	warnOutput := warn.String()
	expectedLines := []string{
		"BREAKING CHANGE: Slash-commands renamed to skills",
		"  Found 1 flat-format skill(s) that must be migrated:",
		"    my-skill.md  ->  my-skill/SKILL.md",
		"  MIGRATION BLOCKED",
		"  The following skills exist in BOTH flat and directory format",
		"  with DIFFERENT content. The migration cannot choose which",
		"  version to keep — you need to resolve this manually.",
		"    Skill: my-skill",
		"  To fix: choose which version to keep for each skill above,",
		"  then delete the other one:",
		"    Keep directory version:  rm .agent-layer/skills/<name>.md",
		"    Keep flat version:       rm -r .agent-layer/skills/<name>/",
		"  Then re-run: al upgrade",
	}
	for _, line := range expectedLines {
		if !strings.Contains(warnOutput, line) {
			t.Errorf("warning output missing expected line: %q\n\nFull output:\n%s", line, warnOutput)
		}
	}
	// The "no conflicts" line must NOT appear.
	if strings.Contains(warnOutput, "No conflicts detected") {
		t.Errorf("warning output should not contain 'No conflicts detected' when conflicts exist\n\nFull output:\n%s", warnOutput)
	}

	// ── Verify conflict detail includes file paths ──
	if !strings.Contains(warnOutput, "Flat file:") || !strings.Contains(warnOutput, "Directory:") {
		t.Errorf("warning output missing conflict file paths\n\nFull output:\n%s", warnOutput)
	}

	// ── Verify NO disk mutations ──
	postVersionContent, err := os.ReadFile(versionPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read post-upgrade version: %v", err)
	}
	if string(postVersionContent) != string(preVersionContent) {
		t.Fatalf("version file was mutated: pre=%q post=%q", string(preVersionContent), string(postVersionContent))
	}
	// Both flat and directory files should be untouched.
	flatData, err := os.ReadFile(flatPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("flat file should still exist: %v", err)
	}
	if string(flatData) != "flat version\n" {
		t.Fatalf("flat file content was mutated: %q", string(flatData))
	}
	dirData, err := os.ReadFile(dirPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("dir file should still exist: %v", err)
	}
	if string(dirData) != "directory version\n" {
		t.Fatalf("dir file content was mutated: %q", string(dirData))
	}
}

func TestExecuteAppendToFile_AppendsWhenMatchAbsent(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, ".agent-layer", "instructions")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	targetPath := filepath.Join(targetDir, "04_conventions.md")
	if err := os.WriteFile(targetPath, []byte("# Existing content\n"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	op := upgradeMigrationOperation{
		ID:        "append_conv",
		Kind:      upgradeMigrationKindAppendToFile,
		Rationale: "Add new convention",
		Path:      ".agent-layer/instructions/04_conventions.md",
		Value:     []byte(`"- **New rule:** Do the thing.\n"`),
		From:      "**New rule:**",
	}
	changed, err := inst.executeAppendToFile(op)
	if err != nil {
		t.Fatalf("executeAppendToFile: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to report changed")
	}
	data, err := os.ReadFile(targetPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if !strings.Contains(string(data), "# Existing content") {
		t.Fatal("expected existing content to be preserved")
	}
	if !strings.Contains(string(data), "**New rule:**") {
		t.Fatal("expected appended content to be present")
	}
}

func TestExecuteAppendToFile_NoopWhenMatchPresent(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, ".agent-layer", "instructions")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	targetPath := filepath.Join(targetDir, "04_conventions.md")
	original := "# Existing content\n- **New rule:** Do the thing.\n"
	if err := os.WriteFile(targetPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	op := upgradeMigrationOperation{
		ID:        "append_conv",
		Kind:      upgradeMigrationKindAppendToFile,
		Rationale: "Add new convention",
		Path:      ".agent-layer/instructions/04_conventions.md",
		Value:     []byte(`"- **New rule:** Do the thing.\n"`),
		From:      "**New rule:**",
	}
	changed, err := inst.executeAppendToFile(op)
	if err != nil {
		t.Fatalf("executeAppendToFile: %v", err)
	}
	if changed {
		t.Fatal("expected no_op when match is present")
	}
	data, err := os.ReadFile(targetPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != original {
		t.Fatalf("file should not be modified, got:\n%s", string(data))
	}
}

func TestExecuteAppendToFile_CreatesFileWhenMissing(t *testing.T) {
	root := t.TempDir()

	inst := &installer{root: root, sys: RealSystem{}}
	op := upgradeMigrationOperation{
		ID:        "append_new",
		Kind:      upgradeMigrationKindAppendToFile,
		Rationale: "Create file with initial content",
		Path:      ".agent-layer/instructions/04_conventions.md",
		Value:     []byte(`"# Conventions\n- **Rule one:** First rule.\n"`),
	}
	changed, err := inst.executeAppendToFile(op)
	if err != nil {
		t.Fatalf("executeAppendToFile: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to report changed")
	}
	targetPath := filepath.Join(root, ".agent-layer", "instructions", "04_conventions.md")
	data, err := os.ReadFile(targetPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	// When a template exists for the path, the full template should be seeded
	// as the base, preventing a partial stub file.
	if !strings.Contains(string(data), "# Project Conventions") {
		t.Fatalf("expected template header to be seeded, got:\n%s", string(data))
	}
	if !strings.Contains(string(data), "# Conventions") {
		t.Fatalf("expected appended content to be present, got:\n%s", string(data))
	}
}

func TestExecuteAppendToFile_CreatesFileWhenMissing_NoTemplate(t *testing.T) {
	root := t.TempDir()

	inst := &installer{root: root, sys: RealSystem{}}
	op := upgradeMigrationOperation{
		ID:        "append_no_tpl",
		Kind:      upgradeMigrationKindAppendToFile,
		Rationale: "Create file without a backing template",
		Path:      ".agent-layer/custom/no_template_here.md",
		Value:     []byte(`"# Custom content\n"`),
	}
	changed, err := inst.executeAppendToFile(op)
	if err != nil {
		t.Fatalf("executeAppendToFile: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to report changed")
	}
	targetPath := filepath.Join(root, ".agent-layer", "custom", "no_template_here.md")
	data, err := os.ReadFile(targetPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != "# Custom content\n" {
		t.Fatalf("expected only appended content for non-template path, got:\n%s", string(data))
	}
}

func TestExecuteAppendToFile_SeedsTemplateWhenMatchAlreadyInTemplate(t *testing.T) {
	root := t.TempDir()

	// Simulates the real 0.9.1 upgrade scenario: appending UTC convention
	// when the template already contains the match string.
	inst := &installer{root: root, sys: RealSystem{}}
	op := upgradeMigrationOperation{
		ID:        "append_utc",
		Kind:      upgradeMigrationKindAppendToFile,
		Rationale: "Move UTC-only internals from rules to conventions",
		Path:      ".agent-layer/instructions/04_conventions.md",
		From:      "UTC-only internals",
		Value:     []byte(`"\n## Time & Data\n- **UTC-only internals:** Store, compute, and transport time in UTC.\n"`),
	}
	changed, err := inst.executeAppendToFile(op)
	if err != nil {
		t.Fatalf("executeAppendToFile: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to report changed (template seeded)")
	}
	targetPath := filepath.Join(root, ".agent-layer", "instructions", "04_conventions.md")
	data, err := os.ReadFile(targetPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	// Should seed the full template without appending (match already in template).
	if !strings.Contains(string(data), "# Project Conventions") {
		t.Fatalf("expected full template header, got:\n%s", string(data))
	}
	if !strings.Contains(string(data), "## Architecture") {
		t.Fatalf("expected full template body with Architecture section, got:\n%s", string(data))
	}
	// Should NOT contain duplicate UTC sections — template already has it.
	count := strings.Count(string(data), "UTC-only internals")
	if count != 1 {
		t.Fatalf("expected exactly 1 occurrence of UTC-only internals, got %d", count)
	}
}

func TestExecuteAppendToFile_HandlesNoTrailingNewline(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, ".agent-layer", "instructions")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	targetPath := filepath.Join(targetDir, "04_conventions.md")
	// No trailing newline.
	if err := os.WriteFile(targetPath, []byte("# Existing content"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	op := upgradeMigrationOperation{
		ID:        "append_no_newline",
		Kind:      upgradeMigrationKindAppendToFile,
		Rationale: "Append after content without trailing newline",
		Path:      ".agent-layer/instructions/04_conventions.md",
		Value:     []byte(`"- **New rule:** Added.\n"`),
		From:      "**New rule:**",
	}
	changed, err := inst.executeAppendToFile(op)
	if err != nil {
		t.Fatalf("executeAppendToFile: %v", err)
	}
	if !changed {
		t.Fatal("expected migration to report changed")
	}
	data, err := os.ReadFile(targetPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	// Should have a newline between original content and appended content.
	if !strings.Contains(string(data), "# Existing content\n- **New rule:**") {
		t.Fatalf("expected newline inserted before appended content, got:\n%q", string(data))
	}
}

func TestExecuteAppendToFile_RollbackCoverage(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, ".agent-layer", "instructions")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	targetPath := filepath.Join(targetDir, "04_conventions.md")
	if err := os.WriteFile(targetPath, []byte("# Existing\n"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}

	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "append_conv",
      "kind": "append_to_file",
      "rationale": "Add new convention",
      "source_agnostic": true,
      "path": ".agent-layer/instructions/04_conventions.md",
      "value": "\"- **New rule:** Something.\\n\"",
      "from": "**New rule:**"
    }
  ]
}`)

	inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
	plan, err := inst.planUpgradeMigrations()
	if err != nil {
		t.Fatalf("planUpgradeMigrations: %v", err)
	}
	targetAbs := filepath.Clean(filepath.Join(root, ".agent-layer", "instructions", "04_conventions.md"))
	if !containsString(plan.rollbackTargets, targetAbs) {
		t.Fatalf("rollback targets missing append path %q: %#v", targetAbs, plan.rollbackTargets)
	}
	if _, ok := plan.coveredPaths[".agent-layer/instructions/04_conventions.md"]; !ok {
		t.Fatalf("expected covered path for append_to_file, got %#v", plan.coveredPaths)
	}
}

func TestValidateUpgradeMigrationOperation_AppendToFile(t *testing.T) {
	// Valid append_to_file operation should pass validation.
	validOp := upgradeMigrationOperation{
		ID:        "append_test",
		Kind:      upgradeMigrationKindAppendToFile,
		Rationale: "Test append",
		Path:      ".agent-layer/instructions/04_conventions.md",
		Value:     []byte(`"appended content"`),
	}
	if err := validateUpgradeMigrationOperation(validOp); err != nil {
		t.Fatalf("expected valid operation to pass, got: %v", err)
	}

	// Missing path.
	missingPath := validOp
	missingPath.Path = ""
	if err := validateUpgradeMigrationOperation(missingPath); err == nil {
		t.Fatal("expected error for missing path")
	}

	// Missing value.
	missingValue := validOp
	missingValue.Value = nil
	if err := validateUpgradeMigrationOperation(missingValue); err == nil {
		t.Fatal("expected error for missing value")
	}

	// Non-string value.
	nonStringValue := validOp
	nonStringValue.Value = []byte(`42`)
	if err := validateUpgradeMigrationOperation(nonStringValue); err == nil {
		t.Fatal("expected error for non-string value")
	}
}

func TestRunMigrations_AppendToFileAppliesAndReports(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, ".agent-layer", "instructions")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "04_conventions.md"), []byte("# Conventions\n"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}

	withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [
    {
      "id": "append_conv",
      "kind": "append_to_file",
      "rationale": "Add new convention",
      "source_agnostic": true,
      "path": ".agent-layer/instructions/04_conventions.md",
      "value": "\"- **New rule:** Do the thing.\\n\"",
      "from": "**New rule:**"
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

	if len(inst.migrationReport.Entries) != 1 {
		t.Fatalf("expected one migration report entry, got %d", len(inst.migrationReport.Entries))
	}
	if inst.migrationReport.Entries[0].Status != UpgradeMigrationStatusApplied {
		t.Fatalf("migration status = %q, want %q", inst.migrationReport.Entries[0].Status, UpgradeMigrationStatusApplied)
	}
	if !containsAll(warn.String(), "Migration report:", "append_conv") {
		t.Fatalf("expected migration report output, got %q", warn.String())
	}

	data, err := os.ReadFile(filepath.Join(targetDir, "04_conventions.md")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if !strings.Contains(string(data), "**New rule:**") {
		t.Fatalf("expected appended content in file, got:\n%s", string(data))
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
