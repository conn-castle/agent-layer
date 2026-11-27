package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestRootedMCPWorkingDir(t *testing.T) {
	// Production passes the symlink-resolved root from resolveRepoRootAndWorkingDir,
	// while the client's cwd may still use the logical path (e.g. macOS /var vs /private/var).
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".agent-layer"), 0o750); err != nil {
		t.Fatal(err)
	}
	root := canonicalPath(dir)
	subdir := filepath.Join(dir, "nested")
	if err := os.Mkdir(subdir, 0o750); err != nil {
		t.Fatal(err)
	}
	if got := rootedMCPWorkingDir(root, root, subdir); got != subdir {
		t.Fatalf("same-project working dir = %q, want %q", got, subdir)
	}
	outside := t.TempDir()
	if got := rootedMCPWorkingDir(root, root, outside); got != root {
		t.Fatalf("unrelated working dir = %q, want fallback %q", got, root)
	}
	if got := rootedMCPWorkingDir(root, root, "relative"); got != root {
		t.Fatalf("relative working dir = %q, want fallback %q", got, root)
	}
	spaced := filepath.Join(dir, "nested ")
	if err := os.Mkdir(spaced, 0o750); err != nil {
		t.Fatal(err)
	}
	if got := rootedMCPWorkingDir(root, root, spaced); got != spaced {
		t.Fatalf("whitespace-suffixed working dir = %q, want %q", got, spaced)
	}
}

func TestDispatchHelpWiresAsyncSurface(t *testing.T) {
	cmd := newDispatchCmd()
	cmd.SetArgs([]string{"--help"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help error: %v", err)
	}
	if !strings.Contains(stdout.String(), messages.DispatchLong) {
		t.Fatalf("expected DispatchLong in help")
	}
	for _, name := range []string{"options", "quota", "reserve", "start", "wait", "continue", "cancel", "inspect", "output"} {
		child, _, err := cmd.Find([]string{name})
		if err != nil || child == cmd || child.Name() != name {
			t.Fatalf("dispatch subcommand %q not wired: child=%v err=%v", name, child, err)
		}
	}
	for _, removed := range []string{"fanout", "resume", "history", "list", "delete"} {
		if child, _, err := cmd.Find([]string{removed}); err == nil && child != cmd {
			t.Fatalf("removed dispatch subcommand %q remains public", removed)
		}
	}
}

// TestDispatchMCPServerIsWiredButHidden proves the stdio entry point exists for
// generated MCP client configuration while staying out of the documented five
// command public surface an agent reads from `al dispatch --help`.
func TestDispatchMCPServerIsWiredButHidden(t *testing.T) {
	cmd := newDispatchCmd()
	child, _, err := cmd.Find([]string{dispatchMCPServerCommand})
	if err != nil || child == cmd || child.Name() != dispatchMCPServerCommand {
		t.Fatalf("mcp-server not wired: child=%v err=%v", child, err)
	}
	if !child.Hidden {
		t.Fatal("mcp-server must stay hidden from the public dispatch surface")
	}
	cmd.SetArgs([]string{"--help"})
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("help error: %v", err)
	}
	if strings.Contains(stdout.String(), dispatchMCPServerCommand) {
		t.Fatalf("mcp-server leaked into dispatch help:\n%s", stdout.String())
	}
}

func TestDispatchPromptFlagsAreWiredOnStartAndContinue(t *testing.T) {
	cmd := newDispatchCmd()
	for _, name := range []string{"start", "continue"} {
		child, _, err := cmd.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		if child.Flags().Lookup("prompt") == nil || child.Flags().Lookup("prompt-file") == nil {
			t.Fatalf("%s prompt flags missing", name)
		}
	}
}

func TestDispatchCommandErrorBranches(t *testing.T) {
	cmd := newDispatchCmd()
	if err := dispatchCommandError(cmd, nil); err != nil {
		t.Fatalf("nil dispatch error = %v", err)
	}
	sentinel := errors.New("plain failure")
	if err := dispatchCommandError(cmd, sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("expected passthrough, got %v", err)
	}
	cmd.SetErr(failingWriter{})
	err := dispatchCommandError(cmd, &agentdispatch.ExitError{Code: 64, Message: "dispatch failed"})
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected stderr write failure, got %v", err)
	}
}

