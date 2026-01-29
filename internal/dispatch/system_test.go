package dispatch

import (
	"os"
	"os/exec"
	"testing"
)

// TestRealSystem_ExecBinary verifies that RealSystem.ExecBinary calls execBinary.
// This test uses a subprocess pattern similar to TestExecBinary in exec_unix_coverage_test.go.
func TestRealSystem_ExecBinary(t *testing.T) {
	if os.Getenv("GO_TEST_REALSYSTEM_EXECBINARY_SUBPROCESS") == "1" {
		// Inside the subprocess.
		bin, err := exec.LookPath("true")
		if err != nil {
			bin = "/bin/true"
		}

		sys := RealSystem{}
		err = sys.ExecBinary(bin, []string{"true"}, os.Environ(), nil)
		// If ExecBinary returns, it failed.
		if err != nil {
			os.Exit(1)
		}
		// Should not be reached on success.
		os.Exit(2)
		return
	}

	// In the parent test process: spawn the subprocess.
	cmd := exec.Command(os.Args[0], "-test.run=TestRealSystem_ExecBinary")
	cmd.Env = append(os.Environ(), "GO_TEST_REALSYSTEM_EXECBINARY_SUBPROCESS=1")
	err := cmd.Run()

	// If ExecBinary succeeded, "true" exit code is 0.
	if err != nil {
		t.Fatalf("subprocess failed: %v", err)
	}
}

func TestMaybeExecWithSystem_NilSystem(t *testing.T) {
	err := MaybeExecWithSystem(nil, []string{"cmd"}, "1.0.0", ".", func(int) {})
	if err == nil {
		t.Fatalf("expected error for nil system")
	}
}
