package wizard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestComputeSkillsChangeSet_CatalogAddAndRemove(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "dispatch-agent"), 0o750))
	choices := NewChoices()
	choices.CLISkillsCatalog = []templates.CLISkillCatalogEntry{
		{ID: "dispatch-agent", Name: "Tavily"},
		{ID: "skill-sync", Name: "Find Docs"},
	}
	choices.EnabledCLISkills["dispatch-agent"] = false // existing → remove
	choices.EnabledCLISkills["skill-sync"] = true      // missing → add

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Equal(t, []string{"skill-sync"}, cs.catalogSkillsToAdd)
	assert.Equal(t, []string{"dispatch-agent"}, cs.catalogSkillsToRemove)
	assert.Empty(t, cs.memoryFilesToCreate)
	assert.NotEmpty(t, buildSkillsPreview(cs))
}

func TestComputeSkillsChangeSet_LegacyDispatchAgentState(t *testing.T) {
	entry := templates.CLISkillCatalogEntry{ID: "dispatch-agent", Name: "Agent dispatch"}

	t.Run("selected legacy installation is preserved for migration", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", legacyDispatchAgentCatalogID), 0o750))
		choices := NewChoices()
		choices.CLISkillsCatalog = []templates.CLISkillCatalogEntry{entry}
		choices.EnabledCLISkills[entry.ID] = true

		changes, err := computeSkillsChangeSet(root, choices)
		require.NoError(t, err)
		assert.Empty(t, changes.catalogSkillsToAdd)
		assert.Empty(t, changes.catalogSkillsToRepair)
		assert.Empty(t, changes.catalogSkillsToRemove)
		assert.NoDirExists(t, filepath.Join(root, ".agent-layer", "skills", entry.ID))
	})

	t.Run("deselected legacy installation is removable", func(t *testing.T) {
		root := t.TempDir()
		legacyDir := filepath.Join(root, ".agent-layer", "skills", legacyDispatchAgentCatalogID)
		require.NoError(t, os.MkdirAll(legacyDir, 0o750))
		choices := NewChoices()
		choices.CLISkillsCatalog = []templates.CLISkillCatalogEntry{entry}

		changes, err := computeSkillsChangeSet(root, choices)
		require.NoError(t, err)
		assert.Equal(t, []string{legacyDispatchAgentCatalogID}, changes.catalogSkillsToRemove)
		require.NoError(t, applySkillsChanges(root, changes))
		assert.NoDirExists(t, legacyDir)
	})
}

func TestComputeSkillsChangeSet_CatalogRepairMissingFiles(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agent-layer", "skills", "benchmark")
	require.NoError(t, os.MkdirAll(skillDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("custom skill text"), 0o600))

	choices := NewChoices()
	choices.CLISkillsCatalog = []templates.CLISkillCatalogEntry{{ID: "benchmark", Name: "Playwright"}}
	choices.EnabledCLISkills["benchmark"] = true

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Empty(t, cs.catalogSkillsToAdd)
	assert.Equal(t, []string{"benchmark"}, cs.catalogSkillsToRepair)
	assert.Empty(t, cs.catalogSkillsToRemove)
}

func TestComputeSkillsChangeSet_CatalogRemoveIgnoresMalformedMissingFiles(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agent-layer", "skills", "benchmark")
	require.NoError(t, os.MkdirAll(skillDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "references"), []byte("file blocks embedded reference dir"), 0o600))

	choices := NewChoices()
	choices.CLISkillsCatalog = []templates.CLISkillCatalogEntry{{ID: "benchmark", Name: "Playwright"}}

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Empty(t, cs.catalogSkillsToAdd)
	assert.Empty(t, cs.catalogSkillsToRepair)
	assert.Equal(t, []string{"benchmark"}, cs.catalogSkillsToRemove)
}

