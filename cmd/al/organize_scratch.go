package main

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/organizescratch"
)

const organizeScratchCommandName = "organize-scratch"

var runOrganizeScratch = organizescratch.Run

// newOrganizeScratchCmd wires the scratch organizer. It is hidden rather than
// absent: the command is a maintenance aid for agents tidying a scratch
// directory, not part of the documented surface, and hiding it keeps `al --help`
// focused on the workflow commands.
func newOrganizeScratchCmd() *cobra.Command {
	options := organizescratch.Options{}
	var keep []string
	command := &cobra.Command{
		Use:   organizeScratchCommandName + " --root <dir>",
		Short: "Sort a scratch directory into a reviewable structure",
		Long: "Sort a scratch directory into reports/, artifacts/, and review/ folders.\n\n" +
			"Nothing is ever deleted, overwritten, or merged: entries are only moved, an\n" +
			"entry whose destination is already taken is left in place, and every removal\n" +
			"decision is left to a human via ORGANIZE-REVIEW.md written into the root.",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(options.Root) == "" {
				return errors.New(organizeScratchCommandName + " requires --root")
			}
			options.Keep = splitKeepNames(keep)
			return runOrganizeScratch(cmd.Context(), options, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	command.Flags().StringVar(&options.Root, "root", "", "directory to organize (required)")
	command.Flags().BoolVar(&options.Apply, "apply", false, "perform the moves instead of only printing the plan")
	command.Flags().StringArrayVar(&keep, "keep", nil,
		"comma-separated top-level names to leave in place; repeatable")
	command.Flags().BoolVar(&options.MoveWorktrees, "move-worktrees", false,
		"also relocate registered git worktrees and repair their registration")
	command.Flags().IntVar(&options.MinGroup, "min-group", organizescratch.DefaultMinGroup,
		"entries a filename prefix needs before it earns its own reports folder")
	return command
}

// splitKeepNames turns every --keep occurrence into names. Values accumulate
// across occurrences rather than the last one winning, so a caller can name a
// path to protect without having to know what earlier flags already listed.
// Empty fields, which trailing or doubled commas produce, are dropped.
func splitKeepNames(values []string) []string {
	var names []string
	for _, value := range values {
		for _, name := range strings.Split(value, ",") {
			if trimmed := strings.TrimSpace(name); trimmed != "" {
				names = append(names, trimmed)
			}
		}
	}
	return names
}
