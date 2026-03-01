package dispatch

import (
	"os"
	"os/exec"
	"testing"
)

// TestExecBinary uses a subprocess to verifying that execBinary calls syscall.Exec.
// The subprocess will execute a simple command (like "echo") via execBinary.
func TestExecBinary(t *testing.T) {
	if os.Getenv("GO_TEST_EXEC_BINARY_SUBPROCESS") == "1" {
		// Inside the subprocess.
		// Use "true" as the binary to exec.
		bin, err := exec.LookPath("true")
		if err != nil {
			// If true is missing, try echo?
			bin = "/bin/true"
		}

		err = execBinary(bin, []string{"true"}, os.Environ(), nil)
		// If execBinary returns, it failed.
		if err != nil {
			os.Exit(1)
		}
		// Should not be reached on success.
		os.Exit(2)
		return
	}

	// In the parent test process: spawn the subprocess.
	cmd := exec.Command(os.Args[0], "-test.run=TestExecBinary") //nolint:gosec // standard test re-exec pattern
	cmd.Env = append(os.Environ(), "GO_TEST_EXEC_BINARY_SUBPROCESS=1")
	err := cmd.Run()

	// If execBinary succeeded, "true" exit code is 0.
	// If execBinary failed (returned error), we exit(1).
	// If execBinary returned nil (impossible for syscall.Exec on success, but if it did), we exit(2).
	if err != nil {
		t.Fatalf("subprocess failed: %v", err)
	}
}