func TestComputeSkillsChangeSet_PreservesUserOwnedCatalogPath(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agent-layer", "skills", "skill-sync")
	require.NoError(t, os.MkdirAll(skillDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# User-owned skill\n"), 0o600))
	entry := templates.CLISkillCatalogEntry{
		ID:              "skill-sync",
		Name:            "Agent Layer skill sync",
		OwnershipMarker: "<!-- agent-layer-catalog-skill: skill-sync -->",
	}

	choices := NewChoices()
	choices.CLISkillsCatalog = []templates.CLISkillCatalogEntry{entry}
	changes, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Empty(t, changes.catalogSkillsToRemove)

	choices.EnabledCLISkills[entry.ID] = true
	_, err = computeSkillsChangeSet(root, choices)
	require.ErrorContains(t, err, "already exists and is not catalog-managed")
	assert.FileExists(t, filepath.Join(skillDir, "SKILL.md"))

	template, err := templates.Read("skills-catalog/skill-sync/SKILL.md")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), template, 0o600))
	choices.EnabledCLISkills[entry.ID] = false
	changes, err = computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Equal(t, []string{"skill-sync"}, changes.catalogSkillsToRemove)
}

func TestComputeSkillsChangeSet_InstructionNoneDoesNotPrune(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "implement"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "dispatch-agent"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "custom-user-skill"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "templates", "docs"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
	issuesTemplate, err := templates.Read("docs/agent-layer/ISSUES.md")
	require.NoError(t, err)
	rulesTemplate := []byte("legacy customized rules\n")
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "agent-layer", "ISSUES.md"), issuesTemplate, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "agent-layer", "BACKLOG.md"), []byte("custom backlog"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "templates", "docs", "ISSUES.md"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"), rulesTemplate, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "custom.md"), []byte("x"), 0o600))

	choices := NewChoices()
	choices.CLISkillsCatalog = []templates.CLISkillCatalogEntry{{ID: "dispatch-agent", Name: "Tavily"}}
	choices.EnabledCLISkills["dispatch-agent"] = true
	choices.InstructionSet = InstructionSetNone
	choices.InstructionSetTouched = true

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Empty(t, cs.memoryFilesToCreate)
	assert.Empty(t, cs.templateMemoryFilesToCreate)
	assert.Empty(t, cs.instructionImports)
	// dispatch-agent is a catalog skill present on disk AND selected → no change.
	assert.Empty(t, cs.catalogSkillsToAdd)
	assert.Empty(t, cs.catalogSkillsToRemove)
}

func TestComputeSkillsChangeSet_NoChanges(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills"), 0o750))
	choices := NewChoices()
	choices.CLISkillsCatalog = []templates.CLISkillCatalogEntry{{ID: "dispatch-agent", Name: "Tavily"}}
	choices.EnabledCLISkills["dispatch-agent"] = false

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Empty(t, buildSkillsPreview(cs))
}

func TestApplySkillsChanges_CatalogAddCopiesEmbeddedFiles(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills"), 0o750))

	changes := skillsChangeSet{catalogSkillsToAdd: []string{"benchmark"}}
	require.NoError(t, applySkillsChanges(root, changes))

	skillPath := filepath.Join(root, ".agent-layer", "skills", "benchmark", "SKILL.md")
	info, err := os.Stat(skillPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0), "SKILL.md should have content from embedded catalog")
	data, err := os.ReadFile(skillPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Contains(t, string(data), "\nname: benchmark\n")
	assert.Contains(t, string(data), "al benchmark")
}

func TestApplySkillsChanges_CatalogRepairCopiesOnlyMissingFiles(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agent-layer", "skills", "benchmark")
	require.NoError(t, os.MkdirAll(skillDir, 0o750))
	skillPath := filepath.Join(skillDir, "SKILL.md")
	require.NoError(t, os.WriteFile(skillPath, []byte("custom skill text"), 0o600))

	changes := skillsChangeSet{catalogSkillsToRepair: []string{"benchmark"}}
	require.NoError(t, applySkillsChanges(root, changes))

	data, err := os.ReadFile(skillPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "custom skill text", string(data))
	assert.FileExists(t, filepath.Join(skillDir, "references", "results-and-artifacts.md"))
}

