package wizard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestCatalogSkillExistsOnDisk(t *testing.T) {
	root := t.TempDir()
	require.False(t, catalogSkillExistsOnDisk(root, "tavily-web"))

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "tavily-web"), 0o750))
	require.True(t, catalogSkillExistsOnDisk(root, "tavily-web"))

	assert.False(t, catalogSkillExistsOnDisk("", "tavily-web"), "empty root returns false")
	assert.False(t, catalogSkillExistsOnDisk(root, ""), "empty id returns false")
}

func TestHasNonCatalogWorkflowSkillHandlesMalformedSkillsDir(t *testing.T) {
	t.Run("returns true when skills path cannot be read as a directory", func(t *testing.T) {
		root := t.TempDir()
		skillsPath := filepath.Join(root, ".agent-layer", "skills")
		require.NoError(t, os.MkdirAll(filepath.Dir(skillsPath), 0o750))
		require.NoError(t, os.WriteFile(skillsPath, []byte("not a directory"), 0o600))

		assert.True(t, hasNonCatalogWorkflowSkill(root))
	})

	t.Run("ignores non-directory entries", func(t *testing.T) {
		root := t.TempDir()
		skillsDir := filepath.Join(root, ".agent-layer", "skills")
		require.NoError(t, os.MkdirAll(skillsDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "README.md"), []byte("not a skill"), 0o600))

		assert.False(t, hasNonCatalogWorkflowSkill(root))
	})
}

func TestDetectAgentLayerEnabledFromDisk(t *testing.T) {
	t.Run("returns false on empty layout", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills"), 0o750))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o750))
		assert.False(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns true when a workflow skill directory exists", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "review-scope"), 0o750))
		assert.True(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns false when only catalog skill directories exist", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "tavily-web"), 0o750))
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "agent-dispatch"), 0o750))
		assert.False(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns false when only custom skill directories exist", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "custom-user-skill"), 0o750))
		assert.False(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns true when a memory file exists", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "agent-layer", "ISSUES.md"), []byte("x"), 0o600))
		assert.True(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns false when minimal placeholder exists with preserved live memory", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_instructions.md"), nil, 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "agent-layer", "ISSUES.md"), []byte("edited memory"), 0o600))
		assert.False(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns true when a memory template exists with the minimal placeholder", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_instructions.md"), nil, 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "templates", "docs"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "templates", "docs", "ISSUES.md"), []byte("x"), 0o600))
		assert.True(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns true when a standard instruction file exists", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"), []byte("x"), 0o600))
		assert.True(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns false when minimal placeholder exists with custom instruction", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_instructions.md"), nil, 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "04_conventions.md"), []byte("custom conventions"), 0o600))
		assert.False(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns true when minimal placeholder exists with template instruction", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_instructions.md"), nil, 0o600))
		rulesTemplate, err := templates.Read("instructions/00_rules.md")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"), rulesTemplate, 0o600))
		assert.True(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns true on instruction read errors with minimal placeholder", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_instructions.md"), nil, 0o600))
		assert.True(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns true for empty root", func(t *testing.T) {
		assert.True(t, detectAgentLayerEnabledFromDisk(""))
	})
}
