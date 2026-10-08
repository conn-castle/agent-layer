package skillimport

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestAdoptionCommitRollbackPreservesOriginalOrSurvivingGeneration(t *testing.T) {
	for _, kind := range []string{"normal absent lock", "normal present lock", "missing lock", "missing local backup", "linked local backup"} {
		t.Run(kind, func(t *testing.T) {
			proj := newProject(t)
			prior := skilllock.New()
			if kind != "normal absent lock" {
				require.NoError(t, prior.Save(proj.paths.SkillsLockPath))
			}
			require.NoError(t, os.Chmod(proj.paths.ConfigPath, 0o644)) // #nosec G302 -- the existing config writer uses canonical mode 0644.
			tree := writeLegacy(t, proj, "implement")
			require.NoError(t, os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750))
			expected := testutil.SnapshotEvidence(t, proj.root)
			tx := newTestTransaction(proj, prior)
			tx.RetireLocal("implement", tree)
			tx.WriteSkill("implement", tree)
			tx.SetConfig(proj.ConfigContent() + "\n# published\n")
			tx.SetLockEntry(lockEntry("implement"))
			cause := errors.New("post-publication failure")
			injected := false
			tx.writeFile = func(path string, data []byte, mode os.FileMode) error {
				if err := fsutil.WriteFileAtomic(path, data, mode); err != nil {
					return err
				}
				if path != proj.paths.SkillsLockPath || injected {
					return nil
				}
				injected = true
				if kind == "missing lock" || kind == "missing local backup" || kind == "linked local backup" {
					backup := filepath.Join(tx.stagingRoot, skilljournal.LocalBackupPrefix+"implement")
					if kind == "missing lock" {
						backup = filepath.Join(tx.stagingRoot, skilljournal.LockBackupName)
					}
					saved := filepath.Join(proj.root, "quarantined-original")
					require.NoError(t, os.Rename(backup, saved))
					if kind == "linked local backup" {
						require.NoError(t, os.Symlink(saved, backup))
					}
					expected = testutil.SnapshotEvidence(t, proj.root)
				}
				return cause
			}
			err := tx.Commit()
			require.True(t, injected)
			require.ErrorIs(t, err, cause)
			if kind == "missing lock" || kind == "missing local backup" || kind == "linked local backup" {
				require.ErrorContains(t, err, "rolling the change back also failed")
			} else {
				require.NotContains(t, err.Error(), "rolling the change back also failed")
			}
			require.Equal(t, expected, testutil.SnapshotEvidence(t, proj.root))
		})
	}
}
