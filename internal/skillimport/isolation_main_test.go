package skillimport

import (
	"os"
	"testing"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestMain(m *testing.M) {
	// This exact private helper inherits the already-isolated parent suite's
	// fixture URL rewrite and temporary root. It runs product recovery code,
	// not fixture setup, and deliberately exits at a transaction checkpoint.
	if len(os.Args) == 2 && os.Args[1] == "-test.run=^TestAdoptionCrashChild$" && os.Getenv("AL_TEST_ADOPTION_CHILD") == "1" {
		os.Exit(m.Run())
	}
	os.Exit(testutil.RunIsolated(m.Run))
}
