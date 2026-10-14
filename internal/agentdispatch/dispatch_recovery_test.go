package agentdispatch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const capturedMuseSession = "43a5e55b-a302-453a-9674-e0194d26f0d7"
const capturedRecoveryRoot = "01a132d6-ffde-7d02-8fe2-fe3ecc324f2f"

func museCapturedFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "muse", name)) // #nosec G304 -- constant test fixture names.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func museBackgroundFinal() string {
	return `{"schema_version":1,"stream":{"kind":"session","id":"` + capturedMuseSession + `"},"payload_type":"run.terminal.completed","payload":{"command_id":"01a132d6-69c2-7b51-bf05-e8b24f9711f3","run_stream":{"kind":"run","id":"01a132d6-69c2-7b51-bf05-e8b24f9711f3"},"terminal":"completed","text":"workflow done"}}` + "\n"
}

func TestMuseObservedTextBackgroundContinuation(t *testing.T) {
	raw := museCapturedFixture(t, "v1.4.6-text-background.jsonl")
	for _, test := range []struct{ name, stream, wantFailure string }{
		{"observed handoff", raw + museBackgroundFinal(), ""},
		{"announced but no final answer", raw, "without required terminal"},
		{"announcement without linkage", strings.Join(strings.Split(strings.TrimSpace(raw), "\n")[:4], "\n"), "without a final answer"},
		{"foreign client", strings.Replace(raw, "muse-runtime-background-terminal", "foreign", 1) + museBackgroundFinal(), "without a background continuation"},
		{"foreign session", strings.ReplaceAll(raw, capturedMuseSession, "foreign"), "different provider session"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var answer string
			var complete bool
			continuation := false
			err := readStructuredEventsWithLineage(strings.NewReader(test.stream), io.Discard, AgentMuse, capturedMuseSession, false, func(event providerEvent) error {
				switch event.Kind {
				case eventAnswer:
					answer = event.Answer
				case eventComplete:
					complete = true
				case eventContinuation:
					complete = false
					answer = ""
					continuation = true
				case eventFailure:
					return fmt.Errorf("%s", event.Reason)
				}
				return nil
			}, nil)
			if test.wantFailure == "without required terminal" {
				if err == nil && complete {
					t.Fatal("intermediate answer stood as completion")
				}
				return
			}
			if test.wantFailure != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantFailure) {
					t.Fatalf("error=%v", err)
				}
				return
			}
			if err != nil || !complete || !continuation || answer != "workflow done" {
				t.Fatalf("error=%v complete=%t continuation=%t answer=%q", err, complete, continuation, answer)
			}
		})
	}
}

func TestMuseRecoveryUsesAuthoritativeBaseline(t *testing.T) {
	raw := museCapturedFixture(t, "v1.4.6-recovery-terminal.jsonl")
	for _, test := range []struct {
		name, root, session, stream string
		wantFailure                 bool
	}{
		{"interrupted root", capturedRecoveryRoot, capturedMuseSession, raw, false},
		{"missing baseline", "", capturedMuseSession, raw, true},
		{"foreign root", "foreign", capturedMuseSession, raw, true},
		{"foreign baseline session", capturedRecoveryRoot, "foreign", raw, true},
		{"foreign stream session", capturedRecoveryRoot, capturedMuseSession, strings.ReplaceAll(raw, capturedMuseSession, "foreign"), true},
		{"duplicate terminal", capturedRecoveryRoot, capturedMuseSession, raw + raw, true},
		{"terminal command mismatch", capturedRecoveryRoot, capturedMuseSession, strings.ReplaceAll(raw, `"command_id":"`+capturedRecoveryRoot+`"`, `"command_id":"foreign"`), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			baseline := &museRecoveryBaseline{SessionID: test.session, RootID: test.root, ViewCursor: "opaque"}
			var complete bool
			err := readStructuredEventsWithLineage(strings.NewReader(test.stream), io.Discard, AgentMuse, capturedMuseSession, false, func(e providerEvent) error {
				if e.Kind == eventFailure {
					return fmt.Errorf("%s", e.Reason)
				}
				if e.Kind == eventComplete {
					complete = true
					if e.LineageSource != "session/read" {
						t.Errorf("source=%q", e.LineageSource)
					}
				}
				return nil
			}, nil, baseline)
			if test.wantFailure {
				if err == nil {
					t.Fatal("unowned terminal accepted")
				}
				return
			}
			if err != nil || !complete {
				t.Fatalf("error=%v complete=%t", err, complete)
			}
		})
	}
}

