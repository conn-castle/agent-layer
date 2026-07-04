package sync

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/launchers"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

// Result holds the outcome of a sync operation.
type Result struct {
	Warnings    []warnings.Warning
	AllWarnings []warnings.Warning
}

// Run regenerates all configured outputs for the repo.
// Returns any sync-time warnings and an error if sync failed.
func Run(root string) (*Result, error) {
	project, err := config.LoadProjectConfigFS(os.DirFS(root), root)
	if err != nil {
		return nil, err
	}

	return RunWithProject(RealSystem{}, root, project)
}

// RunWithSystemFS loads project config from fsys and runs sync with the provided System.
// sys provides OS operations for sync writers; fsys must be rooted at repo root.
func RunWithSystemFS(sys System, fsys fs.FS, root string) (*Result, error) {
	if sys == nil {
		return nil, fmt.Errorf(messages.SyncSystemRequired)
	}
	if fsys == nil {
		return nil, fmt.Errorf(messages.SyncConfigFSRequired)
	}
	project, err := config.LoadProjectConfigFS(fsys, root)
	if err != nil {
		return nil, err
	}
	return RunWithProject(sys, root, project)
}

// RunWithProject regenerates outputs using an already loaded project config.
// Returns any sync-time warnings and an error if sync failed.
func RunWithProject(sys System, root string, project *config.ProjectConfig) (*Result, error) {
	return withProjectSyncLock(root, func() (*Result, error) {
		return runWithProjectLocked(sys, root, project)
	})
}

func runWithProjectLocked(sys System, root string, project *config.ProjectConfig) (*Result, error) {
	agents := project.Config.Agents
	steps := []func() error{
		func() error { return updateGitignore(sys, root) },
		func() error {
			return WriteInstructionShims(sys, root, project.Instructions)
		},
		func() error { return CleanCodexInstructions(sys, root) },
		func() error { return CleanLegacySkillOutputs(sys, root) },
	}

	if config.SharedAgentSkillsEnabled(agents) {
		steps = append(steps, func() error { return WriteAgentSkills(sys, root, project.Skills) })
	} else {
		steps = append(steps, func() error { return CleanSharedAgentSkills(sys, root) })
	}

	// VS Code block — granular split:
	// WriteVSCodeSettings fires for vscode OR claude_vscode.
	// WriteVSCodeMCPConfig and WriteVSCodeLaunchers fire for vscode only.
	vscodeEnabled := config.IsAgentEnabled(agents.VSCode.Enabled)
	claudeVSCodeEnabled := config.IsAgentEnabled(agents.ClaudeVSCode.Enabled)

	if vscodeEnabled || claudeVSCodeEnabled {
		steps = append(steps,
			func() error { return WriteVSCodeSettings(sys, root, project) },
		)
	}
	if vscodeEnabled {
		steps = append(steps,
			func() error { return WriteVSCodeMCPConfig(sys, root, project) },
			func() error { return launchers.WriteVSCodeLaunchers(sys, root) },
		)
	}

	if config.IsAgentEnabled(agents.CopilotCLI.Enabled) {
		steps = append(steps,
			func() error { return WriteCopilotMCPConfig(sys, root, project) },
		)
	} else {
		steps = append(steps, func() error { return CleanCopilotOutputs(sys, root) })
	}

	if config.IsAgentEnabled(agents.Antigravity.Enabled) {
		steps = append(steps,
			func() error { return WriteAntigravitySettings(sys, root, project) },
			func() error { return WriteAntigravityMCPConfig(sys, root, project) },
			func() error { return WriteAntigravityChimePlugin(sys, root, project) },
		)
	} else {
		steps = append(steps,
			func() error { return CleanAntigravityOutputs(sys, root) },
			func() error { return CleanAntigravityChimePlugin(sys, root) },
		)
	}

	// Claude files (.mcp.json, .claude/settings.json, .claude/skills/) fire when claude OR claude_vscode enabled.
	claudeEnabled := config.IsAgentEnabled(agents.Claude.Enabled)
	if claudeEnabled || claudeVSCodeEnabled {
		steps = append(steps,
			func() error { return WriteClaudeStatusline(sys, root, project) },
			func() error { return WriteClaudeSettings(sys, root, project) },
			func() error { return WriteMCPConfig(sys, root, project) },
			func() error { return WriteClaudeSkills(sys, root, project.Skills) },
		)
	} else {
		steps = append(steps, func() error { return CleanClaudeChimeHook(sys, root) })
	}

	if config.IsAgentEnabled(agents.Codex.Enabled) {
		steps = append(steps,
			func() error { return WriteCodexConfig(sys, root, project) },
			func() error { return WriteCodexRules(sys, root, project) },
		)
	} else {
		steps = append(steps, func() error { return CleanCodexChimeHook(sys, root) })
	}

	if err := runSteps(steps); err != nil {
		return nil, err
	}

	// Collect warnings after successful sync, including post-step warnings
	// so that all warnings pass through noise control.
	rawWarnings, err := collectWarnings(project, nil)
	if err != nil {
		return nil, err
	}
	filteredWarnings := warnings.ApplyNoiseControl(rawWarnings, project.Config.Warnings.NoiseMode)

	return &Result{
		Warnings:    filteredWarnings,
		AllWarnings: rawWarnings,
	}, nil
}

// collectWarnings gathers all sync-time warnings based on the project config.
// extra warnings (e.g. from post-steps) are included before noise control is applied.
func collectWarnings(project *config.ProjectConfig, extra []warnings.Warning) ([]warnings.Warning, error) {
	// Instructions size checks run after sync generation.
	instructionWarnings, err := warnings.CheckInstructions(project.Root, project.Config.Warnings.InstructionTokenThreshold)
	if err != nil {
		return nil, err
	}
	policyWarnings := warnings.CheckPolicy(project)
	collected := make([]warnings.Warning, 0, len(extra)+len(instructionWarnings)+len(policyWarnings))
	collected = append(collected, extra...)
	collected = append(collected, instructionWarnings...)
	collected = append(collected, policyWarnings...)

	return collected, nil
}

func runSteps(steps []func() error) error {
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

// EnsureEnabled is a helper for command handlers.
func EnsureEnabled(name string, enabled *bool) error {
	if enabled == nil {
		return fmt.Errorf(messages.SyncAgentEnabledFlagMissingFmt, name)
	}
	if !*enabled {
		return fmt.Errorf(messages.SyncAgentDisabledFmt, name)
	}
	return nil
}

// updateGitignore reads the gitignore block and ensures .gitignore is updated.
func updateGitignore(sys System, root string) error {
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	blockBytes, err := sys.ReadFile(blockPath)
	if err != nil {
		return fmt.Errorf(messages.SyncFailedReadGitignoreBlockFmt, blockPath, err)
	}
	block, err := install.ValidateGitignoreBlock(string(blockBytes), blockPath)
	if err != nil {
		return err
	}
	return install.EnsureGitignore(sys, filepath.Join(root, ".gitignore"), block)
}
