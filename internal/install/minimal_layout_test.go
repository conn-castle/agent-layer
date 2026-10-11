package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
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

	for _, name := range []string{"00_rules.md", "01_memory.md"} {
		_, err := os.Stat(filepath.Join(root, ".agent-layer", "instructions", name))
		assert.True(t, os.IsNotExist(err), "%s should not be seeded under bare init", name)
	}

	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	entries, err := os.ReadDir(skillsDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "skills directory should be empty under bare init")

	for _, name := range []string{"ISSUES.md", "BACKLOG.md", "DECISIONS.md", "COMMANDS.md", "CONTEXT.md"} {
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
	assert.NoFileExists(t, filepath.Join(root, ".agent-layer", "skills", "implement", "SKILL.md"))
	assert.NoFileExists(t, filepath.Join(root, "docs", "agent-layer", "ISSUES.md"))
	assert.FileExists(t, filepath.Join(root, ".agent-layer", "skills", "tavily-web", "SKILL.md"))
}

func TestBuildUpgradePlan_BareLayoutDoesNotPlanWorkflowBundle(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}))

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)

	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/instructions/00_rules.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/skills/implement/SKILL.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, "docs/agent-layer/ISSUES.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateUpdates, "docs/agent-layer/ISSUES.md"))
	// A file absent from a bare layout can only ever be planned as an addition,
	// so asserting against TemplateUpdates here would pass vacuously.
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/instructions/01_memory.md"))
}

func TestBuildUpgradePlan_RulesOnlyDoesNotActivateMemoryOrDevelopmentSkills(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}))
	rulesPath := filepath.Join(root, ".agent-layer", "instructions", "00_rules.md")
	require.NoError(t, os.WriteFile(rulesPath, []byte("custom rules"), 0o600))

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)

	assert.Nil(t, findUpgradeChange(plan.TemplateUpdates, ".agent-layer/instructions/00_rules.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/instructions/01_memory.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/skills/implement/SKILL.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, "docs/agent-layer/ISSUES.md"))
}

func TestBuildUpgradePlan_DevelopmentSkillDoesNotActivateInstructionsOrMemory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}))
	skillPath := filepath.Join(root, ".agent-layer", "skills", "implement", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o750))
	require.NoError(t, os.WriteFile(skillPath, []byte("custom development skill"), 0o600))

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)

	assert.Nil(t, findUpgradeChange(plan.TemplateUpdates, ".agent-layer/skills/implement/SKILL.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/skills/ship-pr/SKILL.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/instructions/00_rules.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, "docs/agent-layer/ISSUES.md"))
}

func TestBuildUpgradePlan_InstalledCatalogSkillIsUpgradeManaged(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}, PinVersion: "1.2.3"}))

	skillPath := filepath.Join(root, ".agent-layer", "skills", "benchmark", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o750))
	require.NoError(t, os.WriteFile(skillPath, []byte("customized catalog skill\n"), 0o600))

	plan, err := BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)

	assert.NotNil(t, findUpgradeChange(plan.TemplateUpdates, ".agent-layer/skills/benchmark/SKILL.md"))
	assert.Nil(t, findUpgradeChange(plan.TemplateAdditions, ".agent-layer/skills/playwright/SKILL.md"))
}

func TestUpgradeEstablishesOfflineInstructionOrderAndRefusesLinkedImports(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, Run(root, Options{System: RealSystem{}}))
	paths := config.DefaultPaths(root)
	raw, err := os.ReadFile(paths.ConfigPath)
	require.NoError(t, err)
	for _, name := range []string{"z.md", "a.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(paths.InstructionsDir, name), []byte("custom "+name+"\r\n"), 0o600))
	}
	_, err = BuildUpgradePlan(root, UpgradePlanOptions{System: RealSystem{}})
	require.NoError(t, err)
	previewRaw, err := os.ReadFile(paths.ConfigPath)
	require.NoError(t, err)
	require.Equal(t, raw, previewRaw, "preview is read-only")
	require.NoError(t, Run(root, Options{Overwrite: true, Prompter: autoApprovePrompter(), System: RealSystem{}}))
	cfg, err := config.LoadConfigLenient(paths.ConfigPath)
	require.NoError(t, err)
	require.Len(t, cfg.Instructions.Local, 2)
	require.Equal(t, "a.md", cfg.Instructions.Local[0].Selectors[0])
	require.Equal(t, 0, *cfg.Instructions.Local[0].Order)
	require.Equal(t, 10, *cfg.Instructions.Local[1].Order)
	body, err := os.ReadFile(filepath.Join(paths.InstructionsDir, "z.md"))
	require.NoError(t, err)
	require.Equal(t, "custom z.md\r\n", string(body))
	external := filepath.Join(t.TempDir(), "rules.md")
	require.NoError(t, os.WriteFile(external, []byte("external bytes\n"), 0o600))
	require.NoError(t, os.MkdirAll(paths.ImportedInstructionsDir, 0o750))
	require.NoError(t, os.Symlink(external, filepath.Join(paths.ImportedInstructionsDir, "rules.md")))
	before, err := os.ReadFile(paths.ConfigPath)
	require.NoError(t, err)
	err = Run(root, Options{Overwrite: true, Prompter: autoApprovePrompter(), System: RealSystem{}})
	require.ErrorContains(t, err, "unlinked")
	after, err := os.ReadFile(paths.ConfigPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
	body, err = os.ReadFile(external) // #nosec G304 -- test-owned external fixture.
	require.NoError(t, err)
	require.Equal(t, "external bytes\n", string(body))
}
