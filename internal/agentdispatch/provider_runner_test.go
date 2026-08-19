package agentdispatch

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	clientgrok "github.com/conn-castle/agent-layer/internal/clients/grok"
	"github.com/conn-castle/agent-layer/internal/config"
)

func TestProviderCommandsUseExactProviderContracts(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{ClaudeModel: "configured-model", ClaudeReasoningEffort: "medium"})
	project, stderr, env, depth, err := loadDispatchProject(root, io.Discard, []string{})
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	if stderr != io.Discard || len(env) != 0 || depth != 0 {
		t.Fatalf("unexpected dispatch context: stderr=%T env=%v depth=%d", stderr, env, depth)
	}
	run, err := newDispatchRun(root, AgentClaude, supportedProviderVersions[AgentClaude], "fresh")
	if err != nil {
		t.Fatalf("new run: %v", err)
	}

	claudeTarget, ok := lookupTarget(AgentClaude)
	if !ok {
		t.Fatal("Claude target missing from registry")
	}
	claudeCommand, err := buildProviderCommand(claudeTarget, project, []string{}, []byte("prompt"), "override", "high", false, "fresh", runtimeSessionID, run, io.Discard)
	if err != nil {
		t.Fatalf("build Claude command: %v", err)
	}
	claudeArgs := strings.Join(claudeCommand.Args, " ")
	if !claudeCommand.Structured || !strings.Contains(claudeArgs, "--session-id "+runtimeSessionID) || !strings.Contains(claudeArgs, "--model override") || !strings.Contains(claudeArgs, "--effort high") {
		t.Fatalf("Claude command = %#v", claudeCommand)
	}

	codexTarget, ok := lookupTarget(AgentCodex)
	if !ok {
		t.Fatal("Codex target missing from registry")
	}
	codexCommand, err := buildProviderCommand(codexTarget, project, []string{}, []byte("prompt"), "", "high", false, "resume", runtimeSessionID, run, io.Discard)
	if err != nil {
		t.Fatalf("build Codex command: %v", err)
	}
	if got := strings.Join(codexCommand.Args, " "); !strings.Contains(got, "exec resume --json "+runtimeSessionID+" -c model_reasoning_effort=high -") {
		t.Fatalf("Codex command = %q", got)
	}
	project.Config.Agents.Codex.Model = "configured-codex"
	project.Config.Agents.Codex.ReasoningEffort = "medium"
	project.Config.Approvals.Mode = config.ApprovalModeYOLO
	codexDefaults, err := buildProviderCommand(codexTarget, project, []string{}, []byte("prompt"), "", "", false, dispatchModeFresh, "", run, io.Discard)
	if err != nil {
		t.Fatalf("build Codex defaults command: %v", err)
	}
	for _, want := range []string{"--model configured-codex", "model_reasoning_effort=medium", "approval_policy=never", "sandbox_mode=danger-full-access", "web_search=live"} {
		if got := strings.Join(codexDefaults.Args, " "); !strings.Contains(got, want) {
			t.Fatalf("Codex defaults command %q omitted %q", got, want)
		}
	}

	antigravityTarget, ok := lookupTarget(AgentAntigravity)
	if !ok {
		t.Fatal("Antigravity target missing from registry")
	}
	if _, err := buildProviderCommand(antigravityTarget, project, []string{}, bytes.Repeat([]byte("x"), AntigravityPromptMaxBytes+1), "", "", false, "fresh", "", run, io.Discard); err == nil {
		t.Fatal("Antigravity accepted an argv-sized prompt")
	} else {
		requireDispatchExitCode(t, err, ExitUsage)
	}

	grokTarget, ok := lookupTarget(AgentGrok)
	if !ok {
		t.Fatal("Grok target missing from registry")
	}
	grokCommand, err := buildProviderCommand(grokTarget, project, []string{"GROK_HOME=/external"}, []byte("prompt text"), "grok-4.6", "high", false, "fresh", runtimeSessionID, run, io.Discard)
	if err != nil {
		t.Fatalf("build Grok command: %v", err)
	}
	grokArgs := strings.Join(grokCommand.Args, " ")
	if !grokCommand.Structured || !slices.Contains(grokCommand.Args, "--no-auto-update") || !strings.Contains(grokArgs, "--session-id "+runtimeSessionID) || !strings.Contains(grokArgs, "--model grok-4.6") || !strings.Contains(grokArgs, "--reasoning-effort high") || !strings.Contains(grokArgs, "--output-format streaming-json") || !strings.Contains(grokArgs, "--permission-mode bypassPermissions --always-approve") {
		t.Fatalf("Grok command = %#v", grokCommand)
	}
	if home, ok := clients.GetEnv(grokCommand.Env, clientgrok.EnvHome); !ok || home != clientgrok.HomeDir(root) {
		t.Fatalf("Grok dispatch GROK_HOME = %q (present %t), want %q", home, ok, clientgrok.HomeDir(root))
	}
	promptContent, err := os.ReadFile(filepath.Join(run.Dir, "prompt.txt"))
	if err != nil || string(promptContent) != "prompt text" {
		t.Fatalf("Grok prompt file content = %q, %v", promptContent, err)
	}
}

