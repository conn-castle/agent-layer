package sync

import (
	"github.com/conn-castle/agent-layer/internal/config"
)

// writeCodexConfig patches Agent Layer-owned entries in .codex/config.toml.
func writeCodexConfig(sys System, root string, project *config.ProjectConfig) error {
	return writeCodexConfigWithCLISettings(sys, root, project, true)
}

func buildCodexConfigWithSystem(sys System, root string, project *config.ProjectConfig) (string, error) {
	managed, err := buildCodexManagedConfigWithSystem(sys, root, project, true)
	if err != nil {
		return "", err
	}
	return managed.Content, nil
}
