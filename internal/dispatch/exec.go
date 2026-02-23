package dispatch

import "syscall"

var syscallExec = syscall.Exec

// execBinary replaces the current process with the target binary.
func execBinary(path string, args []string, env []string, _ func(int)) error {
	return syscallExec(path, args, env)
}
