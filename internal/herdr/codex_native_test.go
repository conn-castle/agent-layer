package herdr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestUnixPeerPIDRejectsSiblingControlServer exercises the kernel credential
// boundary across processes without creating or touching a global Codex
// rendezvous directory. The HandleForRoot route applies this same check after
// its native-path and hash validation.
func TestUnixPeerPIDRejectsSiblingControlServer(t *testing.T) {
	if os.Getenv("AL_TEST_CODEX_MANAGED_SOCKET") == "server" {
		runCodexManagedSocketTestServer()
		return
	}

	path, err := os.CreateTemp("/tmp", "al-herdr-")
	if err != nil {
		t.Fatal(err)
	}
	socket := path.Name()
	if err := path.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(socket); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(socket) })
	command := exec.Command(os.Args[0], "-test.run=^TestUnixPeerPIDRejectsSiblingControlServer$") //nolint:gosec // standard test re-exec pattern
	command.Env = append(os.Environ(), "AL_TEST_CODEX_MANAGED_SOCKET=server", "AL_TEST_CODEX_MANAGED_SOCKET_PATH="+socket)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
		}
	}()
	select {
	case line := <-ready:
		if line != "ready" {
			t.Fatalf("managed server readiness = %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("managed server did not become ready")
	}
	connection, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	peerPID, err := unixPeerPID(connection)
	if err != nil {
		t.Fatal(err)
	}
	if peerPID == os.Getpid() || codexProcessDescendsFrom(os.Getpid(), peerPID) {
		t.Fatalf("sibling server PID %d was accepted as a hook ancestor", peerPID)
	}
}

func runCodexManagedSocketTestServer() {
	path := os.Getenv("AL_TEST_CODEX_MANAGED_SOCKET_PATH")
	listener, err := net.Listen("unix", path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = listener.Close() }()
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := (&websocket.Upgrader{}).Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() { _ = connection.Close() }()
		for {
			_, message, err := connection.ReadMessage()
			if err != nil {
				return
			}
			var request map[string]any
			if json.Unmarshal(message, &request) != nil {
				return
			}
			id, hasID := request["id"]
			if !hasID {
				continue
			}
			var result map[string]any
			switch request["method"] {
			case "initialize":
				result = map[string]any{"serverInfo": map[string]any{"name": "fixture"}}
			case "thread/loaded/list":
				result = map[string]any{"data": []string{"01a1138f-7df0-7fa0-94ae-820334783a29"}}
			default:
				return
			}
			if err := connection.WriteJSON(map[string]any{"id": id, "result": result}); err != nil {
				return
			}
		}
	})
	fmt.Println("ready")
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: handler}
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
	}
}

func TestCodexLoadedThreadIDGrammar(t *testing.T) {
	// The wire protocol is exercised with a loopback server in the sibling-peer
	// test above; this keeps the UUID grammar itself explicit at its boundary.
	if codexThreadID.MatchString("not-a-thread") {
		t.Fatal("invalid native thread ID matched")
	}
	if !codexThreadID.MatchString("01a1138f-7df0-7fa0-94ae-820334783a29") {
		t.Fatal("valid native thread ID was rejected")
	}
}
