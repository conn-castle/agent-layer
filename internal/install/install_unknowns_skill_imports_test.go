package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

// TestBuildKnownPaths_IncludesSkillImportOwnedPaths pins the ownership contract
// for skill imports: the managed content root, its skills, the resolved-state
// lock, the transaction journal, and the staging area are all Agent Layer paths.
// If upgrade classified any of them as unknown it could offer to delete an
// editable imported skill or a lock the user may not have committed.
func TestBuildKnownPaths_IncludesSkillImportOwnedPaths(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	layer := filepath.Join(root, ".agent-layer")
	importedSkill := filepath.Join(layer, config.ImportedSkillsDirName, "code-review")
	if err := os.MkdirAll(filepath.Join(importedSkill, "references"), 0o700); err != nil {
		t.Fatalf("mkdir imported skill: %v", err)
	}
	manifest := filepath.Join(importedSkill, "SKILL.md")
	if err := os.WriteFile(manifest, []byte("---\nname: code-review\ndescription: x\n---\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	reference := filepath.Join(importedSkill, "references", "notes.md")
	if err := os.WriteFile(reference, []byte("notes\n"), 0o600); err != nil {
		t.Fatalf("write reference: %v", err)
	}
	lockPath := filepath.Join(layer, config.SkillImportLockFileName)
	if err := os.WriteFile(lockPath, []byte(`{"version":1,"entries":[]}`), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	known, err := inst.buildKnownPaths()
	if err != nil {
		t.Fatalf("buildKnownPaths: %v", err)
	}

	for _, path := range []string{
		filepath.Join(layer, config.ImportedSkillsDirName),
		importedSkill,
		manifest,
		reference,
		lockPath,
		filepath.Join(layer, config.SkillImportJournalFileName),
		filepath.Join(layer, config.SkillImportStagingDirName),
	} {
		if _, ok := known[filepath.Clean(path)]; !ok {
			t.Fatalf("expected %s to be a known Agent Layer path", path)
		}
	}
}
