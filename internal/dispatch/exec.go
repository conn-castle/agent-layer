package dispatch

import "syscall"

// execBinary replaces the current process with the target binary.
func execBinary(path string, args []string, env []string, _ func(int)) error {
	return syscall.Exec(path, args, env)
}
