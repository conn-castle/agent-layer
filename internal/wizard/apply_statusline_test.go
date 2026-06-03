package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestStatuslineSourceChanges_CreateSelectedVisibleSources(t *testing.T) {
	root := t.TempDir()
	choices := NewChoices()
	choices.EnabledAgents[AgentClaude] = true
	choices.EnabledAgents[AgentCodex] = true
	choices.ClaudeStatusline = true
	choices.ClaudeStatuslineTouched = true
	choices.CodexStatusline = true
	choices.CodexStatuslineTouched = true

	changes, err := computeStatuslineSourceChangeSet(root, choices)
	require.NoError(t, err)
	require.Len(t, changes.sourcesToCreate, 2)
	assert.Contains(t, buildStatuslineSourcePreview(changes), ".agent-layer/claude-statusline.sh")
	assert.Contains(t, buildStatuslineSourcePreview(changes), ".agent-layer/codex-statusline.toml")

	require.NoError(t, applyStatuslineSourceChanges(root, changes))

	assertStatuslineTemplateWritten(t, root, ".agent-layer/claude-statusline.sh", "claude-statusline.sh", 0o755)
	assertStatuslineTemplateWritten(t, root, ".agent-layer/codex-statusline.toml", "codex-statusline.toml", 0o644)

	afterApply, err := computeStatuslineSourceChangeSet(root, choices)
	require.NoError(t, err)
	assert.Empty(t, afterApply.sourcesToCreate, "existing user-owned sources must not be overwritten")
}

func TestComputeStatuslineSourceChangeSet_VisibilityExistingAndErrors(t *testing.T) {
	t.Run("requires enabled visible provider", func(t *testing.T) {
		root := t.TempDir()
		choices := NewChoices()
		choices.EnabledAgentsTouched = true
		choices.ClaudeStatusline = true
		choices.ClaudeStatuslineTouched = true
		choices.CodexStatusline = true
		choices.CodexStatuslineTouched = true

		changes, err := computeStatuslineSourceChangeSet(root, choices)
		require.NoError(t, err)
		assert.Empty(t, changes.sourcesToCreate)
	})

	t.Run("enabled existing config seeds missing source without retoggle", func(t *testing.T) {
		root := t.TempDir()
		choices := NewChoices()
		choices.EnabledAgents[AgentClaude] = true
		choices.ClaudeStatusline = true

		changes, err := computeStatuslineSourceChangeSet(root, choices)
		require.NoError(t, err)
		require.Len(t, changes.sourcesToCreate, 1)
		assert.Equal(t, ".agent-layer/claude-statusline.sh", changes.sourcesToCreate[0].RelPath)
	})

	t.Run("existing source is preserved", func(t *testing.T) {
		root := t.TempDir()
		sourcePath := filepath.Join(root, ".agent-layer", "claude-statusline.sh")
		require.NoError(t, os.MkdirAll(filepath.Dir(sourcePath), 0o750))
		require.NoError(t, os.WriteFile(sourcePath, []byte("# custom\n"), 0o600))
		choices := NewChoices()
		choices.EnabledAgents[AgentClaude] = true
		choices.ClaudeStatusline = true
		choices.ClaudeStatuslineTouched = true

		changes, err := computeStatuslineSourceChangeSet(root, choices)
		require.NoError(t, err)
		assert.Empty(t, changes.sourcesToCreate)
		data, err := os.ReadFile(sourcePath)
		require.NoError(t, err)
		assert.Equal(t, "# custom\n", string(data))
	})

	t.Run("source directory is an error", func(t *testing.T) {
		root := t.TempDir()
		sourcePath := filepath.Join(root, ".agent-layer", "codex-statusline.toml")
		require.NoError(t, os.MkdirAll(sourcePath, 0o750))
		choices := NewChoices()
		choices.EnabledAgents[AgentCodex] = true
		choices.CodexStatusline = true
		choices.CodexStatuslineTouched = true

		_, err := computeStatuslineSourceChangeSet(root, choices)
		require.ErrorContains(t, err, ".agent-layer/codex-statusline.toml is a directory")
	})
}

func TestApplyStatuslineSourceChanges_ReportsTemplateReadError(t *testing.T) {
	err := applyStatuslineSourceChanges(t.TempDir(), statuslineSourceChangeSet{
		sourcesToCreate: []install.StatuslineSourceTemplate{{
			RelPath:      ".agent-layer/missing-statusline",
			TemplatePath: "missing-statusline-template",
			Perm:         0o644,
		}},
	})
	require.Error(t, err)
}

func assertStatuslineTemplateWritten(t *testing.T, root string, relPath string, templatePath string, perm os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	template, err := templates.Read(templatePath)
	require.NoError(t, err)
	assert.Equal(t, string(template), string(data))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, perm, info.Mode().Perm())
}

// TestComputeStatuslineSourceChangeSet_PropagatesNonNotExistStatError verifies the
// stat-error branch: when a parent path component is a file (ENOTDIR), the error is
// surfaced rather than being treated as a missing source to create. Would fail if
// the code collapsed all stat errors into "create source".
func TestComputeStatuslineSourceChangeSet_PropagatesNonNotExistStatError(t *testing.T) {
	root := t.TempDir()
	// Make ".agent-layer" a regular file so stat of any file under it yields ENOTDIR.
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer"), []byte("x"), 0o600))

	choices := NewChoices()
	choices.EnabledAgents[AgentClaude] = true
	choices.ClaudeStatusline = true
	choices.ClaudeStatuslineTouched = true

	_, err := computeStatuslineSourceChangeSet(root, choices)
	require.Error(t, err)
	assert.False(t, os.IsNotExist(err), "ENOTDIR stat error must not be reported as not-exist")
}

// TestApplyStatuslineSourceChanges_PropagatesWriteError verifies the atomic-write
// error branch: when the target path is occupied by a directory, the write failure
// is surfaced instead of being silently ignored. Would fail if write errors were
// swallowed.
func TestApplyStatuslineSourceChanges_PropagatesWriteError(t *testing.T) {
	root := t.TempDir()
	// Occupy the target source path with a directory so the atomic write fails.
	target := filepath.Join(root, ".agent-layer", "claude-statusline.sh")
	require.NoError(t, os.MkdirAll(target, 0o750))

	err := applyStatuslineSourceChanges(root, statuslineSourceChangeSet{
		sourcesToCreate: []install.StatuslineSourceTemplate{{
			RelPath:      ".agent-layer/claude-statusline.sh",
			TemplatePath: "claude-statusline.sh",
			Perm:         0o755,
		}},
	})
	require.Error(t, err)
}

func TestApplyStatuslineSourceChanges_PropagatesCreateDirError(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, ".agent-layer")
	require.NoError(t, os.WriteFile(blocker, []byte("not a directory"), 0o600))

	err := applyStatuslineSourceChanges(root, statuslineSourceChangeSet{
		sourcesToCreate: []install.StatuslineSourceTemplate{{
			RelPath:      ".agent-layer/claude-statusline.sh",
			TemplatePath: "claude-statusline.sh",
			Perm:         0o755,
		}},
	})
	require.Error(t, err)
	assert.False(t, errors.Is(err, os.ErrNotExist))
}
