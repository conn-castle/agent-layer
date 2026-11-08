package skilljournal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestAdoptionEvidencePreflightsBothTiersBeforeRecovery(t *testing.T) {
	for _, kind := range []string{"v2", "retirements", "backup", "backup with unsafe journal"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			skills := filepath.Join(root, ".agent-layer/skills-imported/.staging")
			instructions := filepath.Join(root, ".agent-layer/instructions-imported/.staging")
			writeRecoveryEvidence(t, filepath.Join(skills, skilljournal.FileName), `{"version":1,"committed":true}`)
			switch kind {
			case "v2":
				writeRecoveryEvidence(t, filepath.Join(instructions, skilljournal.FileName), `{"version":2}`)
			case "retirements":
				writeRecoveryEvidence(t, filepath.Join(instructions, skilljournal.FileName), `{"version":1,"local_retirements":["rules.md"]}`)
			case "backup", "backup with unsafe journal":
				writeRecoveryEvidence(t, filepath.Join(instructions, "local-rules.md"), "only surviving content")
				if kind == "backup with unsafe journal" {
					require.NoError(t, os.Mkdir(filepath.Join(instructions, skilljournal.FileName), 0o750))
				}
			}
			before := testutil.SnapshotEvidence(t, root)
			require.ErrorIs(t, skilljournal.RecoverBoth(root), skilljournal.ErrAdoptionJournal)
			require.Equal(t, before, testutil.SnapshotEvidence(t, root))
			require.ErrorIs(t, skilljournal.Recover(skilljournal.Targets{ImportedSkillsDir: filepath.Dir(instructions), ConfigPath: filepath.Join(root, ".agent-layer/config.toml"), SkillsLockPath: filepath.Join(root, ".agent-layer/instructions.lock.json"), ExactFiles: true}), skilljournal.ErrAdoptionJournal)
			require.Equal(t, before, testutil.SnapshotEvidence(t, root))
			require.NoError(t, os.RemoveAll(instructions))
			require.NoError(t, skilljournal.RecoverBoth(root))
			require.NoDirExists(t, skills)
		})
	}
}
