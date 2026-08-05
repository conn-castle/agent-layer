package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

func projectionSkill(t *testing.T, name string, manifest []byte, resources ...skilltree.File) config.Skill {
	t.Helper()
	files := append([]skilltree.File{{Path: "SKILL.md", Data: manifest}}, resources...)
	tree, err := skilltree.NewTree(files)
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	return config.Skill{Name: name, SourceDir: filepath.Join(".agent-layer", "skills", name), Tree: tree}
}

func projectionManifest(name string) []byte {
	return []byte("---\n# retained comment\nname: " + name + "\ndescription: Exact bytes.\nprovider-field: true\n---\nBody without final newline")
}

func TestSkillRootsProjectTheSameCanonicalTreeByteForByte(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skill := projectionSkill(t, "alpha", projectionManifest("alpha"),
		skilltree.File{Path: ".hidden", Data: []byte{0, 1, 2}},
		skilltree.File{Path: "scripts/run.sh", Data: []byte("#!/bin/sh\n"), Executable: true},
	)

	for _, write := range []func(System, string, []config.Skill) error{WriteAgentSkills, WriteClaudeSkills} {
		if err := write(RealSystem{}, root, []config.Skill{skill}); err != nil {
			t.Fatalf("write projection: %v", err)
		}
	}
	for _, rel := range []string{filepath.Join(".agents", "skills"), filepath.Join(".claude", "skills")} {
		for path, want := range map[string][]byte{
			"SKILL.md":       projectionManifest("alpha"),
			".hidden":        {0, 1, 2},
			"scripts/run.sh": []byte("#!/bin/sh\n"),
		} {
			got, err := os.ReadFile(filepath.Join(root, rel, "alpha", filepath.FromSlash(path))) // #nosec G304 -- test-controlled path.
			if err != nil || string(got) != string(want) {
				t.Fatalf("%s %s = %q, err=%v", rel, path, got, err)
			}
		}
		info, err := os.Stat(filepath.Join(root, rel, "alpha", "scripts", "run.sh"))
		if err != nil || info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("%s executable mode = %v, err=%v", rel, info, err)
		}
	}
}

func TestBothClientProjectionsReuseTheLoadedTreeWithoutSourceReads(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	skill := projectionSkill(t, "alpha", projectionManifest("alpha"))
	sys := &MockSystem{
		Fallback: RealSystem{},
		ReadFileFunc: func(name string) ([]byte, error) {
			return nil, errors.New("unexpected projection read: " + name)
		},
	}
	if err := WriteAgentSkills(sys, root, []config.Skill{skill}); err != nil {
		t.Fatalf("agent projection reread source state: %v", err)
	}
	if err := WriteClaudeSkills(sys, root, []config.Skill{skill}); err != nil {
		t.Fatalf("Claude projection reread source state: %v", err)
	}
	for _, parent := range []string{".agents", ".claude"} {
		live := filepath.Join(root, parent, "skills")
		entries, err := os.ReadDir(live)
		if err != nil || len(entries) != 1 || entries[0].Name() != "alpha" {
			t.Fatalf("%s contains non-skill projection state: %v, %v", live, entries, err)
		}
		for _, forbidden := range []string{
			filepath.Join(live, ".agent-layer-skills.json"),
			live + projectionStageSuffix,
			live + projectionDiscardSuffix,
			live + ".al-backup",
			live + ".journal",
		} {
			if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
				t.Fatalf("projection created forbidden ownership/recovery state %s: %v", forbidden, err)
			}
		}
	}
}

func TestNFKCEquivalentSkillNamesFailBeforeProjection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	live := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(live, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "sentinel"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	skills := []config.Skill{
		projectionSkill(t, "file", projectionManifest("file")),
		{SourceDir: filepath.Join(".agent-layer", "skills", "ﬁle"), Tree: projectionSkill(t, "file", projectionManifest("file")).Tree},
	}

	err := WriteAgentSkills(RealSystem{}, root, skills)
	if err == nil || !strings.Contains(err.Error(), "file") || !strings.Contains(err.Error(), "ﬁle") {
		t.Fatalf("expected normalized duplicate error, got %v", err)
	}
	if got, readErr := os.ReadFile(filepath.Join(live, "sentinel")); readErr != nil || string(got) != "old" { // #nosec G304 -- test-controlled path.
		t.Fatalf("live root changed before duplicate validation: %q, %v", got, readErr)
	}
}

