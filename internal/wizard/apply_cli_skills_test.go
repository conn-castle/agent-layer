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
	assert.Empty(t, cs.workflowSkillsToRemove)
	assert.Empty(t, cs.memoryFilesToRemove)
	assert.NotEmpty(t, buildSkillsPreview(cs))
}

func TestComputeSkillsChangeSet_WorkflowBundlePrune(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "review-scope"), 0o750))
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
	choices.EnableAgentLayer = false
	choices.EnableAgentLayerTouched = true

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Equal(t, []string{"review-scope"}, cs.workflowSkillsToRemove)
	assert.Equal(t, []string{"docs/agent-layer/ISSUES.md"}, cs.memoryFilesToRemove)
	assert.NotContains(t, cs.memoryFilesToRemove, "docs/agent-layer/BACKLOG.md")
	assert.Equal(t, []string{".agent-layer/templates/docs/ISSUES.md"}, cs.templateMemoryFilesToRemove)
	assert.Equal(t, []string{".agent-layer/instructions/00_rules.md"}, cs.instructionFilesToRemove)
	assert.NotContains(t, cs.instructionFilesToRemove, ".agent-layer/instructions/04_conventions.md")
	assert.True(t, cs.addInstructionPlaceholder)
	// tavily-web is a catalog skill present on disk AND selected → no change.
	assert.Empty(t, cs.catalogSkillsToAdd)
	assert.Empty(t, cs.catalogSkillsToRemove)
}

func TestComputeSkillsChangeSet_WorkflowBundleRestore(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "instructions", "00_instructions.md"), nil, 0o600))

	choices := NewChoices()
	choices.EnableAgentLayer = true
	choices.EnableAgentLayerTouched = true

	cs, err := computeSkillsChangeSet(root, choices)
	require.NoError(t, err)
	assert.Contains(t, cs.workflowSkillsToAdd, "review-scope")
	assert.Contains(t, cs.memoryFilesToAdd, "docs/agent-layer/ISSUES.md")
	assert.Contains(t, cs.templateMemoryFilesToAdd, ".agent-layer/templates/docs/ISSUES.md")
	assert.Contains(t, cs.instructionFilesToAdd, ".agent-layer/instructions/00_rules.md")
	assert.False(t, cs.addInstructionPlaceholder)
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

