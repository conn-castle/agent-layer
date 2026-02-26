package install

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func writePinVersionFile(t *testing.T, root string, version string) {
	t.Helper()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte(version+"\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}
}

func hasNoteContaining(notes []string, want string) bool {
	for _, note := range notes {
		if strings.Contains(note, want) {
			return true
		}
	}
	return false
}

func TestPlanUpgradeMigrations_HighUncoveredErrorPaths(t *testing.T) {
	t.Run("collect migration chain error", func(t *testing.T) {
		root := t.TempDir()
		writePinVersionFile(t, root, "0.6.0")
		withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [{
    "id": "delete_old",
    "kind": "delete_file",
    "rationale": "remove old file",
    "path": "docs/agent-layer/OLD.md"
  }]
}`)

		origWalk := templates.WalkFunc
		templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
			if rootPath == upgradeMigrationManifestDir {
				return errors.New("walk manifests boom")
			}
			return origWalk(rootPath, fn)
		}
		t.Cleanup(func() { templates.WalkFunc = origWalk })

		inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
		if _, err := inst.planUpgradeMigrations(); err == nil || !strings.Contains(err.Error(), "walk manifests boom") {
			t.Fatalf("expected collect chain error, got %v", err)
		}
	})

	t.Run("compare source semver error", func(t *testing.T) {
		root := t.TempDir()
		writePinVersionFile(t, root, "0.6.0")
		withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "99999999999999999999999999.0.0",
  "operations": [{
    "id": "delete_old",
    "kind": "delete_file",
    "rationale": "remove old file",
    "path": "docs/agent-layer/OLD.md"
  }]
}`)

		inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
		if _, err := inst.planUpgradeMigrations(); err == nil || !strings.Contains(err.Error(), "compare source version") {
			t.Fatalf("expected compareSemver error, got %v", err)
		}
	})

	t.Run("covered path resolves outside repo root", func(t *testing.T) {
		root := t.TempDir()
		writePinVersionFile(t, root, "0.6.0")
		withMigrationManifestOverride(t, "0.7.0", `{
  "schema_version": 1,
  "target_version": "0.7.0",
  "min_prior_version": "0.6.0",
  "operations": [{
    "id": "delete_outside",
    "kind": "delete_generated_artifact",
    "rationale": "invalid outside path",
    "source_agnostic": true,
    "path": "../outside.txt"
  }]
}`)

		inst := &installer{root: root, pinVersion: "0.7.0", sys: RealSystem{}}
		if _, err := inst.planUpgradeMigrations(); err == nil || !strings.Contains(err.Error(), "resolves outside repo root") {
			t.Fatalf("expected outside-root path error, got %v", err)
		}
	})
}

func TestRunMigrations_ExecuteErrorAndRenamePathErrors(t *testing.T) {
	t.Run("execute migration error is wrapped", func(t *testing.T) {
		inst := &installer{
			root:               t.TempDir(),
			sys:                RealSystem{},
			migrationsPrepared: true,
			migrationReport: UpgradeMigrationReport{
				Entries: []UpgradeMigrationEntry{{
					ID:     "bad-op",
					Kind:   "unknown",
					Status: UpgradeMigrationStatusPlanned,
				}},
			},
			pendingMigrationOps: []upgradeMigrationOperation{{
				ID:   "bad-op",
				Kind: "unknown",
			}},
		}
		if err := inst.runMigrations(); err == nil || !strings.Contains(err.Error(), "execute migration bad-op") {
			t.Fatalf("expected wrapped execute migration error, got %v", err)
		}
	})

	t.Run("rename source and destination path resolution errors", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: RealSystem{}}
		if _, err := inst.executeRenameMigration("", ".agent-layer/new.md"); err == nil {
			t.Fatal("expected source path resolution error")
		}
		if _, err := inst.executeRenameMigration(".agent-layer/old.md", ""); err == nil {
			t.Fatal("expected destination path resolution error")
		}
	})
}

