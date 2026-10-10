//go:build unix

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/templates"
)

// Re-exec enters the real hidden command, signal context, and process fd0/fd1.
// os.Exit prevents test harness output from entering MCP stdout.
func TestDispatchMCPLifecycleHelper(t *testing.T) {
	if os.Getenv("AL_TEST_MCP_LIFECYCLE") != "1" {
		return
	}
	Version = "0.0.292"
	os.Args = []string{"al", "dispatch", "mcp-server"}
	main()
}

type cliMCPRecord struct {
	Event        string    `json:"event"`
	Timestamp    time.Time `json:"timestamp"`
	ConnectionID string    `json:"connection_id"`
	Version      string    `json:"version"`
	PID          int       `json:"pid"`
	ParentPID    int       `json:"parent_pid"`
	Condition    string    `json:"condition"`
	Operation    string    `json:"operation"`
	ClientEOF    bool      `json:"client_eof"`
	InputEOF     bool      `json:"input_eof"`
	Uncertain    bool      `json:"uncertain"`
}

func TestDispatchMCPLifecycleProcess(t *testing.T) {
	for _, ending := range []string{"eof", "sigterm", "full-eof", "full-sigterm", "partial-frame", "broken-stdout", "sigkill"} {
		t.Run(ending, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{"instructions", "skills"} {
				if err := os.Mkdir(filepath.Join(root, ".agent-layer", dir), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"env", "commands.allow"} {
				if err := os.WriteFile(filepath.Join(root, ".agent-layer", name), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			configData, err := templates.FS.ReadFile("config.toml")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".agent-layer", "config.toml"), configData, 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDispatchMCPLifecycleHelper$") //nolint:gosec // Test binary re-exec.
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "AL_TEST_MCP_LIFECYCLE=1", "AL_DEV_BYPASS_VERSION_DISPATCH=1", "PRIVATE_TOKEN=cli-credential-canary", "PRIVATE_INPUT=cli-environment-canary")
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stdin.Close() }()
			stdout, output, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = stdout.Close() }()
			defer func() { _ = output.Close() }()
			// Capture Fd before Start: Fd can change the shared nonblocking flags.
			outputReady := []unix.PollFd{{Fd: int32(output.Fd()), Events: unix.POLLOUT}} //nolint:gosec // OS descriptors fit poll's int32 field.
			cmd.Stdout = output
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(ending, "full-") {
				_ = output.Close()
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, 4096), 1024*1024)
			send := func(message string) {
				t.Helper()
				if _, err := io.WriteString(stdin, message+"\n"); err != nil {
					t.Fatal(err)
				}
			}
			receive := func(id int) json.RawMessage {
				t.Helper()
				if !scanner.Scan() {
					_ = cmd.Wait()
					t.Fatalf("missing response %d: %v (stderr: %s)", id, scanner.Err(), stderr.String())
				}
				var msg struct {
					JSONRPC string          `json:"jsonrpc"`
					ID      int             `json:"id"`
					Result  json.RawMessage `json:"result"`
					Error   json.RawMessage `json:"error"`
				}
				if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil || msg.JSONRPC != "2.0" || msg.ID != id || len(msg.Error) != 0 {
					t.Fatalf("stdout is not expected MCP response: %s (%v)", scanner.Text(), err)
				}
				return msg.Result
			}
			send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"cli-private-client-canary","version":"test"}}}`)
			initialized := receive(1)
			if !bytes.Contains(initialized, []byte("0.0.292")) {
				t.Fatalf("CLI version absent: %s", initialized)
			}
			send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
			send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
			var listed struct {
				Tools []json.RawMessage `json:"tools"`
			}
			if err := json.Unmarshal(receive(2), &listed); err != nil || len(listed.Tools) != 7 {
				t.Fatalf("tool discovery: %v, %d tools", err, len(listed.Tools))
			}
			send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"dispatch_start","arguments":{"agent":"cli-tool-response-canary","prompt":"cli-prompt-canary"}}}`)
			if !bytes.Contains(receive(3), []byte("cli-tool-response-canary")) {
				t.Fatal("missing private response canary")
			}
			// Initialization can only be read after the synchronous start write.
			paths, err := filepath.Glob(filepath.Join(root, ".agent-layer", "state", "dispatch-mcp", "*.jsonl"))
			if err != nil || len(paths) != 1 {
				t.Fatalf("start artifact: %v %v", paths, err)
			}
			start, err := os.ReadFile(paths[0])
			if err != nil || bytes.Count(start, []byte("\n")) != 1 {
				t.Fatalf("start before exit: %s %v", start, err)
			}
			full := strings.HasPrefix(ending, "full-")
			trigger := strings.TrimPrefix(ending, "full-")
			if full {
				// The echoed ID makes one reply exceed the default Linux/Darwin
				// pipe capacity, so backpressure proves its write is unfinished.
				// EOF can discard replies that have not begun writing.
				send(fmt.Sprintf(`{"jsonrpc":"2.0","id":"%s","method":"tools/list"}`, strings.Repeat("4", 128*1024)))
				for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(time.Millisecond) {
					if _, err := unix.Poll(outputReady, 0); err != nil {
						t.Fatal(err)
					}
					if outputReady[0].Revents == 0 {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("stdout never backpressured: poll events %d", outputReady[0].Revents)
					}
				}
			}
			shutdownAt := time.Now()
			switch trigger {
			case "eof":
				_ = stdin.Close()
			case "sigterm":
				if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
					t.Fatal(err)
				}
			case "sigkill":
				if err := cmd.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			case "partial-frame":
				if _, err := io.WriteString(stdin, `{"jsonrpc":"2.0","private-malformed-canary":`); err != nil {
					t.Fatal(err)
				}
				_ = stdin.Close()
			case "broken-stdout":
				if err := stdout.Close(); err != nil {
					t.Fatal(err)
				}
				send(`{"jsonrpc":"2.0","id":4,"method":"tools/list"}`)
			}
			if !full && ending != "broken-stdout" {
				for scanner.Scan() {
					var msg map[string]json.RawMessage
					if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil || string(msg["jsonrpc"]) != `"2.0"` {
						t.Fatalf("non-protocol stdout: %s", scanner.Text())
					}
				}
				if err := scanner.Err(); err != nil {
					t.Fatal(err)
				}
			}
			waitErr := cmd.Wait()
			if full && time.Since(shutdownAt) > 3*time.Second {
				t.Fatalf("backpressured shutdown exceeded bound: %v", waitErr)
			}
			if ctx.Err() != nil {
				t.Fatalf("process timed out: %s", stderr.String())
			}
			if ending == "eof" && waitErr != nil {
				t.Fatalf("EOF exit: %v %s", waitErr, stderr.String())
			}
			if ending != "eof" && waitErr == nil {
				t.Fatal("error/signal scenario exited successfully")
			}
			if ending == "broken-stdout" && cmd.ProcessState.Sys().(syscall.WaitStatus).Signaled() {
				t.Fatalf("broken stdout bypassed diagnostics: %v", waitErr)
			}
			data, err := os.ReadFile(paths[0])
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"cli-private-client-canary", "cli-tool-response-canary", "cli-prompt-canary", "cli-credential-canary", "cli-environment-canary", "private-malformed-canary"} {
				if bytes.Contains(data, []byte(secret)) {
					t.Fatalf("diagnostics retained %s", secret)
				}
			}
			var records []cliMCPRecord
			for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
				var r cliMCPRecord
				if err := json.Unmarshal(line, &r); err != nil {
					t.Fatal(err)
				}
				if r.PID != cmd.Process.Pid || r.ParentPID != os.Getpid() || r.Version != "0.0.292" || r.Timestamp.IsZero() || r.Timestamp.Location() != time.UTC || r.ConnectionID == "" {
					t.Fatalf("metadata: %+v", r)
				}
				if len(records) > 0 && r.ConnectionID != records[0].ConnectionID {
					t.Fatal("uncorrelated process records")
				}
				records = append(records, r)
			}
			if ending == "sigkill" {
				if len(records) != 1 || records[0].Event != "start" {
					t.Fatalf("kill synthesized stop evidence: %+v", records)
				}
				return
			}
			want := map[string]string{"eof": "client_eof", "sigterm": "context_cancelled", "partial-frame": "transport_error", "broken-stdout": "transport_error", "full-eof": "transport_error", "full-sigterm": "transport_error"}[ending]
			last := records[len(records)-1]
			if last.Event != "stop" || last.Condition != want || last.ClientEOF != (trigger == "eof") {
				t.Fatalf("stop: %+v (stderr %s)", records, stderr.String())
			}
			if (ending == "eof" || ending == "sigterm") && last.Uncertain {
				t.Fatalf("uncompeted shutdown uncertain: %+v", last)
			}
			if ending == "sigterm" && last.InputEOF {
				t.Fatalf("cancellation invented physical EOF: %+v", last)
			}
			if ending == "partial-frame" || ending == "broken-stdout" || full {
				operation := "read"
				if ending == "broken-stdout" || full {
					operation = "write"
				}
				found := false
				for _, r := range records {
					if r.Event == "error" && r.Operation == operation {
						found = true
					}
				}
				if !found {
					t.Fatalf("missing %s failure: %+v", operation, records)
				}
			}
		})
	}
}
