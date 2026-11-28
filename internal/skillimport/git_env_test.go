package skillimport

import (
	"os"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func init() {
	testutil.IsolateGit()
	// Set constant test Git identity once at package init so individual tests
	// do not call t.Setenv and can safely run with t.Parallel().
	_ = os.Setenv("GIT_AUTHOR_NAME", "Agent Layer Test")
	_ = os.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	_ = os.Setenv("GIT_COMMITTER_NAME", "Agent Layer Test")
	_ = os.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
}
