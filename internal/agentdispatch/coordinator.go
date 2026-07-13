package agentdispatch

// executionHandle is an in-process awaitable execution. It is deliberately
// not exposed as a detached public lifecycle contract.
type executionHandle struct {
	runID string
	done  <-chan error
}

func launchExecution(request dispatchExecution) executionHandle {
	done := make(chan error, 1)
	go func() {
		done <- executeDispatch(request)
		close(done)
	}()
	return executionHandle{runID: request.Run.Record.ID, done: done}
}

func (handle executionHandle) await() error { return <-handle.done }