func TestCopySkillDirToDiskErrorBranches(t *testing.T) {
	root := t.TempDir()

	err := copySkillDirToDisk(root, cliSkillsCatalogTemplateRoot+"/../bad", "../bad")
	require.ErrorContains(t, err, `invalid catalog skill id "../bad"`)

	err = copySkillDirToDisk(root, cliSkillsCatalogTemplateRoot+"/missing-skill", "missing-skill")
	require.Error(t, err)

	rootFile := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(rootFile, []byte("x"), 0o600))
	err = copySkillDirToDisk(rootFile, cliSkillsCatalogTemplateRoot+"/dispatch-agent", "dispatch-agent")
	require.Error(t, err)

	blockedRoot := t.TempDir()
	blockedSkillFile := filepath.Join(blockedRoot, ".agent-layer", "skills", "dispatch-agent", "SKILL.md")
	require.NoError(t, os.MkdirAll(blockedSkillFile, 0o750))
	err = copySkillDirToDisk(blockedRoot, cliSkillsCatalogTemplateRoot+"/dispatch-agent", "dispatch-agent")
	require.Error(t, err)
}

func TestApplySkillsChanges_CatalogRemove(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer", "skills", "dispatch-agent")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0o600))

	changes := skillsChangeSet{catalogSkillsToRemove: []string{"dispatch-agent"}}
	require.NoError(t, applySkillsChanges(root, changes))

	_, err := os.Stat(dir)
	assert.True(t, os.IsNotExist(err))
}

func TestApplySkillsChanges_AddCatalogErrorIncludesSkill(t *testing.T) {
	err := applySkillsChanges(t.TempDir(), skillsChangeSet{catalogSkillsToAdd: []string{"../bad"}})
	require.ErrorContains(t, err, "add catalog skill ../bad")
}

func TestApplySkillsChanges_ErrorBranches(t *testing.T) {
	t.Run("add retained catalog skill blocked by parent file", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "skills")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks catalog install"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{
			catalogSkillsToAdd: []string{"dispatch-agent"},
		})
		require.ErrorContains(t, err, "add catalog skill dispatch-agent")
	})

	t.Run("create memory files reports filesystem error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, "docs", "agent-layer")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks memory create"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{memoryFilesToCreate: []string{"docs/agent-layer/ISSUES.md"}})
		require.ErrorContains(t, err, "create memory files")
	})

	t.Run("create memory templates reports filesystem error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "templates", "docs")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks template create"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{templateMemoryFilesToCreate: []string{".agent-layer/templates/docs/ISSUES.md"}})
		require.ErrorContains(t, err, "create memory templates")
	})

	t.Run("create managed instruction file reports filesystem error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "instructions")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks instruction create"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{instructionImports: []string{".agent-layer/instructions/00_rules.md"}})
		require.ErrorContains(t, err, "instruction import failed")
	})

}

func TestCopyTemplateDirMissingSkipsExistingFiles(t *testing.T) {
	dest := t.TempDir()
	existing := filepath.Join(dest, "CONTEXT.md")
	require.NoError(t, os.WriteFile(existing, []byte("custom rules"), 0o600))

	require.NoError(t, copyTemplateDirMissing("docs/agent-layer", dest))

	data, err := os.ReadFile(existing) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "custom rules", string(data))
	assert.FileExists(t, filepath.Join(dest, "ISSUES.md"))
}

func TestCopyTemplateDirMissingMissingTemplateErrors(t *testing.T) {
	err := copyTemplateDirMissing("missing-template-dir", t.TempDir())
	require.Error(t, err)
}

func TestCopyTemplateDirMissingDestinationParentError(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(dest, []byte("x"), 0o600))

	err := copyTemplateDirMissing("docs/agent-layer", dest)
	require.Error(t, err)
}

func TestCopyTemplateDirMissingErrorsWhenTemplateFilePathIsDirectory(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dest, "CONTEXT.md"), 0o750))

	err := copyTemplateDirMissing("docs/agent-layer", dest)
	require.ErrorContains(t, err, "exists but is not a regular file")
}

