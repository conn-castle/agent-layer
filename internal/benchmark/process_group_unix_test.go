//go:build !windows

package benchmark

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestBenchmarkCommandCancellationTerminatesTheProcessGroup(t *testing.T) {
	notStarted := exec.CommandContext(context.Background(), "sleep", "30")
	finishNotStarted := configureBenchmarkCommandCancellation(notStarted)
	defer finishNotStarted()
	if notStarted.SysProcAttr == nil || !notStarted.SysProcAttr.Setpgid || notStarted.WaitDelay != benchmarkPierShutdownGrace {
		t.Fatalf("cancellation configuration=%#v wait=%s", notStarted.SysProcAttr, notStarted.WaitDelay)
	}
	if err := notStarted.Cancel(); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("cancel before start=%v", err)
	}

	command := exec.CommandContext(context.Background(), "sleep", "30")
	finish := configureBenchmarkCommandCancellation(command)
	defer finish()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	if err := command.Cancel(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("cancelled benchmark process exited successfully")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGTERM {
		t.Fatalf("cancelled benchmark process=%v", err)
	}
	finish()
}
