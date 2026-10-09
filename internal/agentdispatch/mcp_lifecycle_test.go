package agentdispatch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func readMCPLifecycle(t *testing.T, root string) [][]mcpLifecycleRecord {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(root, ".agent-layer", "state", "dispatch-mcp", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	records := make([][]mcpLifecycleRecord, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("lifecycle permissions: %v", info.Mode())
		}
		data, err := os.ReadFile(path) //nolint:gosec // Test-owned lifecycle artifact.
		if err != nil {
			t.Fatal(err)
		}
		var instance []mcpLifecycleRecord
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			var r mcpLifecycleRecord
			if err := json.Unmarshal(line, &r); err != nil {
				t.Fatal(err)
			}
			if r.Schema != 1 || r.ConnectionID == "" || r.PID != os.Getpid() || r.ParentPID <= 0 || r.Version != "lifecycle-test" || r.Timestamp.IsZero() || r.Timestamp.Location() != time.UTC {
				t.Fatalf("invalid metadata: %+v", r)
			}
			if len(instance) > 0 && r.ConnectionID != instance[0].ConnectionID {
				t.Fatal("uncorrelated record")
			}
			instance = append(instance, r)
		}
		records = append(records, instance)
	}
	if len(paths) > 0 {
		info, err := os.Stat(filepath.Dir(paths[0]))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("directory permissions: %v", info.Mode())
		}
	}
	return records
}

func waitMCPRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("MCP server did not stop")
		return nil
	}
}

func TestMCPLifecycleRealTransport(t *testing.T) {
	for _, ending := range []string{"eof", "cancel", "deadline"} {
		t.Run(ending, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if ending == "deadline" {
				// Outlast Connect, discovery, dispatch_start, and the start
				// record. This deadline is what stops the server.
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 2*time.Second)
				defer stop()
			}
			input, clientWriter := io.Pipe()
			clientReader, output := io.Pipe()
			defer func() { _ = clientWriter.Close() }()
			defer func() { _ = clientReader.Close() }()
			var stderr bytes.Buffer
			done := make(chan error, 1)
			go func() {
				done <- runMCPServer(ctx, MCPServerOptions{Root: root, Version: "lifecycle-test", Env: []string{"SECRET=environment-canary", "TOKEN=credential-canary"}}, input, output, &stderr)
			}()
			clientCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			client, err := mcp.NewClient(&mcp.Implementation{Name: "private-client", Version: "test"}, nil).Connect(clientCtx, &mcp.IOTransport{Reader: clientReader, Writer: clientWriter}, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			listed, err := client.ListTools(clientCtx, nil)
			if err != nil || len(listed.Tools) != 7 {
				t.Fatalf("discovery: %v %v", listed, err)
			}
			result, err := client.CallTool(clientCtx, &mcp.CallToolParams{Name: ToolStart, Arguments: StartInput{Agent: "private-provider-canary", Prompt: "private-prompt-canary"}})
			if err != nil || !result.IsError || !strings.Contains(toolResultText(result), "private-provider-canary") {
				t.Fatalf("tool response: %v %v", result, err)
			}
			active := readMCPLifecycle(t, root)
			if len(active) != 1 || len(active[0]) != 1 || active[0][0].Event != "start" {
				t.Fatalf("start not durable while serving: %+v", active)
			}
			switch ending {
			case "eof":
				_ = clientWriter.Close()
			case "cancel":
				cancel()
			}
			runErr := waitMCPRun(t, done)
			want := "client_eof"
			if ending == "cancel" {
				want = mcpLifecycleCancelled
				if !errors.Is(runErr, context.Canceled) {
					t.Fatal(runErr)
				}
			}
			if ending == "deadline" {
				want = "context_deadline"
				if !errors.Is(runErr, context.DeadlineExceeded) {
					t.Fatal(runErr)
				}
			}
			records := readMCPLifecycle(t, root)[0]
			last := records[len(records)-1]
			if last.Event != "stop" || last.Condition != want || last.Uncertain || (ending != "eof" && (last.ClientEOF || last.InputEOF)) {
				t.Fatalf("stop: %+v", last)
			}
			data, err := json.Marshal(records)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"private-client", "private-provider-canary", "private-prompt-canary", "environment-canary", "credential-canary"} {
				if bytes.Contains(data, []byte(secret)) {
					t.Fatalf("private data retained: %s", secret)
				}
			}
			if stderr.Len() != 0 {
				t.Fatalf("unexpected stderr: %s", stderr.String())
			}
		})
	}
}