func TestMuseRecoverySnapshotContract(t *testing.T) {
	for _, test := range []struct {
		name, session, active, lastTerminal, wantRoot string
		wantFailure                                   bool
	}{
		{"active", capturedMuseSession, capturedRecoveryRoot, "", capturedRecoveryRoot, false},
		{"interrupted", capturedMuseSession, "", "failed", capturedRecoveryRoot, false},
		{"cancelled", capturedMuseSession, "", "cancelled", capturedRecoveryRoot, false},
		{"completed is not resumable root", capturedMuseSession, "", "completed", "", false},
		{"foreign session", "foreign", capturedRecoveryRoot, "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{"schema":{"version":1}}}
{"jsonrpc":"2.0","id":2,"result":{"session":{"sessionId":%q,"activeTurnId":%q},"viewCursor":"opaque-view","lastTurn":{"turnId":%q,"terminal":%q}}}
`, test.session, test.active, capturedRecoveryRoot, test.lastTerminal)
			var requests bytes.Buffer
			baseline, err := queryMuseRecoveryBaseline(json.NewEncoder(&requests), json.NewDecoder(strings.NewReader(response)), capturedMuseSession)
			if test.wantFailure {
				if err == nil {
					t.Fatal("foreign baseline accepted")
				}
				return
			}
			if err != nil || baseline.RootID != test.wantRoot || baseline.ViewCursor != "opaque-view" || baseline.ObservedAt.IsZero() {
				t.Fatalf("baseline=%#v err=%v", baseline, err)
			}
			if strings.Contains(requests.String(), "session/resume") || !strings.Contains(requests.String(), "session/read") || !strings.Contains(requests.String(), `"excludeItems":true`) {
				t.Fatalf("mutating or unbounded query: %s", requests.String())
			}
		})
	}
}

func TestMuseTextBackgroundRetractsShutdownDeadline(t *testing.T) {
	root := t.TempDir()
	raw := museCapturedFixture(t, "v1.4.6-text-background.jsonl") + museBackgroundFinal()
	fixture := filepath.Join(root, "fixture.jsonl")
	if err := os.WriteFile(fixture, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	run, err := newDispatchRun(root, AgentMuse, "1.4.6", dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	// Let the semantic shutdown deadline start, retract it at the announcement,
	// then take longer than the original deadline to link/finish the new root.
	result, err := executeProvider(providerCommand{Path: "/bin/sh", Args: []string{"-c", `sed -n '1,3p' "$1"; sleep 0.2; sed -n '4p' "$1"; sleep 6; sed -n '5,6p' "$1"`, "sh", fixture}, Env: os.Environ(), Provider: AgentMuse, SessionID: capturedMuseSession}, nil, run, root, nil, func(string) error { return nil })
	if err != nil || !result.Complete || result.Answer != "workflow done" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestMusePostTimeoutRecoveryWithoutReplayedLink(t *testing.T) {
	root := t.TempDir()
	raw := museCapturedFixture(t, "v1.4.6-recovery-terminal.jsonl")
	fixture := filepath.Join(root, "terminal.jsonl")
	if err := os.WriteFile(fixture, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := filepath.Join(root, "fake-muse")
	script := `#!/bin/sh
if [ "$1" = serve ]; then
 IFS= read -r request
 printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"schema":{"version":1}}}'
 IFS= read -r request
 IFS= read -r request
 printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"session":{"sessionId":"` + capturedMuseSession + `","activeTurnId":null},"viewCursor":"recovery-cursor","lastTurn":{"turnId":"` + capturedRecoveryRoot + `","terminal":"failed"}}}'
 while IFS= read -r request; do :; done
 exit 0