func TestClaudeLineageCapabilityGatesFreshAndResumeCommands(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project, stderr, env, depth, err := loadDispatchProject(root, io.Discard, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if stderr != io.Discard || len(env) != 0 || depth != 0 {
		t.Fatalf("unexpected dispatch context: stderr=%T env=%v depth=%d", stderr, env, depth)
	}
	target, ok := lookupTarget(AgentClaude)
	if !ok {
		t.Fatal("Claude target missing")
	}
	for _, providerVersion := range []string{"2.1.207", "2.1.210", "2.1.211", "2.1.212"} {
		for _, mode := range []string{dispatchModeFresh, dispatchModeResume} {
			t.Run(providerVersion+"/"+mode, func(t *testing.T) {
				run, runErr := newDispatchRun(root, AgentClaude, providerVersion, mode)
				if runErr != nil {
					t.Fatal(runErr)
				}
				command, commandErr := buildProviderCommand(target, project, nil, []byte("prompt"), "", "", false, mode, runtimeSessionID, run, io.Discard)
				if commandErr != nil {
					t.Fatal(commandErr)
				}
				want := providerVersion == "2.1.211" || providerVersion == "2.1.212"
				if got := slices.Contains(command.Args, "--forward-subagent-text"); got != want || command.ClaudeLineage != want {
					t.Fatalf("command lineage gate = args:%t bit:%t, want %t: %#v", got, command.ClaudeLineage, want, command.Args)
				}
				if got := run.Record.LineagePath != ""; got != want {
					t.Fatalf("lineage path present = %t, want %t", got, want)
				}
			})
		}
	}
	if _, err := newDispatchRun(root, AgentClaude, "malformed", dispatchModeFresh); err == nil {
		t.Fatal("malformed Claude version created a run")
	}
	if supportedProviderVersions[AgentClaude] != claudeTestedVersion {
		t.Fatalf("Claude tested baseline changed to %q", supportedProviderVersions[AgentClaude])
	}
}

func TestClaudeDispatchPrintBackgroundWaitCeilingIsAuthoritative(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project, stderr, env, depth, err := loadDispatchProject(root, io.Discard, []string{})
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	if stderr != io.Discard || len(env) != 0 || depth != 0 {
		t.Fatalf("unexpected dispatch context: stderr=%T env=%v depth=%d", stderr, env, depth)
	}
	claudeTarget, ok := lookupTarget(AgentClaude)
	if !ok {
		t.Fatal("Claude target missing from registry")
	}

	tests := []struct {
		name          string
		mode          string
		baseEnv       []string
		projectValue  string
		inputKeyCount int
	}{
		{
			name:          "fresh replaces project value",
			mode:          dispatchModeFresh,
			projectValue:  "600000",
			inputKeyCount: 1,
		},
		{
			name:          "resume replaces duplicate caller values",
			mode:          dispatchModeResume,
			baseEnv:       []string{claudePrintBackgroundWaitCeilingEnv + "=600000", claudePrintBackgroundWaitCeilingEnv + "=1"},
			projectValue:  "900000",
			inputKeyCount: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project.Env = map[string]string{claudePrintBackgroundWaitCeilingEnv: tt.projectValue}
			run, err := newDispatchRun(root, AgentClaude, supportedProviderVersions[AgentClaude], tt.mode)
			if err != nil {
				t.Fatalf("new dispatch run: %v", err)
			}
			childEnv := dispatchEnvironment(tt.baseEnv, project, run, 1)
			if got := len(envValues(childEnv, claudePrintBackgroundWaitCeilingEnv)); got != tt.inputKeyCount {
				t.Fatalf("dispatch environment %q entries = %d, want %d: %#v", claudePrintBackgroundWaitCeilingEnv, got, tt.inputKeyCount, childEnv)
			}
			command, err := buildProviderCommand(claudeTarget, project, childEnv, []byte("prompt"), "", "", false, tt.mode, runtimeSessionID, run, io.Discard)
			if err != nil {
				t.Fatalf("build Claude command: %v", err)
			}
			if values := envValues(command.Env, claudePrintBackgroundWaitCeilingEnv); len(values) != 1 || values[0] != claudePrintBackgroundWaitCeilingValue {
				t.Fatalf("Claude environment %q entries = %#v, want exactly [%q]", claudePrintBackgroundWaitCeilingEnv, values, claudePrintBackgroundWaitCeilingValue)
			}
		})
	}

	for _, agent := range []string{AgentCodex, AgentAntigravity} {
		t.Run(agent+" does not receive Claude override", func(t *testing.T) {
			target, ok := lookupTarget(agent)
			if !ok {
				t.Fatalf("%s target missing from registry", agent)
			}
			run, err := newDispatchRun(root, agent, supportedProviderVersions[agent], dispatchModeFresh)
			if err != nil {
				t.Fatalf("new dispatch run: %v", err)
			}
			command, err := buildProviderCommand(target, project, []string{"KEEP=1"}, []byte("prompt"), "", "", false, dispatchModeFresh, "", run, io.Discard)
			if err != nil {
				t.Fatalf("build %s command: %v", agent, err)
			}
			if values := envValues(command.Env, claudePrintBackgroundWaitCeilingEnv); len(values) != 0 {
				t.Fatalf("%s environment unexpectedly includes %q: %#v", agent, claudePrintBackgroundWaitCeilingEnv, values)
			}
		})
	}
}

