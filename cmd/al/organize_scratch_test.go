package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/organizescratch"
)

// captureOrganizeScratch swaps in a recorder for the organizer and returns the
// options the command hands it.
func captureOrganizeScratch(t *testing.T) *organizescratch.Options {
	t.Helper()
	original := runOrganizeScratch
	captured := &organizescratch.Options{}
	runOrganizeScratch = func(_ context.Context, opts organizescratch.Options, _, _ io.Writer) error {
		*captured = opts
		return nil
	}
	t.Cleanup(func() { runOrganizeScratch = original })
	return captured
}

func TestOrganizeScratchCommandIsHiddenButRegistered(t *testing.T) {
	// Hidden keeps `al --help` focused on the documented workflow; registered is
	// what makes it callable by anyone who finds it in the repo.
	root := newRootCmd()
	command, _, err := root.Find([]string{organizeScratchCommandName})
	if err != nil {
		t.Fatalf("find %s: %v", organizeScratchCommandName, err)
	}
	if !command.Hidden {
		t.Fatalf("%s must be hidden", organizeScratchCommandName)
	}
}

func TestOrganizeScratchLongHelpStatesSafetyBoundaries(t *testing.T) {
	command := newOrganizeScratchCmd()
	for _, fact := range []string{
		"strictly read-only",
		"containing tracked content is refused",
		"over 100",
		"250 MiB",
		"Predicted dry-run collisions",
		"Symlinks are never rewritten",
	} {
		if !strings.Contains(command.Long, fact) {
			t.Errorf("long help does not state %q:\n%s", fact, command.Long)
		}
	}
}

func TestOrganizeScratchCommandRequiresRoot(t *testing.T) {
	// This command moves real files, so it must never pick a directory itself.
	captureOrganizeScratch(t)
	command := newOrganizeScratchCmd()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "requires --root") {
		t.Fatalf("Execute error = %v, want a missing --root error", err)
	}
}

func TestOrganizeScratchCommandPassesFlagsThrough(t *testing.T) {
	captured := captureOrganizeScratch(t)
	command := newOrganizeScratchCmd()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{
		"--root", "  /tmp/scratch  ",
		"--apply",
		"--move-worktrees",
		"--min-group", "3",
		"--keep", "venv, .venv,,node_modules",
	})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if captured.Root != "/tmp/scratch" || !captured.Apply || !captured.MoveWorktrees || captured.MinGroup != 3 {
		t.Fatalf("options = %+v, want the flags forwarded verbatim", *captured)
	}
	if strings.Join(captured.Keep, "|") != "venv|.venv|node_modules" {
		t.Fatalf("Keep = %v, want the blank field from the doubled comma dropped", captured.Keep)
	}
}

func TestOrganizeScratchCommandRejectsNestedKeepPath(t *testing.T) {
	for _, keep := range []string{"sub/dir", `sub\dir`} {
		t.Run(keep, func(t *testing.T) {
			command := newOrganizeScratchCmd()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs([]string{"--root", t.TempDir(), "--keep", keep})
			err := command.Execute()
			if err == nil || !strings.Contains(err.Error(), "must be a top-level name without path separators") {
				t.Fatalf("Execute error = %v, want an actionable invalid --keep error", err)
			}
		})
	}
}

func TestOrganizeScratchCommandDefaultsMinGroup(t *testing.T) {
	// The default is named rather than embedded in the run, so `--help` states it.
	captured := captureOrganizeScratch(t)
	command := newOrganizeScratchCmd()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"--root", "/tmp/scratch"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if captured.MinGroup != organizescratch.DefaultMinGroup {
		t.Fatalf("MinGroup = %d, want DefaultMinGroup (%d)", captured.MinGroup, organizescratch.DefaultMinGroup)
	}
	if captured.Apply {
		t.Fatal("Apply must default to false so the first run is always a dry run")
	}
}

func TestOrganizeScratchCommandAccumulatesRepeatedKeep(t *testing.T) {
	// The script this replaced accumulated every --keep occurrence. Last-one-wins
	// would silently relocate a path the caller explicitly asked to protect.
	captured := captureOrganizeScratch(t)
	command := newOrganizeScratchCmd()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"--root", "/tmp/scratch", "--keep", "venv", "--keep", ".venv,node_modules"})
	if err := command.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Join(captured.Keep, "|") != "venv|.venv|node_modules" {
		t.Fatalf("Keep = %v, want every occurrence retained", captured.Keep)
	}
}
