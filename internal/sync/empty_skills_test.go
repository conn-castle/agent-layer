package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestLoadSourcesRejectsEmptyDeployedSkillDirectory(t *testing.T) {
	root := newSourcesTestRoot(t)
	emptyDir := filepath.Join(root, ".agent-layer", "skills", "abandoned")
	if err := os.Mkdir(emptyDir, 0o750); err != nil {
		t.Fatalf("create empty skill directory: %v", err)
	}

	_, err := LoadSources(os.DirFS(root), root)
	if err == nil || !strings.Contains(err.Error(), "abandoned") || !strings.Contains(err.Error(), "has no SKILL.md") {
		t.Fatalf("expected LoadSources to reject empty skill directory, got %v", err)
	}
	if _, statErr := os.Stat(emptyDir); statErr != nil {
		t.Fatalf("direct LoadSources must not remove the empty directory: %v", statErr)
	}
}

func TestWithLockedProjectRemovesEmptyDeployedSkillDirectoryBeforeLoadingSources(t *testing.T) {
	root := newSourcesTestRoot(t)
	emptyDir := filepath.Join(root, ".agent-layer", "skills", "abandoned")
	if err := os.Mkdir(emptyDir, 0o750); err != nil {
		t.Fatalf("create empty skill directory: %v", err)
	}

	if err := WithLockedProject(RealSystem{}, root, func(*config.ProjectConfig) error {
		return nil
	}); err != nil {
		t.Fatalf("WithLockedProject with empty skill directory: %v", err)
	}
	if _, err := os.Stat(emptyDir); !os.IsNotExist(err) {
		t.Fatalf("empty skill directory still exists after locked load: %v", err)
	}
}

func TestProjectLockedRemovesEmptyDeployedSkillDirectoryBeforeLoadingSources(t *testing.T) {
	fixtureRoot := filepath.Join("testdata", "fixture-repo")
	root := t.TempDir()
	if err := copyFixtureRepo(fixtureRoot, root); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", ".env"), []byte("AL_EXAMPLE_TOKEN=token123\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	writeTemplateToFixtureSource(t, root, "claude-statusline.sh", filepath.Join(".agent-layer", "claude-statusline.sh"), 0o755)
	writeTemplateToFixtureSource(t, root, "codex-statusline.toml", filepath.Join(".agent-layer", "codex-statusline.toml"), 0o644)

	emptyDir := filepath.Join(root, ".agent-layer", "skills", "abandoned")
	if err := os.Mkdir(emptyDir, 0o750); err != nil {
		t.Fatalf("create empty skill directory: %v", err)
	}

	if _, err := ProjectLocked(RealSystem{}, root); err != nil {
		t.Fatalf("ProjectLocked with empty skill directory: %v", err)
	}
	if _, err := os.Stat(emptyDir); !os.IsNotExist(err) {
		t.Fatalf("empty skill directory still exists after locked projection: %v", err)
	}
}

func TestRunRemovesEmptyDeployedSkillDirectoryBeforeLoadingSources(t *testing.T) {
	fixtureRoot := filepath.Join("testdata", "fixture-repo")
	root := t.TempDir()
	if err := copyFixtureRepo(fixtureRoot, root); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", ".env"), []byte("AL_EXAMPLE_TOKEN=token123\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	writeTemplateToFixtureSource(t, root, "claude-statusline.sh", filepath.Join(".agent-layer", "claude-statusline.sh"), 0o755)
	writeTemplateToFixtureSource(t, root, "codex-statusline.toml", filepath.Join(".agent-layer", "codex-statusline.toml"), 0o644)

	emptyDir := filepath.Join(root, ".agent-layer", "skills", "abandoned")
	if err := os.Mkdir(emptyDir, 0o750); err != nil {
		t.Fatalf("create empty skill directory: %v", err)
	}

	if _, err := Run(root); err != nil {
		t.Fatalf("sync with empty skill directory: %v", err)
	}
	if _, err := os.Stat(emptyDir); !os.IsNotExist(err) {
		t.Fatalf("empty skill directory still exists after sync: %v", err)
	}
}

