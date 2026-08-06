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

func TestCatalogSkillIsManagedOnDiskRequiresConfiguredOwnershipMarker(t *testing.T) {
	root := t.TempDir()
	entry := CLISkillCatalogEntry{
		ID:              "skill-sync",
		Name:            "Agent Layer skill sync",
		OwnershipMarker: "<!-- agent-layer-catalog-skill: skill-sync -->",
	}
	skillDir := filepath.Join(root, ".agent-layer", "skills", entry.ID)
	require.NoError(t, os.MkdirAll(skillDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# User skill\n"), 0o600))

	assert.False(t, catalogSkillIsManagedOnDisk(root, entry), "same-name user skill is not catalog-managed")

	template, err := templates.Read("skills-catalog/skill-sync/SKILL.md")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), template, 0o600))
	assert.True(t, catalogSkillIsManagedOnDisk(root, entry), "catalog template carries its ownership marker")
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

func TestAgentLayerDiskEvidenceTreatsStatErrorsAsPresent(t *testing.T) {
	t.Run("live memory stat error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, "docs", "agent-layer")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks memory dir"), 0o600))

		assert.True(t, hasAnyMemoryFile(root))
	})

	t.Run("template memory stat error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "templates", "docs")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks template memory dir"), 0o600))

		assert.True(t, hasAnyTemplateMemoryFile(root))
	})

	t.Run("standard instruction stat error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "instructions")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks instruction dir"), 0o600))

		assert.True(t, hasAnyStandardInstructionFile(root))
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
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "implement"), 0o750))
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

	t.Run("returns true when a memory template exists", func(t *testing.T) {
		root := t.TempDir()
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

	// A repo installed before the 0.16.0 instruction consolidation still holds
	// the pre-consolidation filenames until `al upgrade` runs. Treating those as
	// evidence keeps the wizard from offering to install a workflow bundle over
	// files the user already has.
	t.Run("returns true when only a pre-consolidation instruction file exists", func(t *testing.T) {
		for _, name := range legacyInstructionBasenames {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", name), []byte("legacy instructions"), 0o600))
			assert.True(t, detectAgentLayerEnabledFromDisk(root), "%s should count as workflow-bundle evidence", name)
		}
	})

	t.Run("returns true when managed instruction template exists", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
		rulesTemplate, err := templates.Read("instructions/00_rules.md")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"), rulesTemplate, 0o600))
		assert.True(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns true when edited managed instruction exists", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"), []byte("custom rules"), 0o600))
		assert.True(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns true on instruction read errors", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"), 0o750))
		assert.True(t, detectAgentLayerEnabledFromDisk(root))
	})

	t.Run("returns true for empty root", func(t *testing.T) {
		assert.True(t, detectAgentLayerEnabledFromDisk(""))
	})
}