func TestMCPLifecycleFailuresAndConcurrentInstances(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	done := make(chan error, 2)
	for _, input := range []string{`{"jsonrpc":"2.0","method":"private-malformed-canary"`, `{"jsonrpc":"2.0","id":{"private-invalid-id-canary":true},"method":"initialize"}` + "\n"} {
		go func() {
			done <- runMCPServer(context.Background(), MCPServerOptions{Root: root, Version: "lifecycle-test"}, io.NopCloser(strings.NewReader(input)), io.Discard, io.Discard)
		}()
	}
	for range 2 {
		if waitMCPRun(t, done) == nil {
			t.Fatal("malformed input error lost")
		}
	}
	instances := readMCPLifecycle(t, root)
	if len(instances) != 2 || instances[0][0].ConnectionID == instances[1][0].ConnectionID {
		t.Fatalf("instances: %+v", instances)
	}
	for _, records := range instances {
		if len(records) != 3 || records[1].Event != mcpLifecycleEventError || records[1].Operation != "read" || records[2].Condition != "transport_error" || records[2].ClientEOF {
			t.Fatalf("malformed input classified as clean EOF: %+v", records)
		}
		data, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("canary")) {
			t.Fatal("malformed input retained")
		}
	}
}

func TestMCPLifecycleStartupAndPersistenceFailures(t *testing.T) {
	t.Run("configuration", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".agent-layer", "config.toml"), []byte("private-config-canary = ["), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout bytes.Buffer
		err := runMCPServer(context.Background(), MCPServerOptions{Root: root, Version: "lifecycle-test"}, io.NopCloser(strings.NewReader("")), &stdout, io.Discard)
		if err == nil || stdout.Len() != 0 {
			t.Fatalf("startup: %v stdout %s", err, stdout.String())
		}
		records := readMCPLifecycle(t, root)[0]
		if len(records) != 3 || records[1].Condition != "startup_error" || records[2].Condition != "startup_error" {
			t.Fatalf("startup records: %+v", records)
		}
	})
	t.Run("unwritable", func(t *testing.T) {
		root := writeDispatchRepo(t, dispatchRepoConfig{})
		if err := os.WriteFile(filepath.Join(root, ".agent-layer", "state"), []byte("private-path-canary"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		err := runMCPServer(context.Background(), MCPServerOptions{Root: root, Version: "lifecycle-test"}, io.NopCloser(strings.NewReader("")), &stdout, &stderr)
		if err != nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), "could not be persisted") || strings.Contains(stderr.String(), root) || strings.Contains(stderr.String(), "canary") {
			t.Fatalf("best effort: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
		}
	})
	t.Run("empty-root", func(t *testing.T) {
		if err := runMCPServer(context.Background(), MCPServerOptions{Root: " "}, io.NopCloser(strings.NewReader("")), io.Discard, io.Discard); err == nil {
			t.Fatal("missing root accepted")
		}
	})
}

// A physical read failure must be recorded at the boundary, retaining the
// original error to the caller while dropping its private text from evidence.
type mcpFailingReader struct{ err error }

func (r mcpFailingReader) Read([]byte) (int, error) { return 0, r.err }
func (mcpFailingReader) Close() error               { return nil }

