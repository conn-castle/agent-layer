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
	err = Delete(root, session.Name)
	requireDispatchExitCode(t, err, ExitUnavailable)

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

	var inspection bytes.Buffer
	if err := Inspect(InspectionRequest{Root: root, ID: session.Name, Stdout: &inspection}); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !strings.Contains(inspection.String(), "Provider conversation: "+runtimeSessionID) {
		t.Fatalf("inspection = %q", inspection.String())
	}
	if err := Delete(root, session.Name); err != nil {
		t.Fatalf("delete completed mapping: %v", err)
	}
	if _, err := loadSession(root, session.Name); err == nil {
		t.Fatal("deleted session remained readable")
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

	events, err := reduceStructuredEvent(AgentClaude, runtimeSessionID, []byte(`{"type":"result","session_id":"22222222-2222-4222-8222-222222222222","is_error":false}`))
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
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentCodex,
		PromptArgs: []string{"retry"},
		Env:        []string{"PATH=" + testPath(binDir)},
		Stdout:     &stdout,
		Stderr:     io.Discard,
		LookPath:   mockLookPath(binDir),
		NewCommand: func(string, ...string) *exec.Cmd {
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
