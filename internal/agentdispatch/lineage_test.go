package agentdispatch

import (
	"bytes"
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

func TestDeriveClaudeDescendantSummaryResolvesParentAfterChildStart(t *testing.T) {
	root := t.TempDir()
	run := newLineageTestRun(t, root, "2.1.212")
	writeLineageTestEvidence(t, run,
		claudeLineageEvidence{Kind: lineageKindToolUse, ToolUseID: "tool-parent"},
		claudeLineageEvidence{Kind: lineageKindToolUse, ToolUseID: "tool-child", ParentToolUseID: "tool-parent"},
		claudeLineageEvidence{Kind: lineageKindTaskStarted, TaskID: "task-child", ToolUseID: "tool-child", TaskType: "local_agent"},
		claudeLineageEvidence{Kind: lineageKindTaskStarted, TaskID: "task-parent", ToolUseID: "tool-parent", TaskType: "local_agent"},
		claudeLineageEvidence{Kind: lineageKindTaskTerminal, TaskID: "task-child", Status: "completed"},
		claudeLineageEvidence{Kind: lineageKindTaskTerminal, TaskID: "task-parent", Status: "completed"},
	)

	summary := deriveClaudeDescendantSummary(root, run.Record)
	if summary.State != claudeSummaryProven || len(summary.Reasons) != 0 || len(summary.Tasks) != 2 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.Tasks[0].ParentTaskID != "task-parent" || summary.Tasks[1].TaskID != "task-parent" {
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

func TestDeriveClaudeDescendantSummaryReportsAbsentEvidenceForCapableOldRecord(t *testing.T) {
	record := RunRecord{Agent: AgentClaude, ProviderVersion: "2.1.212"}

	summary := deriveClaudeDescendantSummary(t.TempDir(), record)
	if summary.State != statusUnknown || !slices.Equal(summary.Reasons, []string{"lineage_evidence_absent"}) || len(summary.Tasks) != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestDeriveClaudeDescendantSummaryReportsUnreadableCanonicalLineageFile(t *testing.T) {
	root := t.TempDir()
	run := newLineageTestRun(t, root, "2.1.212")
	if err := os.WriteFile(run.Record.LineagePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(run.Record.LineagePath, 0o000); err != nil { // #nosec G302 -- restrictive test mode exercises the production unreadable-evidence path.
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(run.Record.LineagePath, 0o600) }) // #nosec G302 -- test restores owner access for TempDir cleanup.
	if file, err := os.Open(run.Record.LineagePath); err == nil {
		_ = file.Close()
		t.Skip("fixture remains readable despite mode 0o000")
	}

	summary := deriveClaudeDescendantSummary(root, run.Record)
	if summary.State != statusUnknown || !slices.Equal(summary.Reasons, []string{"lineage_evidence_unreadable"}) || len(summary.Tasks) != 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestDeriveClaudeDescendantSummaryReportsUnresolvedParentToolUse(t *testing.T) {
	root := t.TempDir()
	run := newLineageTestRun(t, root, "2.1.212")
	writeLineageTestEvidence(t, run,
		claudeLineageEvidence{Kind: lineageKindToolUse, ToolUseID: "tool-child", ParentToolUseID: "tool-parent-missing"},
		claudeLineageEvidence{Kind: lineageKindTaskStarted, TaskID: "task-child", ToolUseID: "tool-child", TaskType: claudeTaskTypeAgent},
		claudeLineageEvidence{Kind: lineageKindTaskTerminal, TaskID: "task-child", Status: dispatchStateCompleted},
	)

	summary := deriveClaudeDescendantSummary(root, run.Record)
	if summary.State != statusUnknown || !slices.Equal(summary.Reasons, []string{"task_parent_unresolved"}) {
		t.Fatalf("summary = %#v", summary)
	}
	if len(summary.Tasks) != 1 || summary.Tasks[0].TaskID != "task-child" || summary.Tasks[0].Status != dispatchStateCompleted || summary.Tasks[0].ParentTaskID != "" {
		t.Fatalf("tasks = %#v", summary.Tasks)
	}
}

func TestDeriveClaudeDescendantSummaryStopsInferenceAfterReadFailure(t *testing.T) {
	root := t.TempDir()
	run := newLineageTestRun(t, root, "2.1.212")
	if err := os.WriteFile(run.Record.LineagePath, []byte(strings.Repeat("x", claudeLineageEvidenceMaxLineBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}

	summary := deriveClaudeDescendantSummary(root, run.Record)
	if summary.State != statusUnknown || !slices.Equal(summary.Reasons, []string{lineageReasonEvidenceMalformed}) {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestDeriveClaudeDescendantSummaryEnforcesCumulativeArtifactLimit(t *testing.T) {
	root := t.TempDir()
	run := newLineageTestRun(t, root, "2.1.212")
	evidence := []claudeLineageEvidence{
		{Kind: lineageKindToolUse, ToolUseID: "tool"},
		{Kind: lineageKindTaskStarted, TaskID: "task", ToolUseID: "tool", TaskType: claudeTaskTypeAgent},
		{Kind: lineageKindTaskTerminal, TaskID: "task", Status: dispatchStateCompleted},
	}
	var prefix bytes.Buffer
	encoder := json.NewEncoder(&prefix)
	for _, event := range evidence {
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	padding := bytes.Repeat([]byte(" \n"), (claudeLineageArtifactMaxBytes-prefix.Len())/2)
	artifact := append(prefix.Bytes(), padding...)
	if len(artifact) < claudeLineageArtifactMaxBytes {
		artifact = append(artifact, ' ')
	}
	if len(artifact) != claudeLineageArtifactMaxBytes {
		t.Fatalf("artifact size = %d", len(artifact))
	}
	if err := os.WriteFile(run.Record.LineagePath, artifact, 0o600); err != nil {
		t.Fatal(err)
	}

	exact := deriveClaudeDescendantSummary(root, run.Record)
	if exact.State != claudeSummaryProven || len(exact.Reasons) != 0 || len(exact.Tasks) != 1 {
		t.Fatalf("exact-limit summary = %#v", exact)
	}
	if err := os.WriteFile(run.Record.LineagePath, append(artifact, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	exceeded := deriveClaudeDescendantSummary(root, run.Record)
	if exceeded.State != statusUnknown || !slices.Equal(exceeded.Reasons, []string{lineageReasonLimitExceeded}) {
		t.Fatalf("over-limit summary = %#v", exceeded)
	}
	if len(exceeded.Tasks) != 1 || exceeded.Tasks[0].TaskID != "task" || exceeded.Tasks[0].Status != dispatchStateCompleted {
		t.Fatalf("over-limit presentation = %#v", exceeded.Tasks)
	}

	tiePrefixSize := claudeLineageArtifactMaxBytes - claudeLineageEvidenceMaxLineBytes
	tieArtifact := append([]byte{}, prefix.Bytes()...)
	tieArtifact = append(tieArtifact, bytes.Repeat([]byte(" \n"), (tiePrefixSize-len(tieArtifact))/2)...)
	if len(tieArtifact) < tiePrefixSize {
		tieArtifact = append(tieArtifact, ' ')
	}
	tieArtifact = append(tieArtifact, bytes.Repeat([]byte{'x'}, claudeLineageEvidenceMaxLineBytes+1)...)
	if err := os.WriteFile(run.Record.LineagePath, tieArtifact, 0o600); err != nil {
		t.Fatal(err)
	}
	tied := deriveClaudeDescendantSummary(root, run.Record)
	if tied.State != statusUnknown || !slices.Equal(tied.Reasons, []string{lineageReasonLimitExceeded}) {
		t.Fatalf("tied cumulative/line limit summary = %#v", tied)
	}
}

func TestDeriveClaudeDescendantSummaryRejectsChildAfterParentTerminal(t *testing.T) {
	for _, test := range []struct {
		name     string
		evidence []claudeLineageEvidence
	}{
		{
			name: "parent terminates before started child terminates",
			evidence: []claudeLineageEvidence{
				{Kind: lineageKindToolUse, ToolUseID: "parent-tool"},
				{Kind: lineageKindTaskStarted, TaskID: "parent", ToolUseID: "parent-tool", TaskType: claudeTaskTypeAgent},
				{Kind: lineageKindToolUse, ToolUseID: "child-tool", ParentToolUseID: "parent-tool"},
				{Kind: lineageKindTaskStarted, TaskID: "child", ToolUseID: "child-tool", TaskType: claudeTaskTypeAgent},
				{Kind: lineageKindTaskTerminal, TaskID: "parent", Status: dispatchStateCompleted},
				{Kind: lineageKindTaskTerminal, TaskID: "child", Status: dispatchStateCompleted},
			},
		},
		{
			name: "child starts after parent terminal",
			evidence: []claudeLineageEvidence{
				{Kind: lineageKindToolUse, ToolUseID: "parent-tool"},
				{Kind: lineageKindTaskStarted, TaskID: "parent", ToolUseID: "parent-tool", TaskType: claudeTaskTypeAgent},
				{Kind: lineageKindTaskTerminal, TaskID: "parent", Status: dispatchStateCompleted},
				{Kind: lineageKindToolUse, ToolUseID: "child-tool", ParentToolUseID: "parent-tool"},
				{Kind: lineageKindTaskStarted, TaskID: "child", ToolUseID: "child-tool", TaskType: claudeTaskTypeAgent},
				{Kind: lineageKindTaskTerminal, TaskID: "child", Status: dispatchStateCompleted},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			run := newLineageTestRun(t, root, "2.1.212")
			writeLineageTestEvidence(t, run, test.evidence...)

			summary := deriveClaudeDescendantSummary(root, run.Record)
			if summary.State != statusUnknown || !slices.Contains(summary.Reasons, lineageReasonTaskEventOrderInvalid) {
				t.Fatalf("summary = %#v", summary)
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
	for _, reason := range []string{lineageReasonTaskCycle, lineageReasonTaskRelationshipConflict} {
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