func TestExecuteConfigMigrations_AdditionalErrorBranches(t *testing.T) {
	t.Run("config rename read error", func(t *testing.T) {
		root := t.TempDir()
		cfgPath := writeTestConfigFile(t, root, "[from]\nkey = \"value\"\n")
		fault := newFaultSystem(RealSystem{})
		fault.readErrs[normalizePath(cfgPath)] = errors.New("read config boom")
		inst := &installer{root: root, sys: fault}
		if _, err := inst.executeConfigRenameKeyMigration("from.key", "to.key"); err == nil || !strings.Contains(err.Error(), "read config boom") {
			t.Fatalf("expected config read error, got %v", err)
		}
	})

	t.Run("config set default decode default error", func(t *testing.T) {
		root := t.TempDir()
		writeTestConfigFile(t, root, "[agents]\n")
		inst := &installer{root: root, sys: RealSystem{}}
		op := upgradeMigrationOperation{
			ID:        "cfg-default-decode",
			Kind:      upgradeMigrationKindConfigSetDefault,
			Key:       "agents.codex.enabled",
			Value:     []byte("{"),
			Rationale: "decode error branch",
		}
		if _, err := inst.executeConfigSetDefaultMigration(op); err == nil || !strings.Contains(err.Error(), "decode default value") {
			t.Fatalf("expected decode default error, got %v", err)
		}
	})

	t.Run("config set default prompt error after field lookup", func(t *testing.T) {
		root := t.TempDir()
		writeTestConfigFile(t, root, "[agents]\n")
		prompter := PromptFuncs{
			ConfigSetDefaultFunc: func(string, any, string, *config.FieldDef) (any, error) {
				return nil, errors.New("prompt boom")
			},
		}
		inst := &installer{root: root, sys: RealSystem{}, prompter: prompter}
		op := upgradeMigrationOperation{
			ID:        "cfg-default-prompt",
			Kind:      upgradeMigrationKindConfigSetDefault,
			Key:       "agents.claude_vscode.enabled",
			Value:     []byte("true"),
			Rationale: "prompt error branch",
		}
		if _, err := inst.executeConfigSetDefaultMigration(op); err == nil || !strings.Contains(err.Error(), "prompt for config key") {
			t.Fatalf("expected prompt error, got %v", err)
		}
	})

	t.Run("write migration config marshal error", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		err := inst.writeMigrationConfigMap(filepath.Join(t.TempDir(), "config.toml"), map[string]any{"bad": func() {}})
		if err == nil || !strings.Contains(err.Error(), "encode config migration output") {
			t.Fatalf("expected marshal error, got %v", err)
		}
	})
}

func TestResolveAndInferSourceVersion_ErrorNotesAndBranches(t *testing.T) {
	root := t.TempDir()
	snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
	if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
		t.Fatalf("mkdir snapshot dir: %v", err)
	}
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}

	fault := newFaultSystem(RealSystem{})
	fault.readErrs[normalizePath(pinPath)] = errors.New("pin read boom")
	fault.walkErrs[normalizePath(snapshotDir)] = errors.New("snapshot walk boom")

	origWalk := templates.WalkFunc
	origMap := allTemplateManifestByV
	origErr := allTemplateManifestErr
	allTemplateManifestOnce = sync.Once{}
	allTemplateManifestByV = nil
	allTemplateManifestErr = nil
	templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
		if rootPath == templateManifestDir {
			return errors.New("manifest walk boom")
		}
		return origWalk(rootPath, fn)
	}
	t.Cleanup(func() {
		templates.WalkFunc = origWalk
		allTemplateManifestOnce = sync.Once{}
		allTemplateManifestByV = origMap
		allTemplateManifestErr = origErr
	})

	inst := &installer{root: root, sys: fault}
	res := inst.resolveUpgradeMigrationSourceVersion()
	if res.origin != UpgradeMigrationSourceUnknown {
		t.Fatalf("expected unknown origin, got %q", res.origin)
	}
	if !hasNoteContaining(res.notes, "pin version unavailable") {
		t.Fatalf("expected pin error note, got %v", res.notes)
	}
	if !hasNoteContaining(res.notes, "snapshot source inference failed") {
		t.Fatalf("expected snapshot error note, got %v", res.notes)
	}
	if !hasNoteContaining(res.notes, "manifest source inference failed") {
		t.Fatalf("expected manifest error note, got %v", res.notes)
	}
}

func TestParseAndLoadMigrationManifest_AdditionalErrors(t *testing.T) {
	t.Run("parse semver integer overflow", func(t *testing.T) {
		if _, err := parseSemver("999999999999999999999999999999.1.2"); err == nil {
			t.Fatal("expected parseSemver overflow error")
		}
	})

	t.Run("load migration manifest non-not-exist read error", func(t *testing.T) {
		origRead := templates.ReadFunc
		templates.ReadFunc = func(name string) ([]byte, error) {
			if name == "migrations/0.7.0.json" {
				return nil, errors.New("read manifest boom")
			}
			return origRead(name)
		}
		t.Cleanup(func() { templates.ReadFunc = origRead })

		if _, _, err := loadUpgradeMigrationManifestByVersion("0.7.0"); err == nil || !strings.Contains(err.Error(), "read manifest boom") {
			t.Fatalf("expected non-not-exist read error, got %v", err)
		}
	})
}