func envValues(env []string, key string) []string {
	prefix := key + "="
	values := make([]string, 0, 1)
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			values = append(values, strings.TrimPrefix(entry, prefix))
		}
	}
	return values
}

// reduceStructuredTestEvent routes one raw provider record through the whole
// production path — the selective parser and then the per-provider reducer —
// so reducer contracts stay testable from raw JSON fixtures.
//
// It must not parse with encoding/json. The selective parser retains only the
// fields in retainedStructuredPath and does not extend the path for array
// elements, so the two produce different shapes for nested fields: a record
// encoding/json turns into a []any can reach the reducer as a map. A reducer
// test fed by encoding/json therefore proves nothing about the input the
// reducer actually receives.
func reduceStructuredTestEvent(agent string, expectedSession string, raw []byte) ([]providerEvent, error) {
	var events []providerEvent
	if err := readStructuredEventsWithLineage(bytes.NewReader(append(raw, '\n')), io.Discard, agent, expectedSession, false, func(event providerEvent) error {
		events = append(events, event)
		return nil
	}, nil); err != nil {
		return nil, err
	}
	return events, nil
}

func TestStructuredEventsRejectChangedProviderContracts(t *testing.T) {
	claudeEvents, err := reduceStructuredTestEvent(AgentClaude, runtimeSessionID, []byte(`{"type":"result","session_id":"22222222-2222-4222-8222-222222222222","is_error":false}`))
	if err != nil || len(claudeEvents) != 1 || claudeEvents[0].Kind != eventFailure {
		t.Fatalf("Claude events = %#v, %v", claudeEvents, err)
	}
	codexEvents, err := reduceStructuredTestEvent(AgentCodex, "", []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"final answer"}}`))
	if err != nil || len(codexEvents) != 1 || codexEvents[0].Answer != "final answer" {
		t.Fatalf("Codex events = %#v, %v", codexEvents, err)
	}
	progressEvents, err := reduceStructuredTestEvent(AgentCodex, "", []byte(`{"type":"item.completed","item":{"type":"command_execution","command":"pwd"}}`))
	if err != nil || len(progressEvents) != 1 || progressEvents[0].Kind != eventProgress || progressEvents[0].Activity != "item.completed" {
		t.Fatalf("Codex non-agent item.completed events = %#v, %v", progressEvents, err)
	}
	flatEvents, err := reduceStructuredTestEvent(AgentCodex, "", []byte(`{"type":"agent_message","message":"compatible answer"}`))
	if err != nil || len(flatEvents) != 1 || flatEvents[0].Answer != "compatible answer" {
		t.Fatalf("Codex flat compatibility events = %#v, %v", flatEvents, err)
	}
	escapedSlashEvents, err := reduceStructuredTestEvent(AgentCodex, "", []byte(`{"type":"agent_message","message":"https:\/\/example.com\/answer"}`))
	if err != nil || len(escapedSlashEvents) != 1 || escapedSlashEvents[0].Answer != "https://example.com/answer" {
		t.Fatalf("Codex escaped-slash events = %#v, %v", escapedSlashEvents, err)
	}
	failureEvents, err := reduceStructuredTestEvent(AgentCodex, "", []byte(`{"type":"turn.failed","error":{"message":"model quota exhausted"}}`))
	if err != nil || len(failureEvents) != 1 || failureEvents[0].Kind != eventFailure || failureEvents[0].Reason != "model quota exhausted" {
		t.Fatalf("Codex nested failure events = %#v, %v", failureEvents, err)
	}
	stringFailureEvents, err := reduceStructuredTestEvent(AgentCodex, "", []byte(`{"type":"error","error":"quota exhausted"}`))
	if err != nil || len(stringFailureEvents) != 1 || stringFailureEvents[0].Kind != eventFailure || stringFailureEvents[0].Reason != "quota exhausted" {
		t.Fatalf("Codex string failure events = %#v, %v", stringFailureEvents, err)
	}
	var raw bytes.Buffer
	var recovered []providerEvent
	invalidThenValid := "not-json\n" + `{"type":"turn.completed"}` + "\n"
	if err := readStructuredEventsWithLineage(strings.NewReader(invalidThenValid), &raw, AgentCodex, "", false, func(event providerEvent) error {
		recovered = append(recovered, event)
		return nil
	}, nil); err != nil {
		t.Fatalf("invalid provider JSON prevented later record recovery: %v", err)
	}
	if len(recovered) != 2 || recovered[0].Kind != eventProgress || recovered[0].Activity != "invalid_structured_event" || recovered[1].Kind != eventComplete {
		t.Fatalf("recovered events = %#v", recovered)
	}
	if raw.String() != invalidThenValid {
		t.Fatalf("raw evidence = %q", raw.String())
	}
	raw.Reset()
	if err := readStructuredEventsWithLineage(strings.NewReader("\n  \n"), &raw, AgentCodex, "", false, func(providerEvent) error { return nil }, nil); err != nil {
		t.Fatalf("blank provider lines failed: %v", err)
	}
	if raw.String() != "\n  \n" {
		t.Fatalf("blank raw evidence = %q", raw.String())
	}
	raw.Reset()
	const skippedOutputBytes = 16 * 1024 * 1024
	largeEvent := io.MultiReader(
		strings.NewReader(`{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"`),
		io.LimitReader(repeatingByteReader('x'), skippedOutputBytes),
		strings.NewReader(`"}}`+"\n"),
	)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	var largeEvents []providerEvent
	retainedBytes := int64(0)
	if err := readStructuredEventsWithLineage(largeEvent, countingWriter{count: &retainedBytes}, AgentCodex, "", false, func(event providerEvent) error {
		largeEvents = append(largeEvents, event)
		return nil
	}, nil); err != nil {
		t.Fatalf("large valid provider event failed: %v", err)
	}
	runtime.ReadMemStats(&after)
	if len(largeEvents) != 1 || largeEvents[0].Kind != eventProgress || largeEvents[0].Activity != codexItemCompletedType {
		t.Fatalf("large provider events = %#v", largeEvents)
	}
	wantRetained := int64(skippedOutputBytes + len(`{"type":"item.completed","item":{"type":"command_execution","aggregated_output":"`+`"}}`+"\n"))
	if retainedBytes != wantRetained {
		t.Fatalf("large raw evidence retained %d bytes, want %d", retainedBytes, wantRetained)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4*1024*1024 {
		t.Fatalf("parsing %d skipped bytes allocated %d bytes; memory use must not scale with command output", skippedOutputBytes, allocated)
	}
}

