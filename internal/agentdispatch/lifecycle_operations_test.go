package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestHistoryUsesImmutableRunsAfterFriendlyMappingAdvances(t *testing.T) {
	root := t.TempDir()
	first, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, first)
	if err != nil {
		t.Fatal(err)
	}
	first.Record.State = dispatchStateCompleted
	first.Record.RecoveryState = recoveryResumeRequired
	completed := time.Now().UTC()
	first.Record.CompletedAt = &completed
	if err := writeRunRecord(first.Dir, &first.Record); err != nil {
		t.Fatal(err)
	}
	second, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	second.Record.Name = session.Name
	second.Record.PreviousRunID = first.Record.ID
	second.Record.State = dispatchStateCompleted
	second.Record.RecoveryState = recoveryResumeRequired
	second.Record.CompletedAt = &completed
	if err := writeRunRecord(second.Dir, &second.Record); err != nil {
		t.Fatal(err)
	}
	session.RunID = second.Record.ID
	session.ActiveRunID = ""
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}
	unrelated, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	unrelated.Record.Name = session.Name
	if err := writeRunRecord(unrelated.Dir, &unrelated.Record); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := History(HistoryRequest{Root: root, Name: session.Name, Stdout: &output}); err != nil {
		t.Fatalf("History: %v", err)
	}
	firstIndex := strings.Index(output.String(), first.Record.ID)
	secondIndex := strings.Index(output.String(), second.Record.ID)
	if firstIndex < 0 || secondIndex < 0 || firstIndex >= secondIndex {
		t.Fatalf("history is not the complete chronological chain: %q", output.String())
	}
	if strings.Contains(output.String(), unrelated.Record.ID) {
		t.Fatalf("history included an unrelated same-name run: %q", output.String())
	}
}

func TestHistoryReportsRetentionBoundaryForMissingPredecessor(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatal(err)
	}
	run.Record.PreviousRunID = "33333333-3333-4333-8333-333333333333"
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := History(HistoryRequest{Root: root, Name: session.Name, Stdout: &output}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), run.Record.ID) || !strings.Contains(output.String(), "History begins at the 30-day retention boundary.") {
		t.Fatalf("retention-boundary history = %q", output.String())
	}
	var jsonOutput bytes.Buffer
	if err := History(HistoryRequest{Root: root, Name: session.Name, Stdout: &jsonOutput, JSON: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jsonOutput.String(), `"retention_boundary": true`) {
		t.Fatalf("retention-boundary JSON = %s", jsonOutput.String())
	}
}

func TestHistoryRejectsCorruptPredecessorWithoutEmittingOutput(t *testing.T) {
	root := t.TempDir()
	predecessor, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, predecessor)
	if err != nil {
		t.Fatal(err)
	}
	current, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	current.Record.Name = session.Name
	current.Record.PreviousRunID = predecessor.Record.ID
	if err := writeRunRecord(current.Dir, &current.Record); err != nil {
		t.Fatal(err)
	}
	session.RunID = current.Record.ID
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("not-json")
	predecessorPath := filepath.Join(predecessor.Dir, dispatchRunFile)
	if err := os.WriteFile(predecessorPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	for _, jsonOutput := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonOutput], func(t *testing.T) {
			var output bytes.Buffer
			err := History(HistoryRequest{Root: root, Name: session.Name, Stdout: &output, JSON: jsonOutput})
			requireDispatchExitCode(t, err, ExitConfig)
			if !strings.Contains(err.Error(), predecessor.Record.ID) || !strings.Contains(err.Error(), "read dispatch run record") {
				t.Fatalf("History error = %v, want predecessor ID and parse cause", err)
			}
			var syntaxErr *json.SyntaxError
			if !errors.As(err, &syntaxErr) {
				t.Fatalf("History error cause = %v, want JSON syntax error", err)
			}
			if output.Len() != 0 {
				t.Fatalf("History emitted output for a corrupt predecessor: %q", output.String())
			}
			if strings.Contains(output.String(), "retention_boundary") || strings.Contains(output.String(), "retention boundary") {
				t.Fatalf("History mislabeled corruption as retention: %q", output.String())
			}
		})
	}
	retained, err := os.ReadFile(predecessorPath) // #nosec G304 -- test-controlled path under t.TempDir.
	if err != nil {
		t.Fatalf("read corrupt predecessor evidence: %v", err)
	}
	if !bytes.Equal(retained, corrupt) {
		t.Fatalf("History mutated corrupt predecessor evidence: got %q, want %q", retained, corrupt)
	}
}