func TestMigrationManifestListingAndChain_AdditionalErrors(t *testing.T) {
	t.Run("list migration versions callback walk error argument", func(t *testing.T) {
		origWalk := templates.WalkFunc
		templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
			if rootPath != upgradeMigrationManifestDir {
				return origWalk(rootPath, fn)
			}
			return fn(path.Join(rootPath, "broken.json"), staticDirEntry{name: "broken.json"}, errors.New("walk entry boom"))
		}
		t.Cleanup(func() { templates.WalkFunc = origWalk })

		if _, err := listMigrationManifestVersions(); err == nil || !strings.Contains(err.Error(), "walk migration manifests") {
			t.Fatalf("expected callback walk error, got %v", err)
		}
	})

	t.Run("list migration versions skips non-json files", func(t *testing.T) {
		origWalk := templates.WalkFunc
		templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
			if rootPath != upgradeMigrationManifestDir {
				return origWalk(rootPath, fn)
			}
			if err := fn(rootPath, staticDirEntry{name: path.Base(rootPath), dir: true}, nil); err != nil {
				return err
			}
			if err := fn(path.Join(rootPath, "notes.txt"), staticDirEntry{name: "notes.txt"}, nil); err != nil {
				return err
			}
			return fn(path.Join(rootPath, "0.7.0.json"), staticDirEntry{name: "0.7.0.json"}, nil)
		}
		t.Cleanup(func() { templates.WalkFunc = origWalk })

		versions, err := listMigrationManifestVersions()
		if err != nil {
			t.Fatalf("listMigrationManifestVersions: %v", err)
		}
		if len(versions) == 0 {
			t.Fatal("expected at least one version")
		}
		if containsString(versions, "notes") {
			t.Fatalf("non-json file should not appear in versions: %v", versions)
		}
	})

	t.Run("list migration versions walk failure", func(t *testing.T) {
		origWalk := templates.WalkFunc
		templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
			if rootPath == upgradeMigrationManifestDir {
				return errors.New("walk top-level boom")
			}
			return origWalk(rootPath, fn)
		}
		t.Cleanup(func() { templates.WalkFunc = origWalk })

		if _, err := listMigrationManifestVersions(); err == nil || !strings.Contains(err.Error(), "walk migration manifests") {
			t.Fatalf("expected top-level walk error, got %v", err)
		}
	})

	t.Run("collect migration chain list error", func(t *testing.T) {
		origWalk := templates.WalkFunc
		templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
			if rootPath == upgradeMigrationManifestDir {
				return errors.New("collect list boom")
			}
			return origWalk(rootPath, fn)
		}
		t.Cleanup(func() { templates.WalkFunc = origWalk })

		if _, err := collectMigrationChain("0.6.0", "0.7.0"); err == nil || !strings.Contains(err.Error(), "collect list boom") {
			t.Fatalf("expected list error from collectMigrationChain, got %v", err)
		}
	})

	t.Run("collect migration chain source compare error", func(t *testing.T) {
		if _, err := collectMigrationChain("999999999999999999999999999999.0.0", "0.7.0"); err == nil || !strings.Contains(err.Error(), "compare migration version") {
			t.Fatalf("expected source compare error, got %v", err)
		}
	})

	t.Run("collect migration chain target compare error", func(t *testing.T) {
		if _, err := collectMigrationChain("0.0.0", "999999999999999999999999999999.0.0"); err == nil || !strings.Contains(err.Error(), "compare migration version") {
			t.Fatalf("expected target compare error, got %v", err)
		}
	})

	t.Run("collect migration chain load manifest error", func(t *testing.T) {
		origWalk := templates.WalkFunc
		origRead := templates.ReadFunc
		templates.WalkFunc = func(rootPath string, fn fs.WalkDirFunc) error {
			if rootPath != upgradeMigrationManifestDir {
				return origWalk(rootPath, fn)
			}
			if err := fn(rootPath, staticDirEntry{name: path.Base(rootPath), dir: true}, nil); err != nil {
				return err
			}
			return fn(path.Join(rootPath, "0.6.1.json"), staticDirEntry{name: "0.6.1.json"}, nil)
		}
		templates.ReadFunc = func(name string) ([]byte, error) {
			if name == "migrations/0.6.1.json" {
				return nil, fs.ErrNotExist
			}
			return origRead(name)
		}
		t.Cleanup(func() {
			templates.WalkFunc = origWalk
			templates.ReadFunc = origRead
		})

		if _, err := collectMigrationChain("0.6.0", "0.6.1"); err == nil || !strings.Contains(err.Error(), "missing migration manifest") {
			t.Fatalf("expected load manifest error, got %v", err)
		}
	})
}

func TestValidateUpgradeMigrationManifest_OperationValidationErrorBranch(t *testing.T) {
	manifest := upgradeMigrationManifest{
		SchemaVersion:   1,
		TargetVersion:   "0.7.0",
		MinPriorVersion: "0.6.0",
		Operations: []upgradeMigrationOperation{{
			ID:        "bad-op",
			Kind:      upgradeMigrationKindDeleteFile,
			Rationale: "",
			Path:      "docs/agent-layer/old.md",
		}},
	}
	if err := validateUpgradeMigrationManifest(manifest); err == nil || !strings.Contains(err.Error(), "rationale is required") {
		t.Fatalf("expected operation validation error, got %v", err)
	}
}
