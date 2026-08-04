package sync

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

const generatedMarkerFixture = "<!--\n  GENERATED FILE\n  Source: .agent-layer/skills/test.md\n  Regenerate: al sync\n-->\n"

type unknownTypeDirEntry struct {
	name string
}

func (e unknownTypeDirEntry) Name() string    { return e.name }
func (unknownTypeDirEntry) IsDir() bool       { return false }
func (unknownTypeDirEntry) Type() fs.FileMode { return 0 }
func (unknownTypeDirEntry) Info() (fs.FileInfo, error) {
	return nil, errors.New("directory entry info should not be used")
}

// skillManifestFixture returns a valid SKILL.md body for a named skill.
func skillManifestFixture(name string) string {
	return "---\nname: " + name + "\ndescription: The " + name + " skill.\n---\n\nBody for " + name + "\n"
}

// sourceSkill materializes a skill source directory containing SKILL.md and
// returns the loaded-skill value the projection writes from.
func sourceSkill(t *testing.T, name string) config.Skill {
	t.Helper()
	return writeSkillSource(t, t.TempDir(), name)
}

// writeSkillSource materializes a skill source at dir. Projection is a byte
// copy, so every test needs a real source tree rather than parsed fields.
func writeSkillSource(t *testing.T, dir string, name string) config.Skill {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir source skill: %v", err)
	}
	manifest := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(manifest, []byte(skillManifestFixture(name)), 0o600); err != nil {
		t.Fatalf("write source SKILL.md: %v", err)
	}
	return config.Skill{Name: name, SourceDir: dir, SourcePath: manifest}
}

// managedSkillSource materializes a skill under a project root's managed skills
// directory, the way internal/config would load it.
func managedSkillSource(t *testing.T, root string, name string) config.Skill {
	t.Helper()
	return writeSkillSource(t, filepath.Join(root, ".agent-layer", "skills", name), name)
}

// projectedSkillsDir is the client directory a projection writes into.
func projectedSkillsDir(root string, client string) string {
	if client == "claude" {
		return filepath.Join(root, ".claude", "skills")
	}
	return filepath.Join(root, ".agents", "skills")
}

// readOwnedManifest decodes the ownership manifest for a projected skills directory.
func readOwnedManifest(t *testing.T, skillsDir string) ownedSkillsManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(skillsDir, ownedSkillsManifestName)) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read ownership manifest: %v", err)
	}
	var manifest ownedSkillsManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode ownership manifest: %v", err)
	}
	return manifest
}

func TestRecoverSkillProjectionRepairsEveryInterruptedSwapShape(t *testing.T) {
	t.Parallel()
	t.Run("removes a newly created target", func(t *testing.T) {
		skillsDir := t.TempDir()
		stagingRoot := filepath.Join(skillsDir, ".agent-layer-projection-staging")
		target := filepath.Join(skillsDir, "alpha")
		if err := os.MkdirAll(filepath.Join(stagingRoot, "created"), 0o750); err != nil {
			t.Fatalf("mkdir creation markers: %v", err)
		}
		if err := os.MkdirAll(target, 0o750); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		if err := os.WriteFile(filepath.Join(stagingRoot, "created", "alpha"), nil, 0o600); err != nil {
			t.Fatalf("write creation marker: %v", err)
		}
		if err := recoverSkillProjection(RealSystem{}, skillsDir, stagingRoot); err != nil {
			t.Fatalf("recover: %v", err)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("new target survived recovery: %v", err)
		}
	})

	t.Run("restores a displaced target", func(t *testing.T) {
		skillsDir := t.TempDir()
		stagingRoot := filepath.Join(skillsDir, ".agent-layer-projection-staging")
		backup := filepath.Join(stagingRoot, "backup", "alpha")
		if err := os.MkdirAll(backup, 0o750); err != nil {
			t.Fatalf("mkdir backup: %v", err)
		}
		if err := os.WriteFile(filepath.Join(backup, "old.txt"), []byte("old"), 0o600); err != nil {
			t.Fatalf("write backup: %v", err)
		}
		if err := recoverSkillProjection(RealSystem{}, skillsDir, stagingRoot); err != nil {
			t.Fatalf("recover: %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(skillsDir, "alpha", "old.txt")); err != nil || string(data) != "old" { // #nosec G304 -- path is constructed entirely from test-controlled temporary paths.
			t.Fatalf("restored target = %q, %v", data, err)
		}
	})

	t.Run("discards a backup after the target was published", func(t *testing.T) {
		skillsDir := t.TempDir()
		stagingRoot := filepath.Join(skillsDir, ".agent-layer-projection-staging")
		backup := filepath.Join(stagingRoot, "backup", "alpha")
		target := filepath.Join(skillsDir, "alpha")
		if err := os.MkdirAll(backup, 0o750); err != nil {
			t.Fatalf("mkdir backup: %v", err)
		}
		if err := os.MkdirAll(target, 0o750); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		if err := os.WriteFile(filepath.Join(target, "new.txt"), []byte("new"), 0o600); err != nil {
			t.Fatalf("write target: %v", err)
		}
		if err := recoverSkillProjection(RealSystem{}, skillsDir, stagingRoot); err != nil {
			t.Fatalf("recover: %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(target, "new.txt")); err != nil || string(data) != "new" { // #nosec G304 -- path is constructed entirely from test-controlled temporary paths.
			t.Fatalf("published target = %q, %v", data, err)
		}
	})

	t.Run("rejects unsafe recovery names", func(t *testing.T) {
		for _, directory := range []string{"created", "backup"} {
			t.Run(directory, func(t *testing.T) {
				skillsDir := t.TempDir()
				stagingRoot := filepath.Join(skillsDir, ".agent-layer-projection-staging")
				unsafe := filepath.Join(stagingRoot, directory, "Not-Normalized")
				if directory == "created" {
					if err := os.MkdirAll(filepath.Dir(unsafe), 0o750); err != nil {
						t.Fatalf("mkdir marker dir: %v", err)
					}
					if err := os.WriteFile(unsafe, nil, 0o600); err != nil {
						t.Fatalf("write marker: %v", err)
					}
				} else if err := os.MkdirAll(unsafe, 0o750); err != nil {
					t.Fatalf("mkdir backup: %v", err)
				}
				if err := recoverSkillProjection(RealSystem{}, skillsDir, stagingRoot); err == nil {
					t.Fatal("unsafe recovery state must fail")
				}
			})
		}
	})
}

func TestLoadOwnedSkillsValidatesLegacyAndCurrentManifests(t *testing.T) {
	t.Parallel()
	validHash := desiredSkillHash(map[string]desiredSkillResource{
		"SKILL.md": {data: []byte("manifest"), mode: 0o644},
	})
	cases := map[string]struct {
		manifest string
		wantName string
		wantErr  bool
	}{
		"legacy":            {`{"version":1,"skills":["alpha"]}`, "alpha", false},
		"legacy unsafe":     {`{"version":1,"skills":["../alpha"]}`, "", true},
		"legacy duplicate":  {`{"version":1,"skills":["alpha","alpha"]}`, "", true},
		"unsupported":       {`{"version":99,"skills":[]}`, "", true},
		"current unsafe":    {`{"version":2,"skills":["../alpha"],"hashes":{"../alpha":"` + validHash + `"}}`, "", true},
		"current duplicate": {`{"version":2,"skills":["alpha","alpha"],"hashes":{"alpha":"` + validHash + `"}}`, "", true},
		"invalid hash":      {`{"version":2,"skills":["alpha"],"hashes":{"alpha":"sha256-v1:nope"}}`, "", true},
		"extra hash":        {`{"version":2,"skills":[],"hashes":{"alpha":"` + validHash + `"}}`, "", true},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			skillsDir := t.TempDir()
			path := filepath.Join(skillsDir, ownedSkillsManifestName)
			if err := os.WriteFile(path, []byte(testCase.manifest), 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			owned, err := loadOwnedSkills(RealSystem{}, skillsDir)
			if testCase.wantErr {
				if err == nil {
					t.Fatal("invalid ownership manifest must fail")
				}
				return
			}
			if err != nil {
				t.Fatalf("load manifest: %v", err)
			}
			if _, ok := owned[testCase.wantName]; !ok {
				t.Fatalf("owned = %v, want %q", owned, testCase.wantName)
			}
		})
	}
}

func TestProjectedSkillIsAByteForByteCopyOfItsSource(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcDir := t.TempDir()

	// Deliberately unusual formatting: a comment, an unsorted optional field, a
	// blank line inside the frontmatter, and no trailing newline. A re-rendering
	// projection would normalize all of it away.
	manifest := "---\n" +
		"# author's note, kept verbatim\n" +
		"license: MIT\n" +
		"name: alpha\n" +
		"\n" +
		"description: The alpha skill.\n" +
		"---\n\nBody with  odd   spacing and no trailing newline"
	sourceManifest := filepath.Join(srcDir, "SKILL.md")
	if err := os.WriteFile(sourceManifest, []byte(manifest), 0o600); err != nil {
		t.Fatalf("write source SKILL.md: %v", err)
	}

	skill := config.Skill{Name: "alpha", SourceDir: srcDir, SourcePath: sourceManifest}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("WriteClaudeSkills error: %v", err)
	}

	projected, err := os.ReadFile(filepath.Join(root, ".claude", "skills", "alpha", "SKILL.md")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read projected SKILL.md: %v", err)
	}
	if string(projected) != manifest {
		t.Fatalf("projected SKILL.md is not byte-identical to its source.\nwant:\n%q\ngot:\n%q", manifest, projected)
	}
	// Agent Layer metadata must never appear inside a skill an agent reads.
	if strings.Contains(string(projected), generatedMarkerHeader) {
		t.Fatalf("a generated header was injected into the projected skill:\n%s", projected)
	}
}

