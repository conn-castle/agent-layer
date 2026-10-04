package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

// clientLaunchChildRootEnv marks a re-executed test process and names the
// repository root it launches from. Client launches end in a syscall.Exec
// handoff that replaces the process, so running one inside the go test process
// would end the whole package run as soon as the stub client exits.
const clientLaunchChildRootEnv = "AL_TEST_CLIENT_LAUNCH_ROOT"

func TestClientArgsPassThrough(t *testing.T) {
	got := launchClientInSubprocess(t, "claude", "claude", "--foo", "bar", "--baz=qux")
	want := []string{"--foo", "bar", "--baz=qux"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected args %v, got %v", want, got)
	}
}

func TestClientArgsPassThroughWithSeparator(t *testing.T) {
	got := launchClientInSubprocess(t, "claude", "claude", "--", "--help")
	want := []string{"--help"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("expected args %v, got %v", want, got)
	}
}

// launchClientInSubprocess runs `al <args>` in a re-executed copy of the
// calling test and returns the arguments the stub executable received.
func launchClientInSubprocess(t *testing.T, executable string, args ...string) []string {
	t.Helper()
	if root := os.Getenv(clientLaunchChildRootEnv); root != "" {
		testutil.WithWorkingDir(t, root, func() {
			cmd := newRootCmd()
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("execute error: %v", err)
			}
		})
		t.Fatal("client launch returned instead of handing off to the client")
	}

	root := t.TempDir()
	writeTestRepo(t, root)

	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "client-args.txt")
	writeArgsStub(t, binDir, executable, argsFile)
	testutil.WriteStub(t, binDir, "al")

	child := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$") //nolint:gosec // standard test re-exec pattern
	child.Env = append(os.Environ(), "PATH="+binDir, clientLaunchChildRootEnv+"="+root)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("client launch subprocess failed: %v\n%s", err, out)
	}

	data, err := os.ReadFile(argsFile) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// TestNoArgsCommandsRejectExtraArgs verifies that commands which take no
// positional arguments fail loud (rather than silently ignoring) when given a
// stray token. cobra's Args validator runs before RunE, so Execute returns the
// "unknown command" error before any side effect or root resolution occurs.
func TestNoArgsCommandsRejectExtraArgs(t *testing.T) {
	cases := []struct {
		name string
		cmd  func() *cobra.Command
		args []string
	}{
		{name: "sync", cmd: newSyncCmd, args: []string{"unexpected"}},
		{name: "init", cmd: newInitCmd, args: []string{"unexpected"}},
		{name: "doctor", cmd: newDoctorCmd, args: []string{"unexpected"}},
		{name: "probe antigravity", cmd: newProbeAntigravityCmd, args: []string{"unexpected"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.cmd()
			cmd.SetArgs(tc.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("%s accepted unexpected positional arg %v; want error", tc.name, tc.args)
			}
			// cobra.NoArgs rejects extra positionals with "unknown command"
			// before RunE runs, so the error cannot be a side effect of command
			// execution (repository resolution, env, etc.).
			if !strings.Contains(err.Error(), "unknown command") {
				t.Fatalf("%s failed with unexpected error %q; want 'unknown command'", tc.name, err)
			}
		})
	}
}

// TestParentCommandsRejectUnknownSubcommands verifies that a mistyped
// subcommand fails instead of printing help and exiting 0. It runs through the
// root command because cobra only applies the legacy unknown-command check to a
// root, so a parent tested on its own would pass even without the fix.
func TestParentCommandsRejectUnknownSubcommands(t *testing.T) {
	for _, parent := range []string{"dispatch", "benchmark", "probe", "hook"} {
		t.Run(parent, func(t *testing.T) {
			root := newRootCmd()
			root.SetArgs([]string{parent, "bogus"})
			root.SetOut(io.Discard)
			root.SetErr(io.Discard)
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), `unknown command "bogus"`) {
				t.Fatalf("al %s bogus returned %v; want unknown command error", parent, err)
			}

			var out strings.Builder
			root = newRootCmd()
			root.SetArgs([]string{parent})
			root.SetOut(&out)
			root.SetErr(io.Discard)
			if err := root.Execute(); err != nil {
				t.Fatalf("al %s returned %v; want help", parent, err)
			}
			if !strings.Contains(out.String(), "Usage:") {
				t.Fatalf("al %s output = %q; want help", parent, out.String())
			}
		})
	}
}

