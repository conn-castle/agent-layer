package wizard

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilltree"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// The wizard diagnoses moved optional skills through CatalogState. Ordinary
// project loading would reject malformed local candidates before that preview.
// Keep all other project inputs strict; sync remains the authoritative loader.
func loadWizardProjectConfig(root string) (*config.ProjectConfig, error) {
	return config.LoadProjectConfigFS(catalogPreviewFS{FS: os.DirFS(root), root: root}, root)
}

type catalogPreviewFS struct {
	fs.FS
	root string
}

func (f catalogPreviewFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	if dir != ".agent-layer/skills" {
		return fs.ReadDir(f.FS, dir)
	}
	// Never follow a bad tier root even during inspection. CatalogState reports
	// the actionable root problem on every affected remote member instead.
	info, err := os.Lstat(filepath.Join(f.root, filepath.FromSlash(dir)))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil //nolint:nilerr // CatalogState reports this tier root problem on the affected rows.
	}
	entries, err := fs.ReadDir(f.FS, dir)
	if err != nil {
		return nil, nil //nolint:nilerr // CatalogState reports this tier root problem on the affected rows.
	}
	visible := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !templates.IsRetiredSkill(strings.ToLower(skilltree.NormalizeName(entry.Name()))) {
			visible = append(visible, entry)
		}
	}
	return visible, nil
}