func TestDispatchStartMaxDepthFlagValidation(t *testing.T) {
	newReservationTestRepo(t, "")
	t.Setenv(clients.EnvDispatchActive, "0")
	t.Setenv(clients.EnvDispatchMaxDepth, "")

	cmd := newDispatchCmd()
	startCmd, _, err := cmd.Find([]string{"start"})
	if err != nil {
		t.Fatal(err)
	}
	if startCmd.Flags().Lookup("max-depth") == nil {
		t.Fatal("start --max-depth flag missing")
	}

	for _, arg := range []string{"0", "-1", "bogus", ""} {
		t.Run("max_depth="+arg, func(t *testing.T) {
			_, stderr, code := runDispatchCLI(t, "start", "--agent", "codex", "--max-depth="+arg, "--prompt", "Review")
			if code != agentdispatch.ExitUsage {
				t.Fatalf("exit code = %d, want %d (stderr: %q)", code, agentdispatch.ExitUsage, stderr)
			}
		})
	}
}

func TestDispatchCLIBlocksMutatingOpsAtCapAndAllowsReadOnly(t *testing.T) {
	newReservationTestRepo(t, "")
	t.Setenv(clients.EnvDispatchActive, "0")
	t.Setenv(clients.EnvDispatchMaxDepth, "")
	t.Setenv("AL_NO_NETWORK", "1")

	// Reserve one unstarted handle at depth 0 so inspect/wait/cancel/start --reservation have a real target.
	reserved, stderr, code := runDispatchCLI(t, "reserve")
	if code != 0 || reserved.Handle == "" || reserved.InvocationID == "" {
		t.Fatalf("setup reserve failed: code=%d res=%+v stderr=%q", code, reserved, stderr)
	}

	// Switch to at-cap worker environment.
	t.Setenv(clients.EnvDispatchActive, "1")
	t.Setenv(clients.EnvDispatchMaxDepth, "1")

	mutating := [][]string{
		{"reserve"},
		{"reserve", "--continue", reserved.Handle},
		{"start", "--agent", "codex", "--prompt", "Blocked"},
		{"start", "--agent", "codex", "--reservation", reserved.Handle, "--prompt", "Blocked"},
		{"continue", reserved.Handle, "--prompt", "Blocked"},
		{"continue", "--reservation", reserved.InvocationID, "--prompt", "Blocked"},
		{"cancel", reserved.Handle},
	}
	for _, args := range mutating {
		t.Run("blocked_"+strings.Join(args[:min(2, len(args))], "_"), func(t *testing.T) {
			_, stderr, code := runDispatchCLI(t, args...)
			if code != agentdispatch.ExitNested {
				t.Fatalf("al dispatch %v: exit code = %d, want %d (stderr: %q)", args, code, agentdispatch.ExitNested, stderr)
			}
			if !strings.Contains(stderr, "nested dispatch is blocked") {
				t.Fatalf("al dispatch %v: stderr = %q, want depth-cap message", args, stderr)
			}
		})
	}

	// Read-only operations remain allowed at cap (never ExitNested).
	inspected, stderr, code := runDispatchCLI(t, "inspect", reserved.Handle)
	if code != 0 || inspected.State != "reserved" {
		t.Fatalf("inspect at cap failed: code=%d state=%q stderr=%q", code, inspected.State, stderr)
	}
	_, stderr, code = runDispatchCLI(t, "options")
	if code != 0 {
		t.Fatalf("options at cap failed: code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = runDispatchCLI(t, "output", reserved.InvocationID, "--artifact", "events")
	if code == agentdispatch.ExitNested {
		t.Fatalf("output at cap was unexpectedly blocked with ExitNested: stderr=%q", stderr)
	}
	_, stderr, code = runDispatchCLI(t, "quota")
	if code == agentdispatch.ExitNested {
		t.Fatalf("quota at cap was unexpectedly blocked with ExitNested: stderr=%q", stderr)
	}
}
