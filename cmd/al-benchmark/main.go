package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/root"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Restore default signal handling after the first cancellation so a second
	// signal can still stop an unresponsive shutdown.
	restore := context.AfterFunc(ctx, stop)
	defer restore()
	command := newRootCmd()
	command.SetArgs(args)
	if err := command.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	command := newBenchmarkCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.Version = "dev"
	command.SetVersionTemplate("{{.Version}}\n")
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		command.Version = info.Main.Version
	}
	command.PersistentFlags().BoolP("quiet", "q", false, messages.RootQuietFlag)
	return command
}

func resolveRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	repoRoot, found, err := root.FindAgentLayerRoot(cwd)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf(messages.RootMissingAgentLayer)
	}
	return repoRoot, nil
}
