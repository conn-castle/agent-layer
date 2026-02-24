package clients

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/updatewarn"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

// LaunchFunc launches a client after sync and run setup.
type LaunchFunc func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error

// EnabledSelector returns the enabled flag for a client.
type EnabledSelector func(cfg *config.Config) *bool

// Run performs the standard client launch pipeline: load config, sync, create run dir, launch.
// Warnings from sync are printed to stderr before launching.
func Run(ctx context.Context, root string, name string, enabled EnabledSelector, launch LaunchFunc, quiet bool, args []string, currentVersion string) error {
	return RunWithStderr(ctx, root, name, enabled, launch, quiet, args, currentVersion, os.Stderr)
}

// RunNoSync performs the standard client launch pipeline without running sync.
func RunNoSync(root string, name string, enabled EnabledSelector, launch LaunchFunc, quiet bool, args []string) error {
	return RunNoSyncWithStderr(root, name, enabled, launch, quiet, args, os.Stderr)
}

// RunNoSyncWithStderr is like RunNoSync but allows specifying a custom stderr writer for testing.
func RunNoSyncWithStderr(root string, name string, enabled EnabledSelector, launch LaunchFunc, quiet bool, args []string, stderr io.Writer) error {
	project, err := loadProject(root, name, enabled)
	if err != nil {
		return err
	}

	effectiveQuiet := resolveQuiet(quiet, project)
	if effectiveQuiet {
		stderr = io.Discard
	}

	if project.Config.Approvals.Mode == "yolo" && stderr != nil {
		_, _ = fmt.Fprintln(stderr, messages.WarningsPolicyYOLOAck)
	}

	return launchWithRunInfo(root, project, launch, args)
}

// RunWithStderr is like Run but allows specifying a custom stderr writer for testing.
func RunWithStderr(ctx context.Context, root string, name string, enabled EnabledSelector, launch LaunchFunc, quiet bool, args []string, currentVersion string, stderr io.Writer) error {
	project, err := loadProject(root, name, enabled)
	if err != nil {
		return err
	}
	effectiveQuiet := resolveQuiet(quiet, project)
	if effectiveQuiet {
		stderr = io.Discard
	}
	if project.Config.Warnings.VersionUpdateOnSync != nil && *project.Config.Warnings.VersionUpdateOnSync {
		updatewarn.WarnIfOutdated(ctx, currentVersion, stderr)
	}
	result, err := sync.RunWithProject(sync.RealSystem{}, root, project)
	if err != nil {
		return err
	}

	// Print warnings to stderr before launching
	if stderr != nil {
		for _, w := range result.Warnings {
			_, _ = fmt.Fprintln(stderr, w.String())
		}
	}

	if project.Config.Approvals.Mode == "yolo" && stderr != nil {
		_, _ = fmt.Fprintln(stderr, messages.WarningsPolicyYOLOAck)
	}

	return launchWithRunInfo(root, project, launch, args)
}

// loadProject loads the project config and verifies the client is enabled.
func loadProject(root string, name string, enabled EnabledSelector) (*config.ProjectConfig, error) {
	project, err := config.LoadProjectConfig(root)
	if err != nil {
		return nil, err
	}
	if err := sync.EnsureEnabled(name, enabled(&project.Config)); err != nil {
		return nil, err
	}
	return project, nil
}

// launchWithRunInfo prepares the run info and environment before launching.
func launchWithRunInfo(root string, project *config.ProjectConfig, launch LaunchFunc, args []string) error {
	runInfo, err := run.Create(root)
	if err != nil {
		return err
	}

	env := BuildEnv(os.Environ(), project.Env, runInfo)

	return launch(project, runInfo, env, args)
}

func resolveQuiet(quiet bool, project *config.ProjectConfig) bool {
	if quiet {
		return true
	}
	if project == nil {
		return false
	}
	mode := strings.TrimSpace(project.Config.Warnings.NoiseMode)
	return strings.EqualFold(mode, warnings.NoiseModeQuiet)
}
