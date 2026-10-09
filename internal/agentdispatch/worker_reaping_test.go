package agentdispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Isolate Wait4(-1) from the suite's other subprocess tests. Both leak cases
// use Start's real detached handoff and a provider in a separate process group.
func TestReapDetachedWorkers(t *testing.T) {
	const scenarioEnv = "AL_TEST_REAP_SCENARIO"
	if scenario := os.Getenv(scenarioEnv); scenario != "" {
		if scenario == "exited" {
			cmd := exec.Command("/bin/sh", "-c", "exit 0")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Process.Release(); err != nil {
				t.Fatal(err)
			}
			if err := reapDetachedWorkers(2 * time.Second); err != nil {
				t.Fatal(err)
			}
			return
		}
		testLeakedDetachedWorker(t, scenario == "stopped")
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// A sibling of the isolated test must not be caught by descendant cleanup.
	unrelated := exec.Command("/bin/sleep", "60")
	unrelated.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := unrelated.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() })
	for _, scenario := range []string{"exited", "graceful", "stopped"} {
		t.Run(scenario, func(t *testing.T) {
			cmd := exec.Command(executable, "-test.run=^TestReapDetachedWorkers$", "-test.timeout=30s")
			cmd.Env = append(os.Environ(), scenarioEnv+"="+scenario)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("isolated reaping test: %v\n%s", err, out)
			}
			if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
				t.Fatalf("cleanup signalled an unrelated process: %v", err)
			}
		})
	}
}

func testLeakedDetachedWorker(t *testing.T, stopped bool) {
	t.Helper()
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	childPath := filepath.Join(t.TempDir(), "child.pid")
	writeDispatchStub(t, binDir, "codex", `trap '' TERM
sleep 60 & child=$!
printf '%s\n' "$child" > "$AL_TEST_CHILD_PID"
wait "$child"`)
	t.Setenv("PATH", testPath(binDir))
	t.Setenv("AL_TEST_LOG", filepath.Join(t.TempDir(), "provider.log"))
	t.Setenv("AL_TEST_CHILD_PID", childPath)
	var out bytes.Buffer
	if err := Start(StartOptions{
		Root: root, WorkDir: root, Agent: AgentCodex, Prompt: "test cleanup", Stdout: &out, Env: os.Environ(),
	}); err != nil {
		t.Fatal(err)
	}
	var started Result
	if err := json.Unmarshal(out.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	// Also clean up if an assertion fails before the guard under test runs.
	t.Cleanup(func() {
		if record, err := loadRunRecord(root, started.InvocationID); err == nil {
			if record.ProcessGroupID > 0 {
				_ = syscall.Kill(-record.ProcessGroupID, syscall.SIGKILL)
			}
			if record.SupervisorPID > 0 {
				_ = syscall.Kill(-record.SupervisorPID, syscall.SIGKILL)
			}
			_ = waitForReleasedChildren(3 * time.Second)
		}
	})
	childPID := waitForProviderChildPID(t, childPath)
	waitForRunState(t, root, started.InvocationID, dispatchStateRunning)
	record, err := loadRunRecord(root, started.InvocationID)
	if err != nil {
		t.Fatal(err)
	}
	if record.SupervisorPID <= 0 || record.ProcessGroupID <= 0 || record.SupervisorPID == record.ProcessGroupID {
		t.Fatalf("expected separate worker and provider groups: %+v", record)
	}
	if stopped {
		if err := syscall.Kill(record.SupervisorPID, syscall.SIGSTOP); err != nil {
			t.Fatal(err)
		}
	}
	if err := reapDetachedWorkers(0); err == nil || !strings.Contains(err.Error(), "outlived the test suite") {
		t.Fatalf("guard did not report the leaked worker: %v", err)
	}
	waitForProviderProcessExit(t, childPID)
	waitForProviderProcessGroupExit(t, record.ProcessGroupID)
	waitForProviderProcessGroupExit(t, record.SupervisorPID)
	var status syscall.WaitStatus
	if _, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil); !errors.Is(err, syscall.ECHILD) {
		t.Fatalf("guard left unreaped children: %v", err)
	}
}
