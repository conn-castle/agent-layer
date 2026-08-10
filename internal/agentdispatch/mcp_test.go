package agentdispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newMCPTestSession serves the Agent Dispatch tools over the SDK's in-memory
// transports so tool behavior is exercised through real MCP round trips rather
// than by calling handlers directly.
func newMCPTestSession(t *testing.T, tools *dispatchToolServer) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: mcpServerName, Version: "test"}, nil)
	if err := tools.register(server); err != nil {
		t.Fatalf("register tools: %v", err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("connect server: %v", err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil).
		Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func newMCPTestTools(root string, env ...string) *dispatchToolServer {
	return &dispatchToolServer{
		root:        root,
		workDir:     root,
		env:         env,
		waitTimeout: 200 * time.Millisecond,
		toolTimeout: 30 * time.Second,
	}
}

// newMCPTestSessionWithClient serves the tools to a caller-supplied client so a
// test can observe client-side notifications.
func newMCPTestSessionWithClient(t *testing.T, tools *dispatchToolServer, client *mcp.Client) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: mcpServerName, Version: "test"}, nil)
	if err := tools.register(server); err != nil {
		t.Fatalf("register tools: %v", err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		t.Fatalf("connect server: %v", err)
	}
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callMCPTool(t *testing.T, session *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return result
}

func decodeToolResult(t *testing.T, result *mcp.CallToolResult) Result {
	t.Helper()
	if result.IsError {
		t.Fatalf("tool reported an error: %s", toolResultText(result))
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var decoded Result
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode structured content: %v", err)
	}
	return decoded
}

func toolResultText(result *mcp.CallToolResult) string {
	var parts []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// TestMCPToolsDeclareTheCanonicalDispatchSurface proves every enabled caller
// receives exactly the five Agent Dispatch tools, and that operations which
// can modify the repository or terminate provider work are advertised as
// destructive while discovery and waiting remain read-only.
func TestMCPToolsDeclareTheCanonicalDispatchSurface(t *testing.T) {
	session := newMCPTestSession(t, newMCPTestTools(writeDispatchRepo(t, dispatchRepoConfig{})))
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	byName := make(map[string]*mcp.Tool, len(listed.Tools))
	for _, tool := range listed.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{ToolOptions, ToolStart, ToolWait, ToolContinue, ToolCancel} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("tool %q is missing from %v", name, byName)
		}
	}
	if len(byName) != 5 {
		t.Fatalf("expected exactly five dispatch tools, got %d", len(byName))
	}
	for _, name := range []string{ToolStart, ToolContinue, ToolCancel} {
		annotations := byName[name].Annotations
		if annotations == nil || annotations.ReadOnlyHint ||
			annotations.DestructiveHint == nil || !*annotations.DestructiveHint {
			t.Fatalf("%s annotations = %#v, want destructive and not read-only", name, annotations)
		}
	}
	for _, name := range []string{ToolOptions, ToolWait} {
		annotations := byName[name].Annotations
		if annotations == nil || !annotations.ReadOnlyHint {
			t.Fatalf("%s annotations = %#v, want read-only", name, annotations)
		}
	}
}

// TestDecodeResultDoesNotHideLaunchFailureBehindRunningState proves a worker
// authorization failure remains a tool error even when startup already wrote
// its provisional running acknowledgement.
func TestDecodeResultDoesNotHideLaunchFailureBehindRunningState(t *testing.T) {
	out := bytes.NewBufferString(`{"handle":"example","state":"running"}`)
	_, result, err := decodeResult(out, errors.New("authorize dispatch worker: broken pipe"))
	if err == nil || result != nil {
		t.Fatalf("decodeResult() = (%#v, %v), want tool error with no running result", result, err)
	}
}

