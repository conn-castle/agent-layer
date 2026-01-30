package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/vscode"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
	syncer "github.com/conn-castle/agent-layer/internal/sync"
)

func newVSCodeCmd() *cobra.Command {
	var noSync bool
	cmd := &cobra.Command{
		Use:   messages.VSCodeUse,
		Short: messages.VSCodeShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if noSync {
				return runVSCodeNoSync(root)
			}
			return clients.Run(root, "vscode", func(cfg *config.Config) *bool {
				return cfg.Agents.VSCode.Enabled
			}, vscode.Launch)
		},
	}

	cmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip sync before launching VS Code")

	return cmd
}

// runVSCodeNoSync loads project config and launches VS Code without running sync.
// root is the repo root; returns any load, validation, or launch error.
func runVSCodeNoSync(root string) error {
	project, err := config.LoadProjectConfig(root)
	if err != nil {
		return err
	}
	if err := syncer.EnsureEnabled("vscode", project.Config.Agents.VSCode.Enabled); err != nil {
		return err
	}
	runInfo, err := run.Create(root)
	if err != nil {
		return err
	}
	env := clients.BuildEnv(os.Environ(), project.Env, runInfo)
	return vscode.Launch(project, runInfo, env)
}
