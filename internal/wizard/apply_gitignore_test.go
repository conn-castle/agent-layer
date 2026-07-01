package wizard

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
)

func TestRun_GitTrackingPromptDefaultsFromManagedGitignoreBlock(t *testing.T) {
	t.Run("template defaults ignore agent config and track memory docs", func(t *testing.T) {
		selected := runWizardToGitTrackingPrompt(t, "")
		assert.Equal(t, []string{messages.WizardGitTrackDocsAgentLayerLabel}, selected)
	})

	t.Run("docs default follows managed source when uncommented", func(t *testing.T) {
		selected := runWizardToGitTrackingPrompt(t, strings.ReplaceAll(
			readTemplateGitignoreBlock(t),
			"# /docs/agent-layer/",
			"/docs/agent-layer/",
		))
		assert.Empty(t, selected)
	})

	t.Run("invalid active inline comment defaults to tracked", func(t *testing.T) {
		selected := runWizardToGitTrackingPrompt(t, strings.Replace(
			readTemplateGitignoreBlock(t),
			"/.agent-layer/",
			"/.agent-layer/  # keep generated config ignored",
			1,
		))
		assert.Contains(t, selected, messages.WizardGitTrackAgentLayerLabel)
		assert.Contains(t, selected, messages.WizardGitTrackDocsAgentLayerLabel)
	})
}

func TestGitignoreBlockChanges_UpdateManagedSourceAndPreview(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	choices := NewChoices()
	choices.TrackAgentLayerDir = true
	choices.TrackDocsAgentLayerDir = false
	choices.GitTrackingTouched = true

	changes, err := computeGitignoreBlockChangeSet(root, choices)
	require.NoError(t, err)
	preview := buildGitignoreBlockPreview(changes)
	assert.Contains(t, preview, "Gitignore source changes:")
	assert.Contains(t, preview, "-/.agent-layer/")
	assert.Contains(t, preview, "+# /.agent-layer/")
	assert.Contains(t, preview, "-# /docs/agent-layer/")
	assert.Contains(t, preview, "+/docs/agent-layer/")

	require.NoError(t, applyGitignoreBlockChanges(root, changes))
	block := readGitignoreBlock(t, root)
	assert.Contains(t, block, "# /.agent-layer/\n")
	assert.NotContains(t, block, "\n/.agent-layer/\n")
	assert.Contains(t, block, "\n/docs/agent-layer/\n")
	assert.NotContains(t, block, "# /docs/agent-layer/\n")
}

func TestGitignoreBlockChanges_NormalizesInlineCommentPatterns(t *testing.T) {
	content := "/.agent-layer/  # keep generated config ignored\n# /docs/agent-layer/ - track memory docs\n"

	choices := NewChoices()
	choices.TrackAgentLayerDir = false
	choices.TrackDocsAgentLayerDir = true

	next, err := patchGitignoreBlock(content, choices)
	require.NoError(t, err)
	assert.Contains(t, next, "/.agent-layer/\n")
	assert.NotContains(t, next, "/.agent-layer/  # keep generated config ignored")
	assert.Contains(t, next, "# /docs/agent-layer/ - track memory docs\n")

	choices.TrackAgentLayerDir = true
	choices.TrackDocsAgentLayerDir = false
	next, err = patchGitignoreBlock(content, choices)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(next, agentLayerGitignorePattern))
	assert.Equal(t, 1, strings.Count(next, docsAgentLayerGitignorePattern))
	assert.Contains(t, next, "# /.agent-layer/\n")
	assert.Contains(t, next, "/docs/agent-layer/\n")
}

func TestApplyChanges_GitTrackingUpdatesSourceBeforeSync(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	envPath := filepath.Join(root, ".agent-layer", ".env")
	require.NoError(t, os.WriteFile(configPath, []byte(basicAgentConfig()), 0o600))
	require.NoError(t, os.WriteFile(envPath, []byte(""), 0o600))

	choices := NewChoices()
	choices.TrackAgentLayerDir = true
	choices.TrackDocsAgentLayerDir = false
	choices.GitTrackingTouched = true

	syncCalled := false
	runSync := func(gotRoot string) (*alsync.Result, error) {
		syncCalled = true
		block := readGitignoreBlock(t, gotRoot)
		assert.Contains(t, block, "# /.agent-layer/\n")
		assert.Contains(t, block, "\n/docs/agent-layer/\n")
		return alsync.Run(gotRoot)
	}

	err := applyChanges(root, configPath, envPath, choices, runSync, &bytes.Buffer{})
	require.NoError(t, err)
	assert.True(t, syncCalled)

	gitignore, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	require.NoError(t, err)
	content := string(gitignore)
	assert.Contains(t, content, "# /.agent-layer/")
	assert.Contains(t, content, "/docs/agent-layer/")
	assert.NotContains(t, strings.ReplaceAll(content, "# /.agent-layer/", ""), "/.agent-layer/")
}

func readGitignoreBlock(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".agent-layer", "gitignore.block"))
	require.NoError(t, err)
	return string(data)
}

func runWizardToGitTrackingPrompt(t *testing.T, gitignoreBlockOverride string) []string {
	t.Helper()
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")
	if gitignoreBlockOverride != "" {
		require.NoError(t, os.WriteFile(filepath.Join(configDir, "gitignore.block"), []byte(gitignoreBlockOverride), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(basicAgentConfig()), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600))

	var gitTrackingSelected []string
	sawGitTrackingPrompt := false
	ui := &MockUI{
		NoteFunc:   func(string, string) error { return nil },
		SelectFunc: func(string, []string, *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			switch title {
			case messages.WizardGitTrackingTitle:
				sawGitTrackingPrompt = true
				assert.Equal(t, []string{
					messages.WizardGitTrackAgentLayerLabel,
					messages.WizardGitTrackDocsAgentLayerLabel,
				}, options)
				gitTrackingSelected = append([]string(nil), (*selected)...)
			case messages.WizardEnableAgentsTitle, messages.WizardEnableCLISkillsTitle, messages.WizardEnableDefaultMCPServersTitle:
				*selected = []string{}
			}
			return nil
		},
		ConfirmFunc: func(title string, value *bool) error {
			if title == messages.WizardApplyChangesPrompt {
				*value = false
			}
			return nil
		},
	}

	err := Run(root, ui, func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }, "")
	require.NoError(t, err)
	require.True(t, sawGitTrackingPrompt, "wizard flow must include the git tracking prompt")
	return gitTrackingSelected
}

func readTemplateGitignoreBlock(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	setupRepo(t, root)
	return readGitignoreBlock(t, root)
}
