package install

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestLoadTemplateManifestByVersion_BackfillTags(t *testing.T) {
	versions := []string{"0.6.0", "0.6.1", "0.7.0"}
	excludedPaths := []string{".agent-layer/.env", ".agent-layer/.gitignore", ".agent-layer/config.toml"}
	for _, ver := range versions {
		manifest, err := loadTemplateManifestByVersion(ver)
		if err != nil {
			t.Fatalf("load manifest %s: %v", ver, err)
		}
		if manifest.Version != ver {
			t.Fatalf("manifest version = %q, want %q", manifest.Version, ver)
		}
		if len(manifest.Files) == 0 {
			t.Fatalf("manifest %s has no files", ver)
		}
		entries := manifestFileMap(manifest.Files)
		for _, excluded := range excludedPaths {
			if _, ok := entries[excluded]; ok {
				t.Fatalf("manifest %s unexpectedly includes excluded path %s", ver, excluded)
			}
		}
		allowEntry, ok := entries[commandsAllowRelPath]
		if !ok {
			t.Fatalf("manifest %s missing commands.allow entry", ver)
		}
		if allowEntry.PolicyID != ownershipPolicyAllowlist {
			t.Fatalf("manifest %s commands.allow policy_id = %q", ver, allowEntry.PolicyID)
		}
	}
}

func TestLoadTemplateManifestByVersion_MissingVersion(t *testing.T) {
	_, err := loadTemplateManifestByVersion("9.9.9")
	if err == nil {
		t.Fatal("expected missing manifest error")
	}
}

func TestLoadAllTemplateManifests_ContainsBackfill(t *testing.T) {
	manifests, err := loadAllTemplateManifests()
	if err != nil {
		t.Fatalf("load all manifests: %v", err)
	}
	for _, version := range []string{"0.6.0", "0.6.1", "0.7.0"} {
		if _, ok := manifests[version]; !ok {
			t.Fatalf("missing manifest version %s", version)
		}
	}
}

func TestTemplateManifests_BackfillAndSizeBudget(t *testing.T) {
	const maxEmbeddedManifestBytes = 256 * 1024
	requiredVersions := []string{"0.6.0", "0.6.1", "0.7.0"}

	totalBytes := 0
	for _, versionValue := range requiredVersions {
		path := filepath.ToSlash(filepath.Join(templateManifestDir, versionValue+".json"))
		data, err := templates.Read(path)
		if err != nil {
			t.Fatalf("read required manifest %s: %v", versionValue, err)
		}
		totalBytes += len(data)
	}
	if totalBytes > maxEmbeddedManifestBytes {
		t.Fatalf("embedded manifest bytes %d exceed budget %d", totalBytes, maxEmbeddedManifestBytes)
	}
}

func TestWriteReadManagedBaselineState_RoundTrip(t *testing.T) {
	root := t.TempDir()
	sys := RealSystem{}
	state := managedBaselineState{
		SchemaVersion:   baselineStateSchemaVersion,
		BaselineVersion: "0.7.0",
		Source:          BaselineStateSourceWrittenByInit,
		CreatedAt:       "2026-02-09T00:00:00Z",
		UpdatedAt:       "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{
			{
				Path:               commandsAllowRelPath,
				FullHashNormalized: "abc123",
			},
		},
		Metadata: map[string]any{"note": "test"},
	}
	if err := writeManagedBaselineState(root, sys, state); err != nil {
		t.Fatalf("write managed baseline state: %v", err)
	}
	readBack, err := readManagedBaselineState(root, sys)
	if err != nil {
		t.Fatalf("read managed baseline state: %v", err)
	}
	if readBack.BaselineVersion != state.BaselineVersion {
		t.Fatalf("baseline_version = %q, want %q", readBack.BaselineVersion, state.BaselineVersion)
	}
	if readBack.Source != state.Source {
		t.Fatalf("source = %q, want %q", readBack.Source, state.Source)
	}
	if len(readBack.Files) != 1 || readBack.Files[0].Path != commandsAllowRelPath {
		t.Fatalf("unexpected files: %#v", readBack.Files)
	}
}