func TestBothSkillTiersProjectThroughTheSamePath(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	userSkill := sourceSkill(t, "user-tier")
	imported := sourceSkill(t, "import-tier")
	imported.Imported = true

	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{userSkill, imported}); err != nil {
		t.Fatalf("WriteClaudeSkills error: %v", err)
	}

	for _, name := range []string{"user-tier", "import-tier"} {
		projected, err := os.ReadFile(filepath.Join(root, ".claude", "skills", name, "SKILL.md")) // #nosec G304 -- path is constructed from test-controlled inputs.
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		// An imported skill is projected exactly like a user-managed one; nothing
		// in the output records where it came from.
		if string(projected) != skillManifestFixture(name) {
			t.Fatalf("%s projection is not byte-identical: %q", name, projected)
		}
	}
}

func TestProjectionCopiesTheExhaustiveResourceSet(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcDir := t.TempDir()

	write := func(relative string, content string, mode os.FileMode) {
		t.Helper()
		target := filepath.Join(srcDir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(target, []byte(content), mode); err != nil {
			t.Fatalf("write %s: %v", relative, err)
		}
		if err := os.Chmod(target, mode); err != nil {
			t.Fatalf("chmod %s: %v", relative, err)
		}
	}
	write("SKILL.md", skillManifestFixture("alpha"), 0o600)
	write("references/REF.md", "# Ref", 0o600)
	write("scripts/run.sh", "#!/bin/sh\necho hi\n", 0o755)
	write(".hidden", "secret", 0o600)
	write(".DS_Store", "finder", 0o600)
	write("Thumbs.db", "explorer", 0o600)
	write(".git/config", "[core]", 0o600)

	skill := config.Skill{Name: "alpha", SourceDir: srcDir}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("WriteClaudeSkills error: %v", err)
	}
	destDir := filepath.Join(root, ".claude", "skills", "alpha")

	// A dotfile is part of the skill: an agent told to read it at runtime must
	// find it.
	hidden, err := os.ReadFile(filepath.Join(destDir, ".hidden")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil || string(hidden) != "secret" {
		t.Fatalf("hidden resource was not projected: data=%q err=%v", hidden, err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "references", "REF.md")); err != nil {
		t.Fatalf("nested reference was not projected: %v", err)
	}

	// Only repository and platform noise is excluded.
	for _, excluded := range []string{".DS_Store", "Thumbs.db", ".git"} {
		if _, err := os.Stat(filepath.Join(destDir, excluded)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be excluded from the projection", excluded)
		}
	}

	// The executable bit is content: a script the skill tells an agent to run
	// must still be runnable.
	info, err := os.Stat(filepath.Join(destDir, "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("stat projected script: %v", err)
	}
	if info.Mode().Perm() != skillResourceExecutableMode {
		t.Fatalf("projected script mode = %v, want %v", info.Mode().Perm(), skillResourceExecutableMode)
	}
	// A restrictive source permission must not make a projected resource
	// unreadable to the agent.
	refInfo, err := os.Stat(filepath.Join(destDir, "references", "REF.md"))
	if err != nil {
		t.Fatalf("stat projected reference: %v", err)
	}
	if refInfo.Mode().Perm() != skillResourceRegularMode {
		t.Fatalf("projected reference mode = %v, want %v", refInfo.Mode().Perm(), skillResourceRegularMode)
	}
}

func TestProjectionRemovesOwnedSkillsAndKeepsUnownedOnes(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := projectedSkillsDir(root, "claude")

	alpha := sourceSkill(t, "alpha")
	beta := sourceSkill(t, "beta")
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{alpha, beta}); err != nil {
		t.Fatalf("first projection: %v", err)
	}

	// A skill the user wrote straight into the client directory. Agent Layer
	// never created it, so it must survive every later sync.
	manualDir := filepath.Join(skillsDir, "hand-written")
	if err := os.MkdirAll(manualDir, 0o750); err != nil {
		t.Fatalf("mkdir manual skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(manualDir, "SKILL.md"), []byte(skillManifestFixture("hand-written")), 0o600); err != nil {
		t.Fatalf("write manual skill: %v", err)
	}

	if manifest := readOwnedManifest(t, skillsDir); strings.Join(manifest.Skills, ",") != "alpha,beta" {
		t.Fatalf("ownership manifest = %v, want the two projected skills", manifest.Skills)
	}

	// beta leaves the source set.
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{alpha}); err != nil {
		t.Fatalf("second projection: %v", err)
	}

	if _, err := os.Stat(filepath.Join(skillsDir, "beta")); !os.IsNotExist(err) {
		t.Fatalf("a projected skill that left the source set must be removed")
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "alpha", "SKILL.md")); err != nil {
		t.Fatalf("the surviving skill was disturbed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(manualDir, "SKILL.md")); err != nil {
		t.Fatalf("an unowned client skill directory was removed: %v", err)
	}
	if manifest := readOwnedManifest(t, skillsDir); strings.Join(manifest.Skills, ",") != "alpha" {
		t.Fatalf("ownership manifest = %v, want only alpha", manifest.Skills)
	}
}

func TestOwnershipManifestLivesBesideTheSkillsNotInsideThem(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "alpha")}); err != nil {
		t.Fatalf("projection: %v", err)
	}
	skillsDir := projectedSkillsDir(root, "claude")

	// The manifest is a sibling of the skill directories, so it is not mistaken
	// for a skill and it puts no metadata inside any SKILL.md.
	if _, err := os.Stat(filepath.Join(skillsDir, ownedSkillsManifestName)); err != nil {
		t.Fatalf("ownership manifest was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "alpha", ownedSkillsManifestName)); !os.IsNotExist(err) {
		t.Fatalf("ownership metadata must not be written inside a skill directory")
	}
	projected, err := os.ReadFile(filepath.Join(skillsDir, "alpha", "SKILL.md")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read projected skill: %v", err)
	}
	if string(projected) != skillManifestFixture("alpha") {
		t.Fatalf("SKILL.md carries ownership metadata: %q", projected)
	}
}

