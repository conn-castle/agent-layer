package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/messages"
)

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
