package wizard

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skillimport"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func remoteChoices(t *testing.T, id string) (*Choices, templates.CLISkillCatalogEntry) {
	t.Helper()
	catalog, err := templates.LoadCLISkillCatalog()
	require.NoError(t, err)
	for _, entry := range catalog {
		if entry.ID == id {
			c := NewChoices()
			c.CLISkillsCatalog = []templates.CLISkillCatalogEntry{entry}
			c.EnabledCLISkills[id] = true
			return c, entry
		}
	}
	t.Fatal("missing catalog")
	return nil, templates.CLISkillCatalogEntry{}
}

func TestWizardManualPoliciesAndWildcardCoverageArePreserved(t *testing.T) {
	for _, kind := range []string{"pinned", "tracked", "write", "wildcard", "excluded"} {
		t.Run(kind, func(t *testing.T) {
			testutil.CatalogGitFixture(t)
			root := t.TempDir()
			setupRemoteRepo(t, root)
			choices, entry := remoteChoices(t, "development-skills")
			opts := skillimport.AddOptions{Repository: entry.Repository, Selectors: entry.Selectors}
			switch kind {
			case "pinned":
				opts.Tracking = "pinned"
				opts.Ref = "main"
			case "tracked":
				opts.Tracking = "tracked"
			case "write":
				opts.WritePolicy = "direct"
			case "excluded":
				opts.Selectors = []string{"skills/development/*", "!skills/development/interface-audit", entry.Selectors[0]}
			case "wildcard":
				opts.Selectors = []string{"skills/development/*", entry.Selectors[0]}
			}
			_, err := skillimport.New(root).Add(context.Background(), opts)
			require.NoError(t, err)
			before := snapshotManualCatalogSources(t, root)
			if kind == "wildcard" {
				t.Setenv("GIT_CONFIG_KEY_0", "url."+filepath.Join(t.TempDir(), "unavailable")+".insteadOf")
				_, err = skillimport.New(root).InstallCatalog(context.Background(), []string{"skills/tools/playwright"}, nil)
				require.ErrorContains(t, err, "manually managed")
				_, err = skillimport.New(root).RemoveCatalogSelectors(context.Background(), entry.Selectors[:1])
				require.ErrorContains(t, err, "manually managed")
			}
			if kind == "excluded" {
				choices.InitialCLISkills = map[string]bool{entry.ID: true}
			}
			changes, err := computeSkillsChangeSet(root, choices)
			require.NoError(t, err)
			require.Empty(t, changes.importSelectors)
			require.Contains(t, buildSkillsPreview(changes), "Manually managed")
			if kind == "excluded" {
				require.Contains(t, buildSkillsPreview(changes), "Catalog note")
			}
			choices.EnabledCLISkills[entry.ID] = false
			changes, err = computeSkillsChangeSet(root, choices)
			require.NoError(t, err)
			require.Empty(t, changes.removeSelectors)
			require.NoError(t, applySkillsChanges(root, changes))
			require.Equal(t, before, snapshotManualCatalogSources(t, root))
		})
	}
}

func TestWizardOfflineFetchReportsPriorConfigWritesAndRetriesCoherently(t *testing.T) {
	testutil.CatalogGitFixture(t)
	root := t.TempDir()
	setupRemoteRepo(t, root)
	choices, _ := remoteChoices(t, "playwright")
	choices.ApprovalMode = config.ApprovalModeAll
	choices.GitTrackingTouched = true
	choices.CodexStatuslineTouched = true
	choices.CodexStatusline = true
	gitignore := filepath.Join(root, ".agent-layer", "gitignore.block")
	beforeGitignore, err := os.ReadFile(gitignore)
	require.NoError(t, err)
	// Git's harness rewrite points to an absent local remote; no live network is used.
	original := os.Getenv("GIT_CONFIG_KEY_0")
	t.Setenv("GIT_CONFIG_KEY_0", "url."+filepath.Join(t.TempDir(), "absent")+".insteadOf")
	var output bytes.Buffer
	called := false
	syncFailure := errors.New("injected final projection failure")
	syncFn := func(string) (*alsync.Result, error) { called = true; return &alsync.Result{}, syncFailure }
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	envPath := filepath.Join(root, ".agent-layer", ".env")
	err = applyChanges(root, configPath, envPath, choices, syncFn, &output)
	require.ErrorContains(t, err, "prior wizard config/env writes may already be applied")
	require.ErrorContains(t, err, "gitignore block updates, statusline source updates, and sync were not run")
	require.False(t, called)
	afterGitignore, readErr := os.ReadFile(gitignore)
	require.NoError(t, readErr)
	require.Equal(t, beforeGitignore, afterGitignore)
	raw, err := os.ReadFile(configPath)
	require.NoError(t, err)
	require.Contains(t, string(raw), `mode = "all"`)
	require.NoFileExists(t, filepath.Join(root, ".agent-layer", "skills.lock.json"))
	t.Setenv("GIT_CONFIG_KEY_0", original)
	err = applyChanges(root, configPath, envPath, choices, syncFn, &output)
	require.ErrorIs(t, err, syncFailure)
	require.ErrorContains(t, err, "catalog source changes were committed; projection was not completed")
	require.True(t, called)
}

func setupRemoteRepo(t *testing.T, root string) {
	t.Helper()
	setupRepo(t, root)
	raw, err := templates.Read("config.toml")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "config.toml"), raw, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", ".env"), nil, 0o600))
}

func TestWizardMixedAddAndBlockedRemovalReportsCommittedInstallation(t *testing.T) {
	testutil.CatalogGitFixture(t)
	root := t.TempDir()
	setupRemoteRepo(t, root)
	choices, entry := remoteChoices(t, "development-skills")
	_, err := skillimport.New(root).Add(context.Background(), skillimport.AddOptions{Repository: entry.Repository, Selectors: entry.Selectors})
	require.NoError(t, err)
	policy := filepath.Join(root, ".agent-layer", "skills-imported", "ship-pr", "policy.txt")
	require.NoError(t, os.WriteFile(policy, []byte("preserve"), 0o600))
	choices.EnabledCLISkills[entry.ID] = false
	_, tool := remoteChoices(t, "playwright")
	choices.CLISkillsCatalog = append(choices.CLISkillsCatalog, tool)
	choices.EnabledCLISkills[tool.ID] = true
	changes, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	err = applySkillsChanges(root, changes)
	require.ErrorContains(t, err, "installation committed; selector removal was not applied")
	require.ErrorContains(t, err, "al skills")
	require.FileExists(t, policy)
	require.FileExists(t, filepath.Join(root, ".agent-layer", "skills-imported", "playwright", "SKILL.md"))
	members, err := skillimport.CatalogState(root, entry)
	require.NoError(t, err)
	for _, member := range members {
		require.True(t, member.Imported)
	}
}