fi
cat "$2"
`
	if err := os.WriteFile(provider, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	} // #nosec G306 -- executable test fixture.
	first, err := newDispatchRun(root, AgentMuse, "1.4.6", dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	link := `{"schema_version":1,"stream":{"kind":"session","id":"` + capturedMuseSession + `"},"payload_type":"session.run.linked","payload":{"command_id":"` + capturedRecoveryRoot + `","run_stream":{"kind":"run","id":"` + capturedRecoveryRoot + `"}}}`
	_, err = executeProvider(providerCommand{Path: "/bin/sh", Args: []string{"-c", `printf '%s\n' "$1"; cat "$2"; sleep 30`, "sh", link, fixture}, Env: os.Environ(), Provider: AgentMuse, SessionID: capturedMuseSession}, nil, first, root, nil, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "within 5s") || !providerProcessGroupDead(first.Record.ProcessGroupID) {
		t.Fatalf("timeout=%v", err)
	}
	events, err := os.ReadFile(first.Record.EventsPath)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot *providerLifecycleSnapshot
	for _, line := range bytes.Split(events, []byte{'\n'}) {
		var event providerEvent
		if json.Unmarshal(line, &event) == nil && event.Lifecycle != nil {
			snapshot = event.Lifecycle
		}
	}
	if snapshot == nil || snapshot.Trigger != "semantic_terminal" || !snapshot.SemanticTerminal || snapshot.LeaderReaped || snapshot.LeaderObservation != processStatusAlive || snapshot.StdoutWritersClosed || snapshot.StderrWritersClosed || snapshot.Deadline == nil || snapshot.TriggeredAt == nil || snapshot.LastReceivedAt == nil || snapshot.Build.SHA256 == "" {
		t.Fatalf("timeout snapshot=%#v", snapshot)
	}
	if delta := snapshot.Deadline.Sub(*snapshot.TriggeredAt); delta != providerShutdownGrace {
		t.Fatalf("deadline=%s", delta)
	}
	// Mirror the worker's failed-run finalization before continuing.
	completed := time.Now().UTC()
	first.Record.State = dispatchStateFailed
	first.Record.CompletedAt = &completed
	first.Record.TerminalExitCode = ExitTargetFailure
	applyLaunchFence(&first.Record)
	applyTerminationEvidence(&first.Record, nil, true, completed)
	if !first.Record.TerminationConfirmed {
		t.Fatal("timeout termination not confirmed")
	}
	if err := writeRunRecord(first.Dir, &first.Record); err != nil {
		t.Fatal(err)
	}
	second, err := newDispatchRun(root, AgentMuse, "1.4.6", dispatchModeResume)
	if err != nil {
		t.Fatal(err)
	}
	second.Record.PreviousRunID = first.Record.ID
	if err := writeRunRecord(second.Dir, &second.Record); err != nil {
		t.Fatal(err)
	}
	result, err := executeProvider(providerCommand{Path: provider, Args: []string{execSubcommand, fixture}, Env: os.Environ(), Provider: AgentMuse, SessionID: capturedMuseSession, RunMode: dispatchModeResume}, nil, second, root, nil, func(string) error { return nil })
	if err != nil || !result.Complete || result.Answer != "sanitized answer" {
		t.Fatalf("recovery=%#v err=%v", result, err)
	}
	persisted, err := loadRunRecord(root, second.Record.ID)
	if err != nil || persisted.MuseRecovery == nil || persisted.MuseRecovery.RootID != capturedRecoveryRoot || persisted.MuseRecovery.ViewCursor != "recovery-cursor" || persisted.AgentLayerBuild.SHA256 == "" {
		t.Fatalf("persisted recovery=%#v err=%v", persisted.MuseRecovery, err)
	}
}

func TestOutputPagesLateFailedEvidence(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	now := time.Now().UTC()
	run.Record.State = dispatchStateFailed
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = "contained timeout"
	run.Record.TerminalExitCode = ExitTargetFailure
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("€", maxOutputReadBytes) + "\n" + `{"Kind":"answer","Answer":"late partial answer"}` + "\n" + `{"Kind":"failure","Reason":"late failure"}` + "\n"
	if err := os.WriteFile(run.Record.EventsPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	var assembled strings.Builder
	var offset int64
	for offset < int64(len(content)) {
		var out bytes.Buffer
		if err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactEvents, Offset: offset, Stdout: &out}); err != nil {
			t.Fatal(err)
		}
		var page OutputResult
		if err := json.Unmarshal(out.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Offset != offset || page.NextOffset <= offset || len(page.Content) > maxOutputReadBytes || page.State != dispatchStateFailed {
			t.Fatalf("page=%#v", page)
		}
		assembled.WriteString(page.Content)
		offset = page.NextOffset
	}
	if assembled.String() != content {
		t.Fatal("pagination lost or duplicated content")
	}
	var out bytes.Buffer
	if err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactEvents, Tail: true, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	var page OutputResult
	if err := json.Unmarshal(out.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.Content, "late partial answer") || !strings.Contains(page.Content, "late failure") || page.NextOffset != int64(len(content)) || !page.Truncated {
		t.Fatalf("tail=%#v", page)
	}
	for _, request := range []OutputRequest{{Offset: -1}, {Offset: 1, Tail: true}, {Offset: int64(len(content)) + 1}, {Offset: 1}} {
		request.Root, request.ID, request.Artifact, request.Stdout = root, run.Record.ID, artifactEvents, io.Discard
		if err := Output(request); err == nil {
			t.Fatalf("invalid cursor accepted: %#v", request)
		}
	}
	if err := Output(OutputRequest{Root: root, ID: run.Record.ID, Artifact: artifactFinalAnswer, Tail: true}); err == nil {
		t.Fatal("failed text was exposed as successful final answer")
	}
}

func TestCrossProviderShutdownSnapshots(t *testing.T) {
	for _, test := range []struct {
		agent, stream string
		terminal      bool
	}{
		{AgentCodex, `{"type":"thread.started","thread_id":"` + runtimeSessionID + `"}
{"type":"agent_message","message":"private captured answer"}
{"type":"turn.completed"}`, true},
		{AgentGrok, `{"type":"text","data":"private captured answer"}
{"type":"end","session_id":"` + runtimeSessionID + `","stopReason":"end_turn"}`, true},
		{AgentClaude, `{"type":"result","session_id":"` + runtimeSessionID + `","is_error":false,"result":"private captured answer"}`, true},
		{AgentCodex, `{"type":"agent_message","message":"private captured answer"}`, false},
		{AgentClaude, `{"type":"content_block_delta","delta":{"type":"text_delta","text":"private captured answer"}}`, false},
	} {
		name := fmt.Sprintf("%s-terminal-%t", test.agent, test.terminal)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			run, err := newDispatchRun(root, test.agent, supportedProviderVersions[test.agent], dispatchModeFresh)
			if err != nil {
				t.Fatal(err)
			}
			closeOutput := "keep"
			if !test.terminal {
				closeOutput = "close"
			}
			_, err = executeProvider(providerCommand{Path: "/bin/sh", Args: []string{"-c", `printf '%s\n' "$1"; if [ "$2" = close ]; then exec 1>&-; fi; sleep 30`, "sh", test.stream, closeOutput}, Env: os.Environ(), Provider: test.agent, SessionID: runtimeSessionID}, []byte("private caller prompt"), run, root, nil, func(string) error { return nil })
			if err == nil || !strings.Contains(err.Error(), "within 5s") || !providerProcessGroupDead(run.Record.ProcessGroupID) {
				t.Fatalf("shutdown=%v", err)
			}
			data, err := os.ReadFile(run.Record.EventsPath)
			if err != nil {
				t.Fatal(err)
			}
			var snapshot *providerLifecycleSnapshot
			for _, line := range bytes.Split(data, []byte{'\n'}) {
				var event providerEvent
				if json.Unmarshal(line, &event) == nil && event.Lifecycle != nil {
					snapshot = event.Lifecycle
				}
			}
			wantTrigger := "stdout_reader_done"
			if test.terminal {
				wantTrigger = shutdownSemanticTerminal
			}
			if snapshot == nil || snapshot.Trigger != wantTrigger || snapshot.SemanticTerminal != test.terminal || !snapshot.LeaderReapedKnown || snapshot.LeaderReaped || snapshot.StderrWritersClosed || snapshot.StdoutDrained == test.terminal || snapshot.Build.SHA256 == "" {
				t.Fatalf("snapshot=%#v", snapshot)
			}
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encoded, []byte("private")) {
				t.Fatalf("snapshot retained private content: %s", encoded)
			}
		})
	}
}

func TestMuseRecoveryRetainsOwnedRootAcrossMissingViewFold(t *testing.T) {
	raw := museCapturedFixture(t, "v1.4.6-text-background.jsonl")
	const linkedBackground = "01a132d6-69c2-7b51-bf05-e8b24f9711f3"
	for _, test := range []struct {
		name                                   string
		legacy, foreign, unconfirmed, conflict bool
	}{
		{name: "recorded root"},
		{name: "legacy captured linkage", legacy: true},
		{name: "foreign prior session", foreign: true},
		{name: "unconfirmed prior termination", unconfirmed: true},
		{name: "conflicting live turn", conflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			previous, err := newDispatchRun(root, AgentMuse, "1.4.6", dispatchModeFresh)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			previous.Record.State = dispatchStateFailed
			previous.Record.CompletedAt = &now
			previous.Record.ProviderLaunchIntent = true
			previous.Record.ProviderSessionID = capturedMuseSession
			previous.Record.TerminationConfirmed = !test.unconfirmed
			if !test.unconfirmed {
				previous.Record.TerminationConfirmedAt = &now
			}
			previous.Record.LaunchFenced = true
			if !test.legacy {
				previous.Record.MuseRootID = linkedBackground
				previous.Record.MuseRootCursor = "owned-cursor"
			}
			if test.foreign {
				previous.Record.ProviderSessionID = "foreign"
			}
			if err := writeRunRecord(previous.Dir, &previous.Record); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(previous.Record.StdoutPath, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			baseline := &museRecoveryBaseline{SessionID: capturedMuseSession, ObservedAt: now}
			if test.conflict {
				baseline.ActiveAtRead = true
				baseline.RootID = "foreign-active"
			}
			baseline, err = resolveMuseRecoveryBaseline(root, previous.Record.ID, baseline)
			if test.foreign || test.unconfirmed || test.conflict {
				if err == nil {
					t.Fatal("unsafe root baseline accepted")
				}
				return
			}
			if err != nil || baseline.RootID != linkedBackground || baseline.Source != "previous_invocation" || baseline.PreviousRunID != previous.Record.ID {
				t.Fatalf("baseline=%#v err=%v", baseline, err)
			}
			completed := false
			err = readStructuredEventsWithLineage(strings.NewReader(museBackgroundFinal()), io.Discard, AgentMuse, capturedMuseSession, false, func(event providerEvent) error {
				if event.Kind == eventFailure {
					return fmt.Errorf("%s", event.Reason)
				}
				if event.Kind == eventComplete {
					completed = true
				}
				return nil
			}, nil, baseline)
			if err != nil || !completed {
				t.Fatalf("retained-root recovery failed: %v", err)
			}
		})
	}
}

func TestMuseNewResumeRootRequiresTaskAndFreshAuthoritativeView(t *testing.T) {
	observed := museCapturedFixture(t, "v1.4.6-recovery-terminal.jsonl")
	for _, test := range []struct {
		name, afterRoot, afterCursor, afterState, stream string
		wantFailure                                      bool
	}{
		{"new resumed turn", capturedRecoveryRoot, "after-cursor", "completed", observed, false},
		{"stale cursor", capturedRecoveryRoot, "before-cursor", "completed", observed, true},
		{"foreign snapshot root", "foreign", "after-cursor", "completed", observed, true},
		{"contradictory terminal", capturedRecoveryRoot, "after-cursor", "failed", observed, true},
		{"terminal only cannot self-authorize", capturedRecoveryRoot, "after-cursor", "completed", strings.Split(strings.TrimSpace(observed), "\n")[3] + "\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			fixture := filepath.Join(root, "stream.jsonl")
			if err := os.WriteFile(fixture, []byte(test.stream), 0o600); err != nil {
				t.Fatal(err)
			}
			provider := filepath.Join(root, "fake-muse")
			after := fmt.Sprintf(`{"session":{"sessionId":%q,"activeTurnId":null},"viewCursor":%q,"lastTurn":{"turnId":%q,"terminal":%q}}`, capturedMuseSession, test.afterCursor, test.afterRoot, test.afterState)
			script := `#!/bin/sh
