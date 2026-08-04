package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skillimports"
	"github.com/conn-castle/agent-layer/internal/sync"
)

const skillsCommandName = "skills"

// projectSkillSources regenerates every client projection from the current local
// skill sources. Import commands call it after they publish source state, so a
// successful import is visible to agents without a separate `al sync`.
var projectSkillSources skillimports.Projector = func(root string) error {
	_, err := sync.Run(root)
	return err
}

// newSkillsCmd wires the skill import commands. `add`, `remove`, `pull`, and
// `push` are the only commands that contact a skill remote; `status` and
// ordinary `al sync` stay local and network-free.
func newSkillsCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   skillsCommandName,
		Short: "Import Agent Skills from Git repositories and reconcile local edits",
		Long: "Manage skills imported from Git repositories into .agent-layer/imported-skills/.\n\n" +
			"Imported skills are ordinary editable sources: edit them in place, pull upstream\n" +
			"changes with 'al skills pull', and contribute changes back with 'al skills push'\n" +
			"when the import is configured to write. Ordinary 'al sync' never contacts a skill\n" +
			"remote.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	command.AddCommand(
		newSkillsAddCmd(),
		newSkillsRemoveCmd(),
		newSkillsStatusCmd(),
		newSkillsPullCmd(),
		newSkillsPushCmd(),
	)
	return command
}

func newSkillsAddCmd() *cobra.Command {
	options := skillimports.AddOptions{}
	command := &cobra.Command{
		Use:   "add <repository> <selector>...",
		Short: "Import one or more skills from a Git repository",
		Long: "Import skills from a Git repository into .agent-layer/imported-skills/.\n\n" +
			"A selector is a repository-relative path. It may be an exact path, a wildcard\n" +
			"(* stays inside one path segment, ** crosses segments), or an exclusion written\n" +
			"with a leading '!'. Quote exclusions so the shell does not expand them.\n\n" +
			"Examples:\n" +
			"  al skills add https://github.com/example/skills skills/code-review\n" +
			"  al skills add https://github.com/example/skills 'skills/*' '!skills/internal'\n" +
			"  al skills add https://github.com/example/skills skills/deploy --ref v1.4.0\n" +
			"  al skills add https://github.com/example/skills 'skills/*' \\\n" +
			"      --write branch --push-repository https://github.com/me/skills --push-branch skill-updates\n\n" +
			"This command never searches, recommends, or previews skills.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			options.Repository = args[0]
			options.Selectors = args[1:]
			service := skillimports.New(root, projectSkillSources, reportWriter(cmd))
			return service.Add(cmd.Context(), options)
		},
	}
	command.Flags().StringVar(&options.Ref, "ref", "",
		"branch, tag, or full commit id to import from (default: the repository's default branch)")
	command.Flags().StringVar(&options.Tracking, "tracking", "",
		fmt.Sprintf("%q to advance on pull or %q to stay at the locked commit (default: branches track, tags and commits pin)",
			config.SkillTrackingTracked, config.SkillTrackingPinned))
	command.Flags().StringVar(&options.Write, "write", "",
		fmt.Sprintf("upstream write policy: %q, %q, or %q (default: %q)",
			config.SkillWriteNone, config.SkillWriteBranch, config.SkillWriteDirect, config.DefaultSkillWrite))
	command.Flags().StringVar(&options.PushRepository, "push-repository", "",
		"repository to write to (default: the source repository); set it to contribute through a fork")
	command.Flags().StringVar(&options.PushBranch, "push-branch", "",
		fmt.Sprintf("destination branch, required when --write %s", config.SkillWriteBranch))
	return command
}

func newSkillsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <repository> <selector>",
		Short: "Remove one configured selector and retire the skills it owned",
		Long: "Remove exactly one configured selector, then recompute the desired set.\n\n" +
			"Skills still matched by another selector stay managed. A retired skill whose\n" +
			"local content is unmodified is deleted; a modified one is preserved and reported\n" +
			"so you can adopt or delete it deliberately. Quote an exclusion selector so the\n" +
			"shell does not expand it.\n\n" +
			"Examples:\n" +
			"  al skills remove https://github.com/example/skills skills/code-review\n" +
			"  al skills remove https://github.com/example/skills '!skills/internal'",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			service := skillimports.New(root, projectSkillSources, reportWriter(cmd))
			return service.Remove(cmd.Context(), args[0], args[1])
		},
	}
}

func newSkillsStatusCmd() *cobra.Command {
	var all bool
	command := &cobra.Command{
		Use:   "status",
		Short: "Show local skill import state without contacting any remote",
		Long: "Report the local state of every imported skill.\n\n" +
			"This command is read-only and never contacts a remote. Clean and modified are\n" +
			"ordinary states; a missing, invalid, or colliding skill, an unmanaged directory\n" +
			"in the imported-skills root, or a malformed lock exits nonzero.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			service := skillimports.New(root, nil, nil)
			view, err := service.Status()
			if err != nil {
				return err
			}
			if !skillsQuiet(cmd) {
				skillimports.WriteStatus(cmd.OutOrStdout(), view, all)
			}
			if err := skillimports.StatusError(view); err != nil {
				if skillsQuiet(cmd) {
					return &SilentExitError{Code: 1}
				}
				return err
			}
			return nil
		},
	}
	command.Flags().BoolVar(&all, "all", false, "list every resolved skill and configured exclusion")
	return command
}

func newSkillsPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Fetch configured sources, reconcile local edits, and project the results",
		Long: "Fetch every configured source, reconcile it with local content, update lock\n" +
			"state, and project successful results into the clients.\n\n" +
			"This is the only command that advances tracked imports. Pinned imports stay at\n" +
			"their locked commits. It never commits or pushes upstream. A source failure\n" +
			"blocks only its own import block, and a skill failure blocks only that skill;\n" +
			"everything that succeeded is still projected and the command exits nonzero.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			service := skillimports.New(root, projectSkillSources, reportWriter(cmd))
			return service.Pull(cmd.Context())
		},
	}
}

func newSkillsPushCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "push",
		Short: "Write local changes to imported skills back upstream",
		Long: "Write local changes to imported skills back to their configured destinations.\n\n" +
			"Only imports configured with a write policy are pushed, using the current\n" +
			"contents of .agent-layer/imported-skills/ whether or not they are committed in\n" +
			"this project. This command never pulls first and never force-pushes. A tracked\n" +
			"import whose source ref has moved is refused until 'al skills pull' runs.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			service := skillimports.New(root, projectSkillSources, reportWriter(cmd))
			return service.Push(cmd.Context())
		},
	}
}

// reportWriter returns the sink for a command's human-readable report, honoring
// the global quiet flag. Quiet suppresses the narrative, never the exit status.
func reportWriter(cmd *cobra.Command) io.Writer {
	if skillsQuiet(cmd) {
		return io.Discard
	}
	return cmd.OutOrStdout()
}

// skillsQuiet reports whether the global --quiet flag is set for this command.
func skillsQuiet(cmd *cobra.Command) bool {
	quiet, err := cmd.Flags().GetBool("quiet")
	if err != nil {
		return false
	}
	return quiet
}