func TestTemplateDirHasMissingFiles(t *testing.T) {
	dest := t.TempDir()

	missing, err := templateDirHasMissingFiles("docs/agent-layer", dest)
	require.NoError(t, err)
	assert.True(t, missing)

	require.NoError(t, copyTemplateDirMissing("docs/agent-layer", dest))
	missing, err = templateDirHasMissingFiles("docs/agent-layer", dest)
	require.NoError(t, err)
	assert.False(t, missing)

	_, err = templateDirHasMissingFiles("missing-template-dir", dest)
	require.Error(t, err)
}

func TestTemplateDirHasMissingFilesErrorsWhenTemplateFilePathIsDirectory(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dest, "CONTEXT.md"), 0o750))

	_, err := templateDirHasMissingFiles("docs/agent-layer", dest)
	require.ErrorContains(t, err, "exists but is not a regular file")
}

func TestComputeSkillsChangeSet_CreateReportsBlockedPaths(t *testing.T) {
	t.Run("memory file read error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, "docs", "agent-layer")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks memory scan"), 0o600))
		choices := NewChoices()
		choices.InstructionSetTouched = true
		choices.InstructionSet = InstructionSetRulesAndMemory

		_, err := computeSkillsChangeSet(root, choices)
		require.Error(t, err)
	})

	t.Run("instruction file read error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "instructions")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks instruction scan"), 0o600))
		choices := NewChoices()
		choices.InstructionSetTouched = true
		choices.InstructionSet = InstructionSetRulesAndMemory

		_, err := computeSkillsChangeSet(root, choices)
		require.Error(t, err)
	})
}

func TestListMissingManagedFilesReportStatErrors(t *testing.T) {
	t.Run("memory file scan", func(t *testing.T) {
		root := t.TempDir()
		dest := filepath.Join(root, "docs", "agent-layer")
		require.NoError(t, os.MkdirAll(filepath.Dir(dest), 0o750))
		require.NoError(t, os.WriteFile(dest, []byte("file blocks memory scan"), 0o600))

		_, err := listMissingMemoryFiles(root, dest)
		require.Error(t, err)
	})

	t.Run("instruction file scan", func(t *testing.T) {
		root := t.TempDir()
		instructionsDir := filepath.Join(root, ".agent-layer", "instructions")
		require.NoError(t, os.MkdirAll(filepath.Dir(instructionsDir), 0o750))
		require.NoError(t, os.WriteFile(instructionsDir, []byte("file blocks instruction scan"), 0o600))
		choices := NewChoices()
		choices.InstructionSetTouched = true
		choices.InstructionSet = InstructionSetRulesAndMemory

		_, err := computeSkillsChangeSet(root, choices)
		require.Error(t, err)
	})

	t.Run("memory path is directory", func(t *testing.T) {
		root := t.TempDir()
		dest := filepath.Join(root, "docs", "agent-layer")
		require.NoError(t, os.MkdirAll(filepath.Join(dest, "ISSUES.md"), 0o750))

		_, err := listMissingMemoryFiles(root, dest)
		require.ErrorContains(t, err, "exists but is not a regular file")
	})

	t.Run("managed instruction path is directory", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"), 0o750))
		choices := NewChoices()
		choices.InstructionSetTouched = true
		choices.InstructionSet = InstructionSetRulesAndMemory

		_, err := computeSkillsChangeSet(root, choices)
		require.ErrorContains(t, err, "exists but is not a regular file")
	})

}

func TestBuildSkillsPreview(t *testing.T) {
	t.Run("empty change set returns empty string", func(t *testing.T) {
		assert.Equal(t, "", buildSkillsPreview(skillsChangeSet{}))
	})

	t.Run("renders catalog and instruction changes as a directory summary", func(t *testing.T) {
		preview := buildSkillsPreview(skillsChangeSet{
			catalogSkillsToAdd:    []string{"skill-sync"},
			catalogSkillsToRepair: []string{"benchmark"},
			catalogSkillsToRemove: []string{"dispatch-agent"},
			memoryFilesToCreate:   []string{"docs/agent-layer/BACKLOG.md"},
			templateMemoryFilesToCreate: []string{
				".agent-layer/templates/docs/BACKLOG.md",
			},
			instructionImports: []string{
				".agent-layer/instructions/00_rules.md",
			},
		})
		assert.Contains(t, preview, "+ .agent-layer/skills/skill-sync/")
		assert.Contains(t, preview, "+ .agent-layer/skills/benchmark/  (missing catalog skill files)")
		assert.Contains(t, preview, "- .agent-layer/skills/dispatch-agent/")
		assert.Contains(t, preview, "docs/agent-layer/BACKLOG.md  (memory file)")
		assert.Contains(t, preview, ".agent-layer/templates/docs/BACKLOG.md  (memory template)")
		assert.Contains(t, preview, ".agent-layer/instructions/00_rules.md  (Git instruction import)")
	})
}

