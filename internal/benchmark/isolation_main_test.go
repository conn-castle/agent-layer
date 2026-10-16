package benchmark

import (
	"os"
	"testing"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testutil.RunIsolated(m.Run))
}
