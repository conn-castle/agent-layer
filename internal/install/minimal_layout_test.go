package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallRun_MinimalLayoutSeedsOnlyPlaceholder(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{
		Overwrite:     false,
		MinimalLayout: true,
		System:        RealSystem{},
	}))

	// Placeholder is present and zero-byte.
	placeholder := filepath.Join(root, ".agent-layer", "instructions", MinimalLayoutPlaceholderFile)
	info, err := os.Stat(placeholder)
	require.NoError(t, err, "placeholder should exist after minimal install")
	assert.Equal(t, int64(0), info.Size(), "placeholder is intentionally zero-byte")

	// Standard instruction files are NOT seeded.
	for _, name := range []string{"00_rules.md", "01_base.md", "02_memory.md", "03_tools.md", "04_conventions.md"} {
		_, err := os.Stat(filepath.Join(root, ".agent-layer", "instructions", name))
		assert.True(t, os.IsNotExist(err), "%s should not be seeded under minimal layout", name)
	}

	// Skills directory exists but is empty.
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	entries, err := os.ReadDir(skillsDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "skills directory should be empty under minimal layout")

	// Memory templates are not written.
	for _, name := range []string{"ISSUES.md", "BACKLOG.md", "ROADMAP.md", "DECISIONS.md", "COMMANDS.md", "CONTEXT.md"} {
		_, err := os.Stat(filepath.Join(root, "docs", "agent-layer", name))
		assert.True(t, os.IsNotExist(err), "%s should not be seeded under minimal layout", name)
	}

	// Config and env exist (those are user-owned seed files and still seeded).
	assert.FileExists(t, filepath.Join(root, ".agent-layer", "config.toml"))
	assert.FileExists(t, filepath.Join(root, ".agent-layer", ".env"))
}

func TestInstallRun_UpgradePreservesMinimalLayout(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{
		Overwrite:     false,
		MinimalLayout: true,
		System:        RealSystem{},
	}))

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "tavily-web"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "skills", "tavily-web", "SKILL.md"), []byte("custom"), 0o600))
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(memoryPath), 0o750))
	require.NoError(t, os.WriteFile(memoryPath, []byte("edited memory"), 0o600))
	conventionsPath := filepath.Join(root, ".agent-layer", "instructions", "04_conventions.md")
	require.NoError(t, os.WriteFile(conventionsPath, []byte("custom conventions"), 0o600))

	require.NoError(t, Run(root, Options{
		Overwrite:  true,
		Prompter:   autoApprovePrompter(),
		System:     RealSystem{},
		PinVersion: "0.7.0",
	}))

	assert.FileExists(t, filepath.Join(root, ".agent-layer", "instructions", MinimalLayoutPlaceholderFile))
	assert.NoFileExists(t, filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"))
	assert.NoFileExists(t, filepath.Join(root, ".agent-layer", "skills", "review-scope", "SKILL.md"))
	assert.FileExists(t, memoryPath)
	assert.FileExists(t, conventionsPath)
	assert.FileExists(t, filepath.Join(root, ".agent-layer", "skills", "tavily-web", "SKILL.md"))
}

func TestBuildUpgradePlan_MinimalLayoutDoesNotPlanWorkflowBundle(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{
		Overwrite:     false,
		MinimalLayout: true,
		System:        RealSystem{},
		PinVersion:    "1.2.3",
	}))
	memoryPath := filepath.Join(root, "docs", "agent-layer", "ISSUES.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(memoryPath), 0o750))
	require.NoError(t, os.WriteFile(memoryPath, []byte("edited memory"), 0o600))
	conventionsPath := filepath.Join(root, ".agent-layer", "instructions", "04_conventions.md")
	require.NoError(t, os.WriteFile(conventionsPath, []byte("custom conventions"), 0o600))

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)

	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/instructions/00_rules.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/skills/review-scope/SKILL.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, "docs/agent-layer/ISSUES.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateUpdates, "docs/agent-layer/ISSUES.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateUpdates, ".agent-layer/instructions/04_conventions.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateRemovalsOrOrphans, ".agent-layer/instructions/"+MinimalLayoutPlaceholderFile))
}

func TestBuildUpgradePlan_EditedManagedInstructionExitsMinimalLayout(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{
		Overwrite:     false,
		MinimalLayout: true,
		System:        RealSystem{},
		PinVersion:    "1.2.3",
	}))
	rulesPath := filepath.Join(root, ".agent-layer", "instructions", "00_rules.md")
	require.NoError(t, os.WriteFile(rulesPath, []byte("custom rules"), 0o600))

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)

	assert.NotNil(t, findUpgradeChange(plan.TemplateUpdates, ".agent-layer/instructions/00_rules.md"))
	assert.NotNil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/instructions/01_base.md"))
	assert.NotNil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/skills/review-scope/SKILL.md"))
	assert.NotNil(t, findUpgradeChange(plan.TemplateRemovalsOrOrphans, ".agent-layer/instructions/"+MinimalLayoutPlaceholderFile))
}

func TestBuildUpgradePlan_InstalledCatalogSkillIsUpgradeManaged(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}))

	skillPath := filepath.Join(root, ".agent-layer", "skills", "tavily-web", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o750))
	require.NoError(t, os.WriteFile(skillPath, []byte("customized catalog skill\n"), 0o600))

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)

	assert.NotNil(t, findUpgradeChange(plan.TemplateUpdates, ".agent-layer/skills/tavily-web/SKILL.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/skills/playwright-cli/SKILL.md"))
}