// TestUpgradeRejectsUnknownSubcommand verifies that a mistyped upgrade
// subcommand such as `al upgrade rollbak` fails instead of running a real
// upgrade. --diff-lines 0 makes the test fail if the upgrade's own validation
// is reached, so only the argument check can produce the expected error.
func TestUpgradeRejectsUnknownSubcommand(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"upgrade", "rollbak", "--diff-lines", "0"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown command "rollbak"`) {
		t.Fatalf("al upgrade rollbak returned %v; want unknown command error", err)
	}
}

// TestRunnableCommandsDeclareArgs walks the command tree so that a new
// command cannot silently ignore stray positional arguments. Cobra only
// rejects unknown words on the root, so a runnable command without an Args
// validator runs with a mistyped subcommand, and a parent without RunE prints
// help and exits 0 for one.
func TestRunnableCommandsDeclareArgs(t *testing.T) {
	// Client launchers forward their arguments to the client, and mcp-prompts
	// is a deprecated no-op stub.
	passThrough := map[string]bool{
		"al claude": true, "al codex": true, "al vscode": true, "al agy": true,
		"al copilot": true, "al grok": true, "al muse": true, "al mcp-prompts": true,
	}
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			path := child.CommandPath()
			if child.HasSubCommands() && !child.Runnable() {
				t.Errorf("%s has subcommands but no RunE, so a mistyped subcommand prints help and exits 0", path)
			}
			if child.Runnable() && child.Args == nil && !passThrough[path] {
				t.Errorf("%s has no Args validator, so it ignores stray positional arguments", path)
			}
			walk(child)
		}
	}
	walk(newRootCmd())
}

func TestSplitQuietArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantQuiet bool
		wantArgs  []string
		wantErr   bool
	}{
		{
			name:      "quiet flag",
			args:      []string{"--quiet", "--foo"},
			wantQuiet: true,
			wantArgs:  []string{"--foo"},
		},
		{
			name:      "quiet shorthand",
			args:      []string{"-q", "--foo"},
			wantQuiet: true,
			wantArgs:  []string{"--foo"},
		},
		{
			name:      "quiet value true",
			args:      []string{"--quiet=true", "--foo"},
			wantQuiet: true,
			wantArgs:  []string{"--foo"},
		},
		{
			name:      "quiet value false consumed",
			args:      []string{"--quiet=false", "--foo"},
			wantQuiet: false,
			wantArgs:  []string{"--foo"},
		},
		{
			name:    "quiet invalid value",
			args:    []string{"--quiet=maybe"},
			wantErr: true,
		},
		{
			name:      "quiet after separator",
			args:      []string{"--", "--quiet"},
			wantQuiet: false,
			wantArgs:  []string{"--quiet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quiet, gotArgs, err := splitQuietArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if quiet != tt.wantQuiet {
				t.Fatalf("expected quiet=%v, got %v", tt.wantQuiet, quiet)
			}
			if strings.Join(gotArgs, ",") != strings.Join(tt.wantArgs, ",") {
				t.Fatalf("expected args %v, got %v", tt.wantArgs, gotArgs)
			}
		})
	}
}

func writeArgsStub(t *testing.T, dir string, name string, outputPath string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := []byte(fmt.Sprintf("#!/bin/sh\nprintf '%s\\n' \"$@\" > %s\n", "%s", strconv.Quote(outputPath)))
	if err := os.WriteFile(path, content, 0o755); err != nil { // #nosec G306 -- test writes an executable shell stub (PATH-shadowed) for subprocess invocation.
		t.Fatalf("write stub: %v", err)
	}
}
