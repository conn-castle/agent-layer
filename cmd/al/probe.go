package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
	probeantigravity "github.com/conn-castle/agent-layer/internal/probe/antigravity"
)

var runAntigravityProbe = probeantigravity.Probe

func newProbeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   messages.ProbeUse,
		Short: messages.ProbeShort,
		Long:  messages.ProbeLong,
	}
	cmd.AddCommand(newProbeAntigravityCmd())
	return cmd
}

func newProbeAntigravityCmd() *cobra.Command {
	return &cobra.Command{
		Use:   messages.ProbeAntigravityUse,
		Short: messages.ProbeAntigravityShort,
		Long:  messages.ProbeAntigravityLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			result, err := runAntigravityProbe(cmd.Context(), filepath.Join(root, ".agent-layer", "tmp"))
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			if encodeErr := encoder.Encode(result); encodeErr != nil {
				return encodeErr
			}
			// Surface a non-zero exit when agy exited non-zero or the probe
			// reported an internal error. Keep the JSON on stdout so callers
			// piping into jq still get the full machine-readable output.
			if result.ExitCode != 0 || result.Error != "" {
				return fmt.Errorf(messages.ProbeAntigravityNonZeroExitFmt, result.ExitCode, result.Error)
			}
			return nil
		},
	}
}