func TestWizardInstructionOptionsUseImportsAndPreserveLocalFiles(t *testing.T) {
	for _, set := range []InstructionSet{InstructionSetNone, InstructionSetRules, InstructionSetRulesAndMemory} {
		t.Run(string(set), func(t *testing.T) {
			testutil.CatalogGitFixture(t)
			root := t.TempDir()
			require.NoError(t, install.Run(root, install.Options{System: install.RealSystem{}}))
			paths := config.DefaultPaths(root)
			raw, err := os.ReadFile(paths.ConfigPath)
			require.NoError(t, err)
			raw = append(raw, []byte("\n[[instructions.local]]\nselectors = [\"conventions.md\"]\norder = 40\n")...)
			require.NoError(t, os.WriteFile(paths.ConfigPath, raw, 0o600)) // #nosec G703 -- test-owned project configuration.
			require.NoError(t, os.WriteFile(filepath.Join(paths.InstructionsDir, "conventions.md"), []byte("project owned\n"), 0o600))
			choices := NewChoices()
			choices.InstructionSetTouched = true
			choices.InstructionSet = set
			changes, err := computeSkillsChangeSet(root, choices)
			require.NoError(t, err)
			require.NoError(t, applySkillsChanges(root, changes))
			cfg, err := config.LoadConfigLenient(paths.ConfigPath)
			require.NoError(t, err)
			count := 0
			if set != InstructionSetNone {
				count = 1
			}
			if set == InstructionSetRulesAndMemory {
				count = 2
			}
			require.Len(t, cfg.Instructions.Imports, count)
			for _, imp := range cfg.Instructions.Imports {
				expectedOrder := 0
				if imp.Selectors[0] == "instructions/memory.md" {
					expectedOrder = 10
				}
				require.Equal(t, expectedOrder, *imp.Order)
				require.Equal(t, templates.GeneralSkillsRepository, imp.Repository)
			}
			bytes, err := os.ReadFile(filepath.Join(paths.InstructionsDir, "conventions.md"))
			require.NoError(t, err)
			require.Equal(t, "project owned\n", string(bytes))
			_, err = os.Stat(filepath.Join(root, "docs", "agent-layer", "CONTEXT.md"))
			require.Equal(t, set == InstructionSetRulesAndMemory, err == nil)
		})
	}
}

func TestWizardInstructionPreviewValidatesCombinedAdoptionOrdersOffline(t *testing.T) {
	root := t.TempDir()
	paths := config.DefaultPaths(root)
	require.NoError(t, os.MkdirAll(paths.InstructionsDir, 0o750))
	raw := []byte("[[instructions.local]]\nselectors = [\"00_rules.md\"]\norder = 10\n")
	require.NoError(t, os.WriteFile(paths.ConfigPath, raw, 0o600))
	legacy := filepath.Join(paths.InstructionsDir, "00_rules.md")
	body := []byte("customized legacy rules\n")
	require.NoError(t, os.WriteFile(legacy, body, 0o600))
	choices := NewChoices()
	choices.InstructionSetTouched = true
	choices.InstructionSet = InstructionSetRulesAndMemory

	_, err := computeSkillsChangeSet(root, choices)
	require.ErrorContains(t, err, "duplicate instruction order 10")
	actual, err := os.ReadFile(paths.ConfigPath)
	require.NoError(t, err)
	require.Equal(t, raw, actual)
	actual, err = os.ReadFile(legacy)
	require.NoError(t, err)
	require.Equal(t, body, actual)
	require.NoDirExists(t, paths.ImportedInstructionsDir)
}