func TestRun_WritesManagedBaselineState(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("run install: %v", err)
	}
	statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("expected baseline state file at %s: %v", statePath, err)
	}
	state, err := readManagedBaselineState(root, RealSystem{})
	if err != nil {
		t.Fatalf("read baseline state: %v", err)
	}
	if state.Source != BaselineStateSourceWrittenByInit {
		t.Fatalf("baseline source = %q, want %q", state.Source, BaselineStateSourceWrittenByInit)
	}
	if len(state.Files) == 0 {
		t.Fatal("expected baseline files")
	}
}

func TestRun_DoesNotWriteBaselineWhenManagedDiffsRemain(t *testing.T) {
	root := t.TempDir()
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.MkdirAll(filepath.Dir(allowPath), 0o755); err != nil {
		t.Fatalf("mkdir allow dir: %v", err)
	}
	if err := os.WriteFile(allowPath, []byte("custom allow\n"), 0o644); err != nil {
		t.Fatalf("write custom allow: %v", err)
	}
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("run install with preexisting diff: %v", err)
	}
	statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	_, err := os.Stat(statePath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no baseline state file when diffs remain, got err=%v", err)
	}
}

func TestValidateTemplateManifest_ErrorPaths(t *testing.T) {
	base := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "0.7.0",
		GeneratedAt:   "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{{
			Path:               commandsAllowRelPath,
			FullHashNormalized: "abc",
		}},
	}
	if err := validateTemplateManifest(base); err != nil {
		t.Fatalf("expected base manifest to validate, got %v", err)
	}

	badSchema := base
	badSchema.SchemaVersion = 2
	if err := validateTemplateManifest(badSchema); err == nil {
		t.Fatal("expected schema validation error")
	}

	badVersion := base
	badVersion.Version = "not-semver"
	if err := validateTemplateManifest(badVersion); err == nil {
		t.Fatal("expected version validation error")
	}

	badTime := base
	badTime.GeneratedAt = "invalid-time"
	if err := validateTemplateManifest(badTime); err == nil {
		t.Fatal("expected generated_at_utc validation error")
	}

	dupPath := base
	dupPath.Files = append(dupPath.Files, dupPath.Files[0])
	if err := validateTemplateManifest(dupPath); err == nil {
		t.Fatal("expected duplicate path validation error")
	}
}

func TestValidateManagedBaselineState_ErrorPaths(t *testing.T) {
	base := managedBaselineState{
		SchemaVersion:   baselineStateSchemaVersion,
		BaselineVersion: "0.7.0",
		Source:          BaselineStateSourceWrittenByInit,
		CreatedAt:       "2026-02-09T00:00:00Z",
		UpdatedAt:       "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{{
			Path:               commandsAllowRelPath,
			FullHashNormalized: "abc",
		}},
	}
	if err := validateManagedBaselineState(base); err != nil {
		t.Fatalf("expected base state to validate, got %v", err)
	}

	badSchema := base
	badSchema.SchemaVersion = 99
	if err := validateManagedBaselineState(badSchema); err == nil {
		t.Fatal("expected schema validation error")
	}

	badTimes := base
	badTimes.CreatedAt = "bad"
	if err := validateManagedBaselineState(badTimes); err == nil {
		t.Fatal("expected created_at validation error")
	}

	missingSource := base
	missingSource.Source = ""
	if err := validateManagedBaselineState(missingSource); err == nil {
		t.Fatal("expected source validation error")
	}
}

