package wizard

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skillimport"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestWizardPartialCatalogLifecycle(t *testing.T) {
	source := testutil.CatalogGitFixture(t)
	root := t.TempDir()
	setupRemoteRepo(t, root)
	_, entry := remoteChoices(t, "development-skills")
	legacy := filepath.Join(root, ".agent-layer", "skills", "ship-pr")
	tree, err := skilltree.Read(skilltree.OSFS{}, filepath.Join(source, entry.Selectors[1]))
	require.NoError(t, err)
	require.NoError(t, skilltree.Materialize(tree, legacy))
	initial, err := initializeChoices(&config.ProjectConfig{Root: root})
	require.NoError(t, err)
	preview, err := computeSkillsChangeSet(root, initial)
	require.NoError(t, err)
	require.Equal(t, []string{entry.Selectors[1]}, preview.importSelectors)
	require.Contains(t, buildSkillsPreview(preview), "older defaults")
	require.Contains(t, buildSkillsPreview(preview), "Missing bundle member")
	beforeLegacy := snapshotManualCatalogSources(t, root)
	initial.EnabledCLISkills[entry.ID] = false
	preview, err = computeSkillsChangeSet(root, initial)
	require.NoError(t, err)
	require.NoError(t, applySkillsChanges(root, preview))
	require.Equal(t, beforeLegacy, snapshotManualCatalogSources(t, root))
	require.NoError(t, os.RemoveAll(legacy))
	_, err = skillimport.New(root).Add(context.Background(), skillimport.AddOptions{Repository: entry.Repository, Selectors: entry.Selectors[:5]})
	require.NoError(t, err)
	choices, err := initializeChoices(&config.ProjectConfig{Root: root})
	require.NoError(t, err)
	clone := choices.Clone()
	choices.InitialCLISkills[entry.ID] = false
	require.True(t, clone.InitialCLISkills[entry.ID])
	before := snapshotManualCatalogSources(t, root)
	t.Setenv("GIT_CONFIG_KEY_0", "url."+filepath.Join(t.TempDir(), "unavailable")+".insteadOf")
	changes, err := computeSkillsChangeSet(root, clone)
	require.NoError(t, err)
	require.Empty(t, changes.importSelectors)
	require.Empty(t, changes.removeSelectors)
	for _, selector := range entry.Selectors[5:] {
		require.Contains(t, buildSkillsPreview(changes), "al skills add "+entry.Repository+" "+selector)
	}
	require.NoError(t, applySkillsChanges(root, changes))
	require.Equal(t, before, snapshotManualCatalogSources(t, root))
	imported := filepath.Join(root, ".agent-layer", "skills-imported")
	saved := filepath.Join(t.TempDir(), "saved-imports")
	require.NoError(t, os.Rename(imported, saved))
	choices, err = initializeChoices(&config.ProjectConfig{Root: root})
	require.NoError(t, err)
	require.True(t, choices.EnabledCLISkills[entry.ID])
	changes, err = computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	require.Empty(t, changes.removeSelectors)
	require.NoError(t, os.Rename(saved, imported))
	clone.InitialCLISkills[entry.ID] = false
	changes, err = computeSkillsChangeSet(root, clone)
	require.NoError(t, err)
	require.Equal(t, entry.Selectors[5:], changes.importSelectors)
	require.Contains(t, buildSkillsPreview(changes), "configured named provider targets")
	require.Contains(t, buildSkillsPreview(changes), "dispatch-agent")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "implement"), 0o750))
	sibling, err := skilltree.Read(skilltree.OSFS{}, filepath.Join(source, entry.Selectors[5]))
	require.NoError(t, err)
	require.NoError(t, skilltree.Materialize(sibling, filepath.Join(root, ".agent-layer", "skills", "audit-tests")))
	choices, err = initializeChoices(&config.ProjectConfig{Root: root})
	require.NoError(t, err)
	require.Contains(t, choices.CLISkillStatus, "imported 5/7, legacy 2, missing 1")
	require.NoError(t, promptCLISkills(&MockUI{MultiSelectFunc: func(title string, options []string, selected *[]string) error {
		require.Contains(t, title, choices.CLISkillStatus)
		require.Contains(t, options, entry.Name)
		return nil
	}}, choices))
	choices.CLISkillsCatalog = []templates.CLISkillCatalogEntry{entry}
	changes, err = computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	require.Empty(t, changes.importSelectors)
	require.Empty(t, changes.removeSelectors)
	require.Contains(t, buildSkillsPreview(changes), "both local and imported tiers")
	choices.EnabledCLISkills[entry.ID] = false
	_, err = computeSkillsChangeSet(root, choices)
	require.Error(t, err)
	choices.EnabledCLISkills[entry.ID] = true
	choices.InitialCLISkills = nil
	_, err = computeSkillsChangeSet(root, choices)
	require.Error(t, err)
}
func TestWizardLoaderDefersOnlyRetiredSkillProblems(t *testing.T) {
	for _, name := range []string{"implement", "custom", "linked tier"} {
		root := t.TempDir()
		setupRemoteRepo(t, root)
		tier := filepath.Join(root, ".agent-layer", "skills")
		if name == "linked tier" {
			require.NoError(t, os.Rename(tier, filepath.Join(t.TempDir(), "saved")))
			require.NoError(t, os.Symlink(t.TempDir(), tier))
		} else {
			require.NoError(t, os.Mkdir(filepath.Join(tier, name), 0o750))
		}
		_, err := loadWizardProjectConfig(root)
		if name == "custom" {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}
