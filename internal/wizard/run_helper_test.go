package wizard

import (
	"context"
	"os"

	"github.com/conn-castle/agent-layer/internal/config"
)

// Run starts the interactive wizard.
// pinVersion is written to .agent-layer/al.version when install is needed.
func Run(root string, ui UI, runSync syncer, pinVersion string) error {
	return RunWithWriter(context.Background(), root, ui, runSync, pinVersion, os.Stdout)
}

func initializeChoices(cfg *config.ProjectConfig) (*Choices, error) {
	return initializeChoicesWithCatalog(context.Background(), cfg, true)
}
