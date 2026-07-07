package wizard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestComputeSkillsChangeSet_CatalogAddAndRemove(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "tavily-web"), 0o750))
	choices := NewChoices()
	choices.CLISkillsCatalog = []CLISkillCatalogEntry{
		{ID: "tavily-web", Name: "Tavily"},
		{ID: "find-docs", Name: "Find Docs"},
	}
	choices.EnabledCLISkills["tavily-web"] = false // existing → remove
	choices.EnabledCLISkills["find-docs"] = true   // missing → add

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Equal(t, []string{"find-docs"}, cs.catalogSkillsToAdd)
	assert.Equal(t, []string{"tavily-web"}, cs.catalogSkillsToRemove)
	assert.Empty(t, cs.workflowSkillsToInstall)
	assert.Empty(t, cs.memoryFilesToCreate)
	assert.NotEmpty(t, buildSkillsPreview(cs))
}

func TestComputeSkillsChangeSet_CatalogRepairMissingFiles(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agent-layer", "skills", "playwright-cli")
	require.NoError(t, os.MkdirAll(skillDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("custom skill text"), 0o600))

	choices := NewChoices()
	choices.CLISkillsCatalog = []CLISkillCatalogEntry{{ID: "playwright-cli", Name: "Playwright"}}
	choices.EnabledCLISkills["playwright-cli"] = true

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Empty(t, cs.catalogSkillsToAdd)
	assert.Equal(t, []string{"playwright-cli"}, cs.catalogSkillsToRepair)
	assert.Empty(t, cs.catalogSkillsToRemove)
}

func TestComputeSkillsChangeSet_CatalogRemoveIgnoresMalformedMissingFiles(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agent-layer", "skills", "playwright-cli")
	require.NoError(t, os.MkdirAll(skillDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "references"), []byte("file blocks embedded reference dir"), 0o600))

	choices := NewChoices()
	choices.CLISkillsCatalog = []CLISkillCatalogEntry{{ID: "playwright-cli", Name: "Playwright"}}

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Empty(t, cs.catalogSkillsToAdd)
	assert.Empty(t, cs.catalogSkillsToRepair)
	assert.Equal(t, []string{"playwright-cli"}, cs.catalogSkillsToRemove)
}

func TestComputeSkillsChangeSet_WorkflowBundleNoDoesNotPrune(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "review-uncommitted-code"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "tavily-web"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "custom-user-skill"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "templates", "docs"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
	issuesTemplate, err := templates.Read("docs/agent-layer/ISSUES.md")
	require.NoError(t, err)
	rulesTemplate, err := templates.Read("instructions/00_rules.md")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "agent-layer", "ISSUES.md"), issuesTemplate, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docs", "agent-layer", "BACKLOG.md"), []byte("custom backlog"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "templates", "docs", "ISSUES.md"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"), rulesTemplate, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "04_conventions.md"), []byte("custom conventions"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "custom.md"), []byte("x"), 0o600))

	choices := NewChoices()
	choices.CLISkillsCatalog = []CLISkillCatalogEntry{{ID: "tavily-web", Name: "Tavily"}}
	choices.EnabledCLISkills["tavily-web"] = true
	choices.InstallWorkflowBundle = false
	choices.InstallWorkflowBundleTouched = true

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Empty(t, cs.workflowSkillsToInstall)
	assert.Empty(t, cs.memoryFilesToCreate)
	assert.Empty(t, cs.templateMemoryFilesToCreate)
	assert.Empty(t, cs.managedInstructionFilesToCreate)
	assert.Empty(t, cs.userInstructionFilesToCreate)
	// tavily-web is a catalog skill present on disk AND selected → no change.
	assert.Empty(t, cs.catalogSkillsToAdd)
	assert.Empty(t, cs.catalogSkillsToRemove)
}

func TestComputeSkillsChangeSet_WorkflowBundleInstallOnlyMissing(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "review-uncommitted-code"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "skills", "review-uncommitted-code", "SKILL.md"), []byte("custom skill"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"), []byte("custom rules"), 0o600))

	choices := NewChoices()
	choices.InstallWorkflowBundle = true
	choices.InstallWorkflowBundleTouched = true

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.NotContains(t, cs.workflowSkillsToInstall, "review-uncommitted-code")
	assert.Contains(t, cs.workflowSkillsToInstall, "plan-work")
	assert.Contains(t, cs.memoryFilesToCreate, "docs/agent-layer/ISSUES.md")
	assert.Contains(t, cs.templateMemoryFilesToCreate, ".agent-layer/templates/docs/ISSUES.md")
	assert.NotContains(t, cs.managedInstructionFilesToCreate, ".agent-layer/instructions/00_rules.md")
	assert.NotContains(t, cs.managedInstructionFilesToCreate, ".agent-layer/instructions/04_conventions.md")
	assert.Contains(t, cs.managedInstructionFilesToCreate, ".agent-layer/instructions/01_base.md")
	assert.Contains(t, cs.userInstructionFilesToCreate, ".agent-layer/instructions/04_conventions.md")
}

