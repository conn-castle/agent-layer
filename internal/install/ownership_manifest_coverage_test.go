package install

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/templates"
)

type staticDirEntry struct {
	name string
	dir  bool
}

func (d staticDirEntry) Name() string { return d.name }
func (d staticDirEntry) IsDir() bool  { return d.dir }
func (d staticDirEntry) Type() fs.FileMode {
	if d.dir {
		return fs.ModeDir
	}
	return 0
}
func (d staticDirEntry) Info() (fs.FileInfo, error) {
	return staticFileInfo(d), nil
}

type staticFileInfo struct {
	name string
	dir  bool
}

func (fi staticFileInfo) Name() string { return fi.name }
func (fi staticFileInfo) Size() int64  { return 0 }
func (fi staticFileInfo) Mode() fs.FileMode {
	if fi.dir {
		return fs.ModeDir
	}
	return 0
}
func (fi staticFileInfo) ModTime() time.Time { return time.Time{} }
func (fi staticFileInfo) IsDir() bool        { return fi.dir }
func (fi staticFileInfo) Sys() any           { return nil }

func TestLoadTemplateManifestByVersion_InvalidPinVersion(t *testing.T) {
	_, err := loadTemplateManifestByVersion("not-a-version")
	if err == nil || !strings.Contains(err.Error(), "invalid pin version") {
		t.Fatalf("expected invalid pin version error, got %v", err)
	}
}

func TestLoadTemplateManifestByVersion_DecodeValidateAndVersionMismatchErrors(t *testing.T) {
	original := templates.ReadFunc
	t.Cleanup(func() { templates.ReadFunc = original })

	decodePath := path.Join(templateManifestDir, "0.0.1.json")
	validatePath := path.Join(templateManifestDir, "0.0.2.json")
	mismatchPath := path.Join(templateManifestDir, "0.0.3.json")

	invalidManifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion + 1,
		Version:       "0.0.2",
		GeneratedAt:   "2026-02-09T00:00:00Z",
		Files:         []manifestFileEntry{{Path: commandsAllowRelPath, FullHashNormalized: "abc"}},
	}
	invalidManifestBytes, err := json.Marshal(invalidManifest)
	if err != nil {
		t.Fatalf("marshal invalid manifest: %v", err)
	}

	mismatchManifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "0.0.4",
		GeneratedAt:   "2026-02-09T00:00:00Z",
		Files:         []manifestFileEntry{{Path: commandsAllowRelPath, FullHashNormalized: "abc"}},
	}
	mismatchBytes, err := json.Marshal(mismatchManifest)
	if err != nil {
		t.Fatalf("marshal mismatch manifest: %v", err)
	}

	templates.ReadFunc = func(name string) ([]byte, error) {
		switch name {
		case decodePath:
			return []byte("{not-json"), nil
		case validatePath:
			return invalidManifestBytes, nil
		case mismatchPath:
			return mismatchBytes, nil
		default:
			return original(name)
		}
	}

	if _, err := loadTemplateManifestByVersion("0.0.1"); err == nil || !strings.Contains(err.Error(), "decode template manifest") {
		t.Fatalf("expected decode error, got %v", err)
	}
	if _, err := loadTemplateManifestByVersion("0.0.2"); err == nil || !strings.Contains(err.Error(), "validate template manifest") {
		t.Fatalf("expected validate error, got %v", err)
	}
	if _, err := loadTemplateManifestByVersion("0.0.3"); err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("expected version mismatch error, got %v", err)
	}
}

func TestLoadAllTemplateManifests_ErrorPaths(t *testing.T) {
	t.Run("walk error", func(t *testing.T) {
		resetManifestCacheForTest()
		original := templates.WalkFunc
		templates.WalkFunc = func(string, fs.WalkDirFunc) error {
			return errors.New("walk boom")
		}
		t.Cleanup(func() {
			templates.WalkFunc = original
			resetManifestCacheForTest()
		})

		if _, err := loadAllTemplateManifests(); err == nil || !strings.Contains(err.Error(), "walk boom") {
			t.Fatalf("expected walk error, got %v", err)
		}
	})

	t.Run("callback receives err", func(t *testing.T) {
		resetManifestCacheForTest()
		original := templates.WalkFunc
		templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
			return fn(path.Join(root, "bad.json"), nil, errors.New("callback boom"))
		}
		t.Cleanup(func() {
			templates.WalkFunc = original
			resetManifestCacheForTest()
		})

		if _, err := loadAllTemplateManifests(); err == nil || !strings.Contains(err.Error(), "callback boom") {
			t.Fatalf("expected callback error, got %v", err)
		}
	})

	t.Run("no manifests", func(t *testing.T) {
		resetManifestCacheForTest()
		original := templates.WalkFunc
		templates.WalkFunc = func(string, fs.WalkDirFunc) error {
			return nil
		}
		t.Cleanup(func() {
			templates.WalkFunc = original
			resetManifestCacheForTest()
		})

		if _, err := loadAllTemplateManifests(); err == nil || !strings.Contains(err.Error(), "no embedded template manifests found") {
			t.Fatalf("expected no manifests error, got %v", err)
		}
	})
}

