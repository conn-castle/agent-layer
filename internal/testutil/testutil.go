package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

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
