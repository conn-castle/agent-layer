package wizard

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	alsync "github.com/conn-castle/agent-layer/internal/sync"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestWizardCatalogApplyRefusesUnpreviewedLegacyCopies(t *testing.T) {
	for _, alreadyApproved := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing only", true: "another legacy already approved"}[alreadyApproved], func(t *testing.T) {
			source := testutil.CatalogGitFixture(t)
			root := t.TempDir()
			setupRemoteRepo(t, root)
			choices, _ := remoteChoices(t, "development-skills")
			if alreadyApproved {
				tree, err := skilltree.Read(skilltree.OSFS{}, filepath.Join(source, "skills/development/ship-pr"))
				require.NoError(t, err)
				require.NoError(t, skilltree.Materialize(tree, filepath.Join(root, ".agent-layer", "skills", "ship-pr")))
			}
			changes, err := computeSkillsChangeSet(root, choices)
			require.NoError(t, err)
			require.NotEmpty(t, changes.importSelectors)
			choices.previewedSkills = &changes
			// A new local copy after preview requires its own adoption approval.
			tree, err := skilltree.Read(skilltree.OSFS{}, filepath.Join(source, "skills/development/implement"))
			require.NoError(t, err)
			require.NoError(t, skilltree.Materialize(tree, filepath.Join(root, ".agent-layer", "skills", "implement")))
			roots := []string{filepath.Join(root, ".agent-layer", "skills"), filepath.Join(root, ".agent-layer", "skills-imported")}
			require.NoError(t, os.MkdirAll(roots[1], 0o750))
			before := testutil.SnapshotEvidence(t, roots...)
			t.Setenv("GIT_CONFIG_KEY_0", "url."+filepath.Join(t.TempDir(), "unavailable")+".insteadOf")
			err = applyChanges(root, filepath.Join(root, ".agent-layer", "config.toml"), filepath.Join(root, ".agent-layer", ".env"), choices, func(string) (*alsync.Result, error) {
				t.Fatal("unapproved adoption must fail before sync")
				return nil, nil
			}, io.Discard)
			require.ErrorContains(t, err, "review adoption")
			require.NotContains(t, err.Error(), "succeeded: 0 skills")
			require.Equal(t, before, testutil.SnapshotEvidence(t, roots...))
			require.NoFileExists(t, filepath.Join(root, ".agent-layer", "skills.lock.json"))
			require.NoDirExists(t, filepath.Join(root, ".agent-layer", "skills-imported", ".staging"))
		})
	}
}

func snapshotManualCatalogSources(t *testing.T, root string) map[string]string {
	t.Helper()
	return testutil.SnapshotEvidence(t, filepath.Join(root, ".agent-layer"))
}
