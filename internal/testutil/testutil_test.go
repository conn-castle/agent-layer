package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestWriteStubCreatesExecutableThatSucceeds(t *testing.T) {
	dir := t.TempDir()
	stubPath := filepath.Join(dir, "ok-stub")
	WriteStub(t, dir, "ok-stub")

	info, err := os.Stat(stubPath)
	if err != nil {
		t.Fatalf("stat stub: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("expected mode 0755, got %#o", info.Mode().Perm())
	}

	cmd := exec.Command(stubPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected success exit, got %v", err)
	}
}

func TestWriteStubWithExitCreatesExecutableWithRequestedExitCode(t *testing.T) {
	dir := t.TempDir()
	stubPath := filepath.Join(dir, "exit-stub")
	WriteStubWithExit(t, dir, "exit-stub", 7)

	cmd := exec.Command(stubPath)
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit status")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T", err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("expected exit code 7, got %d", exitErr.ExitCode())
	}
}

func TestWriteStubExpectArgHonorsRequiredArg(t *testing.T) {
	dir := t.TempDir()
	stubPath := filepath.Join(dir, "arg-stub")
	WriteStubExpectArg(t, dir, "arg-stub", "--ready")

	cmd := exec.Command(stubPath, "--ready")
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected success with required arg, got %v", err)
	}

	cmd = exec.Command(stubPath, "--missing")
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected non-zero exit without required arg")
	}
}

func TestBoolPtr(t *testing.T) {
	ptr := BoolPtr(true)
	if ptr == nil {
		t.Fatal("expected non-nil pointer")
	}
	if !*ptr {
		t.Fatal("expected pointer value true")
	}
}

func TestWithWorkingDirRunsInTargetDirectoryAndRestoresOriginal(t *testing.T) {
	targetDir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd before test: %v", err)
	}

	var observedDir string
	WithWorkingDir(t, targetDir, func() {
		wd, innerErr := os.Getwd()
		if innerErr != nil {
			t.Fatalf("getwd inside callback: %v", innerErr)
		}
		observedDir = wd
	})

	targetReal, err := filepath.EvalSymlinks(targetDir)
	if err != nil {
		targetReal = targetDir
	}
	observedReal, err := filepath.EvalSymlinks(observedDir)
	if err != nil {
		observedReal = observedDir
	}
	if observedReal != targetReal {
		t.Fatalf("expected callback cwd %q (real %q), got %q (real %q)", targetDir, targetReal, observedDir, observedReal)
	}

	finalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd after callback: %v", err)
	}
	origReal, err := filepath.EvalSymlinks(origDir)
	if err != nil {
		origReal = origDir
	}
	finalReal, err := filepath.EvalSymlinks(finalDir)
	if err != nil {
		finalReal = finalDir
	}
	if finalReal != origReal {
		t.Fatalf("expected cwd restored to %q (real %q), got %q (real %q)", origDir, origReal, finalDir, finalReal)
	}
}
