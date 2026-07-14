package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func newDispatchCmd() *cobra.Command {
	var agent string
	var model string
	var reasoningEffort string
	var skill string
	cmd := &cobra.Command{
		Use:          messages.DispatchUse,
		Short:        messages.DispatchShort,
		Long:         messages.DispatchLong,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, workingDir, err := resolveRepoRootAndWorkingDir()
			if err != nil {
				return err
			}
			quiet, _ := cmd.Flags().GetBool("quiet")
			return dispatchCommandError(cmd, agentdispatch.Run(agentdispatch.RunOptions{
				Root:            root,
				WorkDir:         workingDir,
				Agent:           agent,
				Model:           model,
				ReasoningEffort: reasoningEffort,
				Skill:           skill,
				PromptArgs:      args,
				Stdin:           cmd.InOrStdin(),
				ReadStdin:       stdinIsPiped(cmd.InOrStdin()),
				Stdout:          cmd.OutOrStdout(),
				Stderr:          cmd.ErrOrStderr(),
				Env:             os.Environ(),
				Quiet:           quiet,
			}))
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", messages.DispatchAgentFlag)
	cmd.Flags().StringVar(&model, "model", "", messages.DispatchModelFlag)
	cmd.Flags().StringVar(&reasoningEffort, "reasoning-effort", "", messages.DispatchReasoningEffortFlag)
	cmd.Flags().StringVar(&skill, "skill", "", messages.DispatchSkillFlag)
	cmd.AddCommand(newDispatchOptionsCmd(), newDispatchResumeCmd(), newDispatchFanoutCmd(), newDispatchInspectCmd(), newDispatchHistoryCmd(), newDispatchCancelCmd(), newDispatchListCmd(), newDispatchDeleteCmd())
	return cmd
}

func newDispatchFanoutCmd() *cobra.Command {
	var targets []string
	var skill string
	cmd := &cobra.Command{
		Use:          "fanout [prompt...] --target agent=<provider>[,model=<model>][,reasoning=<effort>] --target ...",
		Short:        "Send one shared prompt and skill to two or more targets concurrently",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, workingDir, err := resolveRepoRootAndWorkingDir()
			if err != nil {
				return err
			}
			parsed := make([]agentdispatch.FanoutTarget, 0, len(targets))
			for _, value := range targets {
				target, parseErr := agentdispatch.ParseFanoutTarget(value)
				if parseErr != nil {
					return dispatchCommandError(cmd, parseErr)
				}
				parsed = append(parsed, target)
			}
			quiet, _ := cmd.Flags().GetBool("quiet")
			return dispatchCommandError(cmd, agentdispatch.Fanout(agentdispatch.FanoutOptions{
				Root: root, WorkDir: workingDir, Targets: parsed, Skill: skill, PromptArgs: args,
				Stdin: cmd.InOrStdin(), ReadStdin: stdinIsPiped(cmd.InOrStdin()),
				Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(), Env: os.Environ(), Quiet: quiet,
			}))
		},
	}
	cmd.Flags().StringArrayVar(&targets, "target", nil, "Self-contained target: agent=<provider>[,model=<model>][,reasoning=<effort>]")
	cmd.Flags().StringVar(&skill, "skill", "", messages.DispatchSkillFlag)
	return cmd
}

func newDispatchHistoryCmd() *cobra.Command {
	var emitJSON bool
	cmd := &cobra.Command{
		Use: "history <name>", Short: "Show retained immutable turns for a dispatch conversation",
		Args: cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return dispatchCommandError(cmd, agentdispatch.History(agentdispatch.HistoryRequest{Root: root, Name: args[0], Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(), JSON: emitJSON}))
		},
	}
	cmd.Flags().BoolVar(&emitJSON, "json", false, messages.DispatchOptionsJSONFlag)
	return cmd
}

func newDispatchCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use: "cancel <name-or-run-or-fanout-id>", Short: "Cancel exact active Agent Dispatch work",
		Args: cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return dispatchCommandError(cmd, agentdispatch.Cancel(agentdispatch.CancelRequest{Root: root, ID: args[0]}))
		},
	}
}

func newDispatchResumeCmd() *cobra.Command {
	var skill string
	cmd := &cobra.Command{
		Use:          "resume <name> [prompt...]",
		Short:        "Continue an explicitly named Agent Dispatch conversation",
		Long:         "Continue only the provider conversation stored under <name>. Agent Dispatch never infers a prior conversation from a prompt, target, or artifact path.",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, workingDir, err := resolveRepoRootAndWorkingDir()
			if err != nil {
				return err
			}
			quiet, _ := cmd.Flags().GetBool("quiet")
			return dispatchCommandError(cmd, agentdispatch.Resume(agentdispatch.ResumeOptions{
				Root:       root,
				WorkDir:    workingDir,
				Name:       args[0],
				Skill:      skill,
				PromptArgs: args[1:],
				Stdin:      cmd.InOrStdin(),
				ReadStdin:  stdinIsPiped(cmd.InOrStdin()),
				Stdout:     cmd.OutOrStdout(),
				Stderr:     cmd.ErrOrStderr(),
				Env:        os.Environ(),
				Quiet:      quiet,
			}))
		},
	}
	cmd.Flags().StringVar(&skill, "skill", "", messages.DispatchSkillFlag)
	return cmd
}

func newDispatchInspectCmd() *cobra.Command {
	var emitJSON bool
	cmd := &cobra.Command{
		Use:          "inspect <name-or-run-id>",
		Short:        "Inspect factual Agent Dispatch state",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return dispatchCommandError(cmd, agentdispatch.Inspect(agentdispatch.InspectionRequest{Root: root, ID: args[0], Stdout: cmd.OutOrStdout(), JSON: emitJSON}))
		},
	}
	cmd.Flags().BoolVar(&emitJSON, "json", false, messages.DispatchOptionsJSONFlag)
	return cmd
}

func newDispatchListCmd() *cobra.Command {
	var emitJSON bool
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List current Agent Dispatch sessions",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return dispatchCommandError(cmd, agentdispatch.List(agentdispatch.ListRequest{Root: root, Stdout: cmd.OutOrStdout(), JSON: emitJSON}))
		},
	}
	cmd.Flags().BoolVar(&emitJSON, "json", false, messages.DispatchOptionsJSONFlag)
	return cmd
}

func newDispatchDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "delete <name>",
		Short:        "Delete an inactive Agent Dispatch name mapping",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return dispatchCommandError(cmd, agentdispatch.Delete(root, args[0]))
		},
	}
}

func newDispatchOptionsCmd() *cobra.Command {
	var emitJSON bool
	cmd := &cobra.Command{
		Use:          messages.DispatchOptionsUse,
		Short:        messages.DispatchOptionsShort,
		Long:         messages.DispatchOptionsLong,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return dispatchCommandError(cmd, agentdispatch.WriteOptions(agentdispatch.OptionsRequest{Root: root, Env: os.Environ(), Stdout: cmd.OutOrStdout(), JSON: emitJSON}))
		},
	}
	cmd.Flags().BoolVar(&emitJSON, "json", false, messages.DispatchOptionsJSONFlag)
	return cmd
}

func dispatchCommandError(cmd *cobra.Command, err error) error {
	if err == nil {
		return nil
	}
	var dispatchErr *agentdispatch.ExitError
	if errors.As(err, &dispatchErr) {
		if _, writeErr := fmt.Fprintln(cmd.ErrOrStderr(), dispatchErr.Error()); writeErr != nil {
			return writeErr
		}
		return &SilentExitError{Code: dispatchErr.Code}
	}
	return err
}

func stdinIsPiped(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}