func TestApplySkillsChanges_WorkflowAndMemoryPrune(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agent-layer", "skills", "review-scope")
	require.NoError(t, os.MkdirAll(skillDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x"), 0o600))
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(memoryPath), 0o750))
	require.NoError(t, os.WriteFile(memoryPath, []byte("x"), 0o600))
	templateMemoryPath := filepath.Join(root, ".agent-layer", "templates", "docs", "ISSUES.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(templateMemoryPath), 0o750))
	require.NoError(t, os.WriteFile(templateMemoryPath, []byte("x"), 0o600))
	instructionPath := filepath.Join(root, ".agent-layer", "instructions", "00_rules.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(instructionPath), 0o750))
	require.NoError(t, os.WriteFile(instructionPath, []byte("x"), 0o600))

	changes := skillsChangeSet{
		workflowSkillsToRemove:      []string{"review-scope"},
		memoryFilesToRemove:         []string{"docs/agent-layer/ISSUES.md"},
		templateMemoryFilesToRemove: []string{".agent-layer/templates/docs/ISSUES.md"},
		instructionFilesToRemove:    []string{".agent-layer/instructions/00_rules.md"},
		addInstructionPlaceholder:   true,
	}
	require.NoError(t, applySkillsChanges(root, changes))

	_, err := os.Stat(skillDir)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(memoryPath)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(templateMemoryPath)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(instructionPath)
	assert.True(t, os.IsNotExist(err))
	placeholder := filepath.Join(root, ".agent-layer", "instructions", "00_instructions.md")
	info, err := os.Stat(placeholder)
	require.NoError(t, err)
	assert.Equal(t, int64(0), info.Size())
}

func TestApplySkillsChanges_AddCatalogErrorIncludesSkill(t *testing.T) {
	err := applySkillsChanges(t.TempDir(), skillsChangeSet{catalogSkillsToAdd: []string{"../bad"}})
	require.ErrorContains(t, err, "add catalog skill ../bad")
}

func TestApplySkillsChanges_ErrorBranches(t *testing.T) {
	t.Run("restore workflow skill blocked by file", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "skills", "review-scope")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks directory restore"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{workflowSkillsToAdd: []string{"review-scope"}})
		require.ErrorContains(t, err, "restore workflow skill review-scope")
	})

	t.Run("remove memory file reports filesystem error", func(t *testing.T) {
		root := t.TempDir()
		blockedDir := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
		require.NoError(t, os.MkdirAll(blockedDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(blockedDir, "child"), []byte("x"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{memoryFilesToRemove: []string{"docs/agent-layer/ISSUES.md"}})
		require.ErrorContains(t, err, "remove memory file docs/agent-layer/ISSUES.md")
	})

	t.Run("restore memory files reports filesystem error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, "docs", "agent-layer")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks memory restore"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{memoryFilesToAdd: []string{"docs/agent-layer/ISSUES.md"}})
		require.ErrorContains(t, err, "restore memory files")
	})

	t.Run("remove memory template reports filesystem error", func(t *testing.T) {
		root := t.TempDir()
		blockedDir := filepath.Join(root, ".agent-layer", "templates", "docs", "ISSUES.md")
		require.NoError(t, os.MkdirAll(blockedDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(blockedDir, "child"), []byte("x"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{templateMemoryFilesToRemove: []string{".agent-layer/templates/docs/ISSUES.md"}})
		require.ErrorContains(t, err, "remove memory template .agent-layer/templates/docs/ISSUES.md")
	})

	t.Run("restore memory templates reports filesystem error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "templates", "docs")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks template restore"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{templateMemoryFilesToAdd: []string{".agent-layer/templates/docs/ISSUES.md"}})
		require.ErrorContains(t, err, "restore memory templates")
	})

	t.Run("remove instruction file reports filesystem error", func(t *testing.T) {
		root := t.TempDir()
		blockedDir := filepath.Join(root, ".agent-layer", "instructions", "00_rules.md")
		require.NoError(t, os.MkdirAll(blockedDir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(blockedDir, "child"), []byte("x"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{instructionFilesToRemove: []string{".agent-layer/instructions/00_rules.md"}})
		require.ErrorContains(t, err, "remove instruction file .agent-layer/instructions/00_rules.md")
	})

	t.Run("restore instruction files reports filesystem error", func(t *testing.T) {
		root := t.TempDir()
		blocker := filepath.Join(root, ".agent-layer", "instructions")
		require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
		require.NoError(t, os.WriteFile(blocker, []byte("file blocks instruction restore"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{instructionFilesToAdd: []string{".agent-layer/instructions/00_rules.md"}})
		require.ErrorContains(t, err, "restore instruction files")
	})

	t.Run("create instruction placeholder reports directory error", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer"), []byte("file blocks placeholder dir"), 0o600))

		err := applySkillsChanges(root, skillsChangeSet{addInstructionPlaceholder: true})
		require.ErrorContains(t, err, "create instruction placeholder dir")
	})

	t.Run("create instruction placeholder reports write error", func(t *testing.T) {
		root := t.TempDir()
		placeholder := filepath.Join(root, ".agent-layer", "instructions", "00_instructions.md")
		require.NoError(t, os.MkdirAll(placeholder, 0o750))

		err := applySkillsChanges(root, skillsChangeSet{addInstructionPlaceholder: true})
		require.ErrorContains(t, err, "write instruction placeholder")
	})
}

func TestApplySkillsChanges_WorkflowAndMemoryRestore(t *testing.T) {
	root := t.TempDir()
	placeholder := filepath.Join(root, ".agent-layer", "instructions", "00_instructions.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(placeholder), 0o750))
	require.NoError(t, os.WriteFile(placeholder, nil, 0o600))

	changes := skillsChangeSet{
		workflowSkillsToAdd:       []string{"review-scope"},
		memoryFilesToAdd:          []string{"docs/agent-layer/ISSUES.md"},
		templateMemoryFilesToAdd:  []string{".agent-layer/templates/docs/ISSUES.md"},
		instructionFilesToAdd:     []string{".agent-layer/instructions/00_rules.md"},
		addInstructionPlaceholder: false,
	}
	require.NoError(t, applySkillsChanges(root, changes))

	assert.FileExists(t, filepath.Join(root, ".agent-layer", "skills", "review-scope", "SKILL.md"))
	assert.FileExists(t, filepath.Join(root, "docs", "agent-layer", "ISSUES.md"))
	assert.FileExists(t, filepath.Join(root, ".agent-layer", "templates", "docs", "ISSUES.md"))
	assert.FileExists(t, filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"))
	info, err := os.Stat(placeholder)
	require.NoError(t, err)
	assert.Equal(t, int64(0), info.Size(), "restore keeps the minimal placeholder alongside standard instructions")
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

func TestListWorkflowSkillDirsFiltersEntries(t *testing.T) {
	root := t.TempDir()
	assert.Nil(t, listWorkflowSkillDirs(root, map[string]struct{}{"review-scope": {}}))

	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(skillsDir, "review-scope"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(skillsDir, "custom-skill"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(skillsDir, "review-plan"), []byte("not a directory"), 0o600))

	got := listWorkflowSkillDirs(root, map[string]struct{}{
		"review-scope": {},
		"review-plan":  {},
	})
	assert.Equal(t, []string{"review-scope"}, got)
}

func TestComputeSkillsChangeSet_PruneReportsUnreadableCanonicalFiles(t *testing.T) {
	t.Run("memory file read error", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "docs", "agent-layer", "ISSUES.md"), 0o750))
		choices := NewChoices()
		choices.EnableAgentLayerTouched = true
		choices.EnableAgentLayer = false

		_, err := computeSkillsChangeSet(root, choices)
		require.Error(t, err)
	})

	t.Run("instruction file read error", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"), 0o750))
		choices := NewChoices()
		choices.EnableAgentLayerTouched = true
		choices.EnableAgentLayer = false

		_, err := computeSkillsChangeSet(root, choices)
		require.Error(t, err)
	})
}

func TestComputeSkillsChangeSet_RestoreReportsBlockedWorkflowSkill(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, ".agent-layer", "skills", "review-scope")
	require.NoError(t, os.MkdirAll(filepath.Dir(blocker), 0o750))
	require.NoError(t, os.WriteFile(blocker, []byte("file blocks workflow restore scan"), 0o600))

	choices := NewChoices()
	choices.EnableAgentLayerTouched = true
	choices.EnableAgentLayer = true

	_, err := computeSkillsChangeSet(root, choices)
	require.Error(t, err)
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

		_, err := listMissingInstructionFiles(root)
		require.Error(t, err)
	})
}

