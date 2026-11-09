package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

// TestUpgradeCmd_ConvertsPristineRulesFromV110 reproduces #522 through real CLI
// parsing: a v1.1.0 project created before v1.0.0 that never adopted keeps an
// unmodified shipped 00_rules.md, and the documented non-interactive flags
// convert it to the rules.md import without the wizard.
func TestUpgradeCmd_ConvertsPristineRulesFromV110(t *testing.T) {
	testutil.CatalogGitFixture(t)
	root := t.TempDir()
	require.NoError(t, install.Run(root, install.Options{System: install.RealSystem{}, PinVersion: "1.1.0"}))
	paths := config.DefaultPaths(root)
	require.NoError(t, os.WriteFile(filepath.Join(paths.InstructionsDir, "00_rules.md"), testutil.TransitionFixture(t, "00_rules.md"), 0o600))
	raw, err := os.ReadFile(paths.ConfigPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(paths.ConfigPath, append(raw, []byte("\n[[instructions.local]]\nselectors = [\"00_rules.md\"]\norder = 0\n")...), 0o600)) // #nosec G703 -- test-owned project config.

	origIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = origIsTerminal })
	originalVersion := Version
	Version = "v1.2.0"
	t.Cleanup(func() { Version = originalVersion })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetArgs([]string{"--yes", "--apply-managed-updates", "--apply-memory-updates", "--apply-deletions"})
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetIn(bytes.NewBufferString(""))
		require.NoError(t, cmd.Execute(), "stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
		require.Contains(t, stdout.String(), "1 unmodified shipped local copies will become Git imports")
		require.Contains(t, stdout.String(), ".agent-layer/instructions/00_rules.md -> instructions/rules.md")
		require.NotContains(t, stdout.String()+stderr.String(), "v1.0.0")
	})
	require.NoFileExists(t, filepath.Join(paths.InstructionsDir, "00_rules.md"))
	require.FileExists(t, filepath.Join(paths.ImportedInstructionsDir, "rules.md"))
	cfg, err := config.LoadConfigFS(os.DirFS(root), root, paths.ConfigPath)
	require.NoError(t, err)
	require.Empty(t, cfg.Instructions.Local)
	require.Len(t, cfg.Instructions.Imports, 1)
	require.Equal(t, 0, *cfg.Instructions.Imports[0].Order)
	pin, err := os.ReadFile(filepath.Join(root, ".agent-layer", "al.version")) // #nosec G304 -- test-owned project file.
	require.NoError(t, err)
	require.Equal(t, "1.2.0", strings.TrimSpace(string(pin)))
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md")) // #nosec G304 -- test-owned project file.
	require.NoError(t, err)
	require.Contains(t, string(agents), "BEGIN: rules.md", "post-upgrade sync projects the imported rules")
}

func TestBuildUpgradePrompter_ConvertPristineCopies(t *testing.T) {
	items := []install.PristineConversion{
		{Kind: install.PristineConversionSkill, Path: ".agent-layer/skills/implement", Name: "implement", Selector: "skills/development/implement"},
		{Kind: install.PristineConversionInstruction, Path: ".agent-layer/instructions/00_rules.md", Name: "rules.md", Selector: "instructions/rules.md", LocalName: "00_rules.md", DeleteOnly: true},
	}
	for _, tc := range []struct {
		name   string
		policy upgradeApplyPolicy
		input  string
		want   bool
		prompt bool
	}{
		{"interactive default yes", upgradeApplyPolicy{interactive: true}, "\n", true, true},
		{"interactive decline", upgradeApplyPolicy{interactive: true}, "n\n", false, true},
		{"yes with apply flags", upgradeApplyPolicy{yes: true, explicitCategory: true, applyManaged: true}, "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newUpgradeCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetIn(bytes.NewBufferString(tc.input))
			got, err := buildUpgradePrompter(cmd, tc.policy).ConvertPristineCopiesFunc(items)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Contains(t, out.String(), ".agent-layer/skills/implement -> skills/development/implement")
			require.Contains(t, out.String(), ".agent-layer/instructions/00_rules.md (already imported from instructions/rules.md")
			require.Equal(t, tc.prompt, strings.Contains(out.String(), "Convert 2 unmodified shipped copies to Git imports?"))
		})
	}
}

func TestRenderUpgradePlanListsPristineConversions(t *testing.T) {
	var out bytes.Buffer
	plan := install.UpgradePlan{PristineConversions: []install.PristineConversion{
		{Kind: install.PristineConversionInstruction, Path: ".agent-layer/instructions/01_memory.md", Selector: "instructions/memory.md"},
	}}
	require.NoError(t, renderUpgradePlanText(&out, plan, nil))
	require.Contains(t, out.String(), "requires network access to github.com/nicholasjconn/skills")
	require.Contains(t, out.String(), ".agent-layer/instructions/01_memory.md -> instructions/memory.md")
}

// TestUpgradeCmd_RefusesUnreferencedPristineRules runs the real CLI on a
// project whose unmodified shipped 00_rules.md has no [[instructions.local]]
// block. Sync would refuse it, so upgrade refuses before advancing the pin.
func TestUpgradeCmd_RefusesUnreferencedPristineRules(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, install.Run(root, install.Options{System: install.RealSystem{}, PinVersion: "1.1.0"}))
	paths := config.DefaultPaths(root)
	rules := filepath.Join(paths.InstructionsDir, "00_rules.md")
	require.NoError(t, os.WriteFile(rules, testutil.TransitionFixture(t, "00_rules.md"), 0o600)) // #nosec G703 -- test-owned project file.
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "sync.lock"), nil, 0o600))
	before := testutil.SnapshotEvidence(t, root)

	origIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = origIsTerminal })
	originalVersion := Version
	Version = "v1.2.0"
	t.Cleanup(func() { Version = originalVersion })

	testutil.WithWorkingDir(t, root, func() {
		cmd := newUpgradeCmd()
		var stdout, stderr bytes.Buffer
		cmd.SetArgs([]string{"--yes", "--apply-managed-updates", "--apply-memory-updates", "--apply-deletions"})
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetIn(bytes.NewBufferString(""))
		err := cmd.Execute()
		require.Error(t, err)
		require.Contains(t, err.Error(), "unconfigured local instruction "+rules)
		require.Contains(t, err.Error(), "declare it in .agent-layer/config.toml with [[instructions.local]]")
		require.NotContains(t, err.Error()+stdout.String()+stderr.String(), "v1.0.0")
	})
	require.Equal(t, before, testutil.SnapshotEvidence(t, root))
}
