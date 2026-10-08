package wizard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestRemovalPreviewNamesConfiguredSibling(t *testing.T) {
	root := t.TempDir()
	setupRemoteRepo(t, root)
	choices, entry := remoteChoices(t, "playwright")
	cfg := filepath.Join(root, ".agent-layer", "config.toml")
	raw, err := os.ReadFile(cfg)
	require.NoError(t, err)
	next, err := config.SetSkillImportSelectors(string(raw), (config.SkillImport{Repository: entry.Repository}).Identity(), []string{entry.Selectors[0], "skills/development/implement"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfg, []byte(next), 0o600)) // #nosec G703 -- writes test-owned config.
	choices.EnabledCLISkills[entry.ID] = false
	choices.InitialCLISkills = map[string]bool{entry.ID: true}
	changes, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	require.Equal(t, entry.Selectors, changes.removeSelectors)
	require.Contains(t, buildSkillsPreview(changes), "configured sibling skills/development/implement")
	require.Contains(t, buildSkillsPreview(changes), "materialize or block")
}
