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
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			quiet, _ := cmd.Flags().GetBool("quiet")
			err = agentdispatch.Run(agentdispatch.RunOptions{
				Root:            root,
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
			})
			return dispatchCommandError(cmd, err)
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", messages.DispatchAgentFlag)
	cmd.Flags().StringVar(&model, "model", "", messages.DispatchModelFlag)
	cmd.Flags().StringVar(&reasoningEffort, "reasoning-effort", "", messages.DispatchReasoningEffortFlag)
	cmd.Flags().StringVar(&skill, "skill", "", messages.DispatchSkillFlag)
	cmd.AddCommand(newDispatchOptionsCmd())
	return cmd
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
			err = agentdispatch.WriteOptions(agentdispatch.OptionsRequest{
				Root:   root,
				Env:    os.Environ(),
				Stdout: cmd.OutOrStdout(),
				JSON:   emitJSON,
			})
			return dispatchCommandError(cmd, err)
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