func TestClaudeStructuredEventsNormalizeBoundedLineageSeparately(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"assistant","parent_tool_use_id":null,"message":{"content":[{"type":"text","text":"ignored"},{"type":"tool_use","id":"unrelated","name":"Read","input":{"huge":"ignored"}},{"type":"tool_use","id":"parent","name":"Agent","input":{"prompt":"private"}}]}}`,
		`{"type":"system","subtype":"task_started","task_id":"task-parent","tool_use_id":"parent","task_type":"local_agent","description":"ignored"}`,
		`{"type":"assistant","parent_tool_use_id":"parent","message":{"content":[{"type":"tool_use","id":"child","name":"Agent"}]}}`,
		`{"type":"system","subtype":"task_started","task_id":"task-child","tool_use_id":"child","task_type":"local_agent"}`,
		`{"type":"system","subtype":"task_started","task_id":"bash-task","tool_use_id":"bash-tool","task_type":"local_bash"}`,
		`{"type":"system","subtype":"task_notification","task_id":"bash-task","tool_use_id":"bash-tool","status":"completed"}`,
		`{"type":"system","subtype":"task_notification","task_id":"task-child","tool_use_id":"child","status":"stopped"}`,
		`{"type":"system","subtype":"task_notification","task_id":"task-parent","status":"completed"}`,
	}, "\n") + "\n"
	var raw bytes.Buffer
	var ordinary []providerEvent
	var lineage []claudeLineageEvidence
	err := readStructuredEventsWithLineage(strings.NewReader(input), &raw, AgentClaude, runtimeSessionID, true, func(event providerEvent) error {
		ordinary = append(ordinary, event)
		return nil
	}, func(evidence claudeLineageEvidence) error {
		lineage = append(lineage, evidence)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []claudeLineageEvidence{
		{Kind: lineageKindToolUse, ToolUseID: "parent"},
		{Kind: lineageKindTaskStarted, TaskID: "task-parent", ToolUseID: "parent", TaskType: "local_agent"},
		{Kind: lineageKindToolUse, ToolUseID: "child", ParentToolUseID: "parent"},
		{Kind: lineageKindTaskStarted, TaskID: "task-child", ToolUseID: "child", TaskType: "local_agent"},
		{Kind: lineageKindTaskTerminal, TaskID: "task-child", ToolUseID: "child", Status: "stopped"},
		{Kind: lineageKindTaskTerminal, TaskID: "task-parent", Status: "completed"},
	}
	if !slices.Equal(lineage, want) {
		t.Fatalf("lineage = %#v, want %#v", lineage, want)
	}
	encoded, err := json.Marshal(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"task-parent", "task-child", "child", "parent", "bash-task"} {
		if bytes.Contains(encoded, []byte(private)) {
			t.Fatalf("ordinary events leaked lineage identifier %q: %s", private, encoded)
		}
	}
	if raw.String() != input {
		t.Fatal("raw stream changed")
	}
}

func TestClaudeStructuredEventsInvokeOrdinaryCallbacksBeforeLineage(t *testing.T) {
	input := `{"type":"system","subtype":"task_started","task_id":"task","tool_use_id":"tool","task_type":"unknown"}` + "\n"
	var order []string
	err := readStructuredEventsWithLineage(strings.NewReader(input), io.Discard, AgentClaude, runtimeSessionID, true, func(providerEvent) error {
		order = append(order, "ordinary")
		return nil
	}, func(claudeLineageEvidence) error {
		order = append(order, "lineage")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []string{"ordinary", "lineage"}) {
		t.Fatalf("callback order = %#v", order)
	}

	ordinaryErr := errors.New("ordinary callback failed")
	lineageCalled := false
	err = readStructuredEventsWithLineage(strings.NewReader(input), io.Discard, AgentClaude, runtimeSessionID, true, func(providerEvent) error {
		return ordinaryErr
	}, func(claudeLineageEvidence) error {
		lineageCalled = true
		return nil
	})
	if !errors.Is(err, ordinaryErr) || lineageCalled {
		t.Fatalf("ordinary failure = %v, lineage called = %t", err, lineageCalled)
	}

	lineageErr := errors.New("lineage callback failed")
	err = readStructuredEventsWithLineage(strings.NewReader(input), io.Discard, AgentClaude, runtimeSessionID, true, func(providerEvent) error {
		return nil
	}, func(claudeLineageEvidence) error {
		return lineageErr
	})
	if !errors.Is(err, lineageErr) {
		t.Fatalf("lineage failure = %v", err)
	}
}

func TestClaudeStructuredEventsRecoverWithStableInvalidLineage(t *testing.T) {
	var blocks strings.Builder
	for index := 0; index <= claudeLineageContentLimit; index++ {
		if index > 0 {
			blocks.WriteByte(',')
		}
		blocks.WriteString(`{"type":"text","text":"ignored"}`)
	}
	input := "not-json\n" + `{"type":"assistant","message":{"content":[` + blocks.String() + `]}}` + "\n" +
		`{"type":"system","subtype":"task_started","task_id":"task","tool_use_id":"tool","task_type":"unknown"}` + "\n"
	var lineage []claudeLineageEvidence
	err := readStructuredEventsWithLineage(strings.NewReader(input), io.Discard, AgentClaude, runtimeSessionID, true, func(providerEvent) error { return nil }, func(evidence claudeLineageEvidence) error {
		lineage = append(lineage, evidence)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantReasons := []string{lineageReasonEvidenceMalformed, lineageReasonLimitExceeded, lineageReasonTaskTypeUnknown}
	var gotReasons []string
	for _, evidence := range lineage {
		if evidence.Kind == lineageKindInvalid {
			gotReasons = append(gotReasons, evidence.Reason)
		}
	}
	if !slices.Equal(gotReasons, wantReasons) {
		t.Fatalf("invalid reasons = %#v, want %#v (all %#v)", gotReasons, wantReasons, lineage)
	}
}

func TestClaudeStructuredNullRequiredFieldsUseExistingReasons(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		reason string
	}{
		{name: "task identifier", input: `{"type":"system","subtype":"task_started","task_id":null,"tool_use_id":"tool","task_type":"local_agent"}`, reason: lineageReasonTaskIdentifierMissing},
		{name: "tool identifier", input: `{"type":"system","subtype":"task_started","task_id":"task","tool_use_id":null,"task_type":"local_agent"}`, reason: lineageReasonTaskIdentifierMissing},
		{name: "task type", input: `{"type":"system","subtype":"task_started","task_id":"task","tool_use_id":"tool","task_type":null}`, reason: lineageReasonTaskTypeMissing},
		{name: "terminal task identifier", input: `{"type":"system","subtype":"task_notification","task_id":null,"status":"completed"}`, reason: lineageReasonTaskIdentifierMissing},
		{name: "terminal status", input: `{"type":"system","subtype":"task_notification","task_id":"task","status":null}`, reason: lineageReasonTaskStatusUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var lineage []claudeLineageEvidence
			err := readStructuredEventsWithLineage(strings.NewReader(tc.input+"\n"), io.Discard, AgentClaude, runtimeSessionID, true, func(providerEvent) error { return nil }, func(evidence claudeLineageEvidence) error {
				lineage = append(lineage, evidence)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			want := []claudeLineageEvidence{{Kind: lineageKindInvalid, Reason: tc.reason}}
			if !slices.Equal(lineage, want) {
				t.Fatalf("lineage = %#v, want %#v", lineage, want)
			}
		})
	}
}

func TestClaudeStructuredProjectionRejectsDuplicateAndCrossPairedBlocks(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"one"},{"name":"Agent","id":"two"},{"type":"tool_use","type":"tool_use","id":"three","name":"Agent"}]}}`,
		`{"type":"system","subtype":"task_started","task_id":false,"tool_use_id":"tool","task_type":"local_agent"}`,
	}, "\n") + "\n"
	var lineage []claudeLineageEvidence
	if err := readStructuredEventsWithLineage(strings.NewReader(input), io.Discard, AgentClaude, runtimeSessionID, true, func(providerEvent) error { return nil }, func(evidence claudeLineageEvidence) error {
		lineage = append(lineage, evidence)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []claudeLineageEvidence{
		{Kind: lineageKindInvalid, Reason: lineageReasonStructureInvalid},
		{Kind: lineageKindInvalid, Reason: lineageReasonStructureInvalid},
	}
	if !slices.Equal(lineage, want) {
		t.Fatalf("lineage = %#v", lineage)
	}
}

func TestReadDiagnosticLineRetainsOversizedLinePrefix(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader("conversation-marker-and-noise\nnext\n"), 8)
	line, err := readDiagnosticLine(reader, len("conversation-marker"))
	if err != nil || line != "conversation-marker" {
		t.Fatalf("oversized diagnostic prefix = %q, %v", line, err)
	}
	next, err := readDiagnosticLine(reader, 32)
	if err != nil || next != "next\n" {
		t.Fatalf("diagnostic after oversized line = %q, %v", next, err)
	}
}

