package agentdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
)

// The fake harness withholds credential persistence until the test releases it.
// A catalog is emitted before EOF, matching Claude's native protocol ordering.
func heldClaudeDiscovery(t *testing.T) (saved string, release func()) {
	t.Helper()
	binDir := t.TempDir()
	markerDir := t.TempDir()
	t.Setenv("PATH", testPath(binDir))
	t.Setenv("AL_TEST_CLEANUP_DIR", markerDir)
	body := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ]; then printf '%%s\n' %q; exit 0; fi
read -r request
printf '%%s\n' '{"type":"control_response","response":{"request_id":"models","subtype":"success","response":{"models":[{"value":"account-model"}]}}}'
cat >/dev/null
touch "$AL_TEST_CLEANUP_DIR/draining"
while [ ! -f "$AL_TEST_CLEANUP_DIR/release" ]; do sleep 0.01; done
printf saved > "$AL_TEST_CLEANUP_DIR/saved"
exit "${AL_TEST_CLEANUP_EXIT:-0}"
`, claudeTestedVersion)
	if err := os.WriteFile(filepath.Join(binDir, AgentClaude), []byte(body), 0o700); err != nil { // #nosec G306 -- executable test fixture.
		t.Fatal(err)
	}
	release = func() {
		if err := os.WriteFile(filepath.Join(markerDir, "release"), nil, 0o600); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(release)
	return filepath.Join(markerDir, "saved"), release
}

func assertClaudeOptions(t *testing.T, result *mcp.CallToolResult) {
	t.Helper()
	if result.IsError {
		t.Fatalf("options failed: %s", toolResultText(result))
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var options OptionsResponse
	if err := json.Unmarshal(data, &options); err != nil {
		t.Fatal(err)
	}
	for _, agent := range options.Agents {
		if agent.Agent == AgentClaude && slices.Equal(agent.Model.Suggestions, []string{"account-model"}) {
			return
		}
	}
	t.Fatalf("missing live Claude catalog: %+v", options)
}

func failedMCPConversation(t *testing.T, root, agent string) string {
	t.Helper()
	run, err := newDispatchRun(root, agent, supportedProviderVersions[agent], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := reserveSession(root, run, testDispatchSessionRetention)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.RecoveryState = recoveryRetrySafe
	run.Record.CompletedAt = &now
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := releaseConversation(root, conversation.Name, run.Record.ID); err != nil {
		t.Fatal(err)
	}
	return conversation.Name
}

func TestMCPDiscoveryReturnsCatalogAndGatesCredentialConsumers(t *testing.T) {
	saved, release := heldClaudeDiscovery(t)
	var cleanup agentoptions.DiscoveryCleanup
	defer func() { release(); _ = cleanup.Close() }()
	tools := newMCPTestTools(writeDispatchRepo(t, dispatchRepoConfig{}))
	tools.discoveryCleanup = &cleanup
	conversation := failedMCPConversation(t, tools.root, AgentClaude)
	codexConversation := failedMCPConversation(t, tools.root, AgentCodex)
	session := newMCPTestSession(t, tools)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: ToolOptions, Arguments: OptionsInput{}})
	if err != nil {
		t.Fatalf("catalog waited for shutdown: %v", err)
	}
	assertClaudeOptions(t, result)
	if _, err := os.Stat(saved); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("catalog waited for persistence: %v", err)
	}
	for _, call := range []struct {
		name string
		args any
	}{
		{ToolOptions, OptionsInput{}},
		{ToolStart, StartInput{Agent: AgentClaude, Prompt: "fixture"}},
		{ToolContinue, ContinueInput{Handle: conversation, Prompt: "fixture"}},
	} {
		// Cancellation bounds waiting for the Claude barrier without killing
		// the probe, including requests queued behind another waiter.
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := session.CallTool(ctx, &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s bypassed pending cleanup: %v", call.name, err)
		}
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: ToolStart, Arguments: StartInput{Agent: AgentCodex, Prompt: "fixture"}})
	if err != nil || !result.IsError || strings.Contains(toolResultText(result), "Claude") {
		t.Fatalf("unrelated provider waited for Claude cleanup: result=%v error=%v", result, err)
	}
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: ToolContinue, Arguments: ContinueInput{Handle: codexConversation, Prompt: "fixture"}})
	if err != nil || !result.IsError || strings.Contains(toolResultText(result), "Claude") {
		t.Fatalf("unrelated continuation waited for Claude cleanup: result=%v error=%v", result, err)
	}
	release()
	if err := cleanup.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertFileContains(t, saved, "saved")
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: ToolContinue, Arguments: ContinueInput{Handle: "missing", Prompt: "fixture"}})
	if err != nil || !result.IsError {
		t.Fatalf("continuation remained blocked after persistence: result=%v error=%v", result, err)
	}
}

func TestMCPProcessJoinsDiscoveryCleanupOnShutdown(t *testing.T) {
	for _, shutdown := range []string{"cancel", "disconnect", "cancel-error", "disconnect-error"} {
		t.Run(shutdown, func(t *testing.T) {
			saved, release := heldClaudeDiscovery(t)
			if strings.HasSuffix(shutdown, "error") {
				t.Setenv("AL_TEST_CLEANUP_EXIT", "2")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			input, clientWriter := io.Pipe()
			clientReader, output := io.Pipe()
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			done := make(chan error, 1)
			go func() {
				done <- runMCPServer(ctx, MCPServerOptions{Root: root, Version: "test"}, input, output, io.Discard)
			}()
			clientCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			client, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil).Connect(clientCtx, &mcp.IOTransport{Reader: clientReader, Writer: clientWriter}, nil)
			if err != nil {
				release()
				cancel()
				_ = clientWriter.Close()
				_ = clientReader.Close()
				t.Fatal(err)
			}
			defer func() {
				release()
				cancel()
				_ = clientWriter.Close()
				_ = clientReader.Close()
				_ = client.Close()
			}()
			result, err := client.CallTool(clientCtx, &mcp.CallToolParams{Name: ToolOptions, Arguments: OptionsInput{}})
			if err != nil {
				t.Fatal(err)
			}
			assertClaudeOptions(t, result)
			if strings.HasPrefix(shutdown, "cancel") {
				cancel()
			} else {
				_ = clientWriter.Close()
			}
			select {
			case err := <-done:
				t.Fatalf("MCP process exited before persistence: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			release()
			runErr := waitMCPRun(t, done)
			if strings.HasSuffix(shutdown, "error") && (runErr == nil || !strings.Contains(runErr.Error(), "exit status 2")) {
				t.Fatalf("process owner lost late shutdown failure: %v", runErr)
			}
			assertFileContains(t, saved, "saved")
		})
	}
}

func TestMCPReportsLateShutdownErrorOnlyToClaudeConsumers(t *testing.T) {
	_, release := heldClaudeDiscovery(t)
	t.Setenv("AL_TEST_CLEANUP_EXIT", "2")
	var cleanup agentoptions.DiscoveryCleanup
	defer func() { release(); _ = cleanup.Close() }()
	tools := newMCPTestTools(writeDispatchRepo(t, dispatchRepoConfig{}))
	tools.discoveryCleanup = &cleanup
	session := newMCPTestSession(t, tools)
	assertClaudeOptions(t, callMCPTool(t, session, ToolOptions, OptionsInput{}))
	release()
	result := callMCPTool(t, session, ToolStart, StartInput{Agent: AgentCodex, Prompt: "fixture"})
	if !result.IsError || strings.Contains(toolResultText(result), "Claude") {
		t.Fatalf("unrelated start consumed Claude's shutdown failure: %s", toolResultText(result))
	}
	result = callMCPTool(t, session, ToolStart, StartInput{Agent: AgentClaude, Prompt: "fixture"})
	if !result.IsError || !strings.Contains(toolResultText(result), "exit status 2") {
		t.Fatalf("Claude operation lost late shutdown failure: %s", toolResultText(result))
	}
	if err := cleanup.Close(); err != nil {
		t.Fatalf("reported shutdown failure was duplicated at exit: %v", err)
	}
}
