package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestRootedMCPWorkingDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".agent-layer"), 0o750); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(root, "nested")
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
	spaced := filepath.Join(root, "nested ")
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
	for _, name := range []string{"options", "start", "wait", "continue", "cancel"} {
		child, _, err := cmd.Find([]string{name})
		if err != nil || child == cmd || child.Name() != name {
			t.Fatalf("dispatch subcommand %q not wired: child=%v err=%v", name, child, err)
		}
	}
	for _, removed := range []string{"fanout", "resume", "inspect", "history", "list", "delete"} {
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
