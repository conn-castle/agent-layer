package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/messages"
)

var maybeExecFunc = dispatch.MaybeExec

// Version, Commit, and BuildDate are overridden at build time.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	runMain(os.Args, os.Stdout, os.Stderr, os.Exit)
}

// execute runs the CLI command with the provided args and output writers.
func execute(args []string, stdout io.Writer, stderr io.Writer) error {
	cmd := newRootCmd()
	cmd.Version = versionString()
	cmd.SetVersionTemplate(messages.VersionTemplate)
	if len(args) > 1 {
		cmd.SetArgs(args[1:])
	}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	return cmd.Execute()
}

// runMain handles version dispatch and executes the CLI, exiting on fatal errors.
func runMain(args []string, stdout io.Writer, stderr io.Writer, exit func(int)) {
	cwd, err := getwd()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		exit(1)
		return
	}
	if !shouldBypassDispatch(args) {
		if err := maybeExecFunc(args, Version, cwd, exit); err != nil {
			if errors.Is(err, dispatch.ErrDispatched) {
				return
			}
			_, _ = fmt.Fprintln(stderr, err)
			exit(1)
			return
		}
	}
	if err := execute(args, stdout, stderr); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		exit(1)
	}
}

// shouldBypassDispatch reports whether dispatch should be skipped for this invocation.
// `al init` and `al upgrade` run through the invoking CLI so upgrade planning is based on
// the currently installed binary templates, not an older repo-pinned version.
func shouldBypassDispatch(args []string) bool {
	if len(args) < 2 {
		return false
	}
	command := firstCommandArg(args[1:])
	return command == "init" || command == "upgrade"
}

// firstCommandArg extracts the first non-flag token from root command arguments.
func firstCommandArg(args []string) string {
	for idx, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		if trimmed == "--" {
			if idx+1 >= len(args) {
				return ""
			}
			return strings.TrimSpace(args[idx+1])
		}
		if strings.HasPrefix(trimmed, "-") {
			continue
		}
		return trimmed
	}
	return ""
}

// versionString formats Version with optional commit and build date metadata.
func versionString() string {
	meta := []string{}
	if Commit != "" && Commit != "unknown" {
		meta = append(meta, fmt.Sprintf(messages.VersionCommitFmt, Commit))
	}
	if BuildDate != "" && BuildDate != "unknown" {
		meta = append(meta, fmt.Sprintf(messages.VersionBuildFmt, BuildDate))
	}
	if len(meta) == 0 {
		return Version
	}
	return fmt.Sprintf(messages.VersionFullFmt, Version, strings.Join(meta, ", "))
}
