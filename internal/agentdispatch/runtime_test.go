package agentdispatch

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const runtimeSessionID = "11111111-1111-4111-8111-111111111111"

func TestProcStatStartTimeSurvivesCommWithSpacesAndParens(t *testing.T) {
	remainder := "S 1 42 42 0 -1 4194560 100 0 0 0 5 3 0 0 20 0 1 0 777 123456 0"
	for _, comm := range []string{"(codex)", "(tmux: server)", "(a) (b)"} {
		content := "42 " + comm + " " + remainder
		if got := procStatStartTime(content); got != "777" {
			t.Fatalf("comm %q shifted starttime: got %q", comm, got)
		}
	}
	if got := procStatStartTime("no stat shape"); got != "" {
		t.Fatalf("malformed stat produced identity %q", got)
	}
}

func TestSessionLifecycleIsExplicitAndInspectable(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], "fresh")
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve session: %v", err)
	}
	session.ProviderSessionID = runtimeSessionID
	session.State = "durable"
	if err := persistSession(root, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}
	now := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &now
	run.Record.ProviderSessionID = runtimeSessionID
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatalf("complete record: %v", err)
	}
	loaded, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if loaded.ProviderSessionID != runtimeSessionID || loaded.State != "durable" {
		t.Fatalf("session lifecycle state = %#v", loaded)
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatalf("load record: %v", err)
	}
	if record.ProviderSessionID != runtimeSessionID || record.State != dispatchStateCompleted {
		t.Fatalf("completed record = %#v", record)
	}
}

func TestRunnerRequiresProviderTerminalEvidence(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], "fresh")
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	_, err = executeProvider(providerCommand{
		Path:       "/bin/sh",
		Args:       []string{"-c", `printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n{"type":"agent_message","message":"partial"}\n'`},
		Env:        os.Environ(),
		Provider:   AgentCodex,
		SessionID:  runtimeSessionID,
		Structured: true,
	}, []byte("prompt"), run, root, nil, func(string) error { return nil })
	requireDispatchExitCode(t, err, ExitTargetFailure)

	events, err := reduceStructuredTestEvent(AgentClaude, runtimeSessionID, []byte(`{"type":"result","session_id":"22222222-2222-4222-8222-222222222222","is_error":false}`))
	if err != nil || len(events) != 1 || events[0].Kind != eventFailure {
		t.Fatalf("Claude mismatched result = %#v, %v", events, err)
	}
}

func TestPreStartRetryCleansOnlyPrivateCaptures(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	writeDispatchStub(t, binDir, "codex", "")
	attempts := 0
	var stdout bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root:     root,
		Agent:    AgentCodex,
		Prompt:   "retry",
		Env:      []string{"PATH=" + testPath(binDir)},
		Stdout:   &stdout,
		Stderr:   io.Discard,
		LookPath: mockLookPath(binDir),
		NewCommand: func(_ string, args ...string) *exec.Cmd {
			attempts++
			if attempts == 1 {
				return exec.Command(filepath.Join(root, "missing-provider")) // #nosec G204 -- test-only missing executable.
			}
			return exec.Command("/bin/sh", "-c", `cat >/dev/null; printf '{"type":"thread.started","thread_id":"11111111-1111-4111-8111-111111111111"}\n{"type":"agent_message","message":"retried"}\n{"type":"turn.completed"}\n'`) // #nosec G204 -- fixed test shell command.
		},
	})
	if err != nil {
		t.Fatalf("retry run: %v", err)
	}
	if attempts != 2 || stdout.String() != "retried" {
		t.Fatalf("attempts=%d stdout=%q", attempts, stdout.String())
	}
}

func TestCapableClaudePreStartRetryRecreatesLineageCapture(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	writeDispatchStub(t, binDir, "claude", "")
	attempts := 0
	var stdout bytes.Buffer
	err := executeFreshDispatch(dispatchExecRequest{
		Root:          root,
		Agent:         AgentClaude,
		Prompt:        "retry",
		Env:           []string{"PATH=" + testPath(binDir)},
		Stdout:        &stdout,
		Stderr:        io.Discard,
		LookPath:      mockLookPath(binDir),
		VersionLookup: func(string, string) (string, error) { return "2.1.212", nil },
		NewCommand: func(_ string, args ...string) *exec.Cmd {
			attempts++
			if attempts == 1 {
				return exec.Command(filepath.Join(root, "missing-provider")) // #nosec G204 -- test-only missing executable.
			}
			sessionID := ""
			for index, arg := range args {
				if arg == "--session-id" && index+1 < len(args) {
					sessionID = args[index+1]
				}
			}
			return exec.Command("/bin/sh", "-c", `printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tool","name":"Agent"}]}}' '{"type":"system","subtype":"task_started","task_id":"task","tool_use_id":"tool","task_type":"local_agent"}' '{"type":"system","subtype":"task_notification","task_id":"task","status":"completed"}'; printf '{"type":"result","session_id":"%s","is_error":false,"result":"retried"}\n' "$1"`, "sh", sessionID) // #nosec G204 -- fixed test shell command with caller-assigned test session.
		},
	})
	if err != nil {
		t.Fatalf("retry run: %v", err)
	}
	if attempts != 2 || stdout.String() != "retried" {
		t.Fatalf("attempts=%d stdout=%q", attempts, stdout.String())
	}
	sessions, err := listSessions(root)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%#v err=%v", sessions, err)
	}
	record, err := loadRunRecord(root, sessions[0].RunID)
	if err != nil {
		t.Fatalf("load retried record: %v", err)
	}
	lineage, err := os.ReadFile(record.LineagePath)
	if err != nil {
		t.Fatalf("retried lineage capture missing: %v", err)
	}
	for _, want := range []string{`"kind":"task_started"`, `"kind":"task_terminal"`, `"status":"completed"`} {
		if !strings.Contains(string(lineage), want) {
			t.Fatalf("retried lineage capture missing %q: %s", want, lineage)
		}
	}
}

