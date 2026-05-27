package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

// clearDispatchEnv scrubs AL_DISPATCH_* markers inherited from the parent shell
// so newDispatchCmd's RunE closure (which calls os.Environ()) cannot be
// short-circuited by a developer's or CI runner's ambient AL_DISPATCH_ACTIVE=1
// (would return ExitNested=75) or AL_DISPATCH_CALLER_AGENT (would pick a
// configured default that bypasses the missing-prompt validation under test).
// t.Setenv to empty works because dispatch treats empty AL_DISPATCH_ACTIVE
// as not-set (compared against "1") and knownCallerFromEnv returns ("", false)
// for non-matching values.
func clearDispatchEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AL_DISPATCH_ACTIVE", "")
	t.Setenv("AL_DISPATCH_CALLER_AGENT", "")
}

func TestDispatchCommandMapsExitError(t *testing.T) {
	clearDispatchEnv(t)
	root := t.TempDir()
	writeTestRepo(t, root)
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "codex")
	t.Setenv("PATH", binDir)
	testutil.WithWorkingDir(t, root, func() {
		cmd := newDispatchCmd()
		cmd.SetArgs([]string{"--agent", "codex"})
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		err := cmd.Execute()
		var silent *SilentExitError
		if !errors.As(err, &silent) || silent.Code != 64 {
			t.Fatalf("expected SilentExitError{Code:64}, got %T: %v", err, err)
		}
		if stdout.String() != "" {
			t.Fatalf("expected empty stdout, got %q", stdout.String())
		}
		if !strings.Contains(stderr.String(), "`al dispatch` requires prompt text") {
			t.Fatalf("unexpected stderr: %q", stderr.String())
		}
	})
}

// TestDispatchHelpWiresLongMessage guards the wiring contract: cobra's --help
// output must include the DispatchLong constant. The full prose of the long
// message lives in messages.DispatchLong and is not asserted here per
// docs/agent-layer/CONTEXT.md "Test policy".
func TestDispatchHelpWiresLongMessage(t *testing.T) {
	cmd := newDispatchCmd()
	cmd.SetArgs([]string{"--help"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help error: %v", err)
	}
	if !strings.Contains(stdout.String(), messages.DispatchLong) {
		t.Fatalf("expected DispatchLong to appear in --help output")
	}
}

// TestDispatchOptionsHelpWiresLongMessage guards the same wiring contract for
// the `al dispatch options` subcommand.
func TestDispatchOptionsHelpWiresLongMessage(t *testing.T) {
	cmd := newDispatchCmd()
	cmd.SetArgs([]string{"options", "--help"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help error: %v", err)
	}
	if !strings.Contains(stdout.String(), messages.DispatchOptionsLong) {
		t.Fatalf("expected DispatchOptionsLong to appear in options --help output")
	}
}

func TestDispatchOptionsJSONInvalidConfigNoStdout(t *testing.T) {
	clearDispatchEnv(t)
	root := t.TempDir()
	writeTestRepoInvalidConfig(t, root)
	testutil.WithWorkingDir(t, root, func() {
		cmd := newDispatchCmd()
		cmd.SetArgs([]string{"options", "--json"})
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		err := cmd.Execute()
		var silent *SilentExitError
		if !errors.As(err, &silent) || silent.Code != 65 {
			t.Fatalf("expected SilentExitError{Code:65}, got %T: %v", err, err)
		}
		if stdout.String() != "" {
			t.Fatalf("expected no JSON stdout on invalid config, got %q", stdout.String())
		}
		if !strings.Contains(stderr.String(), "invalid config") {
			t.Fatalf("expected config error in stderr, got %q", stderr.String())
		}
	})
}

func TestStdinIsPipedNonFileReader(t *testing.T) {
	if stdinIsPiped(bytes.NewBufferString("prompt")) {
		t.Fatal("non-file reader should not be treated as piped stdin")
	}
}

func TestDispatchCommandErrorBranches(t *testing.T) {
	cmd := newDispatchCmd()
	if err := dispatchCommandError(cmd, nil); err != nil {
		t.Fatalf("nil dispatch error = %v", err)
	}

	sentinel := errors.New("plain failure")
	if err := dispatchCommandError(cmd, sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("expected non-dispatch error passthrough, got %v", err)
	}

	cmd.SetErr(failingWriter{})
	err := dispatchCommandError(cmd, &agentdispatch.ExitError{Code: 64, Message: "dispatch failed"})
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected stderr write failure, got %v", err)
	}
}