func TestProjectionAdoptsSkillsGeneratedBeforeTheOwnershipManifest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := projectedSkillsDir(root, "claude")
	if err := os.MkdirAll(filepath.Join(skillsDir, "legacy"), 0o750); err != nil {
		t.Fatalf("mkdir legacy skill: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(skillsDir, "hand-written"), 0o750); err != nil {
		t.Fatalf("mkdir manual skill: %v", err)
	}
	// A skill an older release generated, identified by the header it injected.
	if err := os.WriteFile(filepath.Join(skillsDir, "legacy", "SKILL.md"), []byte(generatedMarkerFixture), 0o600); err != nil {
		t.Fatalf("write legacy skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "hand-written", "SKILL.md"), []byte("# manual\n"), 0o600); err != nil {
		t.Fatalf("write manual skill: %v", err)
	}

	// The upgraded release projects a different skill set, so the previously
	// generated one is stale and must still be cleaned up.
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "alpha")}); err != nil {
		t.Fatalf("projection: %v", err)
	}

	if _, err := os.Stat(filepath.Join(skillsDir, "legacy")); !os.IsNotExist(err) {
		t.Fatalf("a skill generated before the manifest must still be removable")
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "hand-written", "SKILL.md")); err != nil {
		t.Fatalf("a hand-written skill must survive the migration: %v", err)
	}
}

func TestProjectionRejectsAnUnreadableOwnershipManifest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := projectedSkillsDir(root, "claude")
	if err := os.MkdirAll(skillsDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, ownedSkillsManifestName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "alpha")})
	if err == nil {
		t.Fatal("a manifest Agent Layer cannot read must fail rather than silently forget what it owns")
	}
	if !strings.Contains(err.Error(), "delete it") {
		t.Fatalf("the error must say how to recover: %v", err)
	}

	if err := os.WriteFile(filepath.Join(skillsDir, ownedSkillsManifestName), []byte(`{"version":99,"skills":[]}`), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "alpha")}); err == nil {
		t.Fatal("an unsupported manifest version must fail")
	}
}

func TestProjectionRemovesTheManifestWhenNothingIsOwned(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := projectedSkillsDir(root, "claude")
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "alpha")}); err != nil {
		t.Fatalf("first projection: %v", err)
	}
	if err := WriteClaudeSkills(RealSystem{}, root, nil); err != nil {
		t.Fatalf("second projection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillsDir, "alpha")); !os.IsNotExist(err) {
		t.Fatalf("the last owned skill must be removed")
	}
	// An empty manifest would be indistinguishable from "no record", so it is
	// removed instead of left behind.
	if _, err := os.Stat(filepath.Join(skillsDir, ownedSkillsManifestName)); !os.IsNotExist(err) {
		t.Fatalf("an empty ownership manifest must not be left behind")
	}
}

func TestProjectionProjectsALegacyLowercaseSourceManifestUnderTheCanonicalName(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcDir := t.TempDir()
	content := skillManifestFixture("alpha")
	if err := os.WriteFile(filepath.Join(srcDir, "skill.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write lowercase source manifest: %v", err)
	}

	skill := config.Skill{Name: "alpha", SourceDir: srcDir}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("WriteClaudeSkills error: %v", err)
	}

	// internal/config still accepts the lowercase spelling in a source, but
	// clients only look for SKILL.md, so the bytes land under the canonical name.
	projected, err := os.ReadFile(filepath.Join(root, ".claude", "skills", "alpha", "SKILL.md")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read projected skill: %v", err)
	}
	if string(projected) != content {
		t.Fatalf("projected content = %q, want the source bytes", projected)
	}
}

func TestProjectionRejectsASkillWithoutASourceDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{{Name: "alpha"}})
	if err == nil {
		t.Fatal("a skill with nothing to copy must fail rather than project an empty directory")
	}
	if !strings.Contains(err.Error(), "no source directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteSkillFiles_PathTraversalRejected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skill := sourceSkill(t, "alpha")
	skill.Name = "../escape"
	err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill})
	if err == nil {
		t.Fatalf("expected error for path traversal in skill name")
	}
	if !strings.Contains(err.Error(), "invalid skill name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteAgentSkillsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := WriteAgentSkills(RealSystem{}, file, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteAgentSkillsWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			return errors.New("write failed")
		},
	}
	if err := WriteAgentSkills(sys, root, []config.Skill{sourceSkill(t, "alpha")}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteAgentSkillsManifestWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		MarshalIndentFunc: func(any, string, string) ([]byte, error) {
			return nil, errors.New("encode failed")
		},
	}
	// If ownership cannot be recorded, a later sync could not clean up what it
	// just created, so the failure must surface now.
	if err := WriteAgentSkills(sys, root, []config.Skill{sourceSkill(t, "alpha")}); err == nil {
		t.Fatalf("expected an error when the ownership manifest cannot be encoded")
	}
}