func TestSelectiveJSONReaderTruncatesRetainedAnswerAndConsumesRecord(t *testing.T) {
	parser := newSelectiveJSONReader()
	parser.retainedStringBytes = 8
	parser.reset(strings.NewReader(`{"type":"agent_message","message":"abcdefghijk","ignored":"complete"}`))
	value, err := parser.next()
	if err != nil {
		t.Fatalf("parse oversized retained answer: %v", err)
	}
	events := reduceCodexEvent(value.Fields)
	if len(events) != 1 || events[0].Answer != "abcdefgh"+truncatedAnswerNotice {
		t.Fatalf("truncated structured answer events = %#v", events)
	}
	if _, err := parser.next(); err != io.EOF {
		t.Fatalf("parser did not consume complete oversized record: %v", err)
	}
}

func TestAnswerPrefixBufferTruncatesWithoutShortWrite(t *testing.T) {
	buffer := answerPrefixBuffer{limit: 8}
	written, err := buffer.Write([]byte("abcdefghijk"))
	if err != nil || written != len("abcdefghijk") {
		t.Fatalf("answer prefix write = %d, %v", written, err)
	}
	if got := buffer.String(); got != "abcdefgh"+truncatedAnswerNotice {
		t.Fatalf("truncated plain answer = %q", got)
	}
}

