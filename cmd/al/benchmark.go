package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
)

var benchmarkExec = clients.ExecHandoff
var benchmarkLookPath = exec.LookPath

// newBenchmarkCmd preserves the public command while keeping the optional
// evaluation engine outside the core executable's dependency graph.
func newBenchmarkCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "benchmark [init|readiness|run]",
		Short:              "Run the optional al-benchmark companion",
		Args:               cobra.ArbitraryArgs, // The companion validates its own command surface.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := benchmarkLookPath("al-benchmark")
			if err != nil {
				return fmt.Errorf("benchmarking requires the optional al-benchmark executable on PATH; install the matching release with go install github.com/conn-castle/agent-layer/cmd/al-benchmark@<version> (see docs/BENCHMARK.md): %w", err)
			}
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			// Replace this process, as the provider launchers do, preserving the
			// terminal, signals, working directory, environment, and exit status.
			return benchmarkExec(path, append([]string{"al-benchmark"}, args...), os.Environ())
		},
	}
}