func TestComputeSkillsChangeSet_NoChanges(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills"), 0o750))
	choices := NewChoices()
	choices.CLISkillsCatalog = []CLISkillCatalogEntry{{ID: "tavily-web", Name: "Tavily"}}
	choices.EnabledCLISkills["tavily-web"] = false

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Empty(t, buildSkillsPreview(cs))
}

func TestApplySkillsChanges_CatalogAddCopiesEmbeddedFiles(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills"), 0o750))

	changes := skillsChangeSet{catalogSkillsToAdd: []string{"tavily-web"}}
	require.NoError(t, applySkillsChanges(root, changes))

	skillPath := filepath.Join(root, ".agent-layer", "skills", "tavily-web", "SKILL.md")
	info, err := os.Stat(skillPath)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(0), "SKILL.md should have content from embedded catalog")
}

func TestApplySkillsChanges_CatalogRepairCopiesOnlyMissingFiles(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agent-layer", "skills", "playwright-cli")
	require.NoError(t, os.MkdirAll(skillDir, 0o750))
	skillPath := filepath.Join(skillDir, "SKILL.md")
	require.NoError(t, os.WriteFile(skillPath, []byte("custom skill text"), 0o600))

	changes := skillsChangeSet{catalogSkillsToRepair: []string{"playwright-cli"}}
	require.NoError(t, applySkillsChanges(root, changes))

	data, err := os.ReadFile(skillPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "custom skill text", string(data))
	assert.FileExists(t, filepath.Join(skillDir, "LICENSE"))
}

func TestCopyCatalogSkillToDiskErrorBranches(t *testing.T) {
	root := t.TempDir()

	err := copyCatalogSkillToDisk(root, "../bad")
	require.ErrorContains(t, err, `invalid catalog skill id "../bad"`)

	err = copyCatalogSkillToDisk(root, "missing-skill")
	require.Error(t, err)

	rootFile := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(rootFile, []byte("x"), 0o600))
	err = copyCatalogSkillToDisk(rootFile, "tavily-web")
	require.Error(t, err)

	blockedRoot := t.TempDir()
	blockedSkillFile := filepath.Join(blockedRoot, ".agent-layer", "skills", "tavily-web", "SKILL.md")
	require.NoError(t, os.MkdirAll(blockedSkillFile, 0o750))
	err = copyCatalogSkillToDisk(blockedRoot, "tavily-web")
	require.Error(t, err)
}

func TestApplySkillsChanges_CatalogRemove(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".agent-layer", "skills", "tavily-web")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0o600))

	changes := skillsChangeSet{catalogSkillsToRemove: []string{"tavily-web"}}
	require.NoError(t, applySkillsChanges(root, changes))

	_, err := os.Stat(dir)
	assert.True(t, os.IsNotExist(err))
}

