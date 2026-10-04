package agentdispatch

import (
	"io"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/sync"
)

// loadDispatchProject loads the combined skill-source snapshot inside the
// project lock so `--skill` validation sees imported skills exactly as ordinary
// projection does.
func loadDispatchProject(root string, stderr io.Writer, env []string) (*config.ProjectConfig, io.Writer, []string, int, error) {
	project, err := sync.LoadLockedSources(sync.RealSystem{}, root)
	if err != nil {
		return nil, nil, nil, 0, wrapExitError(ExitConfig, err.Error(), err)
	}
	stderr, env, depth, err := dispatchRuntimeInputs(stderr, env)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	return project, stderr, env, depth, nil
}