func TestBuildSkillsPreview(t *testing.T) {
	t.Run("empty change set returns empty string", func(t *testing.T) {
		assert.Equal(t, "", buildSkillsPreview(skillsChangeSet{}))
	})

	t.Run("renders adds and removes as directory summary", func(t *testing.T) {
		preview := buildSkillsPreview(skillsChangeSet{
			catalogSkillsToAdd:     []string{"find-docs"},
			catalogSkillsToRemove:  []string{"tavily-web"},
			workflowSkillsToRemove: []string{"review-scope"},
			workflowSkillsToAdd:    []string{"review-plan"},
			memoryFilesToRemove:    []string{"docs/agent-layer/ISSUES.md"},
			memoryFilesToAdd:       []string{"docs/agent-layer/BACKLOG.md"},
			templateMemoryFilesToRemove: []string{
				".agent-layer/templates/docs/ISSUES.md",
			},
			templateMemoryFilesToAdd: []string{
				".agent-layer/templates/docs/BACKLOG.md",
			},
			instructionFilesToRemove: []string{
				".agent-layer/instructions/00_rules.md",
			},
			instructionFilesToAdd: []string{
				".agent-layer/instructions/01_base.md",
			},
			addInstructionPlaceholder: true,
		})
		assert.Contains(t, preview, "+ .agent-layer/skills/find-docs/")
		assert.Contains(t, preview, "- .agent-layer/skills/tavily-web/")
		assert.Contains(t, preview, "review-scope/  (workflow bundle)")
		assert.Contains(t, preview, "review-plan/  (workflow bundle)")
		assert.Contains(t, preview, "docs/agent-layer/ISSUES.md  (memory file)")
		assert.Contains(t, preview, "docs/agent-layer/BACKLOG.md  (memory file)")
		assert.Contains(t, preview, ".agent-layer/templates/docs/ISSUES.md  (memory template)")
		assert.Contains(t, preview, ".agent-layer/templates/docs/BACKLOG.md  (memory template)")
		assert.Contains(t, preview, ".agent-layer/instructions/00_rules.md  (instruction file)")
		assert.Contains(t, preview, ".agent-layer/instructions/01_base.md  (instruction file)")
		assert.Contains(t, preview, ".agent-layer/instructions/00_instructions.md  (minimal placeholder)")
	})
}