// TestMCPToolDescriptionsComeFromTheEditableCatalog proves the dedicated TOML
// file drives tool copy and input help exposed to clients.
func TestMCPToolDescriptionsComeFromTheEditableCatalog(t *testing.T) {
	const waitMinutes = 7
	catalog, err := loadMCPToolDescriptions()
	if err != nil {
		t.Fatal(err)
	}
	tools := newMCPTestTools(writeDispatchRepo(t, dispatchRepoConfig{}))
	tools.waitTimeout = waitMinutes * time.Minute
	session := newMCPTestSession(t, tools)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	for _, tool := range listed.Tools {
		description, ok := catalog.Tools[tool.Name]
		if !ok {
			t.Fatalf("runtime tool %q is absent from the description catalog", tool.Name)
		}
		wantToolDescription := renderMCPToolDescription(description.Description, waitMinutes)
		if tool.Description != wantToolDescription {
			t.Fatalf("tool %q description = %q, want %q", tool.Name, tool.Description, wantToolDescription)
		}
		schemaJSON, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal %q schema: %v", tool.Name, err)
		}
		var schema struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(schemaJSON, &schema); err != nil {
			t.Fatalf("decode %q schema: %v", tool.Name, err)
		}
		for parameter, wantParameterDescription := range description.Parameters {
			property, ok := schema.Properties[parameter]
			if !ok {
				t.Fatalf("tool %q schema lacks parameter %q", tool.Name, parameter)
			}
			if property.Description != wantParameterDescription {
				t.Fatalf("tool %q parameter %q description = %q, want %q",
					tool.Name, parameter, property.Description, wantParameterDescription)
			}
		}
		if tool.OutputSchema != nil {
			t.Fatalf("tool %q publishes an optional output schema in always-loaded context", tool.Name)
		}
	}
}

// TestMCPToolSchemaFootprintStaysSmall guards the cost this interface trades
// against. These five schemas sit in the always-present context of every
// enabled caller, on every turn, whether or not dispatch is used. Replacing
// polling turns with a permanently larger prompt would be a bad trade, so the
// budget is asserted rather than assumed. The ceiling leaves room for concise
// wording changes but not a verbose schema.
func TestMCPToolSchemaFootprintStaysSmall(t *testing.T) {
	const maxSchemaBytes = 3000
	session := newMCPTestSession(t, newMCPTestTools(writeDispatchRepo(t, dispatchRepoConfig{})))
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	encoded, err := json.Marshal(listed.Tools)
	if err != nil {
		t.Fatalf("marshal tool schemas: %v", err)
	}
	if len(encoded) > maxSchemaBytes {
		t.Fatalf("dispatch tool schemas are %d bytes, over the %d byte always-present context budget",
			len(encoded), maxSchemaBytes)
	}
}

// TestMCPOptionsReportsDiscovery proves dispatch_options answers from the same
// live discovery the CLI uses rather than a second static catalog.
func TestMCPOptionsReportsDiscovery(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session := newMCPTestSession(t, newMCPTestTools(root))
	result := callMCPTool(t, session, ToolOptions, map[string]any{})
	if result.IsError {
		t.Fatalf("dispatch_options failed: %s", toolResultText(result))
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var response OptionsResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Agents) == 0 {
		t.Fatal("dispatch_options returned no agents")
	}
	expected, err := BuildOptions(OptionsRequest{Root: root})
	if err != nil {
		t.Fatalf("build reference options: %v", err)
	}
	if len(response.Agents) != len(expected.Agents) {
		t.Fatalf("dispatch_options returned %d agents, CLI discovery returned %d",
			len(response.Agents), len(expected.Agents))
	}
}

// TestMCPWaitReportsRunningWithoutTouchingTheDispatch proves the bounded MCP
// wait behaves like the CLI wait: expiry is reported as `running` and the
// provider invocation is left exactly as it was.
func TestMCPWaitReportsRunningWithoutTouchingTheDispatch(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, dispatchSession := newRunningMCPTestRun(t, root)
	session := newMCPTestSession(t, newMCPTestTools(root))

	result := decodeToolResult(t, callMCPTool(t, session, ToolWait, HandleInput{Handle: dispatchSession.Name}))
	if result.Handle != dispatchSession.Name || result.State != dispatchStateRunning {
		t.Fatalf("dispatch_wait result = %#v, want running handle %q", result, dispatchSession.Name)
	}
	current, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != dispatchStateRunning {
		t.Fatalf("invocation state = %q, want running", current.State)
	}
}

