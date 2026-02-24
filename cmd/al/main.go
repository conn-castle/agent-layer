package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/messages"
)

var maybeExecFunc = dispatch.MaybeExec
var executeFunc = execute

// Version, Commit, and BuildDate are overridden at build time.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func main() {
	runMain(os.Args, os.Stdout, os.Stderr, os.Exit)
}

// SilentExitError reports an exit code without emitting error output.
type SilentExitError struct {
	Code int
}

func (e SilentExitError) Error() string {
	return fmt.Sprintf("exit %d", e.Code)
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
	quiet := isQuiet(args, cwd)
	dispatchStderr := stderr
	if quiet {
		dispatchStderr = io.Discard
	}
	if !shouldBypassDispatch(args) {
		if err := maybeExecFunc(args, Version, cwd, dispatchStderr, exit); err != nil {
			if errors.Is(err, dispatch.ErrDispatched) {
				return
			}
			var silent *SilentExitError
			if errors.As(err, &silent) {
				exit(silent.Code)
				return
			}
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				_, _ = fmt.Fprintln(stderr, err)
				code := exitErr.ExitCode()
				if code <= 0 {
					code = 1
				}
				exit(code)
				return
			}
			_, _ = fmt.Fprintln(stderr, err)
			exit(1)
			return
		}
	}
	if err := executeFunc(args, stdout, stderr); err != nil {
		var silent *SilentExitError
		if errors.As(err, &silent) {
			exit(silent.Code)
			return
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			_, _ = fmt.Fprintln(stderr, err)
			code := exitErr.ExitCode()
			if code <= 0 {
				code = 1
			}
			exit(code)
			return
		}
		_, _ = fmt.Fprintln(stderr, err)
		exit(1)
	}
}

// shouldBypassDispatch reports whether dispatch should be skipped for this invocation.
// `al init` and `al upgrade` run through the invoking CLI so upgrade planning is based on
// the currently installed binary templates, not an older repo-pinned version.
// `al mcp-prompts` must also bypass dispatch so MCP stdio servers never hop to a
// PATH-installed shim/binary that may not match the local repository.
func shouldBypassDispatch(args []string) bool {
	if len(args) < 2 {
		return false
	}
	command := firstCommandArg(args[1:])
	return command == "init" || command == "upgrade" || command == "mcp-prompts"
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

func hasQuietFlag(args []string) bool {
	for i, arg := range args {
		if i == 0 {
			continue
		}
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}
		if trimmed == "--" {
			break
		}
		if trimmed == flagQuiet || trimmed == flagQuietShort {
			return true
		}
		if strings.HasPrefix(trimmed, flagQuietPrefix) {
			value := strings.TrimPrefix(trimmed, flagQuietPrefix)
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				continue
			}
			if parsed {
				return true
			}
		}
	}
	return false
}

func quietFromConfig(cwd string) bool {
	rootDir, found, err := findAgentLayerRoot(cwd)
	if err != nil || !found {
		return false
	}
	paths := config.DefaultPaths(rootDir)
	cfg, err := config.LoadConfigLenient(paths.ConfigPath)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.Warnings.NoiseMode), "quiet")
}

func isQuiet(args []string, cwd string) bool {
	return hasQuietFlag(args) || quietFromConfig(cwd)
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