func TestWriteAgentSkillsMkdirSkillDirError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Make skills dir read-only so creating the skill directory fails.
	if err := os.Chmod(skillsDir, 0o500); err != nil { // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(skillsDir, 0o755) }) // #nosec G302 -- test toggles dir/file mode bits to drive a production error path; the executable/traversal bit is intentional.
	err := WriteAgentSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "alpha")})
	if err == nil {
		t.Fatalf("expected error for skill dir removal/creation failure")
	}
}

func TestWriteClaudeSkills(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "alpha")}); err != nil {
		t.Fatalf("WriteClaudeSkills error: %v", err)
	}
	assertCanonicalSkillEntrypoint(t, root, filepath.Join(".claude", "skills"), "alpha")
}

func TestWriteAgentSkills(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := WriteAgentSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "beta")}); err != nil {
		t.Fatalf("WriteAgentSkills error: %v", err)
	}
	assertCanonicalSkillEntrypoint(t, root, filepath.Join(".agents", "skills"), "beta")
}

func TestWriteAgentSkillsRefreshKeepsSkillReadable(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skills := []config.Skill{sourceSkill(t, "alpha")}
	if err := WriteAgentSkills(RealSystem{}, root, skills); err != nil {
		t.Fatalf("initial WriteAgentSkills error: %v", err)
	}

	skillDir := filepath.Join(root, ".agents", "skills", "alpha")
	skillPath := filepath.Join(skillDir, "SKILL.md")
	var readerErr error
	observeRemoval := func(path string) {
		relativeSkillPath, err := filepath.Rel(path, skillPath)
		if err != nil || relativeSkillPath == ".." || strings.HasPrefix(relativeSkillPath, ".."+string(filepath.Separator)) {
			return
		}
		_, readerErr = os.ReadFile(skillPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	}
	sys := &MockSystem{
		Fallback: RealSystem{},
		RemoveFunc: func(path string) error {
			if err := (RealSystem{}).Remove(path); err != nil {
				return err
			}
			observeRemoval(path)
			return nil
		},
		RemoveAllFunc: func(path string) error {
			if err := (RealSystem{}).RemoveAll(path); err != nil {
				return err
			}
			observeRemoval(path)
			return nil
		},
	}

	if err := WriteAgentSkills(sys, root, skills); err != nil {
		t.Fatalf("refresh WriteAgentSkills error: %v", err)
	}
	if readerErr != nil {
		t.Fatalf("existing SKILL.md became unreadable during refresh: %v", readerErr)
	}
}

func TestWriteAgentSkillsStageFailureLeavesCompleteLiveTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skill := sourceSkill(t, "alpha")
	if err := WriteAgentSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("initial projection: %v", err)
	}
	live := filepath.Join(root, ".agents", "skills", "alpha", "SKILL.md")
	original, err := os.ReadFile(live) // #nosec G304 -- live is rooted in a test-owned temporary directory.
	if err != nil {
		t.Fatalf("read original projection: %v", err)
	}
	if err := os.WriteFile(skill.SourcePath, []byte(skillManifestFixture("alpha")+"\nchanged\n"), 0o600); err != nil {
		t.Fatalf("change source: %v", err)
	}
	sys := &MockSystem{Fallback: RealSystem{}, WriteFileAtomicFunc: func(path string, data []byte, mode os.FileMode) error {
		if strings.Contains(path, ".agent-layer-projection-staging") && filepath.Base(path) == "SKILL.md" {
			return errors.New("injected stage failure")
		}
		return (RealSystem{}).WriteFileAtomic(path, data, mode)
	}}
	if err := WriteAgentSkills(sys, root, []config.Skill{skill}); err == nil {
		t.Fatal("expected staged projection failure")
	}
	after, err := os.ReadFile(live) // #nosec G304 -- live is rooted in a test-owned temporary directory.
	if err != nil {
		t.Fatalf("live projection became unreadable: %v", err)
	}
	if string(after) != string(original) {
		t.Fatalf("stage failure exposed partial new content: %q", after)
	}
}

func TestWriteAgentSkillsReplacesPreexistingSkillDirectorySymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	externalDir := t.TempDir()
	externalSkillDir := filepath.Join(externalDir, "alpha")
	if err := os.MkdirAll(externalSkillDir, 0o700); err != nil {
		t.Fatalf("mkdir external skill: %v", err)
	}
	externalSkillPath := filepath.Join(externalSkillDir, "SKILL.md")
	if err := os.WriteFile(externalSkillPath, []byte("external content"), 0o600); err != nil {
		t.Fatalf("write external skill: %v", err)
	}

	skillsDir := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(skillsDir, 0o750); err != nil { // #nosec G301 -- the fixture uses the same managed-directory mode as production.
		t.Fatalf("mkdir skills: %v", err)
	}
	skillDir := filepath.Join(skillsDir, "alpha")
	if err := os.Symlink(externalSkillDir, skillDir); err != nil {
		t.Fatalf("symlink skill directory: %v", err)
	}

	if err := WriteAgentSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "alpha")}); err == nil {
		t.Fatal("expected an unowned client skill symlink collision to be refused")
	}

	info, err := os.Lstat(skillDir)
	if err != nil {
		t.Fatalf("lstat generated skill directory: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("unowned client symlink was replaced, got mode %v", info.Mode())
	}

	// Writing through the symlink would have overwritten content outside the
	// projection root.
	data, err := os.ReadFile(externalSkillPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read external skill: %v", err)
	}
	if string(data) != "external content" {
		t.Fatalf("external symlink target was modified: %q", data)
	}
}

func TestCopySkillTree_EmptySourceDir(t *testing.T) {
	t.Parallel()
	skill := config.Skill{Name: "test", SourceDir: ""}
	if err := copySkillTree(RealSystem{}, skill, t.TempDir()); err != nil {
		t.Fatalf("expected nil error for empty SourceDir, got %v", err)
	}
}

