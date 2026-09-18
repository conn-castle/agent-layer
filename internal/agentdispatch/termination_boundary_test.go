package agentdispatch

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// A provider that leaves its launch group is still owned execution. The empty
// original group must not let observation confirm termination of that process.
func TestTerminationWaitsForProviderOutsideOriginalGroup(t *testing.T) {
	const groupEnv = "AL_TEST_PROVIDER_JOIN_GROUP"
	if value := os.Getenv(groupEnv); value != "" {
		group, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := syscall.Setpgid(0, group); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintln(os.Stdout, "moved"); err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestTerminationWaitsForProviderOutsideOriginalGroup$") // #nosec G204 -- run this test binary as a controlled provider fixture.
	cmd.Env = append(os.Environ(), groupEnv+"="+strconv.Itoa(syscall.Getpgrp()))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "moved\n" {
		t.Fatalf("provider handoff: %q, %v", line, err)
	}
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	run.Record.PID = cmd.Process.Pid
	run.Record.ProcessGroupID = cmd.Process.Pid
	run.Record.ProcessStartIdentity = processStartIdentity(cmd.Process.Pid)
	run.Record.ProviderLaunchIntent = true
	run.Record.State = dispatchStateCancelled
	now := time.Now().UTC()
	run.Record.CompletedAt = &now
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if !providerProcessGroupDead(cmd.Process.Pid) {
		t.Fatal("provider did not leave its original group")
	}
	var out bytes.Buffer
	if err := Inspect(InspectRequest{Root: root, InvocationID: run.Record.ID, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	var result InspectResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.TerminationConfirmed {
		t.Fatalf("live provider was confirmed stopped: %+v", result)
	}
	if bytes.Contains(out.Bytes(), []byte("termination_observation")) {
		t.Fatalf("inspect exposed private termination evidence: %s", out.Bytes())
	}
}

func TestOutputRejectsFIFOWithoutWaitingForWriter(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	if err := syscall.Mkfifo(run.Record.EventsPath, 0o600); err != nil {
		t.Fatal(err)
	}
	run.Record.ProviderLaunchIntent = true
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() {
		done <- Output(OutputRequest{Root: root, InvocationID: run.Record.ID, Artifact: artifactEvents, Stdout: &out})
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO retrieval succeeded")
		}
	case <-time.After(time.Second):
		// Unblock a regressed blocking open before failing the test.
		file, _ := os.OpenFile(run.Record.EventsPath, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if file != nil {
			defer func() { _ = file.Close() }()
		}
		<-done
		t.Fatal("output waited for a FIFO writer")
	}
}

func TestMissingStartedCaptureIsRetrievalFailure(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, _ := newWaitTestRun(t, root)
	run.Record.ProviderLaunchIntent = true
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Output(OutputRequest{Root: root, InvocationID: run.Record.ID, Artifact: artifactEvents, Stdout: &out})
	if err == nil {
		t.Fatal("missing partial output capture succeeded")
	}
	if out.Len() != 0 {
		t.Fatalf("failed output wrote a result: %s", out.Bytes())
	}
}

func TestMCPCancellationFailureIsNotATerminalSuccess(t *testing.T) {
	for _, state := range []string{dispatchStateCancelled, dispatchStateFailed} {
		out := bytes.NewBufferString(`{"handle":"tiny-round-capacitor","state":"` + state + `","termination_confirmed":false}`)
		_, result, err := decodeCancelResult(out, errors.New("termination failed"))
		if err == nil || result != nil {
			t.Fatalf("failed cancellation returned a successful terminal result: %+v, %v", result, err)
		}
	}
}

func TestCancelRetryConfirmsAlreadyStoppedGroup(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, session := newWaitTestRun(t, root)
	cmd := exec.Command("sleep", "60")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	run.Record.PID = cmd.Process.Pid
	run.Record.ProcessGroupID = cmd.Process.Pid
	run.Record.ProcessStartIdentity = processStartIdentity(cmd.Process.Pid)
	run.Record.ProviderLaunchIntent = true
	run.Record.State = dispatchStateCancelled
	now := time.Now().UTC()
	run.Record.CompletedAt = &now
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	var out bytes.Buffer
	if err := Cancel(CancelRequest{Root: root, InvocationID: run.Record.ID, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.TerminationConfirmed {
		t.Fatal("retry did not confirm stopped provider")
	}
	released, err := loadSession(root, session.Name)
	if err != nil || released.ActiveRunID != "" {
		t.Fatalf("retry did not release the confirmed claim: %+v, %v", released, err)
	}
}
