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
	_, err := skillimport.New(root).Add(context.Background(), skillimport.AddOptions{Repository: entry.Repository, Selectors: entry.Selectors[:5]})
	require.NoError(t, err)
	choices, err := initializeChoices(&config.ProjectConfig{Root: root})
	require.NoError(t, err)
	clone := choices.Clone()
	choices.InitialCLISkills[entry.ID] = false
	require.True(t, clone.InitialCLISkills[entry.ID])
	before := snapshotManualCatalogSources(t, root)
	t.Setenv("GIT_CONFIG_KEY_0", "url."+filepath.Join(t.TempDir(), "unavailable")+".insteadOf")
	changes, err := computeSkillsChangeSet(context.Background(), root, clone)
	require.NoError(t, err)
	require.Empty(t, changes.importSelectors)
	require.Empty(t, changes.removeSelectors)
	for _, selector := range entry.Selectors[5:] {
		require.Contains(t, buildSkillsPreview(changes), "al skills add "+entry.Repository+" "+selector)
	}
	require.NoError(t, applySkillsChanges(context.Background(), root, changes))
	require.Equal(t, before, snapshotManualCatalogSources(t, root))
	imported := filepath.Join(root, ".agent-layer", "skills-imported")
	saved := filepath.Join(t.TempDir(), "saved-imports")
	require.NoError(t, os.Rename(imported, saved))
	choices, err = initializeChoices(&config.ProjectConfig{Root: root})
	require.NoError(t, err)
	require.True(t, choices.EnabledCLISkills[entry.ID])
	changes, err = computeSkillsChangeSet(context.Background(), root, choices)
	require.NoError(t, err)
	require.Empty(t, changes.removeSelectors)
	require.NoError(t, os.Rename(saved, imported))
	clone.InitialCLISkills[entry.ID] = false
	changes, err = computeSkillsChangeSet(context.Background(), root, clone)
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
	require.Contains(t, choices.CLISkillStatus, "imported 5/7, missing 2")
	require.NoError(t, promptCLISkills(&MockUI{MultiSelectFunc: func(title string, options []string, selected *[]string) error {
		require.Contains(t, title, choices.CLISkillStatus)
		require.Contains(t, options, entry.Name)
		return nil
	}}, choices))
	choices.CLISkillsCatalog = []templates.CLISkillCatalogEntry{entry}
	changes, err = computeSkillsChangeSet(context.Background(), root, choices)
	require.NoError(t, err)
	require.Empty(t, changes.importSelectors)
	require.Empty(t, changes.removeSelectors)
	require.Contains(t, buildSkillsPreview(changes), "both local and imported tiers")
	choices.EnabledCLISkills[entry.ID] = false
	_, err = computeSkillsChangeSet(context.Background(), root, choices)
	require.Error(t, err)
	choices.EnabledCLISkills[entry.ID] = true
	choices.InitialCLISkills = nil
	_, err = computeSkillsChangeSet(context.Background(), root, choices)
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
		if name == "linked tier" {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
}

func TestWizardCustomizedLocalCatalogMemberOnlyConflictsWhenImportRequested(t *testing.T) {
	for _, mode := range []string{"unselected", "selected", "configured"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			setupRemoteRepo(t, root)
			choices, entry := remoteChoices(t, "development-skills")
			local := filepath.Join(root, ".agent-layer/skills/ship-pr")
			require.NoError(t, os.MkdirAll(local, 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(local, "SKILL.md"), []byte("---\nname: ship-pr\ndescription: Project-specific workflow.\n---\nCustom user content.\n"), 0o600))
			choices.EnabledCLISkills[entry.ID] = mode != "unselected"
			choices.InitialCLISkills = map[string]bool{entry.ID: mode == "configured"}
			if mode == "configured" {
				configPath := config.DefaultPaths(root).ConfigPath
				raw, err := os.ReadFile(configPath)
				require.NoError(t, err)
				raw = append(raw, []byte("\n[[skills.imports]]\nrepository = \""+entry.Repository+"\"\nselectors = [\"skills/development/ship-pr\"]\n")...)
				require.NoError(t, os.WriteFile(configPath, raw, 0o600)) // #nosec G703 -- test-owned project config.
			}
			members, err := skillimport.CatalogState(t.Context(), root, entry)
			require.NoError(t, err)
			for _, member := range members {
				if member.Name == "ship-pr" {
					require.True(t, member.Local)
					require.Equal(t, mode == "configured", member.Configured)
					require.Equal(t, mode == "configured", member.Problem != "")
				}
			}
			changes, err := computeSkillsChangeSet(t.Context(), root, choices)
			if mode == "selected" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Empty(t, changes.importSelectors)
			if mode == "unselected" {
				require.Empty(t, changes.importPreview)
			} else {
				require.NotEmpty(t, changes.importPreview)
			}
		})
	}
}