func TestCopySkillTreeCopiesNestedManifestsToo(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte("top-level"), 0o600); err != nil {
		t.Fatalf("write top SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir, "references"), 0o700); err != nil {
		t.Fatalf("mkdir references: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "references", "SKILL.md"), []byte("nested"), 0o600); err != nil {
		t.Fatalf("write nested SKILL.md: %v", err)
	}

	skill := config.Skill{Name: "test", SourceDir: srcDir}
	if err := copySkillTree(RealSystem{}, skill, destDir); err != nil {
		t.Fatalf("copySkillTree error: %v", err)
	}

	for relative, want := range map[string]string{
		"SKILL.md":            "top-level",
		"references/SKILL.md": "nested",
	} {
		data, err := os.ReadFile(filepath.Join(destDir, filepath.FromSlash(relative))) // #nosec G304 -- path is constructed from test-controlled inputs.
		if err != nil {
			t.Fatalf("read %s: %v", relative, err)
		}
		if string(data) != want {
			t.Fatalf("%s = %q, want %q", relative, data, want)
		}
	}
}

func TestCleanSharedAgentSkillsRemovesOwnedOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := WriteAgentSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "generated")}); err != nil {
		t.Fatalf("projection: %v", err)
	}
	manualDir := filepath.Join(root, ".agents", "skills", "manual")
	if err := os.MkdirAll(manualDir, 0o750); err != nil {
		t.Fatalf("mkdir manual: %v", err)
	}
	if err := os.WriteFile(filepath.Join(manualDir, "SKILL.md"), []byte("# manual\n"), 0o600); err != nil {
		t.Fatalf("write manual skill: %v", err)
	}

	if err := cleanSharedAgentSkills(RealSystem{}, root); err != nil {
		t.Fatalf("cleanSharedAgentSkills error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "skills", "generated")); !os.IsNotExist(err) {
		t.Fatalf("expected the owned shared skill to be removed")
	}
	if _, err := os.Stat(manualDir); err != nil {
		t.Fatalf("expected the manual shared skill to remain: %v", err)
	}
}

func TestCleanSharedAgentSkillsMissingDir(t *testing.T) {
	t.Parallel()
	if err := cleanSharedAgentSkills(RealSystem{}, t.TempDir()); err != nil {
		t.Fatalf("a project that never projected shared skills must be a no-op, got %v", err)
	}
}

func TestCleanLegacySkillOutputsRemovesRetiredDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, rel := range []string{
		filepath.Join(".codex", "skills", "alpha"),
		filepath.Join(".agent", "skills", "alpha"),
		filepath.Join(".gemini", "skills", "alpha"),
		filepath.Join(".github", "skills", "alpha"),
		filepath.Join(".vscode", "prompts"),
	} {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	if err := cleanLegacySkillOutputs(RealSystem{}, root); err != nil {
		t.Fatalf("cleanLegacySkillOutputs error: %v", err)
	}

	for _, rel := range []string{
		filepath.Join(".codex", "skills"),
		filepath.Join(".agent", "skills"),
		filepath.Join(".gemini", "skills"),
		filepath.Join(".github", "skills"),
		filepath.Join(".vscode", "prompts"),
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed", rel)
		}
	}
}

func TestHasGeneratedMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "file.md")
	if err := os.WriteFile(path, []byte(generatedMarkerFixture), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	ok, err := hasGeneratedMarker(RealSystem{}, path)
	if err != nil || !ok {
		t.Fatalf("expected generated marker, got %v %v", ok, err)
	}
	missing, err := hasGeneratedMarker(RealSystem{}, filepath.Join(dir, "missing.md"))
	if err != nil || missing {
		t.Fatalf("expected missing to return false, got %v %v", missing, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "dir"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err = hasGeneratedMarker(RealSystem{}, filepath.Join(dir, "dir"))
	if err == nil {
		t.Fatalf("expected error for directory path")
	}
}

func TestCopyDirRecursive_ReadFilePermissionError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Create an unreadable file.
	unreadable := filepath.Join(srcDir, "secret.sh")
	if err := os.WriteFile(unreadable, []byte("data"), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })

	err := copyDirRecursive(RealSystem{}, srcDir, destDir)
	if err == nil {
		t.Fatalf("expected error for unreadable file")
	}
	if !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("expected read error, got: %v", err)
	}
}

func TestCopyDirRecursive_NonexistentSourceDir(t *testing.T) {
	t.Parallel()
	destDir := t.TempDir()
	err := copyDirRecursive(RealSystem{}, filepath.Join(t.TempDir(), "nonexistent"), destDir)
	if err != nil {
		t.Fatalf("expected nil for nonexistent source dir, got: %v", err)
	}
}

func TestWriteClaudeSkillsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	err := WriteClaudeSkills(RealSystem{}, file, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteClaudeSkillsWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			return errors.New("write failed")
		},
	}
	if err := WriteClaudeSkills(sys, root, []config.Skill{sourceSkill(t, "alpha")}); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteClaudeSkillsStaleSubFileCleanup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	srcDir1 := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir1, "SKILL.md"), []byte(skillManifestFixture("alpha")), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir1, "scripts"), 0o700); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir1, "scripts", "old.sh"), []byte("#!/bin/sh\necho old"), 0o755); err != nil { // #nosec G306 -- test writes a fixture whose perm value drives the production code path under test.
		t.Fatalf("write old.sh: %v", err)
	}

	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{{Name: "alpha", SourceDir: srcDir1}}); err != nil {
		t.Fatalf("first WriteClaudeSkills error: %v", err)
	}
	oldScript := filepath.Join(root, ".claude", "skills", "alpha", "scripts", "old.sh")
	if _, err := os.Stat(oldScript); err != nil {
		t.Fatalf("expected old.sh after first sync: %v", err)
	}

	// The source now has scripts/new.sh instead.
	srcDir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir2, "SKILL.md"), []byte(skillManifestFixture("alpha")), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(srcDir2, "scripts"), 0o700); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir2, "scripts", "new.sh"), []byte("#!/bin/sh\necho new"), 0o755); err != nil { // #nosec G306 -- test writes a fixture whose perm value drives the production code path under test.
		t.Fatalf("write new.sh: %v", err)
	}

	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{{Name: "alpha", SourceDir: srcDir2}}); err != nil {
		t.Fatalf("second WriteClaudeSkills error: %v", err)
	}

	if _, err := os.Stat(oldScript); !os.IsNotExist(err) {
		t.Fatalf("expected old.sh to be removed after second sync")
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "alpha", "scripts", "new.sh")); err != nil {
		t.Fatalf("expected new.sh after second sync: %v", err)
	}
}

