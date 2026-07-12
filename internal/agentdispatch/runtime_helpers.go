package agentdispatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
)

func defaultProviderCommandFactory(name string, args ...string) *exec.Cmd {
	// #nosec G204 -- the provider binary comes from the fixed Agent Dispatch registry.
	return exec.CommandContext(context.Background(), name, args...)
}

func prepareProviderProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func signalProviderProcess(cmd *exec.Cmd, sig os.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if syscallSig, ok := sig.(syscall.Signal); ok {
		if err := syscall.Kill(-cmd.Process.Pid, syscallSig); err == nil {
			return
		}
	}
	_ = cmd.Process.Signal(sig)
}

func installProviderSignalForwarder(cmd *exec.Cmd) (caught func() os.Signal, stop func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	exited := make(chan struct{})
	var (
		mu    sync.Mutex
		first os.Signal
	)
	go func() {
		defer close(exited)
		for {
			select {
			case sig := <-signals:
				signalProviderProcess(cmd, sig)
				mu.Lock()
				if first == nil {
					first = sig
				}
				mu.Unlock()
			case <-done:
				return
			}
		}
	}()
	return func() os.Signal {
			mu.Lock()
			defer mu.Unlock()
			return first
		}, func() {
			signal.Stop(signals)
			close(done)
			<-exited
		}
}

func providerStartError(target string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		meta, ok := lookupTarget(target)
		binary := target
		if ok {
			binary = meta.Binary
		}
		return exitError(ExitUnavailable, fmt.Sprintf("`al dispatch` target %s requires `%s` on PATH", target, binary))
	}
	return wrapExitError(ExitTargetFailure, fmt.Sprintf("wait for %s: %v", target, err), err)
}

func providerWaitError(target string, err error) error {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		if code <= 0 {
			code = 1
		}
		return exitError(ExitTargetFailure, fmt.Sprintf("%s exited with code %d; `al dispatch` exiting 70", target, code))
	}
	return wrapExitError(ExitTargetFailure, fmt.Sprintf("start %s: %v", target, err), err)
}
