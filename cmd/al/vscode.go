package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/clients/vscode"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newVSCodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                messages.VSCodeUse,
		Short:              messages.VSCodeShort,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			noSync, passArgs, err := splitVSCodeArgs(args)
			if err != nil {
				return err
			}
			if noSync {
				return runVSCodeNoSync(root, passArgs)
			}
			return clients.Run(cmd.Context(), root, "vscode", func(cfg *config.Config) *bool {
				return cfg.Agents.VSCode.Enabled
			}, vscode.Launch, passArgs, Version)
		},
	}

	cmd.Flags().Bool("no-sync", false, "Skip sync before launching VS Code")

	return cmd
}

// runVSCodeNoSync loads project config and launches VS Code without running sync.
// root is the repo root; returns any load, validation, or launch error.
func runVSCodeNoSync(root string, args []string) error {
	return clients.RunNoSync(root, "vscode", func(cfg *config.Config) *bool {
		return cfg.Agents.VSCode.Enabled
	}, vscode.Launch, args)
}

// splitVSCodeArgs manually parses --no-sync because flag parsing is disabled for pass-through.
func splitVSCodeArgs(args []string) (bool, []string, error) {
	noSync := false
	passArgs := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			passArgs = append(passArgs, args[i+1:]...)
			break
		}
		if arg == "--no-sync" {
			noSync = true
			continue
		}
		if strings.HasPrefix(arg, "--no-sync=") {
			value := strings.TrimPrefix(arg, "--no-sync=")
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return false, nil, fmt.Errorf(messages.VSCodeNoSyncInvalidFmt, value)
			}
			noSync = parsed
			continue
		}
		passArgs = append(passArgs, arg)
	}
	return noSync, passArgs, nil
}