func TestWriteClaudeSkillsReconcilesResourceTree(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte(skillManifestFixture("alpha")), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	keepScript := filepath.Join(srcDir, "scripts", "keep.sh")
	staleFile := filepath.Join(srcDir, "references", "obsolete", "nested.txt")
	if err := os.MkdirAll(filepath.Dir(keepScript), 0o700); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	if err := os.WriteFile(keepScript, []byte("#!/bin/sh\necho keep"), 0o755); err != nil { // #nosec G306 -- execute permission is the behavior under test.
		t.Fatalf("write keep script: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(staleFile), 0o700); err != nil {
		t.Fatalf("mkdir stale references: %v", err)
	}
	if err := os.WriteFile(staleFile, []byte("stale"), 0o600); err != nil {
		t.Fatalf("write stale reference: %v", err)
	}

	skill := config.Skill{Name: "alpha", SourceDir: srcDir}
	refresh := func(sourceDir string) {
		t.Helper()
		skill.SourceDir = sourceDir
		if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
			t.Fatalf("WriteClaudeSkills refresh error: %v", err)
		}
	}

	refresh(srcDir)
	refresh(srcDir)
	assertCanonicalSkillEntrypoint(t, root, filepath.Join(".claude", "skills"), "alpha")
	destScript := filepath.Join(root, ".claude", "skills", "alpha", "scripts", "keep.sh")
	data, err := os.ReadFile(destScript) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil || !strings.Contains(string(data), "echo keep") {
		t.Fatalf("unchanged desired resource did not survive refresh: data=%q err=%v", data, err)
	}
	info, err := os.Stat(destScript)
	if err != nil {
		t.Fatalf("stat refreshed executable: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("expected execute permission after refresh, got %v", info.Mode())
	}

	if err := os.RemoveAll(filepath.Join(srcDir, "references")); err != nil {
		t.Fatalf("remove source references: %v", err)
	}
	refresh(srcDir)
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "alpha", "references")); !os.IsNotExist(err) {
		t.Fatalf("expected whole stale resource directory to be removed, got %v", err)
	}

	// A source directory that vanished empties the projected skill rather than
	// leaving stale content an agent would still read.
	refresh(filepath.Join(t.TempDir(), "missing"))
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "alpha", "scripts")); !os.IsNotExist(err) {
		t.Fatalf("expected a missing SourceDir to remove resources, got %v", err)
	}
}

func TestWriteClaudeSkillsReconcilesResourceTypeTransitions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "SKILL.md"), []byte(skillManifestFixture("alpha")), 0o600); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	sourceResource := filepath.Join(srcDir, "resource")
	if err := os.WriteFile(sourceResource, []byte("file first"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	skill := config.Skill{Name: "alpha", SourceDir: srcDir}
	refresh := func() {
		t.Helper()
		if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
			t.Fatalf("WriteClaudeSkills refresh error: %v", err)
		}
		assertCanonicalSkillEntrypoint(t, root, filepath.Join(".claude", "skills"), skill.Name)
	}
	destResource := filepath.Join(root, ".claude", "skills", "alpha", "resource")

	refresh()
	if err := os.Remove(sourceResource); err != nil {
		t.Fatalf("remove source file: %v", err)
	}
	if err := os.MkdirAll(sourceResource, 0o700); err != nil {
		t.Fatalf("mkdir source resource: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceResource, "nested.txt"), []byte("nested"), 0o600); err != nil {
		t.Fatalf("write nested source file: %v", err)
	}
	refresh()
	if data, err := os.ReadFile(filepath.Join(destResource, "nested.txt")); err != nil || string(data) != "nested" { // #nosec G304 -- path is constructed from test-controlled inputs.
		t.Fatalf("file-to-directory transition failed: data=%q err=%v", data, err)
	}

	if err := os.RemoveAll(sourceResource); err != nil {
		t.Fatalf("remove source resource directory: %v", err)
	}
	if err := os.WriteFile(sourceResource, []byte("file again"), 0o600); err != nil {
		t.Fatalf("write replacement source file: %v", err)
	}
	refresh()
	if data, err := os.ReadFile(destResource); err != nil || string(data) != "file again" { // #nosec G304 -- path is constructed from test-controlled inputs.
		t.Fatalf("non-empty-directory-to-file transition failed: data=%q err=%v", data, err)
	}
}

func assertCanonicalSkillEntrypoint(t *testing.T, root string, skillsPath string, skillName string) {
	t.Helper()
	path := filepath.Join(root, skillsPath, skillName, "SKILL.md")
	data, err := os.ReadFile(path) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("canonical SKILL.md is unreadable after refresh: %v", err)
	}
	if !strings.Contains(string(data), "name: "+skillName) {
		t.Fatalf("canonical SKILL.md has unexpected content: %q", data)
	}
}

func TestCopyDirRecursive_LstatError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		LstatFunc: func(name string) (os.FileInfo, error) {
			if strings.HasSuffix(name, "data.txt") {
				return nil, errors.New("lstat failed")
			}
			return RealSystem{}.Lstat(name)
		},
	}

	err := copyDirRecursive(sys, srcDir, destDir)
	if err == nil {
		t.Fatalf("expected error from Lstat failure")
	}
	if !strings.Contains(err.Error(), "lstat failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCopyDirRecursive_DestinationLstatError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		LstatFunc: func(string) (os.FileInfo, error) {
			return nil, errors.New("destination lstat failed")
		},
	}
	err := copyDirRecursive(sys, srcDir, destDir)
	if err == nil || !strings.Contains(err.Error(), "destination lstat failed") {
		t.Fatalf("expected actionable destination lstat error, got %v", err)
	}
}

func TestCopyDirRecursive_ResourceConflictErrors(t *testing.T) {
	t.Parallel()

	t.Run("remove conflicting file", func(t *testing.T) {
		srcDir := t.TempDir()
		destDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(srcDir, "resource"), 0o700); err != nil {
			t.Fatalf("mkdir source resource: %v", err)
		}
		if err := os.WriteFile(filepath.Join(destDir, "resource"), []byte("old"), 0o600); err != nil {
			t.Fatalf("write destination resource: %v", err)
		}

		sys := &MockSystem{
			Fallback: RealSystem{},
			RemoveFunc: func(string) error {
				return errors.New("conflict removal failed")
			},
		}
		err := copyDirRecursive(sys, srcDir, destDir)
		if err == nil || !strings.Contains(err.Error(), "conflict removal failed") {
			t.Fatalf("expected actionable conflict removal error, got %v", err)
		}
	})

	t.Run("create desired directory", func(t *testing.T) {
		srcDir := t.TempDir()
		destDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(srcDir, "resource"), 0o700); err != nil {
			t.Fatalf("mkdir source resource: %v", err)
		}

		sys := &MockSystem{
			Fallback: RealSystem{},
			MkdirAllFunc: func(path string, perm os.FileMode) error {
				if filepath.Base(path) == "resource" {
					return errors.New("desired directory creation failed")
				}
				return RealSystem{}.MkdirAll(path, perm)
			},
		}
		err := copyDirRecursive(sys, srcDir, destDir)
		if err == nil || !strings.Contains(err.Error(), "desired directory creation failed") {
			t.Fatalf("expected actionable desired directory error, got %v", err)
		}
	})
}

