package main

import (
	"fmt"
	"io"

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
		newSkillsPullCmd(),
		newSkillsPushCmd(),
	)
	return cmd
}

func newSkillsAddCmd() *cobra.Command {
	var opts skillimport.AddOptions
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
	return cmd
}

func newSkillsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.SkillsRemoveUse,
		Short: messages.SkillsRemoveShort,
		Long:  messages.SkillsRemoveLong,
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			report, err := skillimport.New(root).Remove(cmd.Context(), args[0], args[1])
			return finishSkillsOperation(cmd, "remove", report, err)
		},
	}
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
	return &cobra.Command{
		Use:   messages.SkillsPushUse,
		Short: messages.SkillsPushShort,
		Long:  messages.SkillsPushLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			report, err := skillimport.New(root).Push(cmd.Context())
			return finishSkillsOperation(cmd, "push", report, err)
		},
	}
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
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), fmt.Sprintf(messages.SkillsOperationFailedFmt, operation))
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