func TestRetainedAnswerTruncationDropsIncompleteUTF8Rune(t *testing.T) {
	parser := newSelectiveJSONReader()
	parser.retainedStringBytes = 1
	parser.reset(strings.NewReader(`{"type":"agent_message","message":"éx"}`))
	value, err := parser.next()
	if err != nil {
		t.Fatalf("parse UTF-8 answer at boundary: %v", err)
	}
	events := reduceCodexEvent(value.Fields)
	if len(events) != 1 || events[0].Answer != truncatedAnswerNotice {
		t.Fatalf("UTF-8 structured truncation events = %#v", events)
	}

	buffer := answerPrefixBuffer{limit: 1}
	if _, err := buffer.Write([]byte("éx")); err != nil {
		t.Fatalf("write UTF-8 plain answer: %v", err)
	}
	if got := buffer.String(); got != truncatedAnswerNotice {
		t.Fatalf("UTF-8 plain truncation = %q", got)
	}
}

type repeatingByteReader byte

func (r repeatingByteReader) Read(data []byte) (int, error) {
	for i := range data {
		data[i] = byte(r)
	}
	return len(data), nil
}

type countingWriter struct {
	count *int64
}

func (w countingWriter) Write(data []byte) (int, error) {
	*w.count += int64(len(data))
	return len(data), nil
}

func TestRunnerBuffersOnlyCompletedAnswer(t *testing.T) {
	root := t.TempDir()
	successfulRun, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], "fresh")
	if err != nil {
		t.Fatalf("new successful run: %v", err)
	}
	var persisted string
	result, err := executeProvider(providerCommand{
		Path:       "/bin/sh",
		Args:       []string{"-c", `printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n{"type":"agent_message","message":"answer"}\n{"type":"turn.completed"}\n'`},
		Env:        os.Environ(),
		Provider:   AgentCodex,
		SessionID:  runtimeSessionID,
		Structured: true,
	}, []byte("prompt"), successfulRun, root, nil, func(id string) error {
		persisted = id
		return nil
	})
	if err != nil || !result.Complete || !result.AnswerSeen || persisted != runtimeSessionID {
		t.Fatalf("success result=%#v err=%v persisted=%q", result, err, persisted)
	}
	if result.Answer != "answer" {
		t.Fatalf("terminal answer candidate = %q", result.Answer)
	}
	if _, statErr := os.Stat(successfulRun.Record.AnswerPath); !os.IsNotExist(statErr) {
		t.Fatalf("runner published answer before provider-specific terminal validation: %v", statErr)
	}

	incompleteRun, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], "fresh")
	if err != nil {
		t.Fatalf("new incomplete run: %v", err)
	}
	_, err = executeProvider(providerCommand{
		Path:       "/bin/sh",
		Args:       []string{"-c", `printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n{"type":"agent_message","message":"partial"}\n'`},
		Env:        os.Environ(),
		Provider:   AgentCodex,
		SessionID:  runtimeSessionID,
		Structured: true,
	}, []byte("prompt"), incompleteRun, root, nil, func(string) error { return nil })
	requireDispatchExitCode(t, err, ExitTargetFailure)
	if _, readErr := os.Stat(incompleteRun.Record.AnswerPath); !os.IsNotExist(readErr) {
		t.Fatalf("incomplete turn published a terminal answer: %v", readErr)
	}
	raw, readErr := os.ReadFile(incompleteRun.Record.StdoutPath)
	if readErr != nil || !bytes.Contains(raw, []byte("partial")) {
		t.Fatalf("raw progress evidence = %q, %v", raw, readErr)
	}
}