func TestApplySkillsChanges_WorkflowAndMemoryCreatePreservesExistingFiles(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agent-layer", "skills", "review-uncommitted-code")
	require.NoError(t, os.MkdirAll(skillDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0o600))
	missingSkillDir := filepath.Join(root, ".agent-layer", "skills", "plan-work")
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	missingMemoryPath := filepath.Join(root, "docs", "agent-layer", "BACKLOG.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(memoryPath), 0o750))
	require.NoError(t, os.WriteFile(memoryPath, []byte("x"), 0o600))
	templateMemoryPath := filepath.Join(root, ".agent-layer", "templates", "docs", "ISSUES.md")
	missingTemplateMemoryPath := filepath.Join(root, ".agent-layer", "templates", "docs", "BACKLOG.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(templateMemoryPath), 0o750))
	require.NoError(t, os.WriteFile(templateMemoryPath, []byte("x"), 0o600))
	instructionPath := filepath.Join(root, ".agent-layer", "instructions", "00_rules.md")
	missingInstructionPath := filepath.Join(root, ".agent-layer", "instructions", "01_base.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(instructionPath), 0o750))
	require.NoError(t, os.WriteFile(instructionPath, []byte("x"), 0o600))
	userInstructionPath := filepath.Join(root, ".agent-layer", "instructions", "04_conventions.md")
	require.NoError(t, os.WriteFile(userInstructionPath, []byte("custom conventions"), 0o600))

	changes := skillsChangeSet{
		workflowSkillsToInstall: []string{"review-uncommitted-code", "plan-work"},
		memoryFilesToCreate: []string{
			"docs/agent-layer/ISSUES.md",
			"docs/agent-layer/BACKLOG.md",
		},
		templateMemoryFilesToCreate: []string{
			".agent-layer/templates/docs/ISSUES.md",
			".agent-layer/templates/docs/BACKLOG.md",
		},
		managedInstructionFilesToCreate: []string{
			".agent-layer/instructions/00_rules.md",
			".agent-layer/instructions/01_base.md",
		},
		userInstructionFilesToCreate: []string{".agent-layer/instructions/04_conventions.md"},
	}
	require.NoError(t, applySkillsChanges(root, changes))

	skillData, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md")) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "x", string(skillData))
	assert.FileExists(t, filepath.Join(missingSkillDir, "SKILL.md"))
	assert.FileExists(t, memoryPath)
	memoryData, err := os.ReadFile(memoryPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "x", string(memoryData))
	assert.FileExists(t, missingMemoryPath)
	assert.FileExists(t, templateMemoryPath)
	templateMemoryData, err := os.ReadFile(templateMemoryPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "x", string(templateMemoryData))
	assert.FileExists(t, missingTemplateMemoryPath)
	instructionData, err := os.ReadFile(instructionPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "x", string(instructionData))
	assert.FileExists(t, missingInstructionPath)
	userInstructionData, err := os.ReadFile(userInstructionPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "custom conventions", string(userInstructionData))
}

func TestApplySkillsChanges_AddCatalogErrorIncludesSkill(t *testing.T) {
	err := applySkillsChanges(t.TempDir(), skillsChangeSet{catalogSkillsToAdd: []string{"../bad"}})
	require.ErrorContains(t, err, "add catalog skill ../bad")
}

func TestApplySkillsChanges_ErrorBranches(t *testing.T) {
	t.Run("install workflow skill blocked by parent file", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "skills")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks workflow install"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{workflowSkillsToInstall: []string{"review-uncommitted-code"}})
		require.ErrorContains(t, err, "install workflow skill review-uncommitted-code")
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

		err := applySkillsChanges(root, skillsChangeSet{managedInstructionFilesToCreate: []string{".agent-layer/instructions/00_rules.md"}})
		require.ErrorContains(t, err, "create managed instruction file .agent-layer/instructions/00_rules.md")
	})

	t.Run("create user instruction file reports filesystem error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "instructions")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks instruction create"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{userInstructionFilesToCreate: []string{".agent-layer/instructions/04_conventions.md"}})
		require.ErrorContains(t, err, "create instruction file .agent-layer/instructions/04_conventions.md")
	})
}

func TestApplySkillsChanges_WorkflowAndMemoryInstall(t *testing.T) {
	root := t.TempDir()
	customInstruction := filepath.Join(root, ".agent-layer", "instructions", "custom.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(customInstruction), 0o750))
	require.NoError(t, os.WriteFile(customInstruction, []byte("custom instruction"), 0o600))

	changes := skillsChangeSet{
		workflowSkillsToInstall:         []string{"review-uncommitted-code"},
		memoryFilesToCreate:             []string{"docs/agent-layer/ISSUES.md"},
		templateMemoryFilesToCreate:     []string{".agent-layer/templates/docs/ISSUES.md"},
		managedInstructionFilesToCreate: []string{".agent-layer/instructions/00_rules.md"},
		userInstructionFilesToCreate:    []string{".agent-layer/instructions/04_conventions.md"},
	}
	require.NoError(t, applySkillsChanges(root, changes))

	assert.FileExists(t, filepath.Join(root, ".agent-layer", "skills", "review-uncommitted-code", "SKILL.md"))
	assert.FileExists(t, filepath.Join(root, "docs", "agent-layer", "ISSUES.md"))
	assert.FileExists(t, filepath.Join(root, ".agent-layer", "templates", "docs", "ISSUES.md"))
	assert.FileExists(t, filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"))
	assert.FileExists(t, filepath.Join(root, ".agent-layer", "instructions", "04_conventions.md"))
	data, err := os.ReadFile(customInstruction) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "custom instruction", string(data), "workflow install leaves unrelated files in place")
}

func TestCopyTemplateDirMissingSkipsExistingFiles(t *testing.T) {
	dest := t.TempDir()
	existing := filepath.Join(dest, "00_rules.md")
	require.NoError(t, os.WriteFile(existing, []byte("custom rules"), 0o600))

	require.NoError(t, copyTemplateDirMissing("instructions", dest))

	data, err := os.ReadFile(existing) // #nosec G304 -- path is constructed from test-controlled inputs.
	require.NoError(t, err)
	assert.Equal(t, "custom rules", string(data))
	assert.FileExists(t, filepath.Join(dest, "01_base.md"))
}

func TestCopyTemplateDirMissingMissingTemplateErrors(t *testing.T) {
	err := copyTemplateDirMissing("missing-template-dir", t.TempDir())
	require.Error(t, err)
}

func TestCopyTemplateDirMissingDestinationParentError(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(dest, []byte("x"), 0o600))

	err := copyTemplateDirMissing("instructions", dest)
	require.Error(t, err)
}

