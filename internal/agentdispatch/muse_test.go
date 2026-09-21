package agentdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestMuseReducerUsesLinkedRootTerminal(t *testing.T) {
	stream := []byte(`{"schema_version":1,"stream":{"kind":"session","id":"df759fc5-335d-4614-82c7-2322557d7817"},"payload_type":"session.run.linked","payload":{"kind":"session_run_linked","command_id":"6f12c390-27e9-4bed-b06e-e2cc6783d727","run_stream":{"kind":"run","id":"6f12c390-27e9-4bed-b06e-e2cc6783d727"}}}
{"schema_version":1,"stream":{"kind":"session","id":"df759fc5-335d-4614-82c7-2322557d7817"},"payload_type":"mcp.startup.task_handle","payload":{"kind":"mcp_startup_task_handle"}}
{"schema_version":1,"stream":{"kind":"session","id":"df759fc5-335d-4614-82c7-2322557d7817"},"payload_type":"task.lifecycle.failed","payload":{"kind":"task_lifecycle","command_id":"6f12c390-27e9-4bed-b06e-e2cc6783d727","run_stream":{"kind":"run","id":"6f12c390-27e9-4bed-b06e-e2cc6783d727"}}}
{"schema_version":1,"stream":{"kind":"session","id":"df759fc5-335d-4614-82c7-2322557d7817"},"payload_type":"run.terminal.completed","payload":{"kind":"run_terminal","command_id":"6f12c390-27e9-4bed-b06e-e2cc6783d727","run_stream":{"kind":"run","id":"6f12c390-27e9-4bed-b06e-e2cc6783d727"},"terminal":"completed","text":"done"}}
`)
	var events []providerEvent
	err := readStructuredEventsWithLineage(bytes.NewReader(stream), io.Discard, AgentMuse, "df759fc5-335d-4614-82c7-2322557d7817", false, func(event providerEvent) error { events = append(events, event); return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].Kind != eventComplete {
		t.Fatalf("events = %#v", events)
	}
	found := false
	for _, event := range events {
		if event.Kind == eventAnswer && event.Answer == "done" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing final answer: %#v", events)
	}
}

func TestMuseReducerReportsRootTerminalFailureReason(t *testing.T) {
	stream := []byte(`{"schema_version":1,"stream":{"kind":"session","id":"session"},"payload_type":"session.run.linked","payload":{"kind":"session_run_linked","command_id":"root","run_stream":{"kind":"run","id":"root"}}}
{"schema_version":1,"stream":{"kind":"session","id":"session"},"payload_type":"run.terminal.failed","payload":{"kind":"run_terminal","command_id":"root","run_stream":{"kind":"run","id":"root"},"terminal":"failed","text":"","reason":"required MCP startup failed"}}
`)
	var events []providerEvent
	err := readStructuredEventsWithLineage(bytes.NewReader(stream), io.Discard, AgentMuse, "session", false, func(event providerEvent) error { events = append(events, event); return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := events[len(events)-1]; got.Kind != eventFailure || got.Reason != "required MCP startup failed" {
		t.Fatalf("terminal event = %#v", got)
	}
}

func TestMuseApprovalObserverFailsOnlyOnPendingApproval(t *testing.T) {
	clientToServerReader, clientToServerWriter := io.Pipe()
	serverToClientReader, serverToClientWriter := io.Pipe()
	serverErr := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(clientToServerReader)
		encoder := json.NewEncoder(serverToClientWriter)
		var request map[string]any
		if err := decoder.Decode(&request); err != nil {
			serverErr <- err
			return
		}
		if err := encoder.Encode(map[string]any{
			jsonRPCKey: jsonRPCVersion, "id": 1,
			"result": map[string]any{"schema": map[string]any{"version": 1}},
		}); err != nil {
			serverErr <- err
			return
		}
		if err := decoder.Decode(&request); err != nil {
			serverErr <- err
			return
		}
		if request[jsonMethodKey] != "initialized" {
			serverErr <- io.ErrUnexpectedEOF
			return
		}
		if err := decoder.Decode(&request); err != nil {
			serverErr <- err
			return
		}
		// Ordinary status commentary is not a human block and must not affect
		// classification. The authoritative pull result below is what matters.
		if err := encoder.Encode(map[string]any{
			jsonRPCKey: jsonRPCVersion, jsonMethodKey: "session/statusChanged",
			jsonParamsKey: map[string]any{jsonSessionIDCamelKey: runtimeSessionID, "status": "running"},
		}); err != nil {
			serverErr <- err
			return
		}
		serverErr <- encoder.Encode(map[string]any{
			jsonRPCKey: jsonRPCVersion, "id": 2,
			"result": map[string]any{
				"approvals":  []map[string]any{{"approvalId": "approval-1", "toolName": "bash"}},
				"userInputs": []any{},
			},
		})
	}()

	err := observeMuseApprovals(context.Background(), clientToServerWriter, serverToClientReader, runtimeSessionID, dispatchModeFresh, func(error) {})
	if err == nil || !strings.Contains(err.Error(), "waiting for human approval approval-1 for tool bash") {
		t.Fatalf("observer error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("fake MSP server: %v", err)
	}
}

func TestMusePendingApprovalTerminatesDispatchWithProof(t *testing.T) {
	root := t.TempDir()
	providerPath := filepath.Join(root, "fake-muse")
	script := `#!/bin/sh
if [ "$1" = "serve" ]; then
  IFS= read -r initialize
  printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"schema":{"version":1}}}'
  IFS= read -r initialized
  IFS= read -r pending
  printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"approvals":[{"approvalId":"approval-1","toolName":"bash"}],"userInputs":[]}}'
  exit 0
fi
printf '%s\n' '{"schema_version":1,"stream":{"kind":"session","id":"11111111-1111-4111-8111-111111111111"},"payload_type":"session.run.linked","payload":{"kind":"session_run_linked","command_id":"root","run_stream":{"kind":"run","id":"root"}}}'
while :; do sleep 1; done
`
	if err := os.WriteFile(providerPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(providerPath, 0o700); err != nil { // #nosec G302 -- this private test fixture must be executable.
		t.Fatal(err)
	}
	run, err := newDispatchRun(root, AgentMuse, supportedProviderVersions[AgentMuse], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeProvider(providerCommand{
		Path:                 providerPath,
		Args:                 []string{execSubcommand},
		Env:                  os.Environ(),
		Provider:             AgentMuse,
		SessionID:            runtimeSessionID,
		ObserveMuseApprovals: true,
	}, nil, run, root, nil, func(string) error { return nil })
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if !strings.Contains(err.Error(), "waiting for human approval approval-1 for tool bash") {
		t.Fatalf("dispatch error = %v", err)
	}
	if !providerProcessGroupDead(run.Record.ProcessGroupID) {
		t.Fatalf("provider process group %d remained live after approval failure", run.Record.ProcessGroupID)
	}
}

func TestMuseObserverHandlesStartupAndHumanInput(t *testing.T) {
	const initReply = `{"jsonrpc":"2.0","id":1,"result":{"schema":{"version":1}}}` + "\n"
	for _, test := range []struct{ name, mode, replies, want string }{
		{"fresh session not created yet", dispatchModeFresh, `{"jsonrpc":"2.0","id":2,"error":{"code":-32020,"message":"session not found","data":{"kind":"sessionNotFound"}}}
{"jsonrpc":"2.0","id":3,"result":{"approvals":[{"approvalId":"waiting","toolName":"bash"}],"userInputs":[]}}
`, "waiting for human approval waiting for tool bash"},
		{"resume session not found at baseline", dispatchModeResume, `{"jsonrpc":"2.0","id":2,"error":{"code":-32020,"message":"session not found","data":{"kind":"sessionNotFound"}}}
{"jsonrpc":"2.0","id":3,"result":{"approvals":[{"approvalId":"waiting","toolName":"bash"}],"userInputs":[]}}
`, "waiting for human approval waiting for tool bash"},
		{"pending user input", dispatchModeFresh, `{"jsonrpc":"2.0","id":2,"result":{"approvals":[],"userInputs":[{"userInputId":"question"}]}}
{"jsonrpc":"2.0","id":3,"result":{"approvals":[{"approvalId":"waiting","toolName":"bash"}],"userInputs":[]}}
`, "waiting for human approval waiting for tool bash"},
		{"pushed user input leaves native auto-resolution authoritative", dispatchModeFresh, `{"jsonrpc":"2.0","id":99,"method":"userInput/request","params":{}}
{"jsonrpc":"2.0","id":2,"result":{"approvals":[{"approvalId":"waiting","toolName":"bash"}],"userInputs":[]}}
`, "waiting for human approval waiting for tool bash"},
		{"invalid response", dispatchModeFresh, `{"jsonrpc":"2.0","id":2,"result":{}}
`, "omitted approvals or userInputs"},
		{"invalid resume baseline", dispatchModeResume, `{"jsonrpc":"2.0","id":2,"result":{}}
`, "pending approvals baseline: muse pending-request response omitted approvals or userInputs"},
		{"session disappeared", dispatchModeFresh, `{"jsonrpc":"2.0","id":2,"result":{"approvals":[],"userInputs":[]}}
{"jsonrpc":"2.0","id":3,"error":{"code":-32020,"message":"session not found","data":{"kind":"sessionNotFound"}}}
`, "MSP error -32020"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requests bytes.Buffer
			err := observeMuseApprovals(context.Background(), &requests, strings.NewReader(initReply+test.replies), runtimeSessionID, test.mode, func(error) {})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %s", err, test.want)
			}
		})
	}
}

func TestMuseResumeIgnoresOnlyBaselinePendingRequests(t *testing.T) {
	for _, test := range []struct {
		name        string
		postPending string
		execBody    string
		wantFailure string
	}{
		{
			name:        "stale approval and user input are ignored",
			postPending: `{"approvals":[{"approvalId":"stale-approval","toolName":"bash"}],"userInputs":[{"userInputId":"stale-input"}]}`,
			execBody: `sleep 0.6
printf '%s\n' '{"schema_version":1,"stream":{"kind":"session","id":"11111111-1111-4111-8111-111111111111"},"payload_type":"run.terminal.completed","payload":{"kind":"run_terminal","command_id":"root","run_stream":{"kind":"run","id":"root"},"terminal":"completed","text":"done"}}'`,
		},
		{
			name:        "new approval fails",
			postPending: `{"approvals":[{"approvalId":"stale-approval","toolName":"bash"},{"approvalId":"fresh-approval","toolName":"write"}],"userInputs":[{"userInputId":"stale-input"}]}`,
			execBody:    `while :; do sleep 1; done`,
			wantFailure: "waiting for human approval fresh-approval for tool write",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			providerPath := filepath.Join(root, "fake-muse")
			script := `#!/bin/sh
if [ "$1" = "serve" ]; then
  IFS= read -r initialize
  printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"schema":{"version":1}}}'
  IFS= read -r initialized
  IFS= read -r baseline
  : > "$BASELINE_MARKER"
  printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"approvals":[{"approvalId":"stale-approval","toolName":"bash"}],"userInputs":[{"userInputId":"stale-input"}]}}'
  IFS= read -r pending
  printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":POST_PENDING}'
  while IFS= read -r pending; do
    printf '%s\n' '{"jsonrpc":"2.0","id":4,"result":POST_PENDING}'
  done
  exit 0
fi
if ! [ -f "$BASELINE_MARKER" ]; then
  exit 42
fi
printf '%s\n' '{"schema_version":1,"stream":{"kind":"session","id":"11111111-1111-4111-8111-111111111111"},"payload_type":"session.run.linked","payload":{"kind":"session_run_linked","command_id":"root","run_stream":{"kind":"run","id":"root"}}}'
EXEC_BODY
`
			script = strings.ReplaceAll(script, "POST_PENDING", test.postPending)
			script = strings.ReplaceAll(script, "EXEC_BODY", test.execBody)
			if err := os.WriteFile(providerPath, []byte(script), 0o700); err != nil { // #nosec G306 -- executable fixture in a test-owned temporary directory.
				t.Fatal(err)
			}
			run, err := newDispatchRun(root, AgentMuse, supportedProviderVersions[AgentMuse], dispatchModeResume)
			if err != nil {
				t.Fatal(err)
			}
			result, err := executeProvider(providerCommand{
				Path:                 providerPath,
				Args:                 []string{execSubcommand},
				Env:                  append(os.Environ(), "BASELINE_MARKER="+filepath.Join(root, "baseline-ready")),
				Provider:             AgentMuse,
				SessionID:            runtimeSessionID,
				RunMode:              dispatchModeResume,
				ObserveMuseApprovals: true,
			}, nil, run, root, nil, func(string) error { return nil })
			if test.wantFailure == "" {
				if err != nil {
					t.Fatalf("resume failed on baseline requests: %v", err)
				}
				if !result.Complete || !result.AnswerSeen {
					t.Fatalf("result = %#v", result)
				}
				return
			}
			requireDispatchExitCode(t, err, ExitTargetFailure)
			if !strings.Contains(err.Error(), test.wantFailure) {
				t.Fatalf("dispatch error = %v, want %q", err, test.wantFailure)
			}
		})
	}
}

func TestMuseApprovalPollingPacesFromCompletedResponse(t *testing.T) {
	clientToServerReader, clientToServerWriter := io.Pipe()
	serverToClientReader, serverToClientWriter := io.Pipe()
	serverErr := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(clientToServerReader)
		encoder := json.NewEncoder(serverToClientWriter)
		var request map[string]any
		for range 3 {
			if err := decoder.Decode(&request); err != nil {
				serverErr <- err
				return
			}
			if request[jsonMethodKey] == "initialize" {
				if err := encoder.Encode(map[string]any{jsonRPCKey: jsonRPCVersion, "id": 1, "result": map[string]any{"schema": map[string]any{"version": 1}}}); err != nil {
					serverErr <- err
					return
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
		if err := encoder.Encode(map[string]any{jsonRPCKey: jsonRPCVersion, "id": 2, "result": map[string]any{"approvals": []any{}, "userInputs": []any{}}}); err != nil {
			serverErr <- err
			return
		}
		responseCompleted := time.Now()
		if err := decoder.Decode(&request); err != nil {
			serverErr <- err
			return
		}
		if elapsed := time.Since(responseCompleted); elapsed < 400*time.Millisecond {
			serverErr <- fmt.Errorf("next poll arrived %s after response, want at least 400ms", elapsed)
			return
		}
		serverErr <- encoder.Encode(map[string]any{jsonRPCKey: jsonRPCVersion, "id": 3, "result": map[string]any{"approvals": []map[string]any{{"approvalId": "fresh", "toolName": "bash"}}, "userInputs": []any{}}})
	}()

	err := observeMuseApprovals(context.Background(), clientToServerWriter, serverToClientReader, runtimeSessionID, dispatchModeResume, func(error) {})
	if err == nil || !strings.Contains(err.Error(), "fresh") {
		t.Fatalf("observer error = %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestMuseApprovalPollDelayBounds(t *testing.T) {
	for _, test := range []struct {
		latency time.Duration
		want    time.Duration
	}{
		{0, 250 * time.Millisecond},
		{20 * time.Millisecond, 250 * time.Millisecond},
		{30 * time.Millisecond, 300 * time.Millisecond},
		{600 * time.Millisecond, 5 * time.Second},
	} {
		if got := museApprovalPollDelay(test.latency); got != test.want {
			t.Errorf("museApprovalPollDelay(%s) = %s, want %s", test.latency, got, test.want)
		}
	}
}

func TestMuseDispatchPreStartFailureRemovesStagedPrompt(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	project.Config.Approvals.Mode = config.ApprovalModeYOLO
	run, err := newDispatchRun(root, AgentMuse, supportedProviderVersions[AgentMuse], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run, testDispatchSessionRetention)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := lookupTarget(AgentMuse)
	promptPath := filepath.Join(run.Dir, "prompt.txt")
	err = executeDispatch(dispatchExecution{Root: root, Project: project, Target: target, Version: supportedProviderVersions[AgentMuse], Mode: dispatchModeFresh, Run: run, Session: session, Prompt: []byte("private prompt"), Stdout: io.Discard, Stderr: io.Discard, NewCommand: func(string, ...string) *exec.Cmd {
		data, err := os.ReadFile(promptPath) // #nosec G304 -- path is contained in this test-owned temporary fixture.
		if err != nil || string(data) != "private prompt" {
			t.Fatalf("prompt not staged: %q %v", data, err)
		}
		return exec.Command(filepath.Join(root, "missing-provider"))
	}})
	if err == nil {
		t.Fatal("expected pre-start failure")
	}
	if _, err := os.Stat(promptPath); !os.IsNotExist(err) {
		t.Fatalf("staged prompt survived: %v", err)
	}
}

func TestMuseObserverReadinessTimeout(t *testing.T) {
	previous := museObserverReadyTimeout
	museObserverReadyTimeout = 200 * time.Millisecond
	t.Cleanup(func() { museObserverReadyTimeout = previous })
	root := t.TempDir()
	providerPath := filepath.Join(root, "fake-muse-silent")
	// The observer host starts but never answers the initialize handshake.
	if err := os.WriteFile(providerPath, []byte("#!/bin/sh\nsleep 30\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(providerPath, 0o700); err != nil { // #nosec G302 -- this private test fixture must be executable.
		t.Fatal(err)
	}
	started := time.Now()
	_, err := startMuseApprovalObserver(providerCommand{
		Path:                 providerPath,
		Provider:             AgentMuse,
		SessionID:            runtimeSessionID,
		ObserveMuseApprovals: true,
	}, root)
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), "did not complete initialization") {
		t.Fatalf("observer error = %v, want readiness timeout", err)
	}
	if elapsed > 10*time.Second {
		t.Fatalf("readiness timeout took %s", elapsed)
	}
}
