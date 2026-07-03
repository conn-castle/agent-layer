package clients

import "syscall"

// ExecHandoff replaces the current al process with the target binary.
// On success it never returns. argv[0] must be the program name.
func ExecHandoff(path string, argv []string, env []string) error {
	return syscall.Exec(path, argv, env) // #nosec G204 -- launchers resolve the target path with LookPath before exec handoff.
}