// TestMCPWaitReturnsTheDurableCompletedResult proves the MCP surface hands back
// the same durable result path the CLI publishes, so an agent reads the answer
// from a file rather than from tool output.
func TestMCPWaitReturnsTheDurableCompletedResult(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, dispatchSession := newWaitTestRun(t, root)
	if err := os.WriteFile(run.Record.AnswerPath, []byte("done"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &now
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := releaseConversation(root, dispatchSession.Name, run.Record.ID); err != nil {
		t.Fatal(err)
	}
	session := newMCPTestSession(t, newMCPTestTools(root))
	callResult := callMCPTool(t, session, ToolWait, HandleInput{Handle: dispatchSession.Name})
	result := decodeToolResult(t, callResult)
	if result.State != dispatchStateCompleted || result.ResultPath != run.Record.AnswerPath {
		t.Fatalf("dispatch_wait result = %#v, want completed result path %q", result, run.Record.AnswerPath)
	}
	if !strings.Contains(toolResultText(callResult), run.Record.AnswerPath) {
		t.Fatalf("dispatch_wait text fallback omits completed result path %q", run.Record.AnswerPath)
	}
}

// TestMCPWaitReportsTerminalFailureAsData proves an expected provider failure
// reaches the agent as a structured `failed` result rather than as a tool
// error, so the coordinator can react instead of retrying blindly.
func TestMCPWaitReportsTerminalFailureAsData(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, dispatchSession := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = "authentication failed"
	run.Record.TerminalExitCode = ExitUnavailable
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	session := newMCPTestSession(t, newMCPTestTools(root))
	result := decodeToolResult(t, callMCPTool(t, session, ToolWait, HandleInput{Handle: dispatchSession.Name}))
	if result.State != dispatchStateFailed || result.Error != "authentication failed" {
		t.Fatalf("dispatch_wait result = %#v, want failed with the recorded reason", result)
	}
}

// TestMCPWaitRejectsUnknownHandleAsToolError proves malformed or unresolvable
// input is a tool error, keeping it distinct from a real dispatch failure.
func TestMCPWaitRejectsUnknownHandleAsToolError(t *testing.T) {
	session := newMCPTestSession(t, newMCPTestTools(writeDispatchRepo(t, dispatchRepoConfig{})))
	result := callMCPTool(t, session, ToolWait, HandleInput{Handle: "missing-handle"})
	if !result.IsError {
		t.Fatalf("expected a tool error for an unknown handle, got %#v", result.StructuredContent)
	}
}

// TestMCPContinueStartsTheNextInvocation proves the fifth tool reaches the
// existing continuation backend, preserves the conversation handle, and
// durably links the next invocation to the terminal one.
func TestMCPContinueStartsTheNextInvocation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, dispatchSession := newWaitTestRun(t, root)
	if err := os.WriteFile(run.Record.AnswerPath, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &now
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := releaseConversation(root, dispatchSession.Name, run.Record.ID); err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	promptPath := filepath.Join(t.TempDir(), "prompt.txt")
	writeDispatchStub(t, binDir, "codex", "sleep 60")
	t.Setenv("PATH", testPath(binDir))
	env := append(os.Environ(), "AL_TEST_LOG="+logPath, "AL_TEST_PROMPT="+promptPath)
	session := newMCPTestSession(t, newMCPTestTools(root, env...))

	started := decodeToolResult(t, callMCPTool(t, session, ToolContinue, ContinueInput{
		Handle: dispatchSession.Name,
		Prompt: "follow up",
	}))
	if started.Handle != dispatchSession.Name || started.State != dispatchStateRunning {
		t.Fatalf("dispatch_continue result = %#v, want running handle %q", started, dispatchSession.Name)
	}
	current, err := resolveWaitRun(root, dispatchSession.Name)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != dispatchStateRunning || current.PreviousRunID != run.Record.ID {
		t.Fatalf("continued invocation = %#v, want running successor to %q", current, run.Record.ID)
	}
	cancelled := decodeToolResult(t, callMCPTool(t, session, ToolCancel, HandleInput{Handle: dispatchSession.Name}))
	if cancelled.State != dispatchStateCancelled {
		t.Fatalf("dispatch_cancel result = %#v, want cancelled cleanup", cancelled)
	}
}

// TestMCPRequestCancellationLeavesTheDispatchRunning proves the central safety
// property of the long wait: a client that gives up on the MCP request stops
// only its own wait. Provider work keeps running and stays resumable.
func TestMCPRequestCancellationLeavesTheDispatchRunning(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, dispatchSession := newRunningMCPTestRun(t, root)
	tools := newMCPTestTools(root)
	tools.waitTimeout = time.Minute
	session := newMCPTestSession(t, tools)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: ToolWait, Arguments: HandleInput{Handle: dispatchSession.Name},
		})
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled dispatch_wait returned success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("cancelled dispatch_wait did not return")
	}
	current, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != dispatchStateRunning {
		t.Fatalf("invocation state = %q after MCP request cancellation, want running", current.State)
	}
}