func TestClaudeRunnerReadsLineageAndLatestResultThroughEOF(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentClaude, "2.1.212", dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := filepath.Abs(filepath.Join("testdata", "claude", "v0.13-2.1.212-lineage.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := executeProvider(providerCommand{
		Path:          "/bin/sh",
		Args:          []string{"-c", `cat "$1"`, "sh", fixture},
		Env:           os.Environ(),
		Provider:      AgentClaude,
		SessionID:     runtimeSessionID,
		Structured:    true,
		ClaudeLineage: true,
	}, []byte("prompt"), run, root, nil, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "final" || !result.Complete || result.SessionID != runtimeSessionID {
		t.Fatalf("result = %#v", result)
	}
	lineage, err := os.ReadFile(run.Record.LineagePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`{"kind":"task_started","task_id":"task-parent","tool_use_id":"agent-parent","task_type":"local_agent"}`,
		`{"kind":"task_terminal","task_id":"task-parent","status":"completed"}`,
		`{"kind":"task_terminal","task_id":"task-child","tool_use_id":"agent-child","status":"failed"}`,
	} {
		if !bytes.Contains(lineage, []byte(want)) {
			t.Fatalf("lineage evidence missing %q: %s", want, lineage)
		}
	}
	events, err := os.ReadFile(run.Record.EventsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"task-parent", "agent-parent", "task-child"} {
		if bytes.Contains(events, []byte(private)) {
			t.Fatalf("ordinary events leaked %q: %s", private, events)
		}
	}
}

func TestAntigravityLogIDIsStrictAndVersionGateFailsLoudly(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "antigravity.log")
	if err := os.WriteFile(logPath, []byte("I0712 19:00:00.123456 42 logger.go] Created conversation AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	id, err := antigravitySessionID(logPath)
	if err != nil || id != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("Antigravity ID = %q, %v", id, err)
	}
	conflicting := "Created conversation AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA\n" +
		"Print mode: conversation=BBBBBBBB-BBBB-4BBB-8BBB-BBBBBBBBBBBB, sending message\n"
	if err := os.WriteFile(logPath, []byte(conflicting), 0o600); err != nil {
		t.Fatalf("write conflicting log: %v", err)
	}
	if id, err := antigravitySessionID(logPath); err == nil || id != "" {
		t.Fatalf("conflicting Antigravity IDs = %q, %v", id, err)
	}
	longDiagnostic := strings.Repeat("x", 128*1024)
	if err := os.WriteFile(logPath, []byte(longDiagnostic+"\nCreated conversation AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA\n"), 0o600); err != nil {
		t.Fatalf("write long diagnostic log: %v", err)
	}
	if id, err := antigravitySessionID(logPath); err != nil || id != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("long diagnostic log ID = %q, %v", id, err)
	}
	_, err = requireSupportedVersion("ignored", AgentCodex, func(string, string) (string, error) { return "0.1.0", nil })
	requireDispatchExitCode(t, err, ExitUnavailable)
}