func TestMCPLifecycleReadFailure(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	sentinel := errors.New("private-read-error-canary")
	err := runMCPServer(context.Background(), MCPServerOptions{Root: root, Version: "lifecycle-test"}, mcpFailingReader{sentinel}, io.Discard, io.Discard)
	if !errors.Is(err, sentinel) {
		t.Fatalf("original read error lost: %v", err)
	}
	records := readMCPLifecycle(t, root)[0]
	if len(records) != 3 || records[1].Operation != "read" || records[2].Condition != "transport_error" {
		t.Fatalf("read failure: %+v", records)
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(sentinel.Error())) {
		t.Fatal("raw error retained")
	}
}

type mcpDataEOFReader struct{ data []byte }

func (r *mcpDataEOFReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) == 0 {
		return n, io.EOF
	}
	return n, nil
}
func (*mcpDataEOFReader) Close() error { return nil }

func TestMCPLifecycleDataWithPhysicalEOF(t *testing.T) {
	for _, input := range []string{"{}\n", `{"private-partial-canary":`} {
		t.Run(input, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			// Both inputs are invalid protocol. n>0, EOF must not turn a parser
			// failure into a clean disconnect merely because physical EOF was seen.
			err := runMCPServer(context.Background(), MCPServerOptions{Root: root, Version: "lifecycle-test"}, &mcpDataEOFReader{[]byte(input)}, io.Discard, io.Discard)
			if err == nil {
				t.Fatal("invalid protocol accepted")
			}
			records := readMCPLifecycle(t, root)[0]
			if records[len(records)-1].Condition != "transport_error" || records[len(records)-1].ClientEOF {
				t.Fatalf("n>0 EOF misclassified: %+v", records)
			}
		})
	}
}

// Decorating an SDK Connection hides its private protocol-state hook, so
// mcp_framing.go duplicates the SDK's batch restriction. Each handshake runs
// through the undecorated SDK too: a go-sdk upgrade that changes negotiation
// or batching fails here instead of silently diverging.
func TestMCPLifecyclePreservesSDKBatches(t *testing.T) {
	initialize := func(version string) string {
		return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"test","version":"test"}}}`, version)
	}
	// Per-request metadata exercises SDK discovery without a legacy initialize.
	discover := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{%q:"2026-07-28",%q:{}}}}`, mcp.MetaKeyProtocolVersion, mcp.MetaKeyClientCapabilities)
	for _, tc := range []struct {
		name, handshake string
		rejected        bool
	}{
		{"empty", initialize(""), true},
		{"2024-11-05", initialize("2024-11-05"), false},
		{"2025-03-26", initialize("2025-03-26"), false},
		{"2025-06-18", initialize("2025-06-18"), true},
		{"2025-11-25", initialize("2025-11-25"), true},
		{"2026-07-28", initialize("2026-07-28"), true},
		{"unsupported", initialize("private-unsupported-version-canary"), true},
		{"discover-2026-07-28", discover, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			opts := MCPServerOptions{Root: root, Version: "lifecycle-test"}
			sdkErr := exchangeMCPBatch(t, tc.handshake, func(ctx context.Context, input io.ReadCloser, output io.Writer) error {
				server, err := newDispatchMCPServer(opts)
				if err != nil {
					return err
				}
				return server.Run(ctx, &mcp.IOTransport{Reader: input, Writer: mcpNopCloseWriter{output}})
			})
			if (sdkErr != nil) != tc.rejected {
				t.Fatalf("undecorated SDK batch error = %v, want rejected=%v", sdkErr, tc.rejected)
			}
			observedErr := exchangeMCPBatch(t, tc.handshake, func(ctx context.Context, input io.ReadCloser, output io.Writer) error {
				return runMCPServer(ctx, opts, input, output, io.Discard)
			})
			if fmt.Sprint(observedErr) != fmt.Sprint(sdkErr) {
				t.Fatalf("decorated batch error = %v, undecorated SDK = %v", observedErr, sdkErr)
			}
			want := "client_eof"
			if tc.rejected {
				want = "transport_error"
			}
			records := readMCPLifecycle(t, root)
			if len(records) != 1 || records[0][len(records[0])-1].Condition != want {
				t.Fatalf("batch lifecycle: %+v", records)
			}
		})
	}
}

