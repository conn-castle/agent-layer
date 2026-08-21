package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skillimport"
)

// newSkillsCmd builds the `al skills` command family.
func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   messages.SkillsUse,
		Short: messages.SkillsShort,
		Long:  messages.SkillsLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(
		newSkillsAddCmd(),
		newSkillsRemoveCmd(),
		newSkillsStatusCmd(),
		newSkillsDiffCmd(),
		newSkillsPullCmd(),
		newSkillsResetCmd(),
		newSkillsResolveCmd(),
		newSkillsPushCmd(),
	)
	return cmd
}

func newSkillsDiffCmd() *cobra.Command {
	var from string
	var to string
	cmd := &cobra.Command{
		Use:   messages.SkillsDiffUse,
		Short: messages.SkillsDiffShort,
		Long:  messages.SkillsDiffLong,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			output, err := skillimport.New(root).Diff(cmd.Context(), args[0], from, to)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(output)
			return err
		},
	}
	cmd.Flags().StringVar(&from, "from", "local", messages.SkillsDiffFromFlag)
	cmd.Flags().StringVar(&to, "to", "upstream", messages.SkillsDiffToFlag)
	return cmd
}

func newSkillsResolveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.SkillsResolveUse,
		Short: messages.SkillsResolveShort,
		Long:  messages.SkillsResolveLong,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			report, err := skillimport.New(root).Resolve(cmd.Context(), args[0])
			return finishSkillsOperation(cmd, "resolve", report, err)
		},
	}
}

func newSkillsResetCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   messages.SkillsResetUse,
		Short: messages.SkillsResetShort,
		Long:  messages.SkillsResetLong,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if err := confirmSkillsMutation(cmd, "reset", fmt.Sprintf(messages.SkillsResetConfirmFmt, args[0]), yes); err != nil {
				return err
			}
			report, err := skillimport.New(root).Reset(cmd.Context(), args[0])
			return finishSkillsOperation(cmd, "reset", report, err)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, messages.SkillsYesFlag)
	return cmd
}

func newSkillsAddCmd() *cobra.Command {
	var opts skillimport.AddOptions
	var yes bool
	cmd := &cobra.Command{
		Use:   messages.SkillsAddUse,
		Short: messages.SkillsAddShort,
		Long:  messages.SkillsAddLong,
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if err := confirmSkillsMutation(cmd, "add", fmt.Sprintf(messages.SkillsAddConfirmFmt, strings.Join(args[1:], ", "), args[0]), yes); err != nil {
				return err
			}
			opts.Repository = args[0]
			opts.Selectors = args[1:]
			report, err := skillimport.New(root).Add(cmd.Context(), opts)
			return finishSkillsOperation(cmd, "add", report, err)
		},
	}
	cmd.Flags().StringVar(&opts.Ref, "ref", "", messages.SkillsRefFlag)
	cmd.Flags().StringVar(&opts.Tracking, "tracking", "", messages.SkillsTrackingFlag)
	cmd.Flags().StringVar(&opts.WritePolicy, "write", "", messages.SkillsWriteFlag)
	cmd.Flags().StringVar(&opts.PushRepository, "push-repository", "", messages.SkillsPushRepositoryFlag)
	cmd.Flags().StringVar(&opts.PushBranch, "push-branch", "", messages.SkillsPushBranchFlag)
	cmd.Flags().BoolVar(&yes, "yes", false, messages.SkillsYesFlag)
	return cmd
}

func newSkillsRemoveCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   messages.SkillsRemoveUse,
		Short: messages.SkillsRemoveShort,
		Long:  messages.SkillsRemoveLong,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if err := confirmSkillsMutation(cmd, "remove", fmt.Sprintf(messages.SkillsRemoveConfirmFmt, args[1], args[0]), yes); err != nil {
				return err
			}
			report, err := skillimport.New(root).Remove(cmd.Context(), args[0], args[1])
			return finishSkillsOperation(cmd, "remove", report, err)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, messages.SkillsYesFlag)
	return cmd
}

func newSkillsStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   messages.SkillsStatusUse,
		Short: messages.SkillsStatusShort,
		Long:  messages.SkillsStatusLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			all, _ := cmd.Flags().GetBool("all")
			status, err := skillimport.New(root).Status()
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.OutOrStdout(), status.Render(all))
			return err
		},
	}
	cmd.Flags().Bool("all", false, messages.SkillsAllFlag)
	return cmd
}

func newSkillsPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.SkillsPullUse,
		Short: messages.SkillsPullShort,
		Long:  messages.SkillsPullLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			report, err := skillimport.New(root).Pull(cmd.Context())
			return finishSkillsOperation(cmd, "pull", report, err)
		},
	}
}

func newSkillsPushCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   messages.SkillsPushUse,
		Short: messages.SkillsPushShort,
		Long:  messages.SkillsPushLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if err := confirmSkillsMutation(cmd, "push", messages.SkillsPushConfirm, yes); err != nil {
				return err
			}
			report, err := skillimport.New(root).Push(cmd.Context())
			return finishSkillsOperation(cmd, "push", report, err)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, messages.SkillsYesFlag)
	return cmd
}

// confirmSkillsMutation protects persistent configuration changes, destructive
// imported-skill replacement, and remote publication. Interactive calls use a
// default-no prompt; automation must opt in explicitly with --yes.
func confirmSkillsMutation(cmd *cobra.Command, operation string, prompt string, yes bool) error {
	if yes {
		return nil
	}
	if !isTerminal() {
		return fmt.Errorf(messages.SkillsNonInteractiveRequiresYesFmt, operation)
	}
	confirmed, err := promptYesNo(cmd.InOrStdin(), cmd.OutOrStdout(), prompt, false)
	if err != nil {
		return fmt.Errorf("read al skills %s confirmation: %w", operation, err)
	}
	if !confirmed {
		return fmt.Errorf(messages.SkillsConfirmationDeclinedFmt, operation)
	}
	return nil
}

// finishSkillsOperation writes the deterministic report and converts partial or
// total failure into a nonzero exit.
//
// A failure that prevented the operation from starting is returned as an
// ordinary error. Once per-skill results exist, the report is printed first so
// the user sees every outcome, and the command exits nonzero without repeating
// the report as an error message.
func finishSkillsOperation(cmd *cobra.Command, operation string, report *skillimport.Report, err error) error {
	if report != nil && (len(report.Skills) > 0 || len(report.Sources) > 0 || report.ProjectionErr != nil) {
		if _, writeErr := io.WriteString(cmd.OutOrStdout(), report.Render("al skills "+operation)); writeErr != nil {
			return writeErr
		}
		if err != nil {
			return err
		}
		if report.Failed() {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), messages.SkillsOperationFailedFmt+"\n", operation)
			return &SilentExitError{Code: 1}
		}
		return nil
	}
	if err != nil {
		return err
	}
	if report != nil {
		_, writeErr := io.WriteString(cmd.OutOrStdout(), report.Render("al skills "+operation))
		return writeErr
	}
	return nil
}