func TestLoadAllTemplateManifests_WalkSkipsNonJSON(t *testing.T) {
	resetManifestCacheForTest()
	originalWalk := templates.WalkFunc
	originalRead := templates.ReadFunc
	t.Cleanup(func() {
		templates.WalkFunc = originalWalk
		templates.ReadFunc = originalRead
		resetManifestCacheForTest()
	})

	manifest := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "0.0.1",
		GeneratedAt:   "2026-02-09T00:00:00Z",
		Files:         []manifestFileEntry{{Path: commandsAllowRelPath, FullHashNormalized: "abc"}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}

	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		if err := fn(root, staticDirEntry{name: root, dir: true}, nil); err != nil {
			return err
		}
		if err := fn(path.Join(root, "readme.txt"), staticDirEntry{name: "readme.txt", dir: false}, nil); err != nil {
			return err
		}
		if err := fn(path.Join(root, "0.0.1.json"), staticDirEntry{name: "0.0.1.json", dir: false}, nil); err != nil {
			return err
		}
		return nil
	}
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == path.Join(templateManifestDir, "0.0.1.json") {
			return manifestBytes, nil
		}
		return originalRead(name)
	}

	manifests, err := loadAllTemplateManifests()
	if err != nil {
		t.Fatalf("loadAllTemplateManifests: %v", err)
	}
	if _, ok := manifests["0.0.1"]; !ok {
		t.Fatalf("expected manifest version 0.0.1 in %#v", manifests)
	}
}

func TestLoadAllTemplateManifests_WalkReadDecodeValidateAndDuplicateErrors(t *testing.T) {
	type testCase struct {
		name       string
		walkPaths  []string
		readByPath map[string]any
		wantSubstr string
	}

	cases := []testCase{
		{
			name:      "read error",
			walkPaths: []string{path.Join(templateManifestDir, "0.0.1.json")},
			readByPath: map[string]any{
				path.Join(templateManifestDir, "0.0.1.json"): errors.New("read boom"),
			},
			wantSubstr: "read boom",
		},
		{
			name:      "decode error",
			walkPaths: []string{path.Join(templateManifestDir, "0.0.1.json")},
			readByPath: map[string]any{
				path.Join(templateManifestDir, "0.0.1.json"): []byte("{bad-json"),
			},
			wantSubstr: "decode template manifest",
		},
		{
			name:      "validate error",
			walkPaths: []string{path.Join(templateManifestDir, "0.0.1.json")},
			readByPath: map[string]any{
				path.Join(templateManifestDir, "0.0.1.json"): templateManifest{
					SchemaVersion: templateManifestSchemaVersion + 1,
					Version:       "0.0.1",
					GeneratedAt:   "2026-02-09T00:00:00Z",
					Files:         []manifestFileEntry{{Path: commandsAllowRelPath, FullHashNormalized: "abc"}},
				},
			},
			wantSubstr: "validate template manifest",
		},
		{
			name:      "duplicate version",
			walkPaths: []string{path.Join(templateManifestDir, "a.json"), path.Join(templateManifestDir, "b.json")},
			readByPath: map[string]any{
				path.Join(templateManifestDir, "a.json"): templateManifest{
					SchemaVersion: templateManifestSchemaVersion,
					Version:       "0.0.1",
					GeneratedAt:   "2026-02-09T00:00:00Z",
					Files:         []manifestFileEntry{{Path: commandsAllowRelPath, FullHashNormalized: "abc"}},
				},
				path.Join(templateManifestDir, "b.json"): templateManifest{
					SchemaVersion: templateManifestSchemaVersion,
					Version:       "0.0.1",
					GeneratedAt:   "2026-02-09T00:00:00Z",
					Files:         []manifestFileEntry{{Path: commandsAllowRelPath, FullHashNormalized: "abc"}},
				},
			},
			wantSubstr: "duplicate template manifest version",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetManifestCacheForTest()
			originalWalk := templates.WalkFunc
			originalRead := templates.ReadFunc
			t.Cleanup(func() {
				templates.WalkFunc = originalWalk
				templates.ReadFunc = originalRead
				resetManifestCacheForTest()
			})

			templates.WalkFunc = func(_ string, fn fs.WalkDirFunc) error {
				for _, p := range tc.walkPaths {
					if err := fn(p, staticDirEntry{name: path.Base(p), dir: false}, nil); err != nil {
						return err
					}
				}
				return nil
			}

			templates.ReadFunc = func(name string) ([]byte, error) {
				value, ok := tc.readByPath[name]
				if !ok {
					return originalRead(name)
				}
				switch v := value.(type) {
				case error:
					return nil, v
				case []byte:
					return v, nil
				case templateManifest:
					data, err := json.Marshal(v)
					if err != nil {
						return nil, err
					}
					return data, nil
				default:
					return nil, errors.New("unsupported readByPath value")
				}
			}

			_, err := loadAllTemplateManifests()
			if err == nil || !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantSubstr, err)
			}
		})
	}
}

