package sync

import (
	"fmt"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// RunWithProject regenerates outputs using an already loaded project config.
//
// Most production entry points use Run or RunWithSystemFS so source loading
// happens inside the lock. Callers that already hold the lock use
// RunLockedProject; this variant exists for focused projection tests.
// Returns any sync-time warnings and an error if sync failed.
func RunWithProject(sys System, root string, project *config.ProjectConfig) (*Result, error) {
	if sys == nil {
		return nil, fmt.Errorf(messages.SyncSystemRequired)
	}
	if project == nil {
		return nil, fmt.Errorf(messages.SyncProjectRequired)
	}
	return withProjectSyncLock(sys, root, func() (*Result, error) {
		return runWithProjectLocked(sys, root, project)
	})
}
