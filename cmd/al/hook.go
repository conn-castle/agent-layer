package main

import (
	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/chime"
)

var chimeSoundRunner chime.SoundRunner = chime.SystemSoundRunner{}

const commandHook = "hook"

func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    commandHook,
		Short:  "Internal provider hook handlers",
		Hidden: true,
	}
	cmd.AddCommand(newHookChimeCmd())
	return cmd
}

func newHookChimeCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "chime <provider>",
		Short:  "Handle a provider completion chime event",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return chime.Handle(args[0], cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), chimeSoundRunner)
		},
	}
}