func TestValidateTemplateManifest_AdditionalErrorPaths(t *testing.T) {
	base := templateManifest{
		SchemaVersion: templateManifestSchemaVersion,
		Version:       "0.7.0",
		GeneratedAt:   "2026-02-09T00:00:00Z",
		Files:         []manifestFileEntry{{Path: commandsAllowRelPath, FullHashNormalized: "abc"}},
	}

	missingVersion := base
	missingVersion.Version = " "
	if err := validateTemplateManifest(missingVersion); err == nil {
		t.Fatal("expected version is required error")
	}

	notNormalized := base
	notNormalized.Version = "v0.7.0"
	if err := validateTemplateManifest(notNormalized); err == nil {
		t.Fatal("expected non-normalized version error")
	}

	missingGeneratedAt := base
	missingGeneratedAt.GeneratedAt = " "
	if err := validateTemplateManifest(missingGeneratedAt); err == nil {
		t.Fatal("expected generated_at_utc is required error")
	}

	missingFiles := base
	missingFiles.Files = nil
	if err := validateTemplateManifest(missingFiles); err == nil {
		t.Fatal("expected files is required error")
	}

	badFileEntry := base
	badFileEntry.Files = []manifestFileEntry{{Path: "", FullHashNormalized: "abc"}}
	if err := validateTemplateManifest(badFileEntry); err == nil {
		t.Fatal("expected manifest file entry validation error")
	}
}

func TestValidateManifestFileEntry_PolicyPayloadInvalid(t *testing.T) {
	file := manifestFileEntry{
		Path:               commandsAllowRelPath,
		FullHashNormalized: "abc",
		PolicyID:           ownershipPolicyAllowlist,
		PolicyPayload:      json.RawMessage(`{}`),
	}
	if err := validateManifestFileEntry(file); err == nil {
		t.Fatal("expected policy payload invalid error")
	}
}

func TestValidateManagedBaselineState_InvalidFileEntry(t *testing.T) {
	state := managedBaselineState{
		SchemaVersion:   baselineStateSchemaVersion,
		BaselineVersion: "0.7.0",
		Source:          BaselineStateSourceWrittenByInit,
		CreatedAt:       "2026-02-09T00:00:00Z",
		UpdatedAt:       "2026-02-09T00:00:00Z",
		Files:           []manifestFileEntry{{Path: "", FullHashNormalized: "abc"}},
	}
	if err := validateManagedBaselineState(state); err == nil {
		t.Fatal("expected baseline file entry validation error")
	}
}

func TestWriteManagedBaselineState_MarshalError(t *testing.T) {
	state := managedBaselineState{
		SchemaVersion:   baselineStateSchemaVersion,
		BaselineVersion: "0.7.0",
		Source:          BaselineStateSourceWrittenByInit,
		CreatedAt:       "2026-02-09T00:00:00Z",
		UpdatedAt:       "2026-02-09T00:00:00Z",
		Files:           []manifestFileEntry{{Path: commandsAllowRelPath, FullHashNormalized: "abc"}},
		Metadata: map[string]any{
			"bad": func() {},
		},
	}

	err := writeManagedBaselineState(t.TempDir(), RealSystem{}, state)
	if err == nil || !strings.Contains(err.Error(), "encode managed baseline state") {
		t.Fatalf("expected marshal error, got %v", err)
	}
}