func TestGrokStructuredEvents(t *testing.T) {
	expectedSession := runtimeSessionID

	t.Run("thought progress event", func(t *testing.T) {
		events, err := reduceStructuredTestEvent(AgentGrok, expectedSession, []byte(`{"type":"thought","data":"thinking about the plan"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 1 || events[0].Kind != eventProgress || events[0].Activity != "thought" {
			t.Fatalf("events = %#v", events)
		}
	})

	t.Run("accumulates text chunks and completes on end", func(t *testing.T) {
		streamData, readErr := os.ReadFile(filepath.Join("testdata", "grok", "v1.0.5-streaming.jsonl"))
		if readErr != nil {
			t.Fatalf("read provider-derived Grok fixture: %v", readErr)
		}
		var events []providerEvent
		err := readStructuredEventsWithLineage(bytes.NewReader(streamData), io.Discard, AgentGrok, expectedSession, false, func(e providerEvent) error {
			events = append(events, e)
			return nil
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 6 {
			t.Fatalf("got %d events, want 6: %#v", len(events), events)
		}
		// Expect: progress(thought), progress(text_delta), progress(text_delta), session, answer("Hello world!"), complete
		if events[3].Kind != eventSession || events[3].SessionID != expectedSession {
			t.Fatalf("expected session event, got %#v", events[3])
		}
		if events[4].Kind != eventAnswer || events[4].Answer != "Hello world!" {
			t.Fatalf("expected answer 'Hello world!', got %#v", events[4])
		}
		if events[5].Kind != eventComplete {
			t.Fatalf("expected complete event, got %#v", events[5])
		}
	})

	t.Run("reports failure on mismatched session ID", func(t *testing.T) {
		events, err := reduceStructuredTestEvent(AgentGrok, expectedSession, []byte(`{"type":"end","sessionId":"other-session","stopReason":"end_turn"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 1 || events[0].Kind != eventFailure {
			t.Fatalf("expected failure event, got %#v", events)
		}
	})

	t.Run("accepts documented completed turn spelling", func(t *testing.T) {
		var text strings.Builder
		text.WriteString("complete")
		events := reduceGrokEvent(expectedSession, map[string]any{
			"type": "end", "sessionId": expectedSession, "stopReason": "end_turn",
		}, &text)
		if len(events) != 3 || events[2].Kind != eventComplete {
			t.Fatalf("expected completed events, got %#v", events)
		}
	})

	t.Run("reports failure on abnormal stop reason", func(t *testing.T) {
		events, err := reduceStructuredTestEvent(AgentGrok, expectedSession, []byte(`{"type":"end","sessionId":"`+expectedSession+`","stopReason":"refusal"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 1 || events[0].Kind != eventFailure || !strings.Contains(events[0].Reason, "refusal") {
			t.Fatalf("expected refusal failure event, got %#v", events)
		}
	})

	t.Run("reports failure on error event", func(t *testing.T) {
		events, err := reduceStructuredTestEvent(AgentGrok, expectedSession, []byte(`{"type":"error","message":"rate limit exceeded"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 1 || events[0].Kind != eventFailure || !strings.Contains(events[0].Reason, "rate limit") {
			t.Fatalf("expected error failure event, got %#v", events)
		}
	})

	t.Run("fails on provider-observed permission denial before completed end", func(t *testing.T) {
		streamData, readErr := os.ReadFile(filepath.Join("testdata", "grok", "v1.0.5-denied-tool.jsonl"))
		if readErr != nil {
			t.Fatalf("read provider-derived Grok denial fixture: %v", readErr)
		}
		var events []providerEvent
		err := readStructuredEventsWithLineage(bytes.NewReader(streamData), io.Discard, AgentGrok, expectedSession, false, func(event providerEvent) error {
			events = append(events, event)
			if event.Kind == eventFailure {
				return errors.New(event.Reason)
			}
			return nil
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "Denied by permission policy") {
			t.Fatalf("denied stream error = %v, events = %#v", err, events)
		}
		if !slices.ContainsFunc(events, func(event providerEvent) bool { return event.Kind == eventFailure }) {
			t.Fatalf("denied stream emitted no failure: %#v", events)
		}
		if slices.ContainsFunc(events, func(event providerEvent) bool { return event.Kind == eventComplete }) {
			t.Fatalf("denied stream reached completion: %#v", events)
		}
	})

	t.Run("accepts provider-observed resumed session echo", func(t *testing.T) {
		const resumedSession = "7b0f2d84-9a63-4e17-8c52-14f6b93d20aa"
		streamData, readErr := os.ReadFile(filepath.Join("testdata", "grok", "v1.0.5-resume-streaming.jsonl"))
		if readErr != nil {
			t.Fatalf("read provider-derived Grok resume fixture: %v", readErr)
		}
		var events []providerEvent
		err := readStructuredEventsWithLineage(bytes.NewReader(streamData), io.Discard, AgentGrok, resumedSession, false, func(event providerEvent) error {
			events = append(events, event)
			return nil
		}, nil)
		if err != nil {
			t.Fatalf("resume stream error: %v", err)
		}
		if !slices.ContainsFunc(events, func(event providerEvent) bool { return event.Kind == eventComplete }) {
			t.Fatalf("resume stream did not complete: %#v", events)
		}
	})

	t.Run("ordinary failed tool update remains progress", func(t *testing.T) {
		events, err := reduceStructuredTestEvent(AgentGrok, expectedSession, []byte(`{"type":"tool_call_update","status":"failed","content":[{"type":"content","content":{"type":"text","text":"Command exited with status 1"}}]}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 1 || events[0].Kind != eventProgress || events[0].Activity != grokToolUpdateType {
			t.Fatalf("ordinary failed tool events = %#v", events)
		}
	})

	t.Run("overlong denial reports the tool-update retention limit", func(t *testing.T) {
		message := "Tool `run_terminal_command` was not executed: Denied by permission policy: " + strings.Repeat("x", 700)
		record := fmt.Sprintf(`{"type":"tool_call_update","status":"failed","content":[{"type":"content","content":{"type":"text","text":%q}}]}`, message)
		events, err := reduceStructuredTestEvent(AgentGrok, expectedSession, []byte(record))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(events) != 1 || events[0].Kind != eventFailure || !strings.Contains(events[0].Reason, "retaining 512 bytes") {
			t.Fatalf("overlong denial events = %#v", events)
		}
		if strings.Contains(events[0].Reason, "256 MiB") {
			t.Fatalf("overlong denial used final-answer notice: %q", events[0].Reason)
		}
	})
}

func TestGrokRunnerReadsStreamingJSONThroughEOF(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentGrok, clientgrok.SupportedVersion, dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	fixtureContent := `{"type":"thought","data":"thinking"}` + "\n" +
		`{"type":"text","data":"Grok output"}` + "\n" +
		`{"type":"end","sessionId":"` + runtimeSessionID + `","stopReason":"end_turn"}` + "\n"
	result, err := executeProvider(providerCommand{
		Path:       "/bin/sh",
		Args:       []string{"-c", `printf '%s' "$1"`, "sh", fixtureContent},
		Env:        os.Environ(),
		Provider:   AgentGrok,
		SessionID:  runtimeSessionID,
		Structured: true,
	}, []byte("prompt"), run, root, nil, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != "Grok output" || !result.Complete || result.SessionID != runtimeSessionID {
		t.Fatalf("result = %#v", result)
	}
}

func TestGrokRunnerFailsProviderObservedPermissionDenial(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentGrok, clientgrok.SupportedVersion, dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	streamData, err := os.ReadFile(filepath.Join("testdata", "grok", "v1.0.5-denied-tool.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeProvider(providerCommand{
		Path:       "/bin/sh",
		Args:       []string{"-c", `printf '%s' "$1"`, "sh", string(streamData)},
		Env:        os.Environ(),
		Provider:   AgentGrok,
		SessionID:  runtimeSessionID,
		Structured: true,
	}, []byte("prompt"), run, root, nil, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "Denied by permission policy") {
		t.Fatalf("denied provider run error = %v", err)
	}
	requireDispatchExitCode(t, err, ExitTargetFailure)
}
