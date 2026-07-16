package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestClientArgsPassThrough(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "claude-args.txt")
	writeArgsStub(t, binDir, "claude", argsFile)
	testutil.WriteStub(t, binDir, "al")

	t.Setenv("PATH", binDir)

	testutil.WithWorkingDir(t, root, func() {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"claude", "--foo", "bar", "--baz=qux"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute error: %v", err)
		}
	})

	data, err := os.ReadFile(argsFile) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"--foo", "bar", "--baz=qux"}
	if strings.Join(lines, ",") != strings.Join(want, ",") {
		t.Fatalf("expected args %v, got %v", want, lines)
	}
}

func TestClientArgsPassThroughWithSeparator(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "claude-args-sep.txt")
	writeArgsStub(t, binDir, "claude", argsFile)
	testutil.WriteStub(t, binDir, "al")

	t.Setenv("PATH", binDir)

	testutil.WithWorkingDir(t, root, func() {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"claude", "--", "--help"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute error: %v", err)
		}
	})

	data, err := os.ReadFile(argsFile) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"--help"}
	if strings.Join(lines, ",") != strings.Join(want, ",") {
		t.Fatalf("expected args %v, got %v", want, lines)
	}
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
