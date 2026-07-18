package agentdispatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDeriveClaudeDescendantSummaryProvesAuthoritativeHierarchy(t *testing.T) {
	root := t.TempDir()
	run := newLineageTestRun(t, root, "2.1.212")
	writeLineageTestEvidence(t, run,
		claudeLineageEvidence{Kind: lineageKindToolUse, ToolUseID: "tool-parent"},
		claudeLineageEvidence{Kind: lineageKindTaskStarted, TaskID: "task-parent", ToolUseID: "tool-parent", TaskType: "local_agent"},
		claudeLineageEvidence{Kind: lineageKindToolUse, ToolUseID: "tool-child", ParentToolUseID: "tool-parent"},
		claudeLineageEvidence{Kind: lineageKindTaskStarted, TaskID: "task-child", ToolUseID: "tool-child", TaskType: "local_agent"},
		claudeLineageEvidence{Kind: lineageKindTaskTerminal, TaskID: "task-child", ToolUseID: "tool-child", Status: "failed"},
		claudeLineageEvidence{Kind: lineageKindTaskTerminal, TaskID: "task-parent", Status: "completed"},
	)

	summary := deriveClaudeDescendantSummary(root, run.Record)
	if summary.State != "proven-terminal" || len(summary.Reasons) != 0 || len(summary.Tasks) != 2 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.Tasks[0].TaskID != "task-parent" || summary.Tasks[0].Status != "completed" || summary.Tasks[1].ParentTaskID != "task-parent" || summary.Tasks[1].Status != "failed" {
		t.Fatalf("tasks = %#v", summary.Tasks)
	}
}

func TestDeriveClaudeDescendantSummaryReportsConservativeUnknowns(t *testing.T) {
	tests := []struct {
		name     string
		evidence []claudeLineageEvidence
		want     []string
	}{
		{name: "zero starts", want: []string{lineageReasonTaskStartAbsent}},
		{name: "agent without start", evidence: []claudeLineageEvidence{{Kind: lineageKindToolUse, ToolUseID: "tool"}}, want: []string{lineageReasonTaskStartAbsent, lineageReasonTaskStartMissing}},
		{name: "start without relationship or terminal", evidence: []claudeLineageEvidence{{Kind: lineageKindTaskStarted, TaskID: "task", ToolUseID: "tool", TaskType: "local_agent"}}, want: []string{lineageReasonTaskRelationshipMissing, lineageReasonTaskTerminalMissing}},
		{name: "terminal before start", evidence: []claudeLineageEvidence{{Kind: lineageKindTaskTerminal, TaskID: "task", Status: "completed"}, {Kind: lineageKindToolUse, ToolUseID: "tool"}, {Kind: lineageKindTaskStarted, TaskID: "task", ToolUseID: "tool", TaskType: "local_agent"}}, want: []string{lineageReasonTaskEventOrderInvalid, lineageReasonTaskStartMissing, lineageReasonTaskTerminalMissing}},
		{name: "terminal mismatch", evidence: []claudeLineageEvidence{{Kind: lineageKindToolUse, ToolUseID: "tool"}, {Kind: lineageKindTaskStarted, TaskID: "task", ToolUseID: "tool", TaskType: "local_agent"}, {Kind: lineageKindTaskTerminal, TaskID: "task", ToolUseID: "other", Status: "stopped"}}, want: []string{lineageReasonTaskToolUseMismatch}},
		{name: "depth continuity missing", evidence: []claudeLineageEvidence{{Kind: lineageKindToolUse, ToolUseID: "parent"}, {Kind: lineageKindTaskStarted, TaskID: "parent-task", ToolUseID: "parent", TaskType: "local_agent"}, {Kind: lineageKindTaskStarted, TaskID: "deep-task", ToolUseID: "missing", TaskType: "local_agent"}, {Kind: lineageKindTaskTerminal, TaskID: "parent-task", Status: "completed"}, {Kind: lineageKindTaskTerminal, TaskID: "deep-task", Status: "completed"}}, want: []string{lineageReasonTaskRelationshipMissing}},
		{name: "invalid status marker", evidence: []claudeLineageEvidence{{Kind: lineageKindInvalid, Reason: lineageReasonTaskStatusUnknown}}, want: []string{lineageReasonTaskStartAbsent, lineageReasonTaskStatusUnknown}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			run := newLineageTestRun(t, root, "2.1.212")
			writeLineageTestEvidence(t, run, test.evidence...)
			summary := deriveClaudeDescendantSummary(root, run.Record)
			if summary.State != statusUnknown || !slices.Equal(summary.Reasons, test.want) {
				t.Fatalf("reasons = %#v, want %#v (summary %#v)", summary.Reasons, test.want, summary)
			}
		})
	}
}

