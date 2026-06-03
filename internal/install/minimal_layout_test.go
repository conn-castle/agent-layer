package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallRun_BareInitSeedsOnlyOperationalScaffolding(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}}))

	for _, dir := range []string{
		filepath.Join(root, ".agent-layer", "instructions"),
		filepath.Join(root, ".agent-layer", "skills"),
		filepath.Join(root, ".agent-layer", "tmp", "runs"),
	} {
		info, err := os.Stat(dir)
		require.NoError(t, err, "%s should exist after bare init", dir)
		assert.True(t, info.IsDir(), "%s should be a directory", dir)
	}

	for _, name := range []string{"00_rules.md", "01_base.md", "02_memory.md", "03_tools.md", "04_conventions.md"} {
		_, err := os.Stat(filepath.Join(root, ".agent-layer", "instructions", name))
		assert.True(t, os.IsNotExist(err), "%s should not be seeded under bare init", name)
	}

	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	entries, err := os.ReadDir(skillsDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "skills directory should be empty under bare init")

	for _, name := range []string{"ISSUES.md", "BACKLOG.md", "ROADMAP.md", "DECISIONS.md", "COMMANDS.md", "CONTEXT.md"} {
		_, err := os.Stat(filepath.Join(root, "docs", "agent-layer", name))
		assert.True(t, os.IsNotExist(err), "%s should not be seeded under bare init", name)
		_, err = os.Stat(filepath.Join(root, ".agent-layer", "templates", "docs", name))
		assert.True(t, os.IsNotExist(err), "%s template should not be seeded under bare init", name)
	}

	assert.FileExists(t, filepath.Join(root, ".agent-layer", "config.toml"))
	assert.FileExists(t, filepath.Join(root, ".agent-layer", ".env"))
	assert.FileExists(t, filepath.Join(root, ".agent-layer", "commands.allow"))
	assert.FileExists(t, filepath.Join(root, ".agent-layer", "gitignore.block"))
	assert.NoFileExists(t, filepath.Join(root, ".agent-layer", "claude-statusline.sh"))
	assert.NoFileExists(t, filepath.Join(root, ".agent-layer", "codex-statusline.toml"))
}

func TestInstallRun_UpgradePreservesBareLayoutWithoutWorkflowEvidence(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}}))

	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "tavily-web"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "skills", "tavily-web", "SKILL.md"), []byte("custom"), 0o600))

	require.NoError(t, Run(root, Options{
		Overwrite:  true,
		Prompter:   autoApprovePrompter(),
		System:     RealSystem{},
		PinVersion: "0.7.0",
	}))

	assert.NoFileExists(t, filepath.Join(root, ".agent-layer", "instructions", "00_rules.md"))
	assert.NoFileExists(t, filepath.Join(root, ".agent-layer", "skills", "review-scope", "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(root, "docs", "agent-layer", "ISSUES.md"))
	assert.FileExists(t, filepath.Join(root, ".agent-layer", "skills", "tavily-web", "SKILL.md"))
}

func TestBuildUpgradePlan_BareLayoutDoesNotPlanWorkflowBundle(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}))

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)

	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/instructions/00_rules.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/skills/review-scope/SKILL.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, "docs/agent-layer/ISSUES.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateUpdates, "docs/agent-layer/ISSUES.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateUpdates, ".agent-layer/instructions/04_conventions.md"))
}

func TestBuildUpgradePlan_ManagedInstructionEvidenceIncludesWorkflowBundle(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}))
	rulesPath := filepath.Join(root, ".agent-layer", "instructions", "00_rules.md")
	require.NoError(t, os.WriteFile(rulesPath, []byte("custom rules"), 0o600))

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)

	assert.NotNil(t, findUpgradeChange(plan.TemplateUpdates, ".agent-layer/instructions/00_rules.md"))
	assert.NotNil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/instructions/01_base.md"))
	assert.NotNil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/skills/review-scope/SKILL.md"))
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
