package sync

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

// Run regenerates all configured outputs for the repo.
// Returns any sync-time warnings and an error if sync failed.
func Run(root string) ([]warnings.Warning, error) {
	project, err := config.LoadProjectConfigFS(os.DirFS(root), root)
	if err != nil {
		return nil, err
	}

	return RunWithProject(RealSystem{}, root, project)
}

// RunWithSystemFS loads project config from fsys and runs sync with the provided System.
// sys provides OS operations for sync writers; fsys must be rooted at repo root.
func RunWithSystemFS(sys System, fsys fs.FS, root string) ([]warnings.Warning, error) {
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
func RunWithProject(sys System, root string, project *config.ProjectConfig) ([]warnings.Warning, error) {
	steps := []func() error{
		func() error {
			return WriteInstructionShims(sys, root, project.Instructions)
		},
	}

	if project.Config.Agents.Codex.Enabled != nil && *project.Config.Agents.Codex.Enabled {
		steps = append(steps,
			func() error { return WriteCodexInstructions(sys, root, project.Instructions) },
			func() error { return WriteCodexSkills(sys, root, project.SlashCommands) },
		)
	}

	if project.Config.Agents.VSCode.Enabled != nil && *project.Config.Agents.VSCode.Enabled {
		steps = append(steps,
			func() error { return WriteVSCodePrompts(sys, root, project.SlashCommands) },
			func() error { return WriteVSCodeSettings(sys, root, project) },
			func() error { return WriteVSCodeMCPConfig(sys, root, project) },
			func() error { return WriteVSCodeLaunchers(sys, root) },
		)
	}

	if project.Config.Agents.Antigravity.Enabled != nil && *project.Config.Agents.Antigravity.Enabled {
		steps = append(steps, func() error { return WriteAntigravitySkills(sys, root, project.SlashCommands) })
	}

	if project.Config.Agents.Gemini.Enabled != nil && *project.Config.Agents.Gemini.Enabled {
		steps = append(steps, func() error { return WriteGeminiSettings(sys, root, project) })
	}

	if project.Config.Agents.Claude.Enabled != nil && *project.Config.Agents.Claude.Enabled {
		steps = append(steps,
			func() error { return WriteClaudeSettings(sys, root, project) },
			func() error { return WriteMCPConfig(sys, root, project) },
		)
	}

	if project.Config.Agents.Codex.Enabled != nil && *project.Config.Agents.Codex.Enabled {
		steps = append(steps,
			func() error { return WriteCodexConfig(sys, root, project) },
			func() error { return WriteCodexRules(sys, root, project) },
		)
	}

	if err := runSteps(steps); err != nil {
		return nil, err
	}

	// Collect warnings after successful sync
	return collectWarnings(project)
}

// collectWarnings gathers all sync-time warnings based on the project config.
func collectWarnings(project *config.ProjectConfig) ([]warnings.Warning, error) {
	// Only check instructions size per spec for sync
	return warnings.CheckInstructions(project.Root, project.Config.Warnings.InstructionTokenThreshold)
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