func TestBuildCurrentTemplateManifest_ErrorPaths(t *testing.T) {
	t.Run("currentTemplateEntries error", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		original := templates.WalkFunc
		templates.WalkFunc = func(string, fs.WalkDirFunc) error {
			return errors.New("walk boom")
		}
		t.Cleanup(func() { templates.WalkFunc = original })

		if _, err := buildCurrentTemplateManifest(inst, time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "walk boom") {
			t.Fatalf("expected walk error, got %v", err)
		}
	})

	t.Run("template read error", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		original := templates.ReadFunc
		templates.ReadFunc = func(name string) ([]byte, error) {
			if name == "commands.allow" {
				return nil, errors.New("read boom")
			}
			return original(name)
		}
		t.Cleanup(func() { templates.ReadFunc = original })

		if _, err := buildCurrentTemplateManifest(inst, time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "failed to read template") {
			t.Fatalf("expected template read error, got %v", err)
		}
	})

	t.Run("buildOwnershipComparable error", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		original := templates.ReadFunc
		templates.ReadFunc = func(name string) ([]byte, error) {
			if name == "docs/agent-layer/ISSUES.md" {
				return []byte("# missing marker\n"), nil
			}
			return original(name)
		}
		t.Cleanup(func() { templates.ReadFunc = original })

		if _, err := buildCurrentTemplateManifest(inst, time.Unix(0, 0)); err == nil || !strings.Contains(err.Error(), "build ownership comparable") {
			t.Fatalf("expected ownership comparable error, got %v", err)
		}
	})
}

func TestReadCurrentPinVersion_EmptyContentReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	pinPath := filepath.Join(root, ".agent-layer", "al.version")
	if err := os.MkdirAll(filepath.Dir(pinPath), 0o755); err != nil {
		t.Fatalf("mkdir pin dir: %v", err)
	}
	if err := os.WriteFile(pinPath, []byte(" \n"), 0o644); err != nil {
		t.Fatalf("write pin file: %v", err)
	}

	pin, err := readCurrentPinVersion(root, RealSystem{})
	if err != nil {
		t.Fatalf("readCurrentPinVersion: %v", err)
	}
	if pin != "" {
		t.Fatalf("expected empty pin, got %q", pin)
	}
}

func TestWriteManagedBaselineIfConsistent_ErrorPaths(t *testing.T) {
	t.Run("listManagedDiffs error", func(t *testing.T) {
		root := t.TempDir()
		allowPath := filepath.Join(root, filepath.FromSlash(commandsAllowRelPath))
		if err := os.MkdirAll(filepath.Dir(allowPath), 0o755); err != nil {
			t.Fatalf("mkdir allow dir: %v", err)
		}
		if err := os.WriteFile(allowPath, []byte("custom allow\n"), 0o644); err != nil {
			t.Fatalf("write allow: %v", err)
		}

		inst := &installer{root: root, sys: RealSystem{}}
		original := templates.ReadFunc
		templates.ReadFunc = func(name string) ([]byte, error) {
			if name == "commands.allow" {
				return nil, errors.New("template boom")
			}
			return original(name)
		}
		t.Cleanup(func() { templates.ReadFunc = original })

		if err := inst.writeManagedBaselineIfConsistent(BaselineStateSourceWrittenByInit); err == nil || !strings.Contains(err.Error(), "template boom") {
			t.Fatalf("expected listManagedDiffs error, got %v", err)
		}
	})

	t.Run("listMemoryDiffs error", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: RealSystem{}}

		resetManifestCacheForTest()
		originalWalk := templates.WalkFunc
		docsCount := 0
		templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
			if root == "docs/agent-layer" {
				docsCount++
				if docsCount == 2 {
					return errors.New("walk boom")
				}
			}
			return originalWalk(root, fn)
		}
		t.Cleanup(func() {
			templates.WalkFunc = originalWalk
			resetManifestCacheForTest()
		})

		if err := inst.writeManagedBaselineIfConsistent(BaselineStateSourceWrittenByInit); err == nil || !strings.Contains(err.Error(), "walk boom") {
			t.Fatalf("expected listMemoryDiffs error, got %v", err)
		}
	})

	t.Run("buildCurrentTemplateManifest error", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: RealSystem{}}

		original := templates.ReadFunc
		templates.ReadFunc = func(name string) ([]byte, error) {
			if name == "commands.allow" {
				return nil, errors.New("read boom")
			}
			return original(name)
		}
		t.Cleanup(func() { templates.ReadFunc = original })

		if err := inst.writeManagedBaselineIfConsistent(BaselineStateSourceWrittenByInit); err == nil || !strings.Contains(err.Error(), "read boom") {
			t.Fatalf("expected manifest build error, got %v", err)
		}
	})

	t.Run("writeManagedBaselineState error", func(t *testing.T) {
		root := t.TempDir()
		writeFault := newFaultSystem(RealSystem{})
		statePath := filepath.Join(root, filepath.FromSlash(baselineStateRelPath))
		writeFault.writeErrs[normalizePath(statePath)] = errors.New("write boom")
		inst := &installer{root: root, sys: writeFault}

		if err := inst.writeManagedBaselineIfConsistent(BaselineStateSourceWrittenByInit); err == nil || !strings.Contains(err.Error(), "write boom") {
			t.Fatalf("expected write error, got %v", err)
		}
	})
}
