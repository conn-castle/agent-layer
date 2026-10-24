package wizard

import (
	"context"
	"os"
)

// Run starts the interactive wizard.
// pinVersion is written to .agent-layer/al.version when install is needed.
func Run(root string, ui UI, runSync syncer, pinVersion string) error {
	return RunWithWriter(context.Background(), root, ui, runSync, pinVersion, os.Stdout)
}
