package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/version"
)

var benchmarkExec = clients.ExecHandoff
var benchmarkLookPath = exec.LookPath
var benchmarkReadVersion = readBenchmarkVersion

func readBenchmarkVersion(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, path, "--version") // #nosec G204 -- companion executable resolved with LookPath; fixed metadata-only argument.
	command.WaitDelay = time.Second
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("read benchmark companion version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func benchmarkVersionIdentity(raw string) string {
	if normalized, err := version.Normalize(raw); err == nil {
		return "v" + normalized
	}
	return strings.TrimSpace(raw)
}

// newBenchmarkCmd preserves the public command while keeping the optional
// evaluation engine outside the core executable's dependency graph.
func newBenchmarkCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "benchmark [init|readiness|run]",
		Short:              "Run the optional al-benchmark companion",
		Args:               cobra.ArbitraryArgs, // The companion validates its own command surface.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			expected := benchmarkVersionIdentity(Version)
			install := "go install github.com/conn-castle/agent-layer/cmd/al-benchmark@" + expected
			if expected == "dev" {
				install = "make benchmark-install from the matching Agent Layer source checkout"
			}
			path, err := benchmarkLookPath("al-benchmark")
			if err != nil {
				return fmt.Errorf("benchmarking requires the optional al-benchmark executable on PATH; install with %s (see docs/BENCHMARK.md): %w", install, err)
			}
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			actual, err := benchmarkReadVersion(cmd.Context(), path)
			if err != nil {
				return err
			}
			if benchmarkVersionIdentity(actual) != expected {
				return fmt.Errorf("al-benchmark version %q does not match active al version %q; install with %s", actual, expected, install)
			}
			// Replace this process, as the provider launchers do, preserving the
			// terminal, signals, working directory, environment, and exit status.
			return benchmarkExec(path, append([]string{"al-benchmark"}, args...), os.Environ())
		},
	}
}