// TestMCPCancelTerminatesTheDispatch proves explicit cancellation reuses the
// CLI's ownership-safe path and publishes the same cancelled result.
func TestMCPCancelTerminatesTheDispatch(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, dispatchSession := newRunningMCPTestRun(t, root)
	session := newMCPTestSession(t, newMCPTestTools(root))

	result := decodeToolResult(t, callMCPTool(t, session, ToolCancel, HandleInput{Handle: dispatchSession.Name}))
	if result.State != dispatchStateCancelled || result.Handle != dispatchSession.Name {
		t.Fatalf("dispatch_cancel result = %#v, want cancelled", result)
	}
	current, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != dispatchStateCancelled {
		t.Fatalf("invocation state = %q, want cancelled", current.State)
	}
}

// TestMCPCancelRefusesCompletedWork proves cancellation cannot rewrite terminal
// evidence: a finished conversation stays finished.
func TestMCPCancelRefusesCompletedWork(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, dispatchSession := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &now
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	session := newMCPTestSession(t, newMCPTestTools(root))
	result := callMCPTool(t, session, ToolCancel, HandleInput{Handle: dispatchSession.Name})
	if !result.IsError {
		t.Fatalf("expected cancelling completed work to fail, got %#v", result.StructuredContent)
	}
	current, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != dispatchStateCompleted {
		t.Fatalf("invocation state = %q, want completed", current.State)
	}
}

// TestMCPStartRejectsAmbiguousPromptSource proves the MCP start enforces the
// CLI's exactly-one-prompt-source contract instead of silently picking one.
func TestMCPStartRejectsAmbiguousPromptSource(t *testing.T) {
	session := newMCPTestSession(t, newMCPTestTools(writeDispatchRepo(t, dispatchRepoConfig{})))
	result := callMCPTool(t, session, ToolStart, StartInput{
		Agent: AgentCodex, Prompt: "hello", PromptFile: "/tmp/prompt.md",
	})
	if !result.IsError {
		t.Fatal("expected a tool error when both prompt sources are supplied")
	}
	if !strings.Contains(toolResultText(result), "exactly one") {
		t.Fatalf("unexpected error text: %s", toolResultText(result))
	}
}

