package wizard

import (
	"io/fs"
	"os"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
)

// Preview accepted local instruction ordering without writing configuration.
func loadWizardProjectConfig(root string) (*config.ProjectConfig, error) {
	raw, err := fsutil.ReadRegularFile(config.DefaultPaths(root).ConfigPath)
	if err != nil {
		return nil, err
	}
	preview, err := config.MigrateInstructionOrderFS(os.DirFS(root), root, string(raw))
	if err != nil {
		return nil, err
	}
	return config.LoadProjectConfigFS(catalogPreviewFS{FS: os.DirFS(root), root: root, configRaw: []byte(preview)}, root)
}

type catalogPreviewFS struct {
	fs.FS
	root      string
	configRaw []byte
}

func (f catalogPreviewFS) ReadFile(name string) ([]byte, error) {
	if name == ".agent-layer/config.toml" {
		return f.configRaw, nil
	}
	return fs.ReadFile(f.FS, name)
}
