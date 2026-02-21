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
	Warnings []warnings.Warning
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

// agentEnabled returns true when the given enabled pointer is non-nil and true.
func agentEnabled(enabled *bool) bool {
	return enabled != nil && *enabled
}

// RunWithProject regenerates outputs using an already loaded project config.
// Returns any sync-time warnings and an error if sync failed.
func RunWithProject(sys System, root string, project *config.ProjectConfig) (*Result, error) {
	agents := project.Config.Agents
	steps := []func() error{
		func() error { return updateGitignore(sys, root) },
		func() error {
			return WriteInstructionShims(sys, root, project.Instructions)
		},
	}

	if agentEnabled(agents.Codex.Enabled) {
		steps = append(steps,
			func() error { return WriteCodexInstructions(sys, root, project.Instructions) },
			func() error { return WriteCodexSkills(sys, root, project.SlashCommands) },
		)
	}

	// VS Code block — granular split:
	// WriteVSCodeSettings fires for vscode OR claude-vscode.
	// WriteVSCodeMCPConfig, WriteVSCodePrompts, WriteVSCodeLaunchers fire for vscode only.
	vscodeEnabled := agentEnabled(agents.VSCode.Enabled)
	claudeVSCodeEnabled := agentEnabled(agents.ClaudeVSCode.Enabled)

	if vscodeEnabled || claudeVSCodeEnabled {
		steps = append(steps,
			func() error { return WriteVSCodeSettings(sys, root, project) },
		)
	}
	if vscodeEnabled {
		steps = append(steps,
			func() error { return WriteVSCodePrompts(sys, root, project.SlashCommands) },
			func() error { return WriteVSCodeMCPConfig(sys, root, project) },
			func() error { return launchers.WriteVSCodeLaunchers(sys, root) },
		)
	}

	if agentEnabled(agents.Antigravity.Enabled) {
		steps = append(steps, func() error { return WriteAntigravitySkills(sys, root, project.SlashCommands) })
	}

	if agentEnabled(agents.Gemini.Enabled) {
		steps = append(steps, func() error { return WriteGeminiSettings(sys, root, project) })
	}

	// Claude files (.mcp.json, .claude/settings.json) fire when claude OR claude-vscode enabled.
	claudeEnabled := agentEnabled(agents.Claude.Enabled)
	if claudeEnabled || claudeVSCodeEnabled {
		steps = append(steps,
			func() error { return WriteClaudeSettings(sys, root, project) },
			func() error { return WriteMCPConfig(sys, root, project) },
		)
	}

	if agentEnabled(agents.Codex.Enabled) {
		steps = append(steps,
			func() error { return WriteCodexConfig(sys, root, project) },
			func() error { return WriteCodexRules(sys, root, project) },
		)
	}

	if err := runSteps(steps); err != nil {
		return nil, err
	}

	// Non-fatal post-steps that produce warnings on failure.
	var postWarnings []warnings.Warning
	if agentEnabled(agents.Gemini.Enabled) {
		if w := EnsureGeminiTrustedFolder(sys, root); w != nil {
			postWarnings = append(postWarnings, *w)
		}
	}

	// Collect warnings after successful sync, including post-step warnings
	// so that all warnings pass through noise control.
	ws, err := collectWarnings(project, postWarnings)
	if err != nil {
		return nil, err
	}

	return &Result{
		Warnings: ws,
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

	return warnings.ApplyNoiseControl(collected, project.Config.Warnings.NoiseMode), nil
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