// TestMCPBenchmarkPolicyLocksTheExecutionTarget proves a benchmark container's
// root-owned policy reaches the MCP surface. Without it, a coordinator could
// select an arbitrary model through MCP and invalidate treatment comparability.
func TestMCPBenchmarkPolicyLocksTheExecutionTarget(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	policy, err := loadBenchmarkPolicy([]string{
		benchmarkPolicyAgentEnv + "=" + AgentClaude,
		benchmarkPolicyModelEnv + "=locked-model",
		benchmarkPolicyEffortEnv + "=low",
	})
	if err != nil {
		t.Fatalf("load benchmark policy: %v", err)
	}
	tools := newMCPTestTools(root)
	tools.policy = policy
	session := newMCPTestSession(t, tools)

	data, err := json.Marshal(callMCPTool(t, session, ToolOptions, map[string]any{}).StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var options OptionsResponse
	if err := json.Unmarshal(data, &options); err != nil {
		t.Fatal(err)
	}
	if len(options.Agents) != 1 || options.Agents[0].Agent != AgentClaude {
		t.Fatalf("dispatch_options under benchmark policy = %#v, want only claude", options.Agents)
	}
	if got := options.Agents[0].Model.Configured; got != "locked-model" {
		t.Fatalf("locked model = %q, want %q", got, "locked-model")
	}

	result := callMCPTool(t, session, ToolStart, StartInput{Agent: AgentCodex, Prompt: "hello"})
	if !result.IsError || !strings.Contains(toolResultText(result), "benchmark dispatch constraint") {
		t.Fatalf("expected a benchmark constraint error, got %q", toolResultText(result))
	}
	result = callMCPTool(t, session, ToolStart, StartInput{Agent: AgentClaude, Model: "other", Prompt: "hello"})
	if !result.IsError || !strings.Contains(toolResultText(result), "benchmark dispatch constraint") {
		t.Fatalf("expected a locked-model error, got %q", toolResultText(result))
	}
}

// TestBenchmarkPolicyRejectsPartialExport proves an incomplete policy fails
// loudly at server construction rather than silently disabling enforcement.
func TestBenchmarkPolicyRejectsPartialExport(t *testing.T) {
	_, err := loadBenchmarkPolicy([]string{benchmarkPolicyAgentEnv + "=" + AgentClaude})
	requireDispatchExitCode(t, err, ExitConfig)
}

// TestBenchmarkPolicyInjectsLockedDefaults proves an MCP start that omits model
// and effort still records the exact treatment identity.
func TestBenchmarkPolicyInjectsLockedDefaults(t *testing.T) {
	policy, err := loadBenchmarkPolicy([]string{
		benchmarkPolicyAgentEnv + "=" + AgentCodex,
		benchmarkPolicyModelEnv + "=gpt-locked",
		benchmarkPolicyEffortEnv + "=high",
	})
	if err != nil {
		t.Fatal(err)
	}
	agent, model, effort := AgentCodex, "", ""
	if err := policy.constrainStart(&agent, &model, &effort); err != nil {
		t.Fatalf("constrain start: %v", err)
	}
	if model != "gpt-locked" || effort != "high" {
		t.Fatalf("injected selection = %q/%q, want gpt-locked/high", model, effort)
	}
}

func TestBenchmarkPolicyAllowsOnlyConfiguredTargetTuples(t *testing.T) {
	policy, err := loadBenchmarkPolicy([]string{
		benchmarkPolicyTargetsEnv + `=[{"agent":"codex","model":"gpt-luna","reasoning_effort":"high"},{"agent":"codex","model":"gpt-terra","reasoning_effort":"low"}]`,
	})
	if err != nil {
		t.Fatal(err)
	}
	options := &OptionsResponse{}
	policy.constrainOptions(options)
	if len(options.Agents) != 1 || len(options.Agents[0].Model.Suggestions) != 2 ||
		len(options.Agents[0].ReasoningEffort.Suggestions) != 2 {
		t.Fatalf("multi-target options = %#v", options.Agents)
	}
	agent, model, effort := AgentCodex, "gpt-luna", "high"
	if err := policy.constrainStart(&agent, &model, &effort); err != nil {
		t.Fatalf("configured tuple rejected: %v", err)
	}
	model, effort = "gpt-luna", "low"
	if err := policy.constrainStart(&agent, &model, &effort); err == nil {
		t.Fatal("unconfigured model/effort cross-product was accepted")
	}
}

// TestMCPServerRejectsMissingRoot proves the stdio entry point fails before
// serving when it cannot resolve an Agent Layer root.
func TestMCPServerRejectsMissingRoot(t *testing.T) {
	_, err := newDispatchMCPServer(MCPServerOptions{Env: []string{}})
	requireDispatchExitCode(t, err, ExitConfig)
}

// TestMCPServerResolvesConfiguredTimeouts proves the configured minute settings
// reach the bounded wait and the hard handler guard.
func TestMCPServerResolvesConfiguredTimeouts(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	replaceDispatchConfigText(t, root, "[dispatch]", "[dispatch]\nmcp_wait_timeout_minutes = 3\nmcp_tool_timeout_minutes = 7")
	tools, err := newDispatchToolServer(MCPServerOptions{Root: root, Env: []string{}})
	if err != nil {
		t.Fatalf("new tool server: %v", err)
	}
	if tools.waitTimeout != 3*time.Minute {
		t.Fatalf("wait timeout = %s, want 3m", tools.waitTimeout)
	}
	if tools.toolTimeout != 7*time.Minute {
		t.Fatalf("tool timeout = %s, want 7m", tools.toolTimeout)
	}
}

// TestMCPWaitRelaysProgressWhenTheClientAsksForIt proves a 30-minute block is
// not silent for clients that supply a progress token: without periodic
// notifications a caller cannot distinguish a working dispatch from a hung
// tool call.
func TestMCPWaitRelaysProgressWhenTheClientAsksForIt(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	_, dispatchSession := newRunningMCPTestRun(t, root)
	tools := newMCPTestTools(root)
	tools.waitTimeout = 2 * time.Second
	tools.progressInterval = 20 * time.Millisecond

	notified := make(chan struct{}, 1)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(context.Context, *mcp.ProgressNotificationClientRequest) {
			select {
			case notified <- struct{}{}:
			default:
			}
		},
	})
	session := newMCPTestSessionWithClient(t, tools, client)

	params := &mcp.CallToolParams{Name: ToolWait, Arguments: HandleInput{Handle: dispatchSession.Name}}
	params.SetProgressToken("probe-token")
	if _, err := session.CallTool(context.Background(), params); err != nil {
		t.Fatalf("dispatch_wait: %v", err)
	}
	select {
	case <-notified:
	default:
		t.Fatal("a long dispatch_wait relayed no progress to a client that supplied a progress token")
	}
}