func TestCopyDirRecursive_StaleCleanupErrors(t *testing.T) {
	t.Parallel()

	t.Run("read destination", func(t *testing.T) {
		destDir := t.TempDir()
		sys := &MockSystem{
			Fallback: RealSystem{},
			ReadDirFunc: func(path string) ([]os.DirEntry, error) {
				if path == destDir {
					return nil, errors.New("destination read failed")
				}
				return RealSystem{}.ReadDir(path)
			},
		}
		err := copyDirRecursive(sys, "", destDir)
		if err == nil || !strings.Contains(err.Error(), "destination read failed") {
			t.Fatalf("expected actionable destination read error, got %v", err)
		}
	})

	t.Run("inspect stale node", func(t *testing.T) {
		destDir := t.TempDir()
		stalePath := filepath.Join(destDir, "stale.txt")
		if err := os.WriteFile(stalePath, []byte("stale"), 0o600); err != nil {
			t.Fatalf("write stale resource: %v", err)
		}
		sys := &MockSystem{
			Fallback: RealSystem{},
			LstatFunc: func(path string) (os.FileInfo, error) {
				if path == stalePath {
					return nil, errors.New("stale node lstat failed")
				}
				return RealSystem{}.Lstat(path)
			},
		}
		err := copyDirRecursive(sys, "", destDir)
		if err == nil || !strings.Contains(err.Error(), "stale node lstat failed") {
			t.Fatalf("expected actionable stale-node lstat error, got %v", err)
		}
	})

	t.Run("remove stale node", func(t *testing.T) {
		destDir := t.TempDir()
		stalePath := filepath.Join(destDir, "stale.txt")
		if err := os.WriteFile(stalePath, []byte("stale"), 0o600); err != nil {
			t.Fatalf("write stale resource: %v", err)
		}
		sys := &MockSystem{
			Fallback: RealSystem{},
			RemoveFunc: func(path string) error {
				if path == stalePath {
					return errors.New("stale node removal failed")
				}
				return RealSystem{}.Remove(path)
			},
		}
		err := copyDirRecursive(sys, "", destDir)
		if err == nil || !strings.Contains(err.Error(), "stale node removal failed") {
			t.Fatalf("expected actionable stale-node removal error, got %v", err)
		}
	})
}

