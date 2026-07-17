package agentdispatch

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
			childEnv := dispatchEnvironment(tt.baseEnv, project, run, 1, AgentClaude)
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

func TestStructuredEventsRejectChangedProviderContracts(t *testing.T) {
	claudeEvents, err := reduceStructuredEvent(AgentClaude, runtimeSessionID, []byte(`{"type":"result","session_id":"22222222-2222-4222-8222-222222222222","is_error":false}`))
	if err != nil || len(claudeEvents) != 1 || claudeEvents[0].Kind != eventFailure {
		t.Fatalf("Claude events = %#v, %v", claudeEvents, err)
	}
	codexEvents, err := reduceStructuredEvent(AgentCodex, "", []byte(`{"type":"item.completed","item":{"type":"agent_message","text":"final answer"}}`))
	if err != nil || len(codexEvents) != 1 || codexEvents[0].Answer != "final answer" {
		t.Fatalf("Codex events = %#v, %v", codexEvents, err)
	}
	progressEvents, err := reduceStructuredEvent(AgentCodex, "", []byte(`{"type":"item.completed","item":{"type":"command_execution","command":"pwd"}}`))
	if err != nil || len(progressEvents) != 1 || progressEvents[0].Kind != eventProgress || progressEvents[0].Activity != "item.completed" {
		t.Fatalf("Codex non-agent item.completed events = %#v, %v", progressEvents, err)
	}
	flatEvents, err := reduceStructuredEvent(AgentCodex, "", []byte(`{"type":"agent_message","message":"compatible answer"}`))
	if err != nil || len(flatEvents) != 1 || flatEvents[0].Answer != "compatible answer" {
		t.Fatalf("Codex flat compatibility events = %#v, %v", flatEvents, err)
	}
	escapedSlashEvents, err := reduceStructuredEvent(AgentCodex, "", []byte(`{"type":"agent_message","message":"https:\/\/example.com\/answer"}`))
	if err != nil || len(escapedSlashEvents) != 1 || escapedSlashEvents[0].Answer != "https://example.com/answer" {
		t.Fatalf("Codex escaped-slash events = %#v, %v", escapedSlashEvents, err)
	}
	failureEvents, err := reduceStructuredEvent(AgentCodex, "", []byte(`{"type":"turn.failed","error":{"message":"model quota exhausted"}}`))
	if err != nil || len(failureEvents) != 1 || failureEvents[0].Kind != eventFailure || failureEvents[0].Reason != "model quota exhausted" {
		t.Fatalf("Codex nested failure events = %#v, %v", failureEvents, err)
	}
	stringFailureEvents, err := reduceStructuredEvent(AgentCodex, "", []byte(`{"type":"error","error":"quota exhausted"}`))
	if err != nil || len(stringFailureEvents) != 1 || stringFailureEvents[0].Kind != eventFailure || stringFailureEvents[0].Reason != "quota exhausted" {
		t.Fatalf("Codex string failure events = %#v, %v", stringFailureEvents, err)
	}
	var raw bytes.Buffer
	var recovered []providerEvent
	invalidThenValid := "not-json\n" + `{"type":"turn.completed"}` + "\n"
	if err := readStructuredEvents(strings.NewReader(invalidThenValid), &raw, AgentCodex, "", func(event providerEvent) error {
		recovered = append(recovered, event)
		return nil
	}); err != nil {
		t.Fatalf("invalid provider JSON prevented later record recovery: %v", err)
	}
	if len(recovered) != 2 || recovered[0].Kind != eventProgress || recovered[0].Activity != "invalid_structured_event" || recovered[1].Kind != eventComplete {
		t.Fatalf("recovered events = %#v", recovered)
	}
	if raw.String() != invalidThenValid {
		t.Fatalf("raw evidence = %q", raw.String())
	}
	raw.Reset()
	if err := readStructuredEvents(strings.NewReader("\n  \n"), &raw, AgentCodex, "", func(providerEvent) error { return nil }); err != nil {
		t.Fatalf("blank provider lines failed: %v", err)
	}
	if raw.String() != "\n  \n" {
		t.Fatalf("blank raw evidence = %q", raw.String())
	}
	raw.Reset()
	const skippedOutputBytes = 482 * 1024 * 1024
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
	if err := readStructuredEvents(largeEvent, countingWriter{count: &retainedBytes}, AgentCodex, "", func(event providerEvent) error {
		largeEvents = append(largeEvents, event)
		return nil
	}); err != nil {
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
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 32*1024*1024 {
		t.Fatalf("parsing %d skipped bytes allocated %d bytes; memory use must not scale with command output", skippedOutputBytes, allocated)
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
	events, err := reduceCodexEvent(value)
	if err != nil || len(events) != 1 || events[0].Answer != "abcdefgh"+truncatedAnswerNotice {
		t.Fatalf("truncated structured answer events = %#v, %v", events, err)
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
	events, err := reduceCodexEvent(value)
	if err != nil || len(events) != 1 || events[0].Answer != truncatedAnswerNotice {
		t.Fatalf("UTF-8 structured truncation events = %#v, %v", events, err)
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
