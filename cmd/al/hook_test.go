package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/chime"
)

func TestHookCommandIsHidden(t *testing.T) {
	root := newRootCmd()
	hook, _, err := root.Find([]string{"hook"})
	if err != nil {
		t.Fatalf("find hook command: %v", err)
	}
	if !hook.Hidden {
		t.Fatal("hook command must be hidden")
	}
	child, _, err := root.Find([]string{"hook", "chime"})
	if err != nil {
		t.Fatalf("find hook chime command: %v", err)
	}
	if !child.Hidden {
		t.Fatal("hook chime command must be hidden")
	}
}

func TestHookChimeCommandWiresStreams(t *testing.T) {
	original := chimeSoundRunner
	plays := 0
	chimeSoundRunner = chime.SoundRunnerFunc(func() error {
		plays++
		return nil
	})
	t.Cleanup(func() { chimeSoundRunner = original })

	cmd := newHookChimeCmd()
	cmd.SetArgs([]string{chime.ProviderCodex})
	cmd.SetIn(strings.NewReader(`{"hook_event_name":"Stop","stop_hook_active":false}`))
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if plays != 1 || stdout.String() != "{}\n" || stderr.Len() != 0 {
		t.Fatalf("plays=%d stdout=%q stderr=%q", plays, stdout.String(), stderr.String())
	}
}

func TestHookChimeCommandPropagatesHandlerErrors(t *testing.T) {
	cmd := newHookChimeCmd()
	cmd.SetArgs([]string{"unsupported"})
	cmd.SetIn(strings.NewReader(`{}`))
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "unsupported provider") {
		t.Fatalf("Execute error = %v, want unsupported provider", err)
	}
}
