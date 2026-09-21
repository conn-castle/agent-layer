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

func TestGrokDispatchCommandStagesPromptPath(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	target, ok := lookupTarget(AgentGrok)
	if !ok {
		t.Fatal("grok target missing from registry")
	}
	run, err := newDispatchRun(root, AgentGrok, supportedProviderVersions[AgentGrok], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	command, err := buildProviderCommand(target, project, []string{}, []byte("private prompt"), "", "", false, dispatchModeFresh, runtimeSessionID, run, io.Discard)
	if err != nil {
		t.Fatalf("build grok command: %v", err)
	}
	// The provider receives the private prompt through the staged path.
	want := filepath.Join(run.Dir, "prompt.txt")
	if len(command.Args) < 3 || command.Args[1] != "--prompt-file" || command.Args[2] != want {
		t.Fatalf("grok prompt arguments = %q", command.Args)
	}
	staged, err := os.ReadFile(want) // #nosec G304 -- want is a test-owned run path.
	if err != nil {
		t.Fatalf("read staged grok prompt: %v", err)
	}
	if string(staged) != "private prompt" {
		t.Fatalf("staged grok prompt = %q", staged)
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

func TestGrokPublicDispatchPromptCleanup(t *testing.T) {
	for _, mode := range []string{"success", "cleanup-failure", "provider-failure"} {
		t.Run(mode, func(t *testing.T) {
			root := writeDispatchRepo(t, dispatchRepoConfig{})
			replaceDispatchConfigText(t, root, "[agents.grok]\nenabled = false", "[agents.grok]\nenabled = true")
			binDir := t.TempDir()
			writeDispatchStub(t, binDir, "grok", `
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
printf '{"type":"text","data":"completed answer"}\n'
printf '{"type":"end","stopReason":"end_turn","sessionId":"%s"}\n' "$session"
`)
			t.Setenv("PATH", testPath(binDir))
			t.Setenv("AL_TEST_LOG", filepath.Join(t.TempDir(), "provider.log"))
			t.Setenv("AL_TEST_CLEANUP_MODE", mode)
			var gate *os.File
			var started bytes.Buffer
			err := Start(StartOptions{Root: root, WorkDir: root, Agent: AgentGrok, Prompt: "private prompt", Stdout: &started, Env: os.Environ(), LookPath: mockLookPath(binDir), VersionLookup: func(string, string) (string, error) { return supportedProviderVersions[AgentGrok], nil }, launchWorker: func(string, string, string) (launchedWorker, error) {
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

func TestGrokPreLaunchFailurePersistsPromptCleanupError(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	run, err := newDispatchRun(root, AgentGrok, supportedProviderVersions[AgentGrok], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run, testDispatchSessionRetention)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := lookupTarget(AgentGrok)
	promptPath := filepath.Join(run.Dir, "prompt.txt")
	err = executeDispatch(dispatchExecution{Root: root, Project: project, Target: target, Version: supportedProviderVersions[AgentGrok], Mode: dispatchModeFresh, Run: run, Session: session, Prompt: []byte("private prompt"), Stdout: io.Discard, Stderr: io.Discard, NewCommand: func(string, ...string) *exec.Cmd {
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

func TestGrokCancelledPromptCleanupFailureReleasesConversation(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project := loadApprovalsTestProject(t, root)
	run, err := newDispatchRun(root, AgentGrok, supportedProviderVersions[AgentGrok], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	session, err := reserveSession(root, run, testDispatchSessionRetention)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := lookupTarget(AgentGrok)
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
	run, err := newDispatchRun(root, AgentGrok, supportedProviderVersions[AgentGrok], dispatchModeFresh)
	if err != nil {
		t.Fatal(err)
	}
	target, _ := lookupTarget(AgentGrok)
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
