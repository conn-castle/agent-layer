package agentdispatch

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGrokDispatchCommandUsesACPAndMuseStagesPromptPath(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	grokTarget, ok := lookupTarget(AgentGrok)
	if !ok {
		t.Fatal("grok target missing from registry")
	}
	grokRun, err := newDispatchRun(root, AgentGrok, supportedProviderVersions[AgentGrok], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	grokCmd, err := buildProviderCommand(grokTarget, project, []string{}, []byte("private prompt"), "", "", false, dispatchModeFresh, "", grokRun, io.Discard)
	if err != nil {
		t.Fatalf("build grok command: %v", err)
	}
	if !grokCmd.UseACP || grokCmd.ACP == nil {
		t.Fatalf("grok command did not configure ACP: %#v", grokCmd)
	}
	if _, statErr := os.Stat(filepath.Join(grokRun.Dir, "prompt.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("grok ACP should not stage prompt.txt: %v", statErr)
	}

	museTarget, ok := lookupTarget(AgentMuse)
	if !ok {
		t.Fatal("muse target missing from registry")
	}
	museRun, err := newDispatchRun(root, AgentMuse, supportedProviderVersions[AgentMuse], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	museCmd, err := buildProviderCommand(museTarget, project, []string{}, []byte("private prompt"), "", "", false, dispatchModeFresh, runtimeSessionID, museRun, io.Discard)
	if err != nil {
		t.Fatalf("build muse command: %v", err)
	}
	want := filepath.Join(museRun.Dir, "prompt.txt")
	if !argPairPresent(museCmd.Args, "--prompt-file", want) {
		t.Fatalf("muse prompt arguments = %q", museCmd.Args)
	}
	staged, err := os.ReadFile(want) // #nosec G304 -- want is a test-owned run path.
	if err != nil {
		t.Fatalf("read staged muse prompt: %v", err)
	}
	if string(staged) != "private prompt" {
		t.Fatalf("staged muse prompt = %q", staged)
	}
	info, err := os.Stat(want)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("staged muse prompt permissions: %v, %v", info, err)
	}
}

func TestGrokDispatchEarlyFailureRemovesPrompt(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	// EnsureHome runs after buildProviderCommand has written prompt.txt.
	if err := os.WriteFile(filepath.Join(root, ".grok-config"), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	run, err := newDispatchRun(root, AgentGrok, supportedProviderVersions[AgentGrok], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run, testDispatchSessionRetention)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := lookupTarget(AgentGrok)
	err = executeDispatch(dispatchExecution{Root: root, Project: project, Target: target, Version: supportedProviderVersions[AgentGrok], Mode: dispatchModeFresh, Run: run, Session: session, Prompt: []byte("private prompt"), Stdout: io.Discard, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "prepare Grok home") {
		t.Fatalf("expected post-staging home failure, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(run.Dir, "prompt.txt")); !os.IsNotExist(err) {
		t.Fatalf("prompt survived construction failure: %v", err)
	}
}

func TestMusePublicDispatchPromptCleanup(t *testing.T) {
	for _, mode := range []string{"success", "cleanup-failure", "provider-failure"} {
		t.Run(mode, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			replaceDispatchConfigText(t, root, "[approvals]\nmode = \"all\"", "[approvals]\nmode = \"yolo\"")
			replaceDispatchConfigText(t, root, "[agents.muse]\nenabled = false", "[agents.muse]\nenabled = true")
			binDir := t.TempDir()
			writeDispatchStub(t, binDir, "muse", `
while [ "$#" -gt 0 ]; do
 case "$1" in
 --prompt-file) prompt="$2"; shift ;;
 --session-id) session="$2"; shift ;;
 esac
 shift
done
[ "$(cat "$prompt")" = "private prompt" ] || exit 9
if [ "$AL_TEST_CLEANUP_MODE" != success ]; then
 rm "$prompt" && mkdir "$prompt" && printf private > "$prompt/retained" || exit 10
fi
if [ "$AL_TEST_CLEANUP_MODE" = provider-failure ]; then exit 11; fi
printf '{"schema_version":1,"stream":{"kind":"session","id":"%s"},"payload_type":"session.run.linked","payload":{"kind":"session_run_linked","command_id":"root-1","run_stream":{"kind":"run","id":"root-1"}}}\n' "$session"
printf '{"schema_version":1,"stream":{"kind":"session","id":"%s"},"payload_type":"run.terminal.completed","payload":{"kind":"run_terminal","command_id":"root-1","run_stream":{"kind":"run","id":"root-1"},"terminal":"completed","text":"completed answer"}}\n' "$session"
`)
			t.Setenv("PATH", testPath(binDir))
			t.Setenv("AL_TEST_LOG", filepath.Join(t.TempDir(), "provider.log"))
			t.Setenv("AL_TEST_CLEANUP_MODE", mode)
			var gate *os.File
			var started bytes.Buffer
			err := Start(StartOptions{Root: root, WorkDir: root, Agent: AgentMuse, Prompt: "private prompt", Stdout: &started, Env: os.Environ(), LookPath: mockLookPath(binDir), VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentMuse], nil }, launchWorker: func(string, string, string) (launchedWorker, error) {
				read, write, err := os.Pipe()
				gate = read
				return launchedWorker{gate: write, pid: os.Getpid(), startIdentity: processStartIdentity(os.Getpid())}, err
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = gate.Close() }()
			var result Result
			if err := json.Unmarshal(started.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			err = RunWorker(root, result.InvocationID, gate)
			if mode == "success" && err != nil {
				t.Fatal(err)
			}
			if mode != "success" && (err == nil || !strings.Contains(err.Error(), "private prompt may remain")) {
				t.Fatalf("cleanup failure not actionable: %v", err)
			}
			var inspected bytes.Buffer
			if err := Inspect(InspectRequest{Root: root, InvocationID: result.InvocationID, Stdout: &inspected}); err != nil {
				t.Fatal(err)
			}
			var inspection InspectResult
			if err := json.Unmarshal(inspected.Bytes(), &inspection); err != nil {
				t.Fatal(err)
			}
			record, err := loadRunRecord(root, result.InvocationID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "success" {
				if inspection.State != dispatchStateCompleted {
					t.Fatalf("inspection: %+v", inspection)
				}
				if _, err := os.Stat(filepath.Join(filepathForRun(root, result.InvocationID), "prompt.txt")); !os.IsNotExist(err) {
					t.Fatalf("prompt survived successful public dispatch: %v", err)
				}
			} else if inspection.State != dispatchStateFailed || !strings.Contains(inspection.Error, "private prompt may remain") {
				t.Fatalf("cleanup failure missing from inspection: %+v", inspection)
			}
			if mode != "provider-failure" {
				answer, err := os.ReadFile(record.AnswerPath) // #nosec G304 -- test-owned dispatch artifact.
				if err != nil || string(answer) != "completed answer" {
					t.Fatalf("completed answer lost: %q, %v", answer, err)
				}
			}
			if mode == "cleanup-failure" && !strings.Contains(inspection.Error, record.AnswerPath) {
				t.Fatalf("retained answer not discoverable: %+v", inspection)
			}
		})
	}
}

func TestMusePreLaunchFailurePersistsPromptCleanupError(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	project.Config.Approvals.Mode = "yolo"
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
		// Command construction has staged the real prompt, but the provider cannot start.
		data, err := os.ReadFile(promptPath) // #nosec G304 -- test-owned run path.
		if err != nil || string(data) != "private prompt" {
			t.Fatalf("prompt was not staged: %q, %v", data, err)
		}
		if err := os.Remove(promptPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(promptPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(promptPath, "retained"), data, 0o600); err != nil { // #nosec G703 -- fixed child of the test-owned dispatch prompt path.
			t.Fatal(err)
		}
		return exec.Command(filepath.Join(root, "missing-provider")) // #nosec G204 -- intentionally nonexistent test-owned executable.
	}})
	if err == nil || !strings.Contains(err.Error(), "private prompt may remain") {
		t.Fatalf("cleanup error missing: %v", err)
	}
	var out bytes.Buffer
	if err := Inspect(InspectRequest{Root: root, InvocationID: run.Record.ID, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	var inspected InspectResult
	if err := json.Unmarshal(out.Bytes(), &inspected); err != nil {
		t.Fatal(err)
	}
	if inspected.State != dispatchStateFailed || !strings.Contains(inspected.Error, "private prompt may remain") || !strings.Contains(inspected.Error, "missing-provider") {
		t.Fatalf("pre-launch and cleanup failures not persisted: %+v", inspected)
	}
}

func TestMuseCancelledPromptCleanupFailureReleasesConversation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	run, err := newDispatchRun(root, AgentMuse, supportedProviderVersions[AgentMuse], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run, testDispatchSessionRetention)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := lookupTarget(AgentMuse)
	if _, err := buildProviderCommand(target, project, nil, []byte("private prompt"), "", "", false, dispatchModeFresh, run.Record.ID, run, io.Discard); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(run.Dir, "prompt.txt")
	if err := os.Remove(prompt); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(prompt, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prompt, "retained"), []byte("private prompt"), 0o600); err != nil { // #nosec G703 -- fixed child of the test-owned dispatch prompt path.
		t.Fatal(err)
	}
	// Model cancellation published concurrently, before the owning execution has
	// finalized termination and released the conversation.
	now := time.Now().UTC()
	run.Record.State = dispatchStateCancelled
	run.Record.CompletedAt = &now
	run.Record.TerminalReason = terminalReasonCancelledByCaller
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	err = executeDispatch(dispatchExecution{Root: root, Project: project, Target: target, Run: run, Session: session})
	if err == nil || !strings.Contains(err.Error(), "private prompt may remain") || strings.Contains(err.Error(), "cannot erase cancellation") {
		t.Fatalf("cleanup failure = %v", err)
	}
	// Check before Inspect, which can independently release confirmed claims.
	released, err := loadSession(root, session.Name)
	if err != nil || released.ActiveRunID != "" {
		t.Fatalf("conversation remains claimed: %+v, %v", released, err)
	}
	var out bytes.Buffer
	if err := Inspect(InspectRequest{Root: root, InvocationID: run.Record.ID, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	var result InspectResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != dispatchStateCancelled || !result.TerminationConfirmed || !strings.Contains(result.Error, "private prompt may remain") {
		t.Fatalf("cancellation cleanup evidence = %+v", result)
	}
}

func TestCancelledDispatchPreservesCleanupErrorWhenEvidenceFails(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, err := newDispatchRun(root, AgentMuse, supportedProviderVersions[AgentMuse], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := lookupTarget(AgentMuse)
	prompt := filepath.Join(run.Dir, "prompt.txt")
	if err := os.Mkdir(prompt, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prompt, "retained"), []byte("private prompt"), 0o600); err != nil { // #nosec G703 -- test-owned prompt path.
		t.Fatal(err)
	}
	// A cancelled execution reaches cleanup before publishing termination.
	// Corrupt evidence makes both cleanup-error recording and termination fail.
	run.Record.State = dispatchStateCancelled
	if err := os.WriteFile(filepath.Join(run.Dir, dispatchRunFile), []byte("invalid JSON"), 0o600); err != nil { // #nosec G703 -- test-owned run path.
		t.Fatal(err)
	}
	err = finishDispatchCancellation(dispatchExecution{Root: root, Target: target, Run: run})
	if err == nil || !strings.Contains(err.Error(), "private prompt may remain") || !strings.Contains(err.Error(), "read dispatch run record before evidence update") {
		t.Fatalf("expected both prompt cleanup and evidence failures, got %v", err)
	}
}