func TestDeriveClaudeDescendantSummaryHandlesDuplicatesConflictsAndCycles(t *testing.T) {
	root := t.TempDir()
	run := newLineageTestRun(t, root, "2.1.212")
	evidence := []claudeLineageEvidence{
		{Kind: lineageKindToolUse, ToolUseID: "a", ParentToolUseID: "b"},
		{Kind: lineageKindToolUse, ToolUseID: "a", ParentToolUseID: "b"}, // exact duplicate is idempotent
		{Kind: lineageKindTaskStarted, TaskID: "task-a", ToolUseID: "a", TaskType: "local_agent"},
		{Kind: lineageKindToolUse, ToolUseID: "b", ParentToolUseID: "a"},
		{Kind: lineageKindTaskStarted, TaskID: "task-b", ToolUseID: "b", TaskType: "local_agent"},
		{Kind: lineageKindTaskTerminal, TaskID: "task-a", Status: "completed"},
		{Kind: lineageKindTaskTerminal, TaskID: "task-b", Status: "completed"},
		{Kind: lineageKindTaskTerminal, TaskID: "task-b", Status: "failed"},
	}
	writeLineageTestEvidence(t, run, evidence...)
	summary := deriveClaudeDescendantSummary(root, run.Record)
	for _, reason := range []string{lineageReasonTaskCycle, lineageReasonTaskParentUnresolved, lineageReasonTaskRelationshipConflict} {
		if !slices.Contains(summary.Reasons, reason) {
			t.Fatalf("summary omitted %q: %#v", reason, summary)
		}
	}
}

func TestDeriveClaudeDescendantSummaryValidatesVersionAndCanonicalPath(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		version string
		reason  string
	}{{version: "", reason: lineageReasonProviderVersionMissing}, {version: "bad", reason: lineageReasonProviderVersionMalformed}, {version: "2.1.210", reason: lineageReasonProviderVersionUnsupported}} {
		record := RunRecord{Agent: AgentClaude, ProviderVersion: test.version}
		if got := deriveClaudeDescendantSummary(root, record).Reasons; !slices.Equal(got, []string{test.reason}) {
			t.Fatalf("version %q reasons = %#v", test.version, got)
		}
	}

	run := newLineageTestRun(t, root, "2.1.212")
	run.Record.LineagePath = filepath.Join(root, "other", "provider.lineage")
	if got := deriveClaudeDescendantSummary(root, run.Record).Reasons; !slices.Equal(got, []string{lineageReasonPathInvalid}) {
		t.Fatalf("cross-run path reasons = %#v", got)
	}

	run.Record.LineagePath = filepath.Join(run.Dir, "provider.lineage")
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, run.Record.LineagePath); err != nil {
		t.Fatal(err)
	}
	if got := deriveClaudeDescendantSummary(root, run.Record).Reasons; !slices.Equal(got, []string{lineageReasonPathInvalid}) {
		t.Fatalf("symlink reasons = %#v", got)
	}
}

func TestDeriveClaudeDescendantSummaryRejectsMalformedNormalizedEvidence(t *testing.T) {
	root := t.TempDir()
	run := newLineageTestRun(t, root, "2.1.212")
	if err := os.WriteFile(run.Record.LineagePath, []byte(`{"kind":"tool_use","tool_use_id":"tool","extra":true}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	summary := deriveClaudeDescendantSummary(root, run.Record)
	if !slices.Contains(summary.Reasons, lineageReasonEvidenceMalformed) {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestNonClaudeRunHasNoClaudeSummary(t *testing.T) {
	if summary := deriveClaudeDescendantSummary(t.TempDir(), RunRecord{Agent: AgentCodex}); summary != nil {
		t.Fatalf("Codex summary = %#v", summary)
	}
}

func newLineageTestRun(t *testing.T, root string, providerVersion string) *dispatchRun {
	t.Helper()
	run, err := newDispatchRun(root, AgentClaude, providerVersion, dispatchModeFresh)
	if err != nil {
		t.Fatalf("new Claude run: %v", err)
	}
	return run
}

func writeLineageTestEvidence(t *testing.T, run *dispatchRun, evidence ...claudeLineageEvidence) {
	t.Helper()
	var output strings.Builder
	for _, item := range evidence {
		encoded, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		output.Write(encoded)
		output.WriteByte('\n')
	}
	if err := os.WriteFile(run.Record.LineagePath, []byte(output.String()), 0o600); err != nil {
		t.Fatalf("write lineage evidence: %v", err)
	}
}
