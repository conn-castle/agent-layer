package skilljournal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/testutil"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/skilljournal"
)

// Linked roots must refuse before consuming another project's recovery evidence.
func TestSharedRecoveryRefusesUnsafeRootsAndPreservesEvidence(t *testing.T) {
	for _, node := range []string{"imported tier", "local tier", "staging", "journal", "tree backup", "config backup"} {
		for _, kind := range []string{"link", "wrong type"} {
			t.Run(node+"/"+kind, func(t *testing.T) {
				root, external := t.TempDir(), t.TempDir()
				paths := missingEvidenceFixture(t, root)
				stage := skilljournal.StagingRoot(paths.ImportedSkillsDir)
				writeRecoveryEvidence(t, filepath.Join(paths.SkillsDir, "ship-pr", "SKILL.md"), "original local skill")
				writeRecoveryEvidence(t, filepath.Join(stage, skilljournal.LocalBackupPrefix+"ship-pr", "SKILL.md"), "staged local backup")
				writeRecoveryEvidence(t, filepath.Join(stage, skilljournal.ConfigBackupName), "previous config")
				writeRecoveryEvidence(t, filepath.Join(stage, skilljournal.LockBackupName), "previous lock")
				require.NoError(t, skilljournal.Write(stage, skilljournal.Document{Version: skilljournal.AdoptionVersion, Committed: node == "tree backup", Writes: []skilljournal.WriteIntent{{Name: "ship-pr"}}, LocalRetirements: []string{"ship-pr"}, Config: true, LockExisted: true}))
				target := map[string]string{"imported tier": paths.ImportedSkillsDir, "local tier": paths.SkillsDir, "staging": stage, "journal": filepath.Join(stage, skilljournal.FileName), "tree backup": filepath.Join(stage, skilljournal.LocalBackupPrefix+"ship-pr"), "config backup": filepath.Join(stage, skilljournal.ConfigBackupName)}[node]
				saved := filepath.Join(external, "preserved-root")
				require.NoError(t, os.Rename(target, saved))
				if kind == "link" {
					require.NoError(t, os.Symlink(saved, target))
				} else if node == "journal" || node == "config backup" {
					require.NoError(t, os.Mkdir(target, 0o750))
				} else {
					writeRecoveryEvidence(t, target, "preserve root file")
				}
				roots := []string{root, external}
				before := testutil.SnapshotEvidence(t, roots...)
				err := skilljournal.Recover(recoveryTargets(paths))
				require.Error(t, err)
				require.Equal(t, before, testutil.SnapshotEvidence(t, roots...), "refusal changed live data, raw evidence, modes, or linked content")
			})
		}
	}
}

func writeRecoveryEvidence(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- exclusively creates a fixture under t.TempDir.
	require.NoError(t, err)
	_, err = file.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, file.Close())
}