func TestProviderVersionFailureIsUnavailable(t *testing.T) {
	_, err := requireSupportedVersion("ignored", AgentCodex, func(string, string) (string, error) { return "", errors.New("unreadable") })
	requireDispatchExitCode(t, err, ExitUnavailable)
}

func TestProviderProcessErrorsNameTheCorrectLifecyclePhase(t *testing.T) {
	startErr := providerStartError(AgentCodex, errors.New("start failed"))
	if !strings.Contains(startErr.Error(), "start codex") {
		t.Fatalf("start error = %q", startErr)
	}
	waitErr := providerWaitError(AgentCodex, errors.New("wait failed"))
	if !strings.Contains(waitErr.Error(), "wait for codex") {
		t.Fatalf("wait error = %q", waitErr)
	}
}

func TestTerminalCommitKeepsAnswerRunAndClaimCoherent(t *testing.T) {
	newRunning := func(t *testing.T) (string, *dispatchRun, Session) {
		t.Helper()
		root := t.TempDir()
		run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], dispatchModeFresh)
		if err != nil {
			t.Fatalf("new run: %v", err)
		}
		session, err := reserveSession(root, run)
		if err != nil {
			t.Fatalf("reserve session: %v", err)
		}
		run.Record.State = dispatchStateRunning
		run.Record.RecoveryState = recoveryAcceptanceUnknown
		if err := writeRunRecord(run.Dir, &run.Record); err != nil {
			t.Fatalf("start run: %v", err)
		}
		return root, run, session
	}

	t.Run("answer write failure terminalizes without publication", func(t *testing.T) {
		root, run, session := newRunning(t)
		blockedParent := filepath.Join(root, "not-a-directory")
		if err := os.WriteFile(blockedParent, []byte("blocked"), 0o600); err != nil {
			t.Fatalf("write blocking file: %v", err)
		}
		run.Record.AnswerPath = filepath.Join(blockedParent, "answer.txt")
		err := completeDispatchSuccess(dispatchExecution{Root: root, Run: run, Session: session, Stderr: io.Discard}, executionResult{Answer: "validated"}, session)
		requireDispatchExitCode(t, err, ExitConfig)
		record, loadErr := loadRunRecord(root, run.Record.ID)
		if loadErr != nil || record.State != dispatchStateFailed {
			t.Fatalf("failed answer publication record = %#v, %v", record, loadErr)
		}
	})

	t.Run("completed record conflict retracts answer and releases claim", func(t *testing.T) {
		root, run, session := newRunning(t)
		current, err := loadRunRecord(root, run.Record.ID)
		if err != nil {
			t.Fatalf("load current run: %v", err)
		}
		now := time.Now().UTC()
		current.State = dispatchStateCancelled
		current.CompletedAt = &now
		if err := writeRunRecord(run.Dir, &current); err != nil {
			t.Fatalf("cancel current run: %v", err)
		}
		err = completeDispatchSuccess(dispatchExecution{Root: root, Run: run, Session: session, Stderr: io.Discard}, executionResult{Answer: "validated"}, session)
		requireDispatchExitCode(t, err, ExitTargetFailure)
		if _, statErr := os.Stat(run.Record.AnswerPath); !os.IsNotExist(statErr) {
			t.Fatalf("terminal record conflict retained published answer: %v", statErr)
		}
		retained, loadErr := loadSession(root, session.Name)
		if loadErr != nil || retained.ActiveRunID != "" {
			t.Fatalf("terminal record conflict retained claim = %#v, %v", retained, loadErr)
		}
	})

	t.Run("claim cleanup failure preserves durable success", func(t *testing.T) {
		root, run, session := newRunning(t)
		mappingPath, err := sessionPath(root, session.Name)
		if err != nil {
			t.Fatalf("session path: %v", err)
		}
		if err := os.WriteFile(mappingPath, []byte("not-json"), 0o600); err != nil {
			t.Fatalf("corrupt test mapping: %v", err)
		}
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		err = completeDispatchSuccess(dispatchExecution{Root: root, Run: run, Session: session, Stdout: &stdout, Stderr: &stderr}, executionResult{Answer: "validated"}, session)
		if err != nil {
			t.Fatalf("claim cleanup changed completed outcome: %v", err)
		}
		record, loadErr := loadRunRecord(root, run.Record.ID)
		if loadErr != nil || record.State != dispatchStateCompleted || stdout.String() != "validated" || stderr.Len() == 0 {
			t.Fatalf("claim cleanup result record=%#v stdout=%q stderr=%q err=%v", record, stdout.String(), stderr.String(), loadErr)
		}
	})
}
