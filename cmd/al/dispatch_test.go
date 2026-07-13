package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

// clearDispatchEnv scrubs AL_DISPATCH_* markers inherited from the parent shell
// so newDispatchCmd's RunE closure (which calls os.Environ()) cannot be
// short-circuited by a developer's or CI runner's ambient AL_DISPATCH_ACTIVE
// (would return ExitNested=75) or AL_DISPATCH_CALLER_AGENT (would pick a
// configured default that bypasses the missing-prompt validation under test).
// The markers are genuinely unset rather than set to "": dispatch now treats a
// present-but-empty AL_DISPATCH_ACTIVE as a malformed value that fails loud, so
// only true absence (GetEnv ok=false) reads as depth 0 / no active dispatch.
func clearDispatchEnv(t *testing.T) {
	t.Helper()
	unsetEnvForTest(t, "AL_DISPATCH_ACTIVE")
	unsetEnvForTest(t, "AL_DISPATCH_CALLER_AGENT")
}

// unsetEnvForTest removes key from the environment for the duration of the test.
// t.Setenv registers restoration of the pre-test value on cleanup; os.Unsetenv
// then removes the variable so lookups see it as absent, not present-but-empty.
func unsetEnvForTest(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
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

func TestDispatchLifecycleSubcommandsAreWired(t *testing.T) {
	cmd := newDispatchCmd()
	for _, name := range []string{"fanout", "history", "cancel"} {
		child, _, err := cmd.Find([]string{name})
		if err != nil || child == cmd || child.Name() != name {
			t.Fatalf("dispatch subcommand %q was not wired: child=%v err=%v", name, child, err)
		}
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
