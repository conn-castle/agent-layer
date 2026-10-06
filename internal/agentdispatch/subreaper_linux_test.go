package agentdispatch

import "golang.org/x/sys/unix"

// Only the test harness adopts orphaned providers; production is unchanged.
func enableTestSubreaper() error {
	return unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
}