func TestCopyTemplateDirMissingErrorsWhenTemplateFilePathIsDirectory(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dest, "00_rules.md"), 0o750))

	err := copyTemplateDirMissing("instructions", dest)
	require.ErrorContains(t, err, "exists but is not a regular file")
}

func TestTemplateDirHasMissingFiles(t *testing.T) {
	dest := t.TempDir()

	missing, err := templateDirHasMissingFiles("instructions", dest)
	require.NoError(t, err)
	assert.True(t, missing)

	require.NoError(t, copyTemplateDirMissing("instructions", dest))
	missing, err = templateDirHasMissingFiles("instructions", dest)
	require.NoError(t, err)
	assert.False(t, missing)

	_, err = templateDirHasMissingFiles("missing-template-dir", dest)
	require.Error(t, err)
}

func TestTemplateDirHasMissingFilesErrorsWhenTemplateFilePathIsDirectory(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dest, "00_rules.md"), 0o750))

	_, err := templateDirHasMissingFiles("instructions", dest)
	require.ErrorContains(t, err, "exists but is not a regular file")
}

func TestComputeSkillsChangeSet_CreateReportsBlockedPaths(t *testing.T) {
	t.Run("memory file read error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, "docs", "agent-layer")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks memory scan"), 0o600))
		choices := NewChoices()
		choices.InstallWorkflowBundleTouched = true
		choices.InstallWorkflowBundle = true

		_, err := computeSkillsChangeSet(root, choices)
		require.Error(t, err)
	})

	t.Run("instruction file read error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "instructions")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks instruction scan"), 0o600))
		choices := NewChoices()
		choices.InstallWorkflowBundleTouched = true
		choices.InstallWorkflowBundle = true

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

		_, err := listMissingUserOwnedInstructionFiles(root)
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
		choices.InstallWorkflowBundleTouched = true
		choices.InstallWorkflowBundle = true

		_, err := computeSkillsChangeSet(root, choices)
		require.ErrorContains(t, err, "exists but is not a regular file")
	})

	t.Run("user-owned instruction path is directory", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions", "04_conventions.md"), 0o750))

		_, err := listMissingUserOwnedInstructionFiles(root)
		require.ErrorContains(t, err, "exists but is not a regular file")
	})
}

func TestBuildSkillsPreview(t *testing.T) {
	t.Run("empty change set returns empty string", func(t *testing.T) {
		assert.Equal(t, "", buildSkillsPreview(skillsChangeSet{}))
	})

	t.Run("renders adds, repairs, removes, installs, and creates as directory summary", func(t *testing.T) {
		preview := buildSkillsPreview(skillsChangeSet{
			catalogSkillsToAdd:    []string{"find-docs"},
			catalogSkillsToRepair: []string{"playwright-cli"},
			catalogSkillsToRemove: []string{"tavily-web"},
			workflowSkillsToInstall: []string{
				"review-uncommitted-code",
			},
			memoryFilesToCreate: []string{"docs/agent-layer/BACKLOG.md"},
			templateMemoryFilesToCreate: []string{
				".agent-layer/templates/docs/BACKLOG.md",
			},
			managedInstructionFilesToCreate: []string{
				".agent-layer/instructions/00_rules.md",
			},
			userInstructionFilesToCreate: []string{
				".agent-layer/instructions/04_conventions.md",
			},
		})
		assert.Contains(t, preview, "+ .agent-layer/skills/find-docs/")
		assert.Contains(t, preview, "+ .agent-layer/skills/playwright-cli/  (missing catalog skill files)")
		assert.Contains(t, preview, "- .agent-layer/skills/tavily-web/")
		assert.Contains(t, preview, "+ .agent-layer/skills/review-uncommitted-code/  (workflow bundle install)")
		assert.Contains(t, preview, "docs/agent-layer/BACKLOG.md  (memory file)")
		assert.Contains(t, preview, ".agent-layer/templates/docs/BACKLOG.md  (memory template)")
		assert.Contains(t, preview, ".agent-layer/instructions/00_rules.md  (managed instruction seed)")
		assert.Contains(t, preview, ".agent-layer/instructions/04_conventions.md  (user-owned instruction seed)")
	})
}
