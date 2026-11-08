package skillimport

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/gitrepo"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestCatalogDiagnosticsAreScopedUntilTierRootIsUnsafe(t *testing.T) {
	proj := newProject(t)
	require.NoError(t, os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750))
	for _, name := range []string{"other", "Other"} {
		require.NoError(t, os.MkdirAll(filepath.Join(proj.paths.ImportedSkillsDir, name), 0o750))
	}
	require.NoError(t, os.WriteFile(filepath.Join(proj.paths.ImportedSkillsDir, "Implement"), []byte("preserve"), 0o600))
	members, err := CatalogState(context.Background(), proj.root, developmentEntry(t))
	require.NoError(t, err)
	for _, member := range members {
		require.Equal(t, member.Name == "implement", member.Problem != "")
	}
	require.NoError(t, os.Rename(proj.paths.SkillsDir, filepath.Join(t.TempDir(), "saved-tier")))
	require.NoError(t, os.Symlink(t.TempDir(), proj.paths.SkillsDir))
	members, err = CatalogState(context.Background(), proj.root, developmentEntry(t))
	require.NoError(t, err)
	for _, member := range members {
		require.Contains(t, member.Problem, "symbolic link")
	}
}

func TestInstallCatalogRefusesOrdinaryLocalNameBeforeFetching(t *testing.T) {
	proj := newProject(t)
	proj.WriteUserSkill("ship-pr")
	service := New(proj.root)
	service.newRunner = func(map[string]string) (*gitrepo.Runner, error) {
		t.Fatal("local conflict reached fetch")
		return nil, nil
	}
	require.NoError(t, os.WriteFile(filepath.Join(proj.root, ".agent-layer/sync.lock"), nil, 0o600))
	before := testutil.SnapshotEvidence(t, proj.root)
	_, err := service.InstallCatalog(t.Context(), []string{"skills/development/ship-pr"})
	require.Error(t, err)
	require.Equal(t, before, testutil.SnapshotEvidence(t, proj.root))
}