// TestMCPHardGuardReleasesAWedgedHandler proves the configured hard timeout
// bounds every handler regardless of the wait setting. Claude has no
// per-server tool timeout, so this server-side guard is its only recovery
// bound — and it must not touch the dispatch itself.
func TestMCPHardGuardReleasesAWedgedHandler(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, dispatchSession := newRunningMCPTestRun(t, root)
	tools := newMCPTestTools(root)
	tools.waitTimeout = time.Hour
	tools.toolTimeout = 150 * time.Millisecond
	session := newMCPTestSession(t, tools)

	start := time.Now()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: ToolWait, Arguments: HandleInput{Handle: dispatchSession.Name},
	})
	if err != nil {
		t.Fatalf("dispatch_wait: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected the hard guard to end the wait as a tool error, got %#v", result.StructuredContent)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("hard guard took %s to release the caller", elapsed)
	}
	current, loadErr := loadRunRecord(root, run.Record.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if current.State != dispatchStateRunning {
		t.Fatalf("invocation state = %q after the hard guard fired, want running", current.State)
	}
}

// TestMCPHardGuardBoundsAContextFreeHandler proves the guard itself releases
// callers even when an operation cannot observe context cancellation.
func TestMCPHardGuardBoundsAContextFreeHandler(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	handler := guard(25*time.Millisecond, func(context.Context, *mcp.CallToolRequest, OptionsInput) (*mcp.CallToolResult, *OptionsResponse, error) {
		<-release
		return nil, &OptionsResponse{}, nil
	})

	started := time.Now()
	_, _, err := handler(context.Background(), nil, OptionsInput{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("guard error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("context-free handler blocked caller for %s", elapsed)
	}
}

func newRunningMCPTestRun(t *testing.T, root string) (*dispatchRun, Session) {
	t.Helper()
	run, session := newWaitTestRun(t, root)
	run.Record.State = dispatchStateRunning
	run.Record.SupervisorPID = os.Getpid()
	run.Record.SupervisorStartIdentity = processStartIdentity(os.Getpid())
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	return run, session
}
