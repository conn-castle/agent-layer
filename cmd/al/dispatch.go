package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	dispatchStartCommand     = "start"
	dispatchMCPServerCommand = "mcp-server"
	dispatchMCPWorkingDirEnv = "AL_MCP_WORKING_DIR" //nolint:gosec // Internal working-directory metadata, not a credential.
)

func newDispatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          messages.DispatchUse,
		Short:        messages.DispatchShort,
		Long:         messages.DispatchLong,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
	}
	cmd.AddCommand(newDispatchOptionsCmd(), newDispatchStartCmd(), newDispatchWaitCmd(), newDispatchContinueCmd(), newDispatchCancelCmd(), newDispatchMCPServerCmd())
	return cmd
}

// newDispatchMCPServerCmd serves the Agent Dispatch MCP tools over stdio. It is
// hidden because clients launch it from generated MCP configuration; the public
// `al dispatch` surface remains the five documented commands.
func newDispatchMCPServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:          dispatchMCPServerCommand,
		Hidden:       true,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, workingDir, err := resolveRepoRootAndWorkingDir()
			if err != nil {
				return err
			}
			workingDir = rootedMCPWorkingDir(root, workingDir, os.Getenv(dispatchMCPWorkingDirEnv))
			return dispatchCommandError(cmd, agentdispatch.RunMCPServer(cmd.Context(), agentdispatch.MCPServerOptions{
				Root: root, WorkDir: workingDir, Version: cmd.Root().Version, Env: os.Environ(),
			}))
		},
	}
}

// rootedMCPWorkingDir preserves the MCP client's original cwd when it belongs
// to the same Agent Layer project. Clients that start the server elsewhere use
// the canonical root embedded by sync instead.
func rootedMCPWorkingDir(root string, fallback string, candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || !filepath.IsAbs(candidate) {
		return fallback
	}
	candidateRoot, found, err := findAgentLayerRoot(candidate)
	if err != nil || !found || filepath.Clean(candidateRoot) != filepath.Clean(root) {
		return fallback
	}
	return filepath.Clean(candidate)
}

func newDispatchOptionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:          messages.DispatchOptionsUse,
		Short:        messages.DispatchOptionsShort,
		Long:         messages.DispatchOptionsLong,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return dispatchCommandError(cmd, agentdispatch.WriteOptions(agentdispatch.OptionsRequest{
				Root: root, Env: os.Environ(), Stdout: cmd.OutOrStdout(),
			}))
		},
	}
}

func newDispatchStartCmd() *cobra.Command {
	var agent, model, effort, skill, prompt, promptFile string
	cmd := &cobra.Command{
		Use:          dispatchStartCommand,
		Short:        "Start a new asynchronous agent conversation",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			root, workingDir, err := resolveRepoRootAndWorkingDir()
			if err != nil {
				return err
			}
			return dispatchCommandError(cmd, agentdispatch.Start(agentdispatch.StartOptions{
				Root: root, WorkDir: workingDir, Agent: agent, Model: model,
				ReasoningEffort: effort, Skill: skill, Prompt: prompt, PromptFile: promptFile,
				Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(), Env: os.Environ(),
			}))
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", messages.DispatchAgentFlag)
	cmd.Flags().StringVar(&model, "model", "", messages.DispatchModelFlag)
	cmd.Flags().StringVar(&effort, "reasoning-effort", "", messages.DispatchReasoningEffortFlag)
	cmd.Flags().StringVar(&skill, "skill", "", messages.DispatchSkillFlag)
	addDispatchPromptFlags(cmd, &prompt, &promptFile)
	return cmd
}

func newDispatchWaitCmd() *cobra.Command {
	return &cobra.Command{
		Use: "wait <handle>", Short: messages.DispatchWaitShort, Long: messages.DispatchWaitLong,
		Args: cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return dispatchCommandError(cmd, agentdispatch.Wait(agentdispatch.WaitRequest{Context: cmd.Context(), Root: root, ID: args[0], Stdout: cmd.OutOrStdout()}))
		},
	}
}

func newDispatchContinueCmd() *cobra.Command {
	var prompt, promptFile string
	cmd := &cobra.Command{
		Use: "continue <handle>", Short: "Continue a terminal agent conversation",
		Args: cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, workingDir, err := resolveRepoRootAndWorkingDir()
			if err != nil {
				return err
			}
			return dispatchCommandError(cmd, agentdispatch.Continue(agentdispatch.ContinueOptions{
				Root: root, WorkDir: workingDir, Handle: args[0], Prompt: prompt,
				PromptFile: promptFile, Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(), Env: os.Environ(),
			}))
		},
	}
	addDispatchPromptFlags(cmd, &prompt, &promptFile)
	return cmd
}

func newDispatchCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use: "cancel <handle>", Short: "Cancel the current running invocation",
		Args: cobra.ExactArgs(1), SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return dispatchCommandError(cmd, agentdispatch.Cancel(agentdispatch.CancelRequest{Root: root, ID: args[0], Stdout: cmd.OutOrStdout()}))
		},
	}
}

func addDispatchPromptFlags(cmd *cobra.Command, prompt *string, promptFile *string) {
	cmd.Flags().StringVar(prompt, "prompt", "", "Prompt text")
	cmd.Flags().StringVar(promptFile, "prompt-file", "", "Path to a file containing the prompt")
}

func newDispatchWorkerCmd() *cobra.Command {
	var root, runID string
	cmd := &cobra.Command{
		Use:    "__dispatch-worker",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			gate := os.NewFile(3, "dispatch-worker-gate")
			if gate == nil {
				return errors.New("dispatch worker gate is unavailable")
			}
			defer func() { _ = gate.Close() }()
			return agentdispatch.RunWorker(root, runID, gate)
		},
	}
	cmd.Flags().StringVar(&root, "root", "", "")
	cmd.Flags().StringVar(&runID, "run", "", "")
	_ = cmd.MarkFlagRequired("root")
	_ = cmd.MarkFlagRequired("run")
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
