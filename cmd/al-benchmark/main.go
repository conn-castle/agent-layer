package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/root"
	"github.com/conn-castle/agent-layer/internal/version"
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
	command.Version = companionVersion(debug.ReadBuildInfo())
	command.SetVersionTemplate("{{.Version}}\n")
	command.PersistentFlags().BoolP("quiet", "q", false, messages.RootQuietFlag)
	return command
}

// companionVersion reports a stable release only for a versioned module
// install (`go install ...@vX.Y.Z`), which carries no VCS build settings. Go
// also stamps a clean tagged source checkout with its tag, but that build is
// source like the unstamped core, so it reports dev.
func companionVersion(info *debug.BuildInfo, ok bool) string {
	const development = "dev"
	if !ok || info == nil {
		return development
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs" || strings.HasPrefix(setting.Key, "vcs.") {
			return development
		}
	}
	normalized, err := version.Normalize(info.Main.Version)
	if err != nil {
		return development
	}
	return "v" + normalized
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