func TestSkillRootReplacementRemovesEveryExtraEntryAndRepairsEdits(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	live := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(filepath.Join(live, "alpha"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "alpha", "SKILL.md"), []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "extra-file"), []byte("extra"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(live, "extra-dir"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("extra-file", filepath.Join(live, "extra-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	skill := projectionSkill(t, "alpha", projectionManifest("alpha"))
	if err := WriteAgentSkills(RealSystem{}, root, []config.Skill{skill}); err != nil {
		t.Fatalf("WriteAgentSkills: %v", err)
	}
	for _, extra := range []string{"extra-file", "extra-dir", "extra-link"} {
		if _, err := os.Lstat(filepath.Join(live, extra)); !os.IsNotExist(err) {
			t.Fatalf("extra entry %s survived: %v", extra, err)
		}
	}
	got, err := os.ReadFile(filepath.Join(live, "alpha", "SKILL.md")) // #nosec G304 -- test-controlled path.
	if err != nil || string(got) != string(projectionManifest("alpha")) {
		t.Fatalf("canonical edit repair = %q, err=%v", got, err)
	}
}

func TestSkillRootRetryRebuildsAfterInterruptedSwap(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	live := filepath.Join(root, ".claude", "skills")
	v1 := projectionSkill(t, "alpha", projectionManifest("alpha"), skilltree.File{Path: "version", Data: []byte("one")})
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{v1}); err != nil {
		t.Fatalf("initial projection: %v", err)
	}

	v2 := projectionSkill(t, "alpha", projectionManifest("alpha"), skilltree.File{Path: "version", Data: []byte("two")})
	publishErr := errors.New("injected publish failure")
	sys := &MockSystem{Fallback: RealSystem{}, RenameFunc: func(oldpath, newpath string) error {
		if oldpath == live+projectionStageSuffix && newpath == live {
			return publishErr
		}
		return os.Rename(oldpath, newpath)
	}}
	if err := WriteClaudeSkills(sys, root, []config.Skill{v2}); !errors.Is(err, publishErr) {
		t.Fatalf("interrupted projection error = %v", err)
	}
	if _, err := os.Lstat(live); !os.IsNotExist(err) {
		t.Fatalf("failed swap exposed a live partial root: %v", err)
	}
	if err := WriteClaudeSkills(RealSystem{}, root, []config.Skill{v2}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(live, "alpha", "version")) // #nosec G304 -- test-controlled path.
	if err != nil || string(got) != "two" {
		t.Fatalf("retry output = %q, err=%v", got, err)
	}
	for _, scratch := range []string{live + projectionStageSuffix, live + projectionDiscardSuffix} {
		if _, err := os.Lstat(scratch); !os.IsNotExist(err) {
			t.Fatalf("scratch path survived: %s: %v", scratch, err)
		}
	}
}

func TestSkillRootValidationFinishesBeforeLiveRootIsTouched(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	live := filepath.Join(root, ".agents", "skills")
	if err := os.MkdirAll(live, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "sentinel"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalidTree, err := skilltree.NewTree([]skilltree.File{{Path: "skill.md", Data: projectionManifest("alpha")}})
	if err != nil {
		t.Fatal(err)
	}
	err = WriteAgentSkills(RealSystem{}, root, []config.Skill{{Name: "alpha", SourceDir: "alpha", Tree: invalidTree}})
	if err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("expected lowercase rename error, got %v", err)
	}
	if got, readErr := os.ReadFile(filepath.Join(live, "sentinel")); readErr != nil || string(got) != "old" { // #nosec G304 -- test-controlled path.
		t.Fatalf("live root changed before validation: %q, %v", got, readErr)
	}
}

func TestDisabledSkillProjectionRemovesOwnedRootAndScratch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	live := filepath.Join(root, ".agents", "skills")
	for _, path := range []string{live, live + projectionStageSuffix, live + projectionDiscardSuffix} {
		if err := os.MkdirAll(path, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := cleanSharedAgentSkills(RealSystem{}, root); err != nil {
		t.Fatalf("cleanSharedAgentSkills: %v", err)
	}
	for _, path := range []string{live, live + projectionStageSuffix, live + projectionDiscardSuffix} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("owned path survived disable: %s: %v", path, err)
		}
	}
}

func TestSkillRootPublicationFailuresIdentifyTheFailedBoundary(t *testing.T) {
	t.Parallel()
	skill := projectionSkill(t, "alpha", projectionManifest("alpha"))
	failure := errors.New("injected boundary failure")

	t.Run("staging cleanup", func(t *testing.T) {
		root := t.TempDir()
		stage := filepath.Join(root, ".agents", "skills") + projectionStageSuffix
		sys := &MockSystem{Fallback: RealSystem{}, RemoveAllFunc: func(path string) error {
			if path == stage {
				return failure
			}
			return os.RemoveAll(path)
		}}
		if err := WriteAgentSkills(sys, root, []config.Skill{skill}); err == nil || !strings.Contains(err.Error(), stage) {
			t.Fatalf("cleanup failure was not actionable: %v", err)
		}
	})

	t.Run("staging creation", func(t *testing.T) {
		root := t.TempDir()
		stage := filepath.Join(root, ".agents", "skills") + projectionStageSuffix
		sys := &MockSystem{Fallback: RealSystem{}, MkdirAllFunc: func(path string, perm os.FileMode) error {
			if path == stage {
				return failure
			}
			return os.MkdirAll(path, perm)
		}}
		if err := WriteAgentSkills(sys, root, []config.Skill{skill}); err == nil || !strings.Contains(err.Error(), stage) {
			t.Fatalf("staging creation failure was not actionable: %v", err)
		}
	})

	t.Run("skill file write", func(t *testing.T) {
		root := t.TempDir()
		manifest := filepath.Join(root, ".agents", "skills") + projectionStageSuffix
		manifest = filepath.Join(manifest, "alpha", "SKILL.md")
		sys := &MockSystem{Fallback: RealSystem{}, WriteFileAtomicFunc: func(path string, data []byte, perm os.FileMode) error {
			if path == manifest {
				return failure
			}
			return RealSystem{}.WriteFileAtomic(path, data, perm)
		}}
		if err := WriteAgentSkills(sys, root, []config.Skill{skill}); err == nil || !strings.Contains(err.Error(), manifest) {
			t.Fatalf("skill write failure was not actionable: %v", err)
		}
	})

	t.Run("live root inspection", func(t *testing.T) {
		root := t.TempDir()
		live := filepath.Join(root, ".agents", "skills")
		sys := &MockSystem{Fallback: RealSystem{}, LstatFunc: func(path string) (os.FileInfo, error) {
			if path == live {
				return nil, failure
			}
			return os.Lstat(path)
		}}
		if err := WriteAgentSkills(sys, root, []config.Skill{skill}); err == nil || !strings.Contains(err.Error(), live) {
			t.Fatalf("live inspection failure was not actionable: %v", err)
		}
	})

	t.Run("live root move", func(t *testing.T) {
		root := t.TempDir()
		live := filepath.Join(root, ".agents", "skills")
		if err := os.MkdirAll(live, 0o750); err != nil {
			t.Fatal(err)
		}
		sys := &MockSystem{Fallback: RealSystem{}, RenameFunc: func(oldPath string, newPath string) error {
			if oldPath == live {
				return failure
			}
			return os.Rename(oldPath, newPath)
		}}
		if err := WriteAgentSkills(sys, root, []config.Skill{skill}); err == nil || !strings.Contains(err.Error(), live) {
			t.Fatalf("live move failure was not actionable: %v", err)
		}
	})

	t.Run("discard cleanup", func(t *testing.T) {
		root := t.TempDir()
		live := filepath.Join(root, ".agents", "skills")
		discard := live + projectionDiscardSuffix
		if err := os.MkdirAll(live, 0o750); err != nil {
			t.Fatal(err)
		}
		sys := &MockSystem{Fallback: RealSystem{}, RemoveAllFunc: func(path string) error {
			if path == discard {
				if _, err := os.Lstat(live); err == nil {
					return failure
				}
			}
			return os.RemoveAll(path)
		}}
		if err := WriteAgentSkills(sys, root, []config.Skill{skill}); err == nil || !strings.Contains(err.Error(), discard) {
			t.Fatalf("discard cleanup failure was not actionable: %v", err)
		}
	})
}