func TestReadManagedBaselineState_DecodeError(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write invalid state: %v", err)
	}
	_, err := readManagedBaselineState(root, RealSystem{})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestWriteManagedBaselineState_ErrorPaths(t *testing.T) {
	root := t.TempDir()
	state := managedBaselineState{
		SchemaVersion:   baselineStateSchemaVersion,
		BaselineVersion: "0.7.0",
		Source:          BaselineStateSourceWrittenByInit,
		CreatedAt:       "2026-02-09T00:00:00Z",
		UpdatedAt:       "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{{
			Path:               commandsAllowRelPath,
			FullHashNormalized: "abc",
		}},
	}
	if err := writeManagedBaselineState(root, nil, state); err == nil {
		t.Fatal("expected nil system error")
	}

	invalid := state
	invalid.Source = ""
	if err := writeManagedBaselineState(root, RealSystem{}, invalid); err == nil {
		t.Fatal("expected validation error for invalid state")
	}
}

func TestRun_WritesManagedBaselineSourceUpgrade(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	allowPath := filepath.Join(root, ".agent-layer", "commands.allow")
	if err := os.WriteFile(allowPath, []byte("custom allow\n"), 0o644); err != nil {
		t.Fatalf("write custom allow: %v", err)
	}
	if err := Run(root, Options{System: RealSystem{}, Overwrite: true, Force: true}); err != nil {
		t.Fatalf("overwrite run: %v", err)
	}
	state, err := readManagedBaselineState(root, RealSystem{})
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if state.Source != BaselineStateSourceWrittenByUpgrade {
		t.Fatalf("source = %q, want %q", state.Source, BaselineStateSourceWrittenByUpgrade)
	}
}

func TestValidateManifestFileEntry_ErrorPaths(t *testing.T) {
	if err := validateManifestFileEntry(manifestFileEntry{
		FullHashNormalized: "abc",
	}); err == nil {
		t.Fatal("expected missing path error")
	}

	if err := validateManifestFileEntry(manifestFileEntry{
		Path: commandsAllowRelPath,
	}); err == nil {
		t.Fatal("expected missing full hash error")
	}

	rawPayload := json.RawMessage(`{"foo":"bar"}`)
	if err := validateManifestFileEntry(manifestFileEntry{
		Path:               commandsAllowRelPath,
		FullHashNormalized: "abc",
		PolicyPayload:      rawPayload,
	}); err == nil {
		t.Fatal("expected payload requires policy_id error")
	}
}

func TestValidateManagedBaselineState_MoreErrorPaths(t *testing.T) {
	base := managedBaselineState{
		SchemaVersion:   baselineStateSchemaVersion,
		BaselineVersion: "0.7.0",
		Source:          BaselineStateSourceWrittenByInit,
		CreatedAt:       "2026-02-09T00:00:00Z",
		UpdatedAt:       "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{{
			Path:               commandsAllowRelPath,
			FullHashNormalized: "abc",
		}},
	}

	noVersion := base
	noVersion.BaselineVersion = ""
	if err := validateManagedBaselineState(noVersion); err == nil {
		t.Fatal("expected missing baseline_version error")
	}

	badUpdated := base
	badUpdated.UpdatedAt = "bad"
	if err := validateManagedBaselineState(badUpdated); err == nil {
		t.Fatal("expected updated_at validation error")
	}

	noFiles := base
	noFiles.Files = nil
	if err := validateManagedBaselineState(noFiles); err == nil {
		t.Fatal("expected files required error")
	}

	dupPath := base
	dupPath.Files = append(dupPath.Files, dupPath.Files[0])
	if err := validateManagedBaselineState(dupPath); err == nil {
		t.Fatal("expected duplicate baseline file path error")
	}
}

func TestReadManagedBaselineState_NilSystem(t *testing.T) {
	_, err := readManagedBaselineState(t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected nil system error")
	}
}

