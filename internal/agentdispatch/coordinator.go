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

// drainExecutionHandles waits for every launched execution and reports each
// completion in arrival order. Consumers may encounter reconciliation errors,
// but those errors must not shorten the lifetime of any launched execution.
func drainExecutionHandles(handles []executionHandle, consume func(index int, handle executionHandle, err error)) {
	type indexedResult struct {
		index int
		err   error
	}
	results := make(chan indexedResult, len(handles))
	for index, handle := range handles {
		go func(index int, handle executionHandle) {
			results <- indexedResult{index: index, err: handle.await()}
		}(index, handle)
	}
	for range handles {
		completed := <-results
		consume(completed.index, handles[completed.index], completed.err)
	}
}