// exchangeMCPBatch sends handshake and one batch to serve. A rejected batch
// returns serve's error; an accepted batch must answer every request and leave
// the session usable until a clean client EOF.
func exchangeMCPBatch(t *testing.T, handshake string, serve func(context.Context, io.ReadCloser, io.Writer) error) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, clientWriter := io.Pipe()
	clientReader, output := io.Pipe()
	defer func() { _ = clientWriter.Close(); _ = clientReader.Close() }()
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, input, output)
	}()
	lines := make(chan []byte, 4)
	go func() {
		scanner := bufio.NewScanner(clientReader)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			lines <- bytes.Clone(scanner.Bytes())
		}
		close(lines)
	}()
	receive := func() []byte {
		t.Helper()
		select {
		case data, ok := <-lines:
			if !ok {
				t.Fatal("missing protocol response")
			}
			return data
		case <-time.After(5 * time.Second):
			t.Fatal("protocol response timeout")
			return nil
		}
	}
	send := func(message string) {
		t.Helper()
		if _, err := io.WriteString(clientWriter, message+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	send(handshake)
	if !json.Valid(receive()) {
		t.Fatal("invalid handshake stdout")
	}
	send(`[{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"dispatch_start","arguments":{"agent":"unknown-private-batch-canary","prompt":"quote: \"; braces: {} []; escape: \\"}}},{"jsonrpc":"2.0","id":3,"method":"tools/list"}]`)
	var batch []byte
	select {
	case err := <-done:
		return err
	case data, ok := <-lines:
		if !ok {
			t.Fatal("missing batch response")
		}
		batch = data
	case <-time.After(5 * time.Second):
		t.Fatal("batch response timeout")
	}
	var responses []struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(batch, &responses); err != nil || len(responses) != 2 {
		t.Fatalf("batch responses: %+v %v", responses, err)
	}
	gotIDs := map[int]bool{}
	for _, response := range responses {
		gotIDs[response.ID] = true
	}
	if !gotIDs[2] || !gotIDs[3] {
		t.Fatalf("batch response IDs: %+v", responses)
	}
	send(`{"jsonrpc":"2.0","id":4,"method":"tools/list"}`)
	var response struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(receive(), &response); err != nil || response.ID != 4 {
		t.Fatalf("post-batch response: %+v %v", response, err)
	}
	_ = clientWriter.Close()
	return waitMCPRun(t, done)
}

func TestMCPLifecycleErrorBeforeInflightWorkFinishes(t *testing.T) {
	testMCPLifecycleInflightShutdown(t, false)
}

func TestMCPLifecycleCompetingEOFAndCancellation(t *testing.T) {
	testMCPLifecycleInflightShutdown(t, true)
}

func testMCPLifecycleInflightShutdown(t *testing.T, compete bool) {
	t.Helper()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	opts := MCPServerOptions{Root: root, Version: "lifecycle-test"}
	server, err := newDispatchMCPServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	mcp.AddTool(server, &mcp.Tool{Name: "hold", Description: "test in-flight shutdown"}, func(context.Context, *mcp.CallToolRequest, OptionsInput) (*mcp.CallToolResult, any, error) {
		close(entered)
		<-release
		return &mcp.CallToolResult{}, nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, clientWriter := io.Pipe()
	clientReader, output := io.Pipe()
	defer func() { _ = clientWriter.Close(); _ = clientReader.Close() }()
	l := newMCPLifecycle(opts, io.Discard)
	done := make(chan error, 1)
	go func() { done <- serveMCPServer(ctx, server, l, input, output) }()
	clientCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(clientCtx, &mcp.IOTransport{Reader: clientReader, Writer: clientWriter}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		unblock()
		cancel()
		_ = clientWriter.Close()
		_ = clientReader.Close()
		_ = client.Close()
	}()
	callDone := make(chan error, 1)
	go func() {
		_, err := client.CallTool(clientCtx, &mcp.CallToolParams{Name: "hold", Arguments: OptionsInput{}})
		callDone <- err
	}()
	select {
	case <-entered:
	case <-clientCtx.Done():
		t.Fatal("handler did not start")
	}
	if compete {
		_ = clientWriter.Close()
	} else {
		if _, err := io.WriteString(clientWriter, `{"private-delayed-error-canary":`+"\n"); err != nil {
			t.Fatal(err)
		}
		_ = clientWriter.Close()
	}
	// Inspect the synchronized observation only to wait for the real SDK read
	// boundary. The assertions below concern persisted evidence and Run, not
	// recorder helper behavior.
	observed := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		observed = l.observed[mcpLifecycleRead]
		if compete {
			observed = l.framedEOF
		}
		l.mu.Unlock()
		if observed {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !observed {
		t.Fatal("SDK terminal read was not observed")
	}
	select {
	case err := <-done:
		t.Fatalf("Run returned before blocked handler released: %v", err)
	default:
	}
	if compete {
		cancel()
	} else {
		records := readMCPLifecycle(t, root)[0]
		if len(records) != 2 || records[1].Event != mcpLifecycleEventError || records[1].Condition != "transport_error" {
			t.Fatalf("failure not durable before Run returns: %+v", records)
		}
		data, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("private-delayed-error-canary")) {
			t.Fatal("parser input retained")
		}
	}
	unblock()
	_ = waitMCPRun(t, done)
	records := readMCPLifecycle(t, root)[0]
	last := records[len(records)-1]
	if compete {
		if last.Condition != mcpLifecycleUncertain || !last.Uncertain || !last.ClientEOF || last.ContextCondition != mcpLifecycleCancelled {
			t.Fatalf("competing observations lost: %+v", last)
		}
	} else if last.Condition != "transport_error" {
		t.Fatalf("delayed error lost: %+v", last)
	}
	_ = clientReader.Close()
	_ = waitMCPRun(t, callDone)
}

func TestMCPLifecycleSurvivesDispatchRetention(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	if err := runMCPServer(context.Background(), MCPServerOptions{Root: root, Version: "lifecycle-test"}, io.NopCloser(strings.NewReader("")), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	before := readMCPLifecycle(t, root)
	path := filepath.Join(root, ".agent-layer", "state", "dispatch-mcp", before[0][0].ConnectionID+".jsonl")
	old := time.Now().Add(-365 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if err := pruneDispatchEvidence(root, time.Now(), time.Hour); err != nil {
		t.Fatal(err)
	}
	after := readMCPLifecycle(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("dispatch retention changed MCP evidence: %+v -> %+v", before, after)
	}
}

type mcpCloseFailingReader struct {
	io.Reader
	err error
}

func (r mcpCloseFailingReader) Close() error { return r.err }

func TestMCPLifecycleUnclassifiedRunError(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	sentinel := errors.New("private-close-error-canary")
	err := runMCPServer(context.Background(), MCPServerOptions{Root: root, Version: "lifecycle-test"}, mcpCloseFailingReader{strings.NewReader(""), sentinel}, io.Discard, io.Discard)
	if !errors.Is(err, sentinel) {
		t.Fatalf("close error lost: %v", err)
	}
	records := readMCPLifecycle(t, root)[0]
	last := records[len(records)-1]
	if len(records) != 3 || records[1].Event != mcpLifecycleEventError || records[1].Operation != mcpLifecycleRun || last.Condition != mcpLifecycleRunError || !last.ClientEOF || !last.Uncertain {
		t.Fatalf("EOF concealed run error: %+v", records)
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(sentinel.Error())) {
		t.Fatal("private close error retained")
	}
}

// Pause after the SDK has returned, while the production observer's operation
// is still open. This makes Close-before-observe deterministic without adding
// hooks to production code or substituting a fake SDK result.
type mcpPausedTransport struct {
	mcp.Transport
	operation                  string
	entered, returned, release chan struct{}
}

func (t *mcpPausedTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	c, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &mcpPausedConnection{Connection: c, pause: t}, nil
}

type mcpPausedConnection struct {
	mcp.Connection
	pause *mcpPausedTransport
}

func (c *mcpPausedConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	if c.pause.operation != mcpLifecycleRead {
		return c.Connection.Read(ctx)
	}
	close(c.pause.entered)
	msg, err := c.Connection.Read(ctx)
	close(c.pause.returned)
	<-c.pause.release
	return msg, err
}
func (c *mcpPausedConnection) Write(ctx context.Context, msg jsonrpc.Message) error {
	if c.pause.operation != "write" {
		return c.Connection.Write(ctx, msg)
	}
	close(c.pause.entered)
	err := c.Connection.Write(ctx, msg)
	close(c.pause.returned)
	<-c.pause.release
	return err
}

// Keep the decoder from seeing the raw read result until after SDK Close. This
// reproduces partial input + physical EOF being masked by its closed channel.
type mcpPausedReader struct {
	io.ReadCloser
	returned, release chan struct{}
}

func (r *mcpPausedReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		close(r.returned)
		<-r.release
	}
	return n, err
}

type mcpDeferredEOFReader struct{ release chan struct{} }

func (r mcpDeferredEOFReader) Read([]byte) (int, error) { <-r.release; return 0, io.EOF }
func (mcpDeferredEOFReader) Close() error               { return nil }

func waitMCPBoundary(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("SDK boundary did not arrive")
	}
}

func TestMCPLifecycleCloseDuringOperation(t *testing.T) {
	for _, ending := range []string{"clean-eof", "partial-eof", "masked-partial-eof", "parser-error", "read-error", "write-error", "closed-channel-eof"} {
		t.Run(ending, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			l := newMCPLifecycle(MCPServerOptions{Root: root, Version: "lifecycle-test"}, io.Discard)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			// Use the same reader and connection observers as production around a
			// real SDK IOTransport, with gates only in this test's transport adapter.
			input, clientWriter := io.Pipe()
			defer func() { _ = clientWriter.Close(); _ = input.Close() }()
			var reader io.ReadCloser = input
			var rawRelease chan struct{}
			var rawReturned chan struct{}
			sentinel := errors.New("private-overlap-error-canary")
			switch ending {
			case "clean-eof":
				reader = io.NopCloser(strings.NewReader(""))
			case "partial-eof", "masked-partial-eof":
				reader = io.NopCloser(strings.NewReader(`{"private-overlap-input-canary":`))
			case "read-error":
				reader = mcpFailingReader{sentinel}
			case "closed-channel-eof":
				rawRelease = make(chan struct{})
				reader = mcpDeferredEOFReader{rawRelease}
			}
			framing := &mcpFrameObserver{}
			reader = &mcpObservedReader{ReadCloser: reader, lifecycle: l, framing: framing}
			if ending == "masked-partial-eof" {
				rawRelease, rawReturned = make(chan struct{}), make(chan struct{})
				reader = &mcpPausedReader{ReadCloser: reader, returned: rawReturned, release: rawRelease}
			}
			var rawOnce sync.Once
			releaseRaw := func() {
				if rawRelease != nil {
					rawOnce.Do(func() { close(rawRelease) })
				}
			}
			defer releaseRaw()
			operation := mcpLifecycleRead
			outputReader, output := io.Pipe()
			defer func() { _ = outputReader.Close(); _ = output.Close() }()
			writer := io.Discard
			if ending == "write-error" {
				operation = "write"
				writer = output
			}
			pause := &mcpPausedTransport{Transport: &mcp.IOTransport{Reader: reader, Writer: mcpNopCloseWriter{writer}}, operation: operation, entered: make(chan struct{}), returned: make(chan struct{}), release: make(chan struct{})}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(pause.release) }) }
			defer release()
			connection, err := (&mcpObservedTransport{Transport: pause, lifecycle: l, framing: framing}).Connect(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = connection.Close() }()
			result := make(chan error, 1)
			go func() {
				if operation == mcpLifecycleRead {
					_, err := connection.Read(context.Background())
					result <- err
				} else {
					msg, err := jsonrpc.DecodeMessage([]byte(`{"jsonrpc":"2.0","method":"private-write-overlap-canary"}`))
					if err == nil {
						err = connection.Write(context.Background(), msg)
					}
					result <- err
				}
			}()
			waitMCPBoundary(t, pause.entered)
			if ending == "parser-error" {
				if _, err := io.WriteString(clientWriter, `{"jsonrpc":"2.0","id":{"private-parser-overlap-canary":true},"method":"initialize"}`+"\n"); err != nil {
					t.Fatal(err)
				}
			}
			switch ending {
			case "masked-partial-eof":
				waitMCPBoundary(t, rawReturned)
			case "closed-channel-eof", "write-error": // Keep SDK Read/Write pending until Close.
			default:
				waitMCPBoundary(t, pause.returned)
			}
			cancel()
			if err := connection.Close(); err != nil {
				t.Fatal(err)
			}
			if ending == "write-error" {
				_ = outputReader.Close()
			}
			waitMCPBoundary(t, pause.returned)
			release()
			observedErr := waitMCPRun(t, result)
			if ending == "read-error" && !errors.Is(observedErr, sentinel) {
				t.Fatalf("original read error lost: %v", observedErr)
			}
			if ending == "write-error" && !errors.Is(observedErr, io.ErrClosedPipe) {
				t.Fatalf("original write error lost: %v", observedErr)
			}
			if ending == "partial-eof" && !errors.Is(observedErr, io.ErrUnexpectedEOF) {
				t.Fatalf("partial frame error lost: %v", observedErr)
			}
			if ending == "clean-eof" || ending == "masked-partial-eof" || ending == "closed-channel-eof" {
				if !errors.Is(observedErr, io.EOF) {
					t.Fatalf("expected SDK EOF: %v", observedErr)
				}
			} else if observedErr == nil {
				t.Fatal("SDK failure lost")
			}
			// SDK Run returns cancellation in this interleaving. The observer must
			// preserve competing evidence independently of that return value.
			l.stop(ctx, "serving", ctx.Err())
			releaseRaw()
			records := readMCPLifecycle(t, root)[0]
			last := records[len(records)-1]
			wantEOF := ending == "clean-eof" || ending == "partial-eof" || ending == "masked-partial-eof"
			if last.InputEOF != wantEOF || last.ClientEOF || last.ContextCondition != mcpLifecycleCancelled {
				t.Fatalf("EOF/context evidence: %+v", records)
			}
			switch ending {
			case "closed-channel-eof":
				if len(records) != 2 || last.Condition != mcpLifecycleCancelled || last.Uncertain {
					t.Fatalf("local close invented competing evidence: %+v", records)
				}
			case "clean-eof", "masked-partial-eof":
				if len(records) != 2 || last.Condition != mcpLifecycleUncertain || !last.Uncertain {
					t.Fatalf("latched EOF lost or overclaimed clean: %+v", records)
				}
			default:
				if len(records) != 3 || records[1].Event != mcpLifecycleEventError || records[1].Operation != operation || !records[1].Uncertain || last.Condition != mcpLifecycleTransportError || !last.Uncertain {
					t.Fatalf("overlapping failure erased: %+v", records)
				}
			}
			data, err := json.Marshal(records)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte("canary")) {
				t.Fatal("overlap diagnostics retained private data")
			}
		})
	}
}