func TestReadManagedBaselineState_ValidationError(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	invalid := `{"schema_version":9,"baseline_version":"0.7.0","source":"written_by_init","created_at_utc":"2026-02-09T00:00:00Z","updated_at_utc":"2026-02-09T00:00:00Z","files":[{"path":"` + commandsAllowRelPath + `","full_hash_normalized":"abc"}]}`
	if err := os.WriteFile(statePath, []byte(invalid), 0o644); err != nil {
		t.Fatalf("write invalid state: %v", err)
	}
	if _, err := readManagedBaselineState(root, RealSystem{}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestWriteManagedBaselineState_MkdirAndWriteErrors(t *testing.T) {
	root := t.TempDir()
	state := managedBaselineState{
		SchemaVersion:   baselineStateSchemaVersion,
		BaselineVersion: "0.7.0",
		Source:          BaselineStateSourceWrittenByInit,
		CreatedAt:       "2026-02-09T00:00:00Z",
		UpdatedAt:       "2026-02-09T00:00:00Z",
		Files: []manifestFileEntry{{
			Path:               commandsAllowRelPath,
			FullHashNormalized: "abc",
		}},
	}

	mkdirFault := newFaultSystem(RealSystem{})
	statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	mkdirFault.mkdirErrs[normalizePath(filepath.Dir(statePath))] = errors.New("mkdir boom")
	if err := writeManagedBaselineState(root, mkdirFault, state); err == nil {
		t.Fatal("expected mkdir error")
	}

	writeFault := newFaultSystem(RealSystem{})
	writeFault.writeErrs[normalizePath(statePath)] = errors.New("write boom")
	if err := writeManagedBaselineState(root, writeFault, state); err == nil {
		t.Fatal("expected write error")
	}
}

func TestResolveBaselineVersion_Cases(t *testing.T) {
	if got := resolveBaselineVersion(nil); got != baselineVersionUnknown {
		t.Fatalf("resolveBaselineVersion(nil) = %q, want unknown", got)
	}

	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}, pinVersion: "1.2.3"}
	if got := resolveBaselineVersion(inst); got != "1.2.3" {
		t.Fatalf("resolveBaselineVersion(pin) = %q, want 1.2.3", got)
	}

	versionPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(versionPath), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(versionPath, []byte("v0.7.0\n"), 0o644); err != nil {
		t.Fatalf("write version: %v", err)
	}
	inst.pinVersion = ""
	if got := resolveBaselineVersion(inst); got != "0.7.0" {
		t.Fatalf("resolveBaselineVersion(file) = %q, want 0.7.0", got)
	}

	if err := os.WriteFile(versionPath, []byte("dev\n"), 0o644); err != nil {
		t.Fatalf("write invalid version: %v", err)
	}
	if got := resolveBaselineVersion(inst); got != baselineVersionUnknown {
		t.Fatalf("resolveBaselineVersion(invalid) = %q, want unknown", got)
	}
}

func TestReadCurrentPinVersion_ErrorPaths(t *testing.T) {
	if _, err := readCurrentPinVersion(t.TempDir(), nil); err == nil {
		t.Fatal("expected nil system error")
	}

	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte("0.7.0\n"), 0o644); err != nil {
		t.Fatalf("write pin: %v", err)
	}

	readFault := newFaultSystem(RealSystem{})
	readFault.readErrs[normalizePath(pinPath)] = errors.New("read boom")
	if _, err := readCurrentPinVersion(root, readFault); err == nil {
		t.Fatal("expected read error")
	}
}

func TestWriteManagedBaselineIfConsistent_EarlyReturnAndBaselineReadError(t *testing.T) {
	var nilInst *installer
	if err := nilInst.writeManagedBaselineIfConsistent(BaselineStateSourceWrittenByInit); err != nil {
		t.Fatalf("nil installer should return nil, got %v", err)
	}

	instNoSystem := &installer{root: t.TempDir()}
	if err := instNoSystem.writeManagedBaselineIfConsistent(BaselineStateSourceWrittenByInit); err != nil {
		t.Fatalf("installer with nil system should return nil, got %v", err)
	}

	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
	if err := os.WriteFile(statePath, []byte("{bad-json"), 0o644); err != nil {
		t.Fatalf("corrupt baseline: %v", err)
	}
	inst := &installer{root: root, sys: RealSystem{}}
	if err := inst.writeManagedBaselineIfConsistent(BaselineStateSourceWrittenByInit); err == nil {
		t.Fatal("expected baseline decode error")
	}
}
