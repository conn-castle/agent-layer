package skillimport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/gitrepo"
)

func TestCatalogDiagnosticsAreScopedUntilTierRootIsUnsafe(t *testing.T) {
	proj := newProject(t)
	require.NoError(t, os.MkdirAll(proj.paths.ImportedSkillsDir, 0o750))
	for _, name := range []string{"other", "Other"} {
		require.NoError(t, os.Mkdir(filepath.Join(proj.paths.ImportedSkillsDir, name), 0o750))
	}
	require.NoError(t, os.WriteFile(filepath.Join(proj.paths.ImportedSkillsDir, "Implement"), []byte("preserve"), 0o600))
	members, err := CatalogState(proj.root, developmentEntry(t))
	require.NoError(t, err)
	for _, member := range members {
		require.Equal(t, member.Name == "implement", member.Problem != "")
	}
	require.NoError(t, os.Rename(proj.paths.SkillsDir, filepath.Join(t.TempDir(), "saved-tier")))
	require.NoError(t, os.Symlink(t.TempDir(), proj.paths.SkillsDir))
	members, err = CatalogState(proj.root, developmentEntry(t))
	require.NoError(t, err)
	for _, member := range members {
		require.Contains(t, member.Problem, "symbolic link")
	}
}
func TestAdoptionRefusesIgnoredRawContentBeforeFetching(t *testing.T) {
	proj := newProject(t)
	writeLegacy(t, proj, "ship-pr")
	ignored := filepath.Join(proj.paths.SkillsDir, "ship-pr", "references", ".git")
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "missing-history"), ignored))
	service := New(proj.root)
	service.newRunner = func(map[string]string) (*gitrepo.Runner, error) {
		t.Fatal("ignored history reached fetch")
		return nil, errors.New("unexpected fetch")
	}
	_, err := service.InstallCatalog(context.Background(), []string{"skills/development/ship-pr"}, []string{"skills/development/ship-pr"})
	require.ErrorContains(t, err, "move and preserve")
	_, err = os.Lstat(ignored)
	require.NoError(t, err)
	require.Nil(t, proj.Lock())
}
