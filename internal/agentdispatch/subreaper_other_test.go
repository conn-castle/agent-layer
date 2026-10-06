//go:build !linux

package agentdispatch

// Other supported platforms leave orphan reaping to the system's init process.
func enableTestSubreaper() error { return nil }