func TestHistoryRejectsMissingOrUnreadableCurrentRun(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, string)
		cause   string
		is      error
		asJSON  bool
	}{
		{name: "missing", prepare: func(t *testing.T, path string) {
			t.Helper()
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}, cause: "cannot read current run", is: errDispatchRunNotFound},
		{name: "unreadable", prepare: func(t *testing.T, path string) {
			t.Helper()
			if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, cause: "read dispatch run record", asJSON: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
			if err != nil {
				t.Fatal(err)
			}
			session, err := reserveSession(root, run)
			if err != nil {
				t.Fatal(err)
			}
			test.prepare(t, filepath.Join(run.Dir, dispatchRunFile))

			var output bytes.Buffer
			err = History(HistoryRequest{Root: root, Name: session.Name, Stdout: &output, JSON: true})
			requireDispatchExitCode(t, err, ExitConfig)
			if !strings.Contains(err.Error(), run.Record.ID) || !strings.Contains(err.Error(), test.cause) {
				t.Fatalf("History error = %v, want current run ID and cause %q", err, test.cause)
			}
			if test.is != nil && !errors.Is(err, test.is) {
				t.Fatalf("History error cause = %v, want %v", err, test.is)
			}
			if test.asJSON {
				var syntaxErr *json.SyntaxError
				if !errors.As(err, &syntaxErr) {
					t.Fatalf("History error cause = %v, want JSON syntax error", err)
				}
			}
			if output.Len() != 0 {
				t.Fatalf("History emitted output for invalid current run: %q", output.String())
			}
		})
	}
}

func TestHistoryRejectsSessionWithoutRunRecord(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(run.Dir, dispatchRunFile)
	evidence, err := os.ReadFile(recordPath) // #nosec G304 -- test-controlled path under t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	session.RunID = ""
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}
	wantError := fmt.Sprintf("dispatch session %q has no run record", session.Name)

	for _, jsonOutput := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[jsonOutput], func(t *testing.T) {
			var output bytes.Buffer
			err := History(HistoryRequest{Root: root, Name: session.Name, Stdout: &output, JSON: jsonOutput})
			requireDispatchExitCode(t, err, ExitConfig)
			if err.Error() != wantError {
				t.Fatalf("History error = %q, want %q", err, wantError)
			}
			if output.Len() != 0 {
				t.Fatalf("History emitted output for a session without a run record: %q", output.String())
			}
			retained, err := os.ReadFile(recordPath) // #nosec G304 -- test-controlled path under t.TempDir.
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(retained, evidence) {
				t.Fatalf("History mutated selected run evidence: got %q, want %q", retained, evidence)
			}
		})
	}
}

func TestHistoryRejectsForeignRunInChainWithoutEmittingOutput(t *testing.T) {
	for _, position := range []string{"current", "predecessor"} {
		t.Run(position, func(t *testing.T) {
			root := t.TempDir()
			predecessor, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
			if err != nil {
				t.Fatal(err)
			}
			session, err := reserveSession(root, predecessor)
			if err != nil {
				t.Fatal(err)
			}
			offending := predecessor
			if position == "current" {
				predecessor.Record.Name = "foreign-run-owner"
				if err := writeRunRecord(predecessor.Dir, &predecessor.Record); err != nil {
					t.Fatal(err)
				}
			} else {
				current, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
				if err != nil {
					t.Fatal(err)
				}
				current.Record.Name = session.Name
				current.Record.PreviousRunID = predecessor.Record.ID
				if err := writeRunRecord(current.Dir, &current.Record); err != nil {
					t.Fatal(err)
				}
				session.RunID = current.Record.ID
				if err := persistSession(root, session); err != nil {
					t.Fatal(err)
				}
				predecessor.Record.Name = "foreign-run-owner"
				if err := writeRunRecord(predecessor.Dir, &predecessor.Record); err != nil {
					t.Fatal(err)
				}
			}
			recordPath := filepath.Join(offending.Dir, dispatchRunFile)
			evidence, err := os.ReadFile(recordPath) // #nosec G304 -- test-controlled path under t.TempDir.
			if err != nil {
				t.Fatal(err)
			}

			for _, jsonOutput := range []bool{false, true} {
				t.Run(map[bool]string{false: "text", true: "json"}[jsonOutput], func(t *testing.T) {
					var output bytes.Buffer
					err := History(HistoryRequest{Root: root, Name: session.Name, Stdout: &output, JSON: jsonOutput})
					requireDispatchExitCode(t, err, ExitConfig)
					for _, evidence := range []string{session.Name, offending.Record.ID, `"foreign-run-owner"`} {
						if !strings.Contains(err.Error(), evidence) {
							t.Fatalf("History error = %q, want identifying evidence %q", err, evidence)
						}
					}
					if output.Len() != 0 {
						t.Fatalf("History emitted output for a foreign %s run: %q", position, output.String())
					}
					retained, err := os.ReadFile(recordPath) // #nosec G304 -- test-controlled path under t.TempDir.
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(retained, evidence) {
						t.Fatalf("History mutated foreign run evidence: got %q, want %q", retained, evidence)
					}
				})
			}
		})
	}
}

