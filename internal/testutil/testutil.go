package testutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// ExecCall records one captured executable handoff.
type ExecCall struct {
	Called bool
	Path   string
	Argv   []string
	Env    []string
}

// AssertCalled verifies that the executable handoff occurred once with the
// expected path and argument vector.
func (call *ExecCall) AssertCalled(t *testing.T, wantPath string, wantArgv []string) {
	t.Helper()
	call.assertCalled(t, wantPath, wantArgv)
}

// assertCalled implements AssertCalled for both production test contexts and
// direct helper behavior tests.
func (call *ExecCall) assertCalled(t testContext, wantPath string, wantArgv []string) {
	t.Helper()
	if !call.Called {
		t.Fatal("expected exec function to be called")
	}
	if call.Path != wantPath {
		t.Fatalf("expected exec path %q, got %q", wantPath, call.Path)
	}
	if !slices.Equal(call.Argv, wantArgv) {
		t.Fatalf("unexpected argv: got %#v want %#v", call.Argv, wantArgv)
	}
}

// CaptureExec replaces target with a one-call capture seam that returns err.
// It restores the original handoff when the test completes.
func CaptureExec(t *testing.T, target *func(string, []string, []string) error, err error) *ExecCall {
	return captureExec(t, target, err)
}

// ForbidExec replaces target with a test failure and restores the original
// handoff when the test completes.
func ForbidExec(t *testing.T, target *func(string, []string, []string) error) {
	forbidExec(t, target)
}

// testContext captures the testing hooks needed by the exec seam helpers.
type testContext interface {
	Cleanup(func())
	Fatal(...any)
	Fatalf(string, ...any)
	Helper()
}

// captureExec installs the capture seam for both production test contexts and
// direct helper behavior tests.
func captureExec(t testContext, target *func(string, []string, []string) error, err error) *ExecCall {
	t.Helper()
	original := *target
	call := &ExecCall{}
	*target = func(path string, argv []string, env []string) error {
		if call.Called {
			t.Fatal("exec function called more than once")
		}
		call.Called = true
		call.Path = path
		call.Argv = append([]string(nil), argv...)
		call.Env = append([]string(nil), env...)
		return err
	}
	t.Cleanup(func() { *target = original })
	return call
}

// forbidExec installs the no-exec seam for both production test contexts and
// direct helper behavior tests.
func forbidExec(t testContext, target *func(string, []string, []string) error) {
	t.Helper()
	original := *target
	*target = func(string, []string, []string) error {
		t.Fatal("exec function should not be called")
		return errors.New("exec function should not be called")
	}
	t.Cleanup(func() { *target = original })
}

// WriteStub writes an executable shell stub that exits successfully.
// t is the active test; dir is the output directory; name is the executable file name.
func WriteStub(t *testing.T, dir string, name string) {
	t.Helper()
	WriteStubWithExit(t, dir, name, 0)
}

// WriteStubWithExit writes an executable shell stub that exits with the provided code.
// t is the active test; dir is the output directory; name is the executable file name.
func WriteStubWithExit(t *testing.T, dir string, name string, exitCode int) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := []byte(fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode))
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}

// WriteStubExpectArg writes an executable shell stub that succeeds only when expectedArg is present.
// t is the active test; dir is the output directory; name is the executable file name.
func WriteStubExpectArg(t *testing.T, dir string, name string, expectedArg string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := []byte(fmt.Sprintf("#!/bin/sh\nfor arg in \"$@\"; do\n  if [ \"$arg\" = \"%s\" ]; then exit 0; fi\ndone\nexit 1\n", expectedArg))
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}

// BoolPtr returns a pointer to v.
// v is the boolean value to take the address of.
func BoolPtr(v bool) *bool {
	return &v
}

// WithWorkingDir runs fn with dir as the current working directory and restores the previous directory.
// t is the active test; dir is the temporary working directory for fn.
func WithWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore chdir: %v", err)
		}
	}()
	fn()
}