func TestCopyDirRecursive_WriteFileAtomicSubFileError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			if strings.HasSuffix(filename, "data.txt") {
				return errors.New("write sub-file failed")
			}
			return RealSystem{}.WriteFileAtomic(filename, data, perm)
		},
	}

	err := copyDirRecursive(sys, srcDir, destDir)
	if err == nil {
		t.Fatalf("expected error from WriteFileAtomic failure")
	}
	if !strings.Contains(err.Error(), "write sub-file failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCopyDirRecursive_SkipsSymlinks(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	// Create a regular file and a symlink.
	if err := os.WriteFile(filepath.Join(srcDir, "real.txt"), []byte("real"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(filepath.Join(srcDir, "real.txt"), filepath.Join(srcDir, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := copyDirRecursive(RealSystem{}, srcDir, destDir); err != nil {
		t.Fatalf("copyDirRecursive error: %v", err)
	}

	// real.txt should be copied.
	if _, err := os.Stat(filepath.Join(destDir, "real.txt")); err != nil {
		t.Fatalf("expected real.txt to be copied: %v", err)
	}
	// link.txt should be skipped.
	if _, err := os.Stat(filepath.Join(destDir, "link.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected link.txt (symlink) to be skipped")
	}
}

func TestCopyDirRecursive_UsesLstatForSourceSymlinkDetection(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()
	externalDir := t.TempDir()
	realPath := filepath.Join(srcDir, "real.txt")
	linkPath := filepath.Join(srcDir, "link.txt")
	externalPath := filepath.Join(externalDir, "target.txt")
	if err := os.WriteFile(realPath, []byte("real"), 0o600); err != nil {
		t.Fatalf("write real source file: %v", err)
	}
	if err := os.WriteFile(externalPath, []byte("external target"), 0o600); err != nil {
		t.Fatalf("write external target: %v", err)
	}
	if err := os.Symlink(externalPath, linkPath); err != nil {
		t.Fatalf("symlink source file: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		ReadDirFunc: func(path string) ([]os.DirEntry, error) {
			if path == srcDir {
				return []os.DirEntry{
					unknownTypeDirEntry{name: "real.txt"},
					unknownTypeDirEntry{name: "link.txt"},
				}, nil
			}
			return RealSystem{}.ReadDir(path)
		},
	}

	if err := copyDirRecursive(sys, srcDir, destDir); err != nil {
		t.Fatalf("copyDirRecursive error: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(destDir, "real.txt")); err != nil || string(data) != "real" { // #nosec G304 -- path is constructed from test-controlled inputs.
		t.Fatalf("real source file was not copied: data=%q err=%v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(destDir, "link.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected source symlink to be skipped, got %v", err)
	}
	data, err := os.ReadFile(externalPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read external target: %v", err)
	}
	if string(data) != "external target" {
		t.Fatalf("external source symlink target changed: %q", data)
	}
}

func TestCopyDirRecursive_NestedSourceReadError(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()
	nestedDir := filepath.Join(srcDir, "scripts")
	if err := os.MkdirAll(nestedDir, 0o700); err != nil {
		t.Fatalf("mkdir nested source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nestedDir, "run.sh"), []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatalf("write nested source file: %v", err)
	}

	// Fail ReadDir of the NESTED source subdirectory only, so the top-level
	// collect succeeds and recurses. This exercises both the source-side ReadDir
	// error return and the collection recursion error propagation in one test.
	sys := &MockSystem{
		Fallback: RealSystem{},
		ReadDirFunc: func(path string) ([]os.DirEntry, error) {
			if path == nestedDir {
				return nil, errors.New("nested source read failed")
			}
			return RealSystem{}.ReadDir(path)
		},
	}

	err := copyDirRecursive(sys, srcDir, destDir)
	if err == nil || !strings.Contains(err.Error(), "nested source read failed") {
		t.Fatalf("expected actionable nested source read error, got %v", err)
	}
}

func TestCopyDirRecursive_DestinationSymlinkStaleRemoval(t *testing.T) {
	t.Parallel()
	destDir := t.TempDir()
	externalDir := t.TempDir()
	externalTarget := filepath.Join(externalDir, "target.txt")
	if err := os.WriteFile(externalTarget, []byte("external target"), 0o600); err != nil {
		t.Fatalf("write external target: %v", err)
	}
	symlinkPath := filepath.Join(destDir, "link.txt")
	if err := os.Symlink(externalTarget, symlinkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Empty srcDir means nothing is desired, so the destination symlink is stale.
	if err := copyDirRecursive(RealSystem{}, "", destDir); err != nil {
		t.Fatalf("copyDirRecursive error: %v", err)
	}

	// The stale symlink itself must be unlinked...
	if _, err := os.Lstat(symlinkPath); !os.IsNotExist(err) {
		t.Fatalf("expected stale destination symlink to be removed, got %v", err)
	}
	// ...without being followed: the external target must survive with intact content.
	data, err := os.ReadFile(externalTarget) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("external symlink target was not preserved: %v", err)
	}
	if string(data) != "external target" {
		t.Fatalf("external target content changed: %q", data)
	}
}

func TestCopyDirRecursive_RemovesDestinationSymlinkBeforeWriting(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	destDir := t.TempDir()
	externalDir := t.TempDir()
	sourcePath := filepath.Join(srcDir, "resource.txt")
	destPath := filepath.Join(destDir, "resource.txt")
	externalPath := filepath.Join(externalDir, "target.txt")
	if err := os.WriteFile(sourcePath, []byte("source content"), 0o600); err != nil {
		t.Fatalf("write source resource: %v", err)
	}
	if err := os.WriteFile(externalPath, []byte("external content"), 0o600); err != nil {
		t.Fatalf("write external target: %v", err)
	}
	if err := os.Symlink(externalPath, destPath); err != nil {
		t.Fatalf("symlink destination resource: %v", err)
	}

	var removedPath string
	sys := &MockSystem{
		Fallback: RealSystem{},
		RemoveFunc: func(path string) error {
			if path == destPath {
				removedPath = path
			}
			return RealSystem{}.Remove(path)
		},
		WriteFileAtomicFunc: func(filename string, data []byte, perm os.FileMode) error {
			if filename == destPath {
				info, err := os.Lstat(filename)
				if err != nil && !os.IsNotExist(err) {
					return err
				}
				if err == nil && info.Mode()&os.ModeSymlink != 0 {
					return errors.New("refused to write over destination symlink")
				}
			}
			return RealSystem{}.WriteFileAtomic(filename, data, perm)
		},
	}

	if err := copyDirRecursive(sys, srcDir, destDir); err != nil {
		t.Fatalf("copyDirRecursive error: %v", err)
	}
	if removedPath != destPath {
		t.Fatalf("expected destination symlink to be removed before writing, got %q", removedPath)
	}
	info, err := os.Lstat(destPath)
	if err != nil {
		t.Fatalf("lstat destination resource: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("destination resource remained a symlink")
	}
	data, err := os.ReadFile(destPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read destination resource: %v", err)
	}
	if string(data) != "source content" {
		t.Fatalf("unexpected destination content: %q", data)
	}
	data, err = os.ReadFile(externalPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read external target: %v", err)
	}
	if string(data) != "external content" {
		t.Fatalf("external symlink target changed: %q", data)
	}
}

func TestProjectionSurfacesOwnershipBookkeepingFailures(t *testing.T) {
	t.Parallel()

	t.Run("unreadable manifest", func(t *testing.T) {
		root := t.TempDir()
		sys := &MockSystem{
			Fallback: RealSystem{},
			ReadFileFunc: func(name string) ([]byte, error) {
				if filepath.Base(name) == ownedSkillsManifestName {
					return nil, errors.New("manifest read failed")
				}
				return RealSystem{}.ReadFile(name)
			},
		}
		err := WriteClaudeSkills(sys, root, []config.Skill{sourceSkill(t, "alpha")})
		if err == nil || !strings.Contains(err.Error(), "manifest read failed") {
			t.Fatalf("a manifest Agent Layer cannot read must fail, got %v", err)
		}
	})

	t.Run("unreadable skills directory during adoption", func(t *testing.T) {
		root := t.TempDir()
		skillsDir := projectedSkillsDir(root, "claude")
		sys := &MockSystem{
			Fallback: RealSystem{},
			ReadDirFunc: func(path string) ([]os.DirEntry, error) {
				if path == skillsDir {
					return nil, errors.New("skills dir read failed")
				}
				return RealSystem{}.ReadDir(path)
			},
		}
		err := WriteClaudeSkills(sys, root, []config.Skill{sourceSkill(t, "alpha")})
		if err == nil || !strings.Contains(err.Error(), "skills dir read failed") {
			t.Fatalf("expected the adoption scan failure to surface, got %v", err)
		}
	})

	t.Run("stale manifest removal", func(t *testing.T) {
		root := t.TempDir()
		if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "alpha")}); err != nil {
			t.Fatalf("first projection: %v", err)
		}
		sys := &MockSystem{
			Fallback: RealSystem{},
			RemoveFunc: func(name string) error {
				if filepath.Base(name) == ownedSkillsManifestName {
					return errors.New("manifest removal failed")
				}
				return RealSystem{}.Remove(name)
			},
		}
		err := WriteClaudeSkills(sys, root, nil)
		if err == nil || !strings.Contains(err.Error(), "manifest removal failed") {
			t.Fatalf("expected the manifest removal failure to surface, got %v", err)
		}
	})
}

func TestProjectionToleratesAnOwnedSkillSomeoneElseAlreadyDeleted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := projectedSkillsDir(root, "claude")
	alpha := sourceSkill(t, "alpha")
	beta := sourceSkill(t, "beta")
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{alpha, beta}); err != nil {
		t.Fatalf("first projection: %v", err)
	}

	// The user deleted the projected directory by hand between syncs. Removing
	// something already gone is the desired end state, not a failure.
	if err := os.RemoveAll(filepath.Join(skillsDir, "beta")); err != nil {
		t.Fatalf("remove projected skill: %v", err)
	}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{alpha}); err != nil {
		t.Fatalf("second projection: %v", err)
	}
	if manifest := readOwnedManifest(t, skillsDir); strings.Join(manifest.Skills, ",") != "alpha" {
		t.Fatalf("ownership manifest = %v, want only alpha", manifest.Skills)
	}
}

func TestProjectionSurfacesAnUninspectableOwnedSkill(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skillsDir := projectedSkillsDir(root, "claude")
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{sourceSkill(t, "alpha")}); err != nil {
		t.Fatalf("first projection: %v", err)
	}

	stalePath := filepath.Join(skillsDir, "alpha")
	sys := &MockSystem{
		Fallback: RealSystem{},
		LstatFunc: func(path string) (os.FileInfo, error) {
			if path == stalePath {
				return nil, errors.New("owned skill lstat failed")
			}
			return RealSystem{}.Lstat(path)
		},
	}
	err := WriteClaudeSkills(sys, root, nil)
	if err == nil || !strings.Contains(err.Error(), "owned skill lstat failed") {
		t.Fatalf("expected an actionable inspection failure, got %v", err)
	}
}

func TestCopySkillTreeSurfacesADestinationInspectionFailure(t *testing.T) {
	t.Parallel()
	destDir := filepath.Join(t.TempDir(), "alpha")
	sys := &MockSystem{
		Fallback: RealSystem{},
		LstatFunc: func(path string) (os.FileInfo, error) {
			if path == destDir {
				return nil, errors.New("destination inspection failed")
			}
			return RealSystem{}.Lstat(path)
		},
	}
	err := copySkillTree(sys, sourceSkill(t, "alpha"), destDir)
	if err == nil || !strings.Contains(err.Error(), "destination inspection failed") {
		t.Fatalf("expected an actionable destination inspection error, got %v", err)
	}
}
