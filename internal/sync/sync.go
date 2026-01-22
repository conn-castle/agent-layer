package sync

import (
	"fmt"

	"github.com/nicholasjconn/agent-layer/internal/config"
	"github.com/nicholasjconn/agent-layer/internal/warnings"
)

// Run regenerates all configured outputs for the repo.
// Returns any sync-time warnings and an error if sync failed.
func Run(root string) ([]warnings.Warning, error) {
	project, err := config.LoadProjectConfig(root)
	if err != nil {
		return nil, err
	}

	return RunWithProject(root, project)
}

// RunWithProject regenerates outputs using an already loaded project config.
// Returns any sync-time warnings and an error if sync failed.
func RunWithProject(root string, project *config.ProjectConfig) ([]warnings.Warning, error) {
	steps := []func() error{
		func() error {
			return WriteInstructionShims(root, project.Instructions)
		},
	}

	if project.Config.Agents.Codex.Enabled != nil && *project.Config.Agents.Codex.Enabled {
		steps = append(steps,
			func() error { return WriteCodexInstructions(root, project.Instructions) },
			func() error { return WriteCodexSkills(root, project.SlashCommands) },
		)
	}

	if project.Config.Agents.VSCode.Enabled != nil && *project.Config.Agents.VSCode.Enabled {
		steps = append(steps,
			func() error { return WriteVSCodePrompts(root, project.SlashCommands) },
			func() error { return WriteVSCodeSettings(root, project) },
			func() error { return WriteVSCodeMCPConfig(root, project) },
			func() error { return WriteVSCodeLaunchers(root) },
		)
	}

	if project.Config.Agents.Antigravity.Enabled != nil && *project.Config.Agents.Antigravity.Enabled {
		steps = append(steps, func() error { return WriteAntigravitySkills(root, project.SlashCommands) })
	}

	if project.Config.Agents.Gemini.Enabled != nil && *project.Config.Agents.Gemini.Enabled {
		steps = append(steps, func() error { return WriteGeminiSettings(root, project) })
	}

	if project.Config.Agents.Claude.Enabled != nil && *project.Config.Agents.Claude.Enabled {
		steps = append(steps,
			func() error { return WriteClaudeSettings(root, project) },
			func() error { return WriteMCPConfig(root, project) },
		)
	}

	if project.Config.Agents.Codex.Enabled != nil && *project.Config.Agents.Codex.Enabled {
		steps = append(steps,
			func() error { return WriteCodexConfig(root, project) },
			func() error { return WriteCodexRules(root, project) },
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
		return fmt.Errorf("agent %s is missing enabled flag in config", name)
	}
	if !*enabled {
		return fmt.Errorf("agent %s is disabled in config", name)
	}
	return nil
}
