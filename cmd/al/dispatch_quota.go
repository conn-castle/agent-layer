package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/quota"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

const (
	dispatchQuotaCommand = "quota"
	quotaFormatFull      = "full"
	quotaFormatSummary   = "summary"
)

type dispatchQuotaFlags struct {
	agents, profiles, expected, models []string
	timeout                            time.Duration
	retries                            int
	format                             string
}

func newDispatchQuotaCmd() *cobra.Command {
	return dispatchQuotaCommandWithReader(quota.Query)
}

func dispatchQuotaCommandWithReader(query func(context.Context, quota.Request) quota.Response) *cobra.Command {
	var options dispatchQuotaFlags
	cmd := &cobra.Command{
		Use: dispatchQuotaCommand, Short: "Read subscription quota as JSON",
		Long:          "Read subscription usage, reset timing, and lockout state without a model turn. By default, read every enabled dispatch agent in the current project.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return dispatchCommandError(cmd, &agentdispatch.ExitError{Code: agentdispatch.ExitUsage, Message: "dispatch quota accepts flags only"})
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDispatchQuota(cmd, options, query)
		},
	}
	// Avoid including caller-supplied flag values in parse errors.
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, _ error) error {
		return dispatchCommandError(cmd, &agentdispatch.ExitError{Code: agentdispatch.ExitUsage, Message: "invalid dispatch quota flags; see al dispatch quota --help"})
	})
	cmd.Flags().StringArrayVar(&options.agents, "agent", nil, "Project agent to query (repeatable)")
	cmd.Flags().StringArrayVar(&options.profiles, "profile", nil, "Detached native credential file, LABEL=AGENT:FILE (repeatable)")
	cmd.Flags().StringArrayVar(&options.expected, "expect-account", nil, "Require a verified account, SELECTOR=ID (repeatable)")
	cmd.Flags().StringArrayVar(&options.models, "model", nil, "Evaluate an additional dispatch model, SELECTOR=MODEL (repeatable)")
	cmd.Flags().DurationVar(&options.timeout, "timeout", agentdispatch.DefaultQuotaTimeout, "Overall timeout, at most 1m")
	cmd.Flags().IntVar(&options.retries, "retries", agentdispatch.DefaultQuotaRetries, "Additional attempts per HTTP read (0–5)")
	cmd.Flags().StringVar(&options.format, "format", quotaFormatFull, "JSON detail level (full or summary)")
	cmd.Flags().Bool("json", true, "Emit structured JSON")
	return cmd
}

func runDispatchQuota(cmd *cobra.Command, options dispatchQuotaFlags, query func(context.Context, quota.Request) quota.Response) error {
	jsonOutput, err := cmd.Flags().GetBool("json")
	if err != nil {
		return err
	}
	if !jsonOutput {
		return dispatchCommandError(cmd, &agentdispatch.ExitError{Code: agentdispatch.ExitUsage, Message: "dispatch quota requires JSON output"})
	}
	if options.format != quotaFormatFull && options.format != quotaFormatSummary {
		return dispatchCommandError(cmd, &agentdispatch.ExitError{Code: agentdispatch.ExitUsage, Message: "quota --format must be full or summary"})
	}
	selection, err := agentdispatch.ParseQuotaSelection(options.agents, options.profiles, options.expected, options.models, options.timeout, options.retries)
	if err != nil {
		return dispatchCommandError(cmd, err)
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), options.timeout)
	defer cancel()
	var req quota.Request
	if selection.NeedsProject() {
		root, workDir, err := resolveRepoRootAndWorkingDir()
		if err != nil {
			return dispatchCommandError(cmd, &agentdispatch.ExitError{Code: agentdispatch.ExitConfig, Message: "dispatch quota requires a readable Agent Layer project"})
		}
		project, err := config.LoadProjectConfig(root)
		if err != nil {
			return dispatchCommandError(cmd, &agentdispatch.ExitError{Code: agentdispatch.ExitConfig, Message: "dispatch quota could not load project configuration"})
		}
		req.Project, req.WorkDir = project, workDir
	}
	req.Sources, err = selection.Sources(req.Project)
	if err != nil {
		return dispatchCommandError(cmd, err)
	}
	req.Env = os.Environ()
	effective := req.Env
	if req.Project != nil {
		effective = clients.BuildEnv(effective, req.Project.Env, nil)
	}
	noNetwork, _ := clients.GetEnv(effective, versiondispatch.EnvNoNetwork)
	req.Offline = strings.TrimSpace(noNetwork) != ""
	req.MaxRetries = options.retries
	output := query(ctx, req)
	var document any = output
	if options.format == quotaFormatSummary {
		document = output.Summary(req.Sources)
	}
	if err := json.NewEncoder(cmd.OutOrStdout()).Encode(document); err != nil {
		return err
	}
	if errors.Is(cmd.Context().Err(), context.Canceled) {
		code := agentdispatch.ExitSigint
		if cause := context.Cause(cmd.Context()); cause != nil && cause.Error() == syscall.SIGTERM.String()+" signal received" {
			code = agentdispatch.ExitSigterm
		}
		return &SilentExitError{Code: code}
	}
	switch output.Status {
	case quota.ResponseComplete:
		return nil
	case quota.ResponsePartial:
		return &SilentExitError{Code: agentdispatch.ExitQuotaPartial}
	default:
		return &SilentExitError{Code: agentdispatch.ExitQuotaNone}
	}
}
