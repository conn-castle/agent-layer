//go:build windows

package benchmark

import "os/exec"

func configureBenchmarkCommandCancellation(_ *exec.Cmd) func() { return func() {} }
