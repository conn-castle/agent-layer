package testutil

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type testFailure struct {
	message string
}

type fakeTestContext struct {
	cleanups []func()
}

func (f *fakeTestContext) Cleanup(cleanup func()) {
	f.cleanups = append(f.cleanups, cleanup)
}

func (f *fakeTestContext) Fatal(args ...any) {
	panic(testFailure{message: fmt.Sprint(args...)})
}

func (f *fakeTestContext) Fatalf(format string, args ...any) {
	panic(testFailure{message: fmt.Sprintf(format, args...)})
}

func (f *fakeTestContext) Helper() {}

func (f *fakeTestContext) runCleanups() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

func requireTestFailure(t *testing.T, fn func()) (failure testFailure) {
	t.Helper()
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected test failure")
		}
		var ok bool
		failure, ok = recovered.(testFailure)
		if !ok {
			t.Fatalf("expected testFailure, got %T", recovered)
		}
	}()
	fn()
	return testFailure{}
}

func TestCaptureExecCapturesIndependentInputsAndRestoresTarget(t *testing.T) {
	context := &fakeTestContext{}
	wantErr := errors.New("exec failed")
	originalErr := errors.New("original handoff")
	originalCalls := 0
	target := func(string, []string, []string) error {
		originalCalls++
		return originalErr
	}

	call := captureExec(context, &target, wantErr)
	argv := []string{"tool", "--flag"}
	env := []string{"PATH=/test", "CUSTOM=1"}
	if err := target("/test/tool", argv, env); !errors.Is(err, wantErr) {
		t.Fatalf("expected captured handoff error %v, got %v", wantErr, err)
	}
	argv[0] = "changed"
	env[0] = "PATH=/changed"

	if !call.Called {
		t.Fatal("expected captured handoff")
	}
	if call.Path != "/test/tool" {
		t.Fatalf("captured path = %q, want /test/tool", call.Path)
	}
	if !reflect.DeepEqual(call.Argv, []string{"tool", "--flag"}) {
		t.Fatalf("captured argv changed with caller input: %#v", call.Argv)
	}
	if !reflect.DeepEqual(call.Env, []string{"PATH=/test", "CUSTOM=1"}) {
		t.Fatalf("captured env changed with caller input: %#v", call.Env)
	}

	context.runCleanups()
	if err := target("/test/tool", nil, nil); !errors.Is(err, originalErr) {
		t.Fatalf("expected restored handoff error %v, got %v", originalErr, err)
	}
	if originalCalls != 1 {
		t.Fatalf("original handoff calls = %d, want 1 after cleanup", originalCalls)
	}
}

func TestCaptureExecRejectsSecondCall(t *testing.T) {
	context := &fakeTestContext{}
	target := func(string, []string, []string) error { return nil }
	captureExec(context, &target, nil)
	if err := target("tool", nil, nil); err != nil {
		t.Fatalf("first captured call returned %v", err)
	}

	failure := requireTestFailure(t, func() {
		_ = target("tool", nil, nil)
	})
	if failure.message != "exec function called more than once" {
		t.Fatalf("unexpected second-call failure: %q", failure.message)
	}
}

func TestForbidExecFailsAndRestoresTarget(t *testing.T) {
	context := &fakeTestContext{}
	wantErr := errors.New("original handoff")
	target := func(string, []string, []string) error { return wantErr }
	forbidExec(context, &target)

	failure := requireTestFailure(t, func() {
		_ = target("tool", nil, nil)
	})
	if failure.message != "exec function should not be called" {
		t.Fatalf("unexpected no-exec failure: %q", failure.message)
	}

	context.runCleanups()
	if err := target("tool", nil, nil); !errors.Is(err, wantErr) {
		t.Fatalf("expected restored handoff error %v, got %v", wantErr, err)
	}
}

// TestCaptureExecExportedSeamCapturesAndAssertsCalled drives the exported
// CaptureExec/AssertCalled entry points with a real *testing.T so the public
// surface used by the launcher suites is exercised (and covered) in-package.
func TestCaptureExecExportedSeamCapturesAndAssertsCalled(t *testing.T) {
	target := func(string, []string, []string) error { return errors.New("real handoff") }
	wantErr := errors.New("stub failure")
	call := CaptureExec(t, &target, wantErr)

	argv := []string{"launch", "--flag"}
	env := []string{"PATH=/bin"}
	if err := target("/bin/tool", argv, env); !errors.Is(err, wantErr) {
		t.Fatalf("captured handoff error = %v, want %v", err, wantErr)
	}

	call.AssertCalled(t, "/bin/tool", []string{"launch", "--flag"})
	if !reflect.DeepEqual(call.Env, []string{"PATH=/bin"}) {
		t.Fatalf("captured env = %#v, want [PATH=/bin]", call.Env)
	}
}

// TestForbidExecExportedSeamReplacesAndRestoresTarget drives the exported
// ForbidExec entry point with a real *testing.T so the public no-exec seam used
// by the launcher suites is exercised (and covered) in-package. The forbidding
// handoff cannot be invoked under a real *testing.T without aborting, so the
// test proves replacement and cleanup restoration via the installed function's
// identity rather than by calling it.
func TestForbidExecExportedSeamReplacesAndRestoresTarget(t *testing.T) {
	original := func(string, []string, []string) error { return errors.New("original handoff") }
	target := original
	originalPtr := reflect.ValueOf(original).Pointer()

	t.Run("install", func(t *testing.T) {
		ForbidExec(t, &target)
		if reflect.ValueOf(target).Pointer() == originalPtr {
			t.Fatal("ForbidExec did not replace the target handoff")
		}
	})

	if reflect.ValueOf(target).Pointer() != originalPtr {
		t.Fatal("ForbidExec cleanup did not restore the original handoff")
	}
}

// TestAssertCalledReportsMismatches proves the shared AssertCalled assertion
// actually detects each failure mode, so a comparison that silently stopped
// failing could not pass unnoticed across the four launcher suites.
func TestAssertCalledReportsMismatches(t *testing.T) {
	t.Run("not called", func(t *testing.T) {
		call := &ExecCall{}
		failure := requireTestFailure(t, func() {
			call.assertCalled(&fakeTestContext{}, "/bin/tool", nil)
		})
		if failure.message != "expected exec function to be called" {
			t.Fatalf("unexpected not-called failure: %q", failure.message)
		}
	})
	t.Run("path mismatch", func(t *testing.T) {
		call := &ExecCall{Called: true, Path: "/bin/other", Argv: []string{"a"}}
		failure := requireTestFailure(t, func() {
			call.assertCalled(&fakeTestContext{}, "/bin/tool", []string{"a"})
		})
		if !strings.HasPrefix(failure.message, "expected exec path") {
			t.Fatalf("unexpected path-mismatch failure: %q", failure.message)
		}
	})
	t.Run("argv mismatch", func(t *testing.T) {
		call := &ExecCall{Called: true, Path: "/bin/tool", Argv: []string{"a"}}
		failure := requireTestFailure(t, func() {
			call.assertCalled(&fakeTestContext{}, "/bin/tool", []string{"b"})
		})
		if !strings.HasPrefix(failure.message, "unexpected argv") {
			t.Fatalf("unexpected argv-mismatch failure: %q", failure.message)
		}
	})
}

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
