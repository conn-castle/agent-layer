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
		Long: "Sort the top level of a scratch directory into reports/, artifacts/, and\n" +
			"review/ folders. Nothing is deleted, overwritten, or merged. Anything that may\n" +
			"be unsafe to remove unexamined stays under review/ or is left in place.\n\n" +
			"The default dry run is strictly read-only: it creates no directories or review\n" +
			"document, predicts existing destination collisions (including dangling links),\n" +
			"and prints the complete proposed review list. --apply performs moves and writes\n" +
			"ORGANIZE-REVIEW.md from actual outcomes. Predicted dry-run collisions and actual\n" +
			"collisions return non-zero, as do move or worktree repair failures.\n\n" +
			"Roots outside Git repositories and untracked directories inside repositories are\n" +
			"supported. A root containing tracked content is refused. Directories over 100\n" +
			"files or 250 MiB, and files over 250 MiB, always require review. Registered main\n" +
			"and linked worktrees stay in place unless --move-worktrees is explicit.\n" +
			"Symlinks are never rewritten; top-level and move-breaking links require review,\n" +
			"including links under caller-provided --keep entries.",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Assign the trimmed value, not just validate it: `--root "  /tmp/x  "`
			// would otherwise pass this check and then fail as an unreadable path.
			options.Root = strings.TrimSpace(options.Root)
			if options.Root == "" {
				return errors.New(organizeScratchCommandName + " requires --root")
			}
			options.Keep = splitKeepNames(keep)
			return runOrganizeScratch(cmd.Context(), options, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	command.Flags().StringVar(&options.Root, "root", "", "directory to organize (required)")
	command.Flags().BoolVar(&options.Apply, "apply", false, "perform moves and write the outcome review document")
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
