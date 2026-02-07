package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/dispatch"
)

func TestMainVersion(t *testing.T) {
	var out bytes.Buffer
	if err := execute([]string{"al", "--version"}, &out, &out); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !strings.Contains(out.String(), Version) {
		t.Fatalf("expected version output, got %q", out.String())
	}
}

func TestMainUnknownCommand(t *testing.T) {
	var out bytes.Buffer
	err := execute([]string{"al", "unknown"}, &out, &out)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunMainSuccess(t *testing.T) {
	var out bytes.Buffer
	called := false
	runMain([]string{"al", "--version"}, &out, &out, func(code int) {
		called = true
	})
	if called {
		t.Fatalf("unexpected exit")
	}
}

func TestRunMainError(t *testing.T) {
	var out bytes.Buffer
	code := 0
	runMain([]string{"al", "unknown"}, &out, &out, func(exitCode int) {
		code = exitCode
	})
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(out.String(), "unknown command") {
		t.Fatalf("expected error output, got %q", out.String())
	}
}

func TestMainCallsExecute(t *testing.T) {
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	os.Args = []string{"al", "--version"}
	main()
}

func TestRunMain_GetwdError(t *testing.T) {
	orig := getwd
	defer func() { getwd = orig }()
	getwd = func() (string, error) { return "", errors.New("getwd failed") }

	var out bytes.Buffer
	var code int
	runMain([]string{"al"}, &out, &out, func(c int) { code = c })

	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(out.String(), "getwd failed") {
		t.Errorf("expected output to contain 'getwd failed', got %q", out.String())
	}
}

func TestRunMain_DispatchError(t *testing.T) {
	orig := maybeExecFunc
	defer func() { maybeExecFunc = orig }()
	maybeExecFunc = func(args []string, currentVersion string, cwd string, exit func(int)) error {
		return errors.New("dispatch failed")
	}

	var out bytes.Buffer
	var code int
	runMain([]string{"al"}, &out, &out, func(c int) { code = c })

	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(out.String(), "dispatch failed") {
		t.Errorf("expected output to contain 'dispatch failed', got %q", out.String())
	}
}

func TestRunMain_Dispatched(t *testing.T) {
	orig := maybeExecFunc
	defer func() { maybeExecFunc = orig }()
	maybeExecFunc = func(args []string, currentVersion string, cwd string, exit func(int)) error {
		return dispatch.ErrDispatched
	}

	var out bytes.Buffer
	var code int
	runMain([]string{"al"}, &out, &out, func(c int) { code = c })

	if code != 0 {
		t.Errorf("expected exit 0 (default), got %d (called exit?)", code)
	}
	// Verify no error output
	if out.String() != "" {
		t.Errorf("expected no output, got %q", out.String())
	}
}

func TestRunMain_InitBypassesDispatch(t *testing.T) {
	orig := maybeExecFunc
	defer func() { maybeExecFunc = orig }()
	dispatchCalled := false
	maybeExecFunc = func(args []string, currentVersion string, cwd string, exit func(int)) error {
		dispatchCalled = true
		return errors.New("dispatch should be bypassed for init")
	}

	var out bytes.Buffer
	exitCode := -1
	runMain([]string{"al", "init", "--help"}, &out, &out, func(code int) {
		exitCode = code
	})

	if dispatchCalled {
		t.Fatal("expected dispatch to be bypassed for init")
	}
	if exitCode != -1 {
		t.Fatalf("expected no exit call, got %d", exitCode)
	}
}

func TestShouldBypassDispatch(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "No subcommand", args: []string{"al"}, want: false},
		{name: "Init command", args: []string{"al", "init"}, want: true},
		{name: "Non-init command", args: []string{"al", "doctor"}, want: false},
		{name: "Global version flag only", args: []string{"al", "--version"}, want: false},
		{name: "Double-dash init", args: []string{"al", "--", "init"}, want: true},
		{name: "Flag then init", args: []string{"al", "--help", "init"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldBypassDispatch(tt.args); got != tt.want {
				t.Fatalf("shouldBypassDispatch(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestVersionString(t *testing.T) {
	origVersion := Version
	origCommit := Commit
	origBuildDate := BuildDate
	defer func() {
		Version = origVersion
		Commit = origCommit
		BuildDate = origBuildDate
	}()

	tests := []struct {
		name      string
		version   string
		commit    string
		buildDate string
		want      string
	}{
		{
			name:      "Version only",
			version:   "v1.0.0",
			commit:    "",
			buildDate: "",
			want:      "v1.0.0",
		},
		{
			name:      "Version and Commit",
			version:   "v1.0.0",
			commit:    "abcdef",
			buildDate: "",
			want:      "v1.0.0 (commit abcdef)",
		},
		{
			name:      "Version and BuildDate",
			version:   "v1.0.0",
			commit:    "",
			buildDate: "2023-01-01",
			want:      "v1.0.0 (built 2023-01-01)",
		},
		{
			name:      "All metadata",
			version:   "v1.0.0",
			commit:    "abcdef",
			buildDate: "2023-01-01",
			want:      "v1.0.0 (commit abcdef, built 2023-01-01)",
		},
		{
			name:      "Unknown metadata filtered",
			version:   "v1.0.0",
			commit:    "unknown",
			buildDate: "unknown",
			want:      "v1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Version = tt.version
			Commit = tt.commit
			BuildDate = tt.buildDate
			if got := versionString(); got != tt.want {
				t.Errorf("versionString() = %v, want %v", got, tt.want)
			}
		})
	}
}
