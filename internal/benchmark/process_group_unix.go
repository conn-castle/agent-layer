//go:build !windows

package benchmark

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const benchmarkPierShutdownGrace = 2 * time.Minute

func configureBenchmarkCommandCancellation(command *exec.Cmd) func() {
	done := make(chan struct{})
	var finishOnce sync.Once
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		pid := command.Process.Pid
		err := syscall.Kill(-pid, syscall.SIGTERM)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		if err == nil {
			go func() {
				timer := time.NewTimer(benchmarkPierShutdownGrace)
				defer timer.Stop()
				select {
				case <-done:
				case <-timer.C:
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
			}()
		}
		return err
	}
	// Pier handles SIGTERM by stopping its environments, repairing bind-mount
	// ownership, and asking Compose to remove the trial. Keep the process group
	// alive long enough for that shielded teardown before os/exec escalates.
	command.WaitDelay = benchmarkPierShutdownGrace
	return func() { finishOnce.Do(func() { close(done) }) }
}
