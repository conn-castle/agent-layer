//go:build unix

package agentdispatch

import (
	"os"
	"os/signal"
	"syscall"
)

// Let fd1 EPIPE reach the transport observer instead of terminating the process
// before diagnostics are synced. Notify/Stop preserve other signal subscribers.
func suppressMCPSIGPIPE() func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGPIPE)
	return func() { signal.Stop(signals) }
}
