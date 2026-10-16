package main

import (
	"os"
	"testing"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestMain(m *testing.M) {
	// These exact private re-execs exit or replace the process. Their parent
	// already owns the isolated fixture context and is responsible for cleanup.
	if len(os.Args) >= 4 && os.Args[1] == "-test.run=TestHelperProcess" && os.Args[2] == "--" && os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		os.Exit(m.Run())
	}
	if len(os.Args) == 2 {
		if os.Args[1] == "-test.run=^TestDispatchMCPLifecycleHelper$" && os.Getenv("AL_TEST_MCP_LIFECYCLE") == "1" {
			os.Exit(m.Run())
		}
		if os.Getenv(clientLaunchChildRootEnv) != "" {
			switch os.Args[1] {
			case "-test.run=^TestClientArgsPassThrough$", "-test.run=^TestClientArgsPassThroughWithSeparator$",
				"-test.run=^TestClientCommandsSuccess/agy$", "-test.run=^TestClientCommandsSuccess/claude$",
				"-test.run=^TestClientCommandsSuccess/codex$", "-test.run=^TestClientCommandsSuccess/muse$":
				os.Exit(m.Run())
			}
		}
	}
	os.Exit(testutil.RunIsolated(m.Run))
}
