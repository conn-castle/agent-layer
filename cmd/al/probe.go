package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
	probeantigravity "github.com/conn-castle/agent-layer/internal/probe/antigravity"
	probegrok "github.com/conn-castle/agent-layer/internal/probe/grok"
)

var runAntigravityProbe = probeantigravity.Probe
var runGrokProbe = probegrok.Probe

type probeExecution func(context.Context, string, string) (result any, exitCode int, resultError string, err error)

func newProbeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   messages.ProbeUse,
		Short: messages.ProbeShort,
		Long:  messages.ProbeLong,
	}
	cmd.AddCommand(newProbeAntigravityCmd())
	cmd.AddCommand(newProbeGrokCmd())
	return cmd
}

// newProbeMCPFixtureCmd serves the probe's deterministic MCP fixture. It is a
// hidden subcommand of this binary rather than a separately built helper so the
// probe never depends on a Go toolchain or a build step at probe time.
func newProbeMCPFixtureCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "__probe-mcp-fixture",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return probeantigravity.RunMCPFixture(cmd.Context())
		},
	}
}

func newProbeAntigravityCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.ProbeAntigravityUse,
		Short: messages.ProbeAntigravityShort,
		Long:  messages.ProbeAntigravityLong,
		Args:  cobra.NoArgs,
		RunE: probeCommandRunner(func(ctx context.Context, _ string, tmpRoot string) (any, int, string, error) {
			result, err := runAntigravityProbe(ctx, tmpRoot)
			if err != nil {
				return nil, 0, "", err
			}
			return result, result.ExitCode, result.Error, nil
		}, messages.ProbeAntigravityNonZeroExitFmt),
	}
}

func newProbeGrokCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.ProbeGrokUse,
		Short: messages.ProbeGrokShort,
		Long:  messages.ProbeGrokLong,
		Args:  cobra.NoArgs,
		RunE: probeCommandRunner(func(ctx context.Context, root string, tmpRoot string) (any, int, string, error) {
			result, err := runGrokProbe(ctx, tmpRoot, probegrok.PreferredAuthHome(root))
			if err != nil {
				return nil, 0, "", err
			}
			return result, result.ExitCode, result.Error, nil
		}, messages.ProbeGrokNonZeroExitFmt),
	}
}

func probeCommandRunner(execute probeExecution, nonZeroFormat string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		root, err := resolveRepoRoot()
		if err != nil {
			return err
		}
		result, exitCode, resultError, err := execute(cmd.Context(), root, filepath.Join(root, ".agent-layer", "tmp"))
		if err != nil {
			return err
		}
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return err
		}
		// Keep the complete machine-readable result on stdout, then fail the
		// command when the provider or the capability validation failed.
		if exitCode != 0 || resultError != "" {
			return fmt.Errorf(nonZeroFormat, exitCode, resultError)
		}
		return nil
	}
}
