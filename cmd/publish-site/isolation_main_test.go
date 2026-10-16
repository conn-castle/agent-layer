package main

import (
	"os"
	"testing"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestMain(m *testing.M) {
	// The existing private helpers call os.Exit, so use the already-isolated
	// parent's fixture context rather than allocating a root they cannot reap.
	if len(os.Args) >= 4 && os.Args[1] == "-test.run=TestHelperProcess" && os.Args[2] == "--" && os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Exit(m.Run())
	}
	if len(os.Args) == 3 && os.Args[1] == "-test.run=TestMainHelper" && os.Args[2] == "--" && os.Getenv("GO_WANT_MAIN") == "1" {
		os.Exit(m.Run())
	}
	os.Exit(testutil.RunIsolated(m.Run))
}