func TestRemoveEmptyDeployedSkillDirsPreservesOtherEntries(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, ".agent-layer", "skills")
	emptyDir := filepath.Join(skillsRoot, "empty")
	nonemptyDir := filepath.Join(skillsRoot, "nonempty")
	plainFile := filepath.Join(skillsRoot, "notes.txt")
	if err := os.MkdirAll(emptyDir, 0o750); err != nil {
		t.Fatalf("create empty skill directory: %v", err)
	}
	if err := os.MkdirAll(nonemptyDir, 0o750); err != nil {
		t.Fatalf("create nonempty skill directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nonemptyDir, ".keep"), []byte("keep"), 0o600); err != nil {
		t.Fatalf("write nonempty marker: %v", err)
	}
	if err := os.WriteFile(plainFile, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write plain file: %v", err)
	}

	if err := removeEmptyDeployedSkillDirs(RealSystem{}, root); err != nil {
		t.Fatalf("remove empty skill directories: %v", err)
	}
	if _, err := os.Stat(emptyDir); !os.IsNotExist(err) {
		t.Fatalf("empty directory still exists: %v", err)
	}
	for _, path := range []string{nonemptyDir, plainFile} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("nonempty entry %s was not preserved: %v", path, err)
		}
	}
}

func TestRemoveEmptyDeployedSkillDirsMissingRootIsNoop(t *testing.T) {
	if err := removeEmptyDeployedSkillDirs(RealSystem{}, t.TempDir()); err != nil {
		t.Fatalf("missing skills root should be a no-op: %v", err)
	}
}

func TestRemoveEmptyDeployedSkillDirsFailsLoudly(t *testing.T) {
	injected := errors.New("injected empty-skill cleanup failure")

	t.Run("skills root read", func(t *testing.T) {
		root := t.TempDir()
		skillsRoot := filepath.Join(root, ".agent-layer", "skills")
		sys := emptySkillCleanupFaultSystem{
			System: RealSystem{},
			readDir: func(path string) ([]os.DirEntry, error) {
				if path == skillsRoot {
					return nil, injected
				}
				return os.ReadDir(path)
			},
		}
		err := removeEmptyDeployedSkillDirs(sys, root)
		if !errors.Is(err, injected) || !strings.Contains(err.Error(), skillsRoot) {
			t.Fatalf("expected path-specific read error, got %v", err)
		}
	})

	t.Run("empty directory remove", func(t *testing.T) {
		root := t.TempDir()
		emptyDir := filepath.Join(root, ".agent-layer", "skills", "empty")
		if err := os.MkdirAll(emptyDir, 0o750); err != nil {
			t.Fatal(err)
		}
		sys := emptySkillCleanupFaultSystem{
			System: RealSystem{},
			remove: func(path string) error {
				if path == emptyDir {
					return injected
				}
				return os.Remove(path)
			},
		}
		err := removeEmptyDeployedSkillDirs(sys, root)
		if !errors.Is(err, injected) || !strings.Contains(err.Error(), emptyDir) {
			t.Fatalf("expected path-specific remove error, got %v", err)
		}
	})

	t.Run("skill directory read", func(t *testing.T) {
		root := t.TempDir()
		skillDir := filepath.Join(root, ".agent-layer", "skills", "unreadable")
		if err := os.MkdirAll(skillDir, 0o750); err != nil {
			t.Fatal(err)
		}
		sys := emptySkillCleanupFaultSystem{
			System: RealSystem{},
			readDir: func(path string) ([]os.DirEntry, error) {
				if path == skillDir {
					return nil, injected
				}
				return os.ReadDir(path)
			},
		}
		err := removeEmptyDeployedSkillDirs(sys, root)
		if !errors.Is(err, injected) || !strings.Contains(err.Error(), skillDir) {
			t.Fatalf("expected path-specific skill read error, got %v", err)
		}
	})
}

type emptySkillCleanupFaultSystem struct {
	System
	readDir func(string) ([]os.DirEntry, error)
	remove  func(string) error
}

func (s emptySkillCleanupFaultSystem) ReadDir(path string) ([]os.DirEntry, error) {
	if s.readDir != nil {
		return s.readDir(path)
	}
	return s.System.ReadDir(path)
}

func (s emptySkillCleanupFaultSystem) Remove(path string) error {
	if s.remove != nil {
		return s.remove(path)
	}
	return s.System.Remove(path)
}