func TestHistoryFailsLoudOnPreviousRunCycle(t *testing.T) {
	root := t.TempDir()
	first, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	second.Record.Name = session.Name
	first.Record.PreviousRunID = second.Record.ID
	second.Record.PreviousRunID = first.Record.ID
	if err := writeRunRecord(first.Dir, &first.Record); err != nil {
		t.Fatal(err)
	}
	if err := writeRunRecord(second.Dir, &second.Record); err != nil {
		t.Fatal(err)
	}
	session.RunID = second.Record.ID
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}

	err = History(HistoryRequest{Root: root, Name: session.Name, Stdout: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "contains a cycle") {
		t.Fatalf("History cycle error = %v", err)
	}
	requireDispatchExitCode(t, err, ExitConfig)
}

func TestHistoryAddsOnlyClaudeDerivedSummaryWithoutChangingFlattenedFields(t *testing.T) {
	root := t.TempDir()
	claudeRun := newLineageTestRun(t, root, "2.1.212")
	session, err := reserveSession(root, claudeRun)
	if err != nil {
		t.Fatal(err)
	}
	writeLineageTestEvidence(t, claudeRun,
		claudeLineageEvidence{Kind: lineageKindToolUse, ToolUseID: "tool"},
		claudeLineageEvidence{Kind: lineageKindTaskStarted, TaskID: "task", ToolUseID: "tool", TaskType: "local_agent"},
		claudeLineageEvidence{Kind: lineageKindTaskTerminal, TaskID: "task", Status: "completed"},
	)
	if err := writeRunRecord(claudeRun.Dir, &claudeRun.Record); err != nil {
		t.Fatal(err)
	}
	var textOutput bytes.Buffer
	if err := History(HistoryRequest{Root: root, Name: session.Name, Stdout: &textOutput}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOutput.String(), "\tclaude_descendants=proven-terminal") {
		t.Fatalf("Claude history = %q", textOutput.String())
	}
	var jsonOutput bytes.Buffer
	if err := History(HistoryRequest{Root: root, Name: session.Name, Stdout: &jsonOutput, JSON: true}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"id":`, `"provider_version":`, `"claude_descendants":`} {
		if !strings.Contains(jsonOutput.String(), want) {
			t.Fatalf("history JSON omitted flattened field %q: %s", want, jsonOutput.String())
		}
	}

	codexRun, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	codexSession, err := reserveSession(root, codexRun)
	if err != nil {
		t.Fatal(err)
	}
	var codexOutput bytes.Buffer
	if err := History(HistoryRequest{Root: root, Name: codexSession.Name, Stdout: &codexOutput, JSON: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(codexOutput.String(), "claude_descendants") {
		t.Fatalf("Codex history exposed Claude summary: %s", codexOutput.String())
	}
}

func TestHistorySkipsUnreadableRunRecordsAndWarns(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatal(err)
	}
	completed := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &completed
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	corruptID := "33333333-3333-4333-8333-333333333333"
	corruptDir := filepath.Join(dispatchRunPath(root), corruptID)
	if err := os.MkdirAll(corruptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, dispatchRunFile), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	var output, warnings bytes.Buffer
	if err := History(HistoryRequest{Root: root, Name: session.Name, Stdout: &output, Stderr: &warnings}); err != nil {
		t.Fatalf("History with a corrupt unrelated record: %v", err)
	}
	if !strings.Contains(output.String(), run.Record.ID) {
		t.Fatalf("history omitted the valid turn: %q", output.String())
	}
	if !strings.Contains(warnings.String(), corruptID) {
		t.Fatalf("history hid the skipped corrupt record: %q", warnings.String())
	}
	if _, err := os.Stat(filepath.Join(corruptDir, dispatchRunFile)); err != nil {
		t.Fatalf("history mutated corrupt evidence: %v", err)
	}
}

func TestInspectDoesNotTerminalizeRunWithUnprovableOwnership(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reserveSession(root, run); err != nil {
		t.Fatal(err)
	}
	run.Record.State = dispatchStateRunning
	run.Record.RecoveryState = recoveryAcceptanceUnknown
	run.Record.PID = os.Getpid()
	run.Record.ProcessStartIdentity = ""
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := Inspect(InspectionRequest{Root: root, ID: run.Record.ID, Stdout: &output}); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateRunning {
		t.Fatalf("unprovable ownership was terminalized to %q", record.State)
	}
}

func TestRetentionRemovesOnlyExpiredUnreferencedTerminalEvidence(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	old := now.Add(-dispatchSessionRetention - time.Hour)
	makeRecord := func(state string, completed *time.Time) *dispatchRun {
		run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
		if err != nil {
			t.Fatal(err)
		}
		run.Record.State = state
		if state == dispatchStateCompleted {
			run.Record.RecoveryState = recoveryResumeRequired
		} else {
			run.Record.RecoveryState = recoveryAcceptanceUnknown
		}
		run.Record.CompletedAt = completed
		if err := writeRunRecord(run.Dir, &run.Record); err != nil {
			t.Fatal(err)
		}
		return run
	}
	expired := makeRecord(dispatchStateCompleted, &old)
	active := makeRecord(dispatchStateRunning, nil)
	current := makeRecord(dispatchStateCompleted, &old)
	session := Session{Name: "tiny-round-capacitor", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID, RunID: current.Record.ID, CreatedAt: now, LastUsedAt: now}
	if err := persistSession(root, session); err != nil {
		t.Fatal(err)
	}
	if err := pruneDispatchEvidence(root, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(expired.Dir); !os.IsNotExist(err) {
		t.Fatalf("expired terminal evidence remains: %v", err)
	}
	for _, dir := range []string{active.Dir, current.Dir} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("preserved evidence removed: %v", err)
		}
	}
}

func TestCancelEscalatesButRetainsClaimUntilOwnedProcessIsReaped(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatal(err)
	}
	readyPath := filepath.Join(t.TempDir(), "sigterm-handler-ready")
	cmd := exec.Command("/bin/sh", "-c", `trap '' TERM; touch "$1"; while :; do sleep 1; done`, "sh", readyPath) // #nosec G204 -- test-owned path passed as a positional argument.
	prepareProviderProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	waitStarted := false
	waitDone := make(chan error, 1)
	defer func() {
		if !stopped {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			if waitStarted {
				<-waitDone
			} else {
				_ = cmd.Wait()
			}
		}
	}()
	waitForFanoutTestPath(t, readyPath)
	run.Record.State = dispatchStateRunning
	run.Record.PID = cmd.Process.Pid
	run.Record.ProcessGroupID = cmd.Process.Pid
	run.Record.ProcessStartIdentity = processStartIdentity(cmd.Process.Pid)
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	waitStarted = true
	go func() { waitDone <- cmd.Wait() }()
	cancelDone := make(chan error, 1)
	go func() { cancelDone <- Cancel(CancelRequest{Root: root, ID: run.Record.ID}) }()
	waitForFanoutRunState(t, root, run.Record.ID, dispatchStateCancelled)
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != dispatchStateCancelled || record.RecoveryState != recoveryAcceptanceUnknown {
		t.Fatalf("cancelled record = %#v", record)
	}
	retainedDuringGrace, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if retainedDuringGrace.ActiveRunID != run.Record.ID || !processOwned(record) {
		t.Fatalf("cancel released a claim during the graceful shutdown window: session = %#v, record = %#v", retainedDuringGrace, record)
	}
	select {
	case err := <-cancelDone:
		if err != nil {
			t.Fatalf("Cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Cancel did not finish after bounded forced escalation")
	}
	if err := <-waitDone; err == nil {
		t.Fatal("automatically SIGKILLed test process exited successfully")
	}
	stopped = true
	released, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if released.ActiveRunID != "" {
		t.Fatalf("cancel retained the claim after proving process-group death: %#v", released)
	}
	replacement, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := claimConversation(root, session.Name, replacement.Record.ID); err != nil {
		t.Fatalf("replace cancelled claim after process-group death proof: %v", err)
	}
	recovered, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ActiveRunID != replacement.Record.ID || recovered.RunID != replacement.Record.ID {
		t.Fatalf("dead-owner recovery did not publish replacement claim: %#v", recovered)
	}
	if _, err := os.Stat(filepath.Join(run.Dir, dispatchRunFile)); err != nil {
		t.Fatalf("cancel removed evidence: %v", err)
	}
}
