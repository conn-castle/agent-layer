package agentdispatch

import "sync"

// The SDK's stdio connection learns the negotiated protocol version through a
// package-private sessionUpdated hook (the unexported serverConnection
// interface, private through go-sdk v1.8.0). A Connection decorator cannot
// forward it, and lifecycle diagnostics must observe Connection reads and
// writes, so the SDK alone would accept batches for every protocol version.
// Preserve its negotiated batching restriction using frame shape only, leaving
// parsing and validation to the SDK. This observer never buffers protocol
// bytes or field values. It duplicates only the SDK's batch frame shape and
// batch item expansion;
// TestMCPLifecyclePreservesSDKBatches compares them with the undecorated SDK.
// An exported session-state hook for Connection decorators would remove them
// (https://github.com/modelcontextprotocol/go-sdk/issues/1328).
type mcpFrameObserver struct {
	mu          sync.Mutex
	depth       int
	quoted      bool
	escaped     bool
	batch       bool
	itemStarted bool
	items       int
	frames      []mcpFrame
	remaining   int
}

type mcpFrame struct {
	batch bool
	items int
}

func (f *mcpFrameObserver) read(data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range data {
		if f.quoted {
			switch {
			case f.escaped:
				f.escaped = false
			case b == '\\':
				f.escaped = true
			case b == '"':
				f.quoted = false
			}
			continue
		}
		if b == ' ' || b == '\n' || b == '\r' || b == '\t' {
			continue
		}
		if f.depth == 0 {
			// Every valid JSON-RPC frame is an object or array. The SDK rejects
			// all other values, so they need no frame metadata.
			if b != '{' && b != '[' {
				continue
			}
			f.batch, f.itemStarted, f.items = b == '[', false, 0
		} else if f.batch && f.depth == 1 {
			if b == ',' {
				f.itemStarted = false
			} else if b != ']' && !f.itemStarted {
				f.items++
				f.itemStarted = true
			}
		}
		switch b {
		case '"':
			f.quoted = true
		case '{', '[':
			f.depth++
		case '}', ']':
			f.depth--
			if f.depth == 0 {
				items := f.items
				if !f.batch {
					items = 1
				}
				f.frames = append(f.frames, mcpFrame{f.batch, items})
			}
		}
	}
}

// next returns true once for the first decoded message in each batch. Older
// protocol versions expand a batch into several consecutive SDK Read results.
func (f *mcpFrameObserver) next() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.remaining > 0 {
		f.remaining--
		return false
	}
	if len(f.frames) == 0 {
		return false
	}
	frame := f.frames[0]
	f.frames = f.frames[1:]
	f.remaining = frame.items - 1
	return frame.batch
}
