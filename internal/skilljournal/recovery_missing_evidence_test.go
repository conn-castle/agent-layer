package skilljournal_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/testutil"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
)

func missingEvidenceFixture(t *testing.T, root string) config.Paths {
	t.Helper()
	paths := config.DefaultPaths(root)
	writeRecoveryEvidence(t, filepath.Join(root, ".agent-layer", "sync.lock"), "")
	writeRecoveryEvidence(t, paths.ConfigPath, "published config")
	writeRecoveryEvidence(t, paths.SkillsLockPath, "published lock")
	require.NoError(t, os.MkdirAll(paths.SkillsDir, 0o750))
	writeRecoveryEvidence(t, filepath.Join(paths.ImportedSkillsDir, "ship-pr", "SKILL.md"), "only surviving customized skill")
	require.NoError(t, os.Mkdir(skilljournal.StagingRoot(paths.ImportedSkillsDir), 0o750))
	return paths
}

func TestSharedRecoveryPreservesAllEvidenceWhenRequiredBackupIsMissing(t *testing.T) {
	for _, missing := range []string{"config", "lock", "existing write", "delete", "retirement", "original file", "original link", "original dangling link"} {
		t.Run(missing, func(t *testing.T) {
			root := t.TempDir()
			paths := missingEvidenceFixture(t, root)
			stage := skilljournal.StagingRoot(paths.ImportedSkillsDir)
			doc := skilljournal.Document{Writes: []skilljournal.WriteIntent{{Name: "ship-pr"}}, Config: true, LockExisted: true}
			wanted := ""
			if missing != "config" {
				writeRecoveryEvidence(t, filepath.Join(stage, skilljournal.ConfigBackupName), "original config")
			}
			if missing != "lock" {
				writeRecoveryEvidence(t, filepath.Join(stage, skilljournal.LockBackupName), "original lock")
			}
			switch missing {
			case "config":
				wanted = skilljournal.ConfigBackupName
			case "lock":
				wanted = skilljournal.LockBackupName
			case "delete":
				doc.Deletes = []string{"alpha"}
				wanted = skilljournal.DeleteBackupPrefix + "alpha"
			case "retirement":
				doc.Version = skilljournal.AdoptionVersion
				doc.LocalRetirements = []string{"ship-pr"}
				wanted = skilljournal.LocalBackupPrefix + "ship-pr"
			default:
				doc.Writes = append(doc.Writes, skilljournal.WriteIntent{Name: "alpha", Existed: true})
				wanted = skilljournal.WriteBackupPrefix + "alpha"
				original := filepath.Join(paths.ImportedSkillsDir, "alpha")
				if missing == "original file" {
					writeRecoveryEvidence(t, original, "not an original tree")
				}
				if missing == "original link" || missing == "original dangling link" {
					target := filepath.Join(root, "outside")
					if missing == "original link" {
						writeRecoveryEvidence(t, filepath.Join(target, "SKILL.md"), "linked content must survive")
					}
					require.NoError(t, os.Symlink(target, original))
				}
			}
			require.NoError(t, skilljournal.Write(stage, doc))
			before := testutil.SnapshotEvidence(t, root)
			err := skilljournal.Recover(recoveryTargets(paths))
			require.ErrorContains(t, err, "could not be fully rolled back")
			require.ErrorContains(t, err, wanted)
			require.ErrorContains(t, err, "preserve")
			require.False(t, errors.Is(err, skilljournal.ErrMalformed), "missing indispensable evidence is incomplete recovery, not invalid intent")
			require.Equal(t, before, testutil.SnapshotEvidence(t, root), "refusal must preserve every tree, metadata byte, mode, type, link and journal")
		})
	}
}

func TestSharedRecoveryPreservesPostIntentBackupsWithoutJournal(t *testing.T) {
	for _, prefix := range []string{skilljournal.LocalBackupPrefix, skilljournal.WriteBackupPrefix, skilljournal.DeleteBackupPrefix} {
		t.Run(prefix, func(t *testing.T) {
			root := t.TempDir()
			paths := missingEvidenceFixture(t, root)
			stage := skilljournal.StagingRoot(paths.ImportedSkillsDir)
			backup := filepath.Join(stage, prefix+"ship-pr")
			writeRecoveryEvidence(t, filepath.Join(backup, "SKILL.md"), "only surviving original tree")
			writeRecoveryEvidence(t, filepath.Join(backup, "support", "policy.txt"), "only surviving support")
			before := testutil.SnapshotEvidence(t, root)
			require.ErrorContains(t, skilljournal.Recover(recoveryTargets(paths)), "preserve")
			require.Equal(t, before, testutil.SnapshotEvidence(t, root))
		})
	}
}

func recoveryTargets(paths config.Paths) skilljournal.Targets {
	return skilljournal.Targets{ImportedSkillsDir: paths.ImportedSkillsDir, LocalSkillsDir: paths.SkillsDir, ConfigPath: paths.ConfigPath, SkillsLockPath: paths.SkillsLockPath}
}
