package clients

import (
	"fmt"
	"io"
	"os"

	"github.com/nicholasjconn/agent-layer/internal/config"
	"github.com/nicholasjconn/agent-layer/internal/run"
	"github.com/nicholasjconn/agent-layer/internal/sync"
)

// LaunchFunc launches a client after sync and run setup.
type LaunchFunc func(project *config.ProjectConfig, runInfo *run.Info, env []string) error

// EnabledSelector returns the enabled flag for a client.
type EnabledSelector func(cfg *config.Config) *bool

// Run performs the standard client launch pipeline: load config, sync, create run dir, launch.
// Warnings from sync are printed to stderr before launching.
func Run(root string, name string, enabled EnabledSelector, launch LaunchFunc) error {
	return RunWithStderr(root, name, enabled, launch, os.Stderr)
}

// RunWithStderr is like Run but allows specifying a custom stderr writer for testing.
func RunWithStderr(root string, name string, enabled EnabledSelector, launch LaunchFunc, stderr io.Writer) error {
	project, err := config.LoadProjectConfig(root)
	if err != nil {
		return err
	}
	if err := sync.EnsureEnabled(name, enabled(&project.Config)); err != nil {
		return err
	}

	warnings, err := sync.RunWithProject(root, project)
	if err != nil {
		return err
	}

	// Print warnings to stderr before launching
	for _, w := range warnings {
		_, _ = fmt.Fprintln(stderr, w.String())
	}

	runInfo, err := run.Create(root)
	if err != nil {
		return err
	}

	env := BuildEnv(os.Environ(), project.Env, runInfo)

	return launch(project, runInfo, env)
}