if [ "$1" = serve ]; then
 IFS= read -r request
 printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"schema":{"version":1}}}'
 IFS= read -r request
 IFS= read -r request
 if [ -f "$ROOT_MARKER" ]; then
  printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":` + after + `}'
 else
  printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"session":{"sessionId":"` + capturedMuseSession + `","activeTurnId":null},"viewCursor":"before-cursor","lastTurn":{"turnId":"old-owned-root","terminal":"failed"}}}'
 fi
 while IFS= read -r request; do :; done
 exit 0
fi
: > "$ROOT_MARKER"
cat "$2"
`
			if err := os.WriteFile(provider, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			} // #nosec G306 -- executable test fixture.
			run, err := newDispatchRun(root, AgentMuse, "1.4.6", dispatchModeResume)
			if err != nil {
				t.Fatal(err)
			}
			result, err := executeProvider(providerCommand{Path: provider, Args: []string{execSubcommand, fixture}, Env: append(os.Environ(), "ROOT_MARKER="+filepath.Join(root, "started")), Provider: AgentMuse, SessionID: capturedMuseSession, RunMode: dispatchModeResume}, nil, run, root, nil, func(string) error { return nil })
			if test.wantFailure {
				if err == nil {
					t.Fatal("unowned resumed root accepted")
				}
				return
			}
			if err != nil || !result.Complete || result.Answer != "sanitized answer" {
				t.Fatalf("result=%#v error=%v", result, err)
			}
			record, err := loadRunRecord(root, run.Record.ID)
			if err != nil || record.MuseRecovery == nil || record.MuseRecovery.Source != "session/read_after_launch" || record.MuseRecovery.ViewCursor != "after-cursor" || record.MuseRootID != capturedRecoveryRoot {
				t.Fatalf("root proof=%#v err=%v", record.MuseRecovery, err)
			}
		})
	}
}
