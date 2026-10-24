//go:build unix

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/agentdispatch"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestDispatchPromptFileCancellationCLI(t *testing.T) {
	for _, command := range []string{"start", "continue"} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(root)
			fifo := testutil.MakeFIFO(t, filepath.Join(root, "prompt"), false)
			cmd := newDispatchCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			args := []string{command}
			if command == "start" {
				args = append(args, "--agent", "codex", "--model", "fixture")
			} else {
				args = append(args, "tiny-round-capacitor")
			}
			cmd.SetArgs(append(args, "--prompt-file", fifo))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timer := time.AfterFunc(100*time.Millisecond, cancel)
			defer timer.Stop()
			err := testutil.BoundedFIFO(t, fifo, func() error { return cmd.ExecuteContext(ctx) })
			var exit *SilentExitError
			if !errors.As(err, &exit) || exit.Code != agentdispatch.ExitUnavailable {
				t.Fatalf("dispatch %s error = %v, want exit 69", command, err)
			}
		})
	}
}
