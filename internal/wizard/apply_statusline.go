package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/install"
)

type statuslineSourceChangeSet struct {
	sourcesToCreate []install.StatuslineSourceTemplate
}

func computeStatuslineSourceChangeSet(root string, choices *Choices) (statuslineSourceChangeSet, error) {
	var out statuslineSourceChangeSet
	for _, file := range selectedStatuslineSourceFiles(choices) {
		path := filepath.Join(root, filepath.FromSlash(file.RelPath))
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				return statuslineSourceChangeSet{}, fmt.Errorf("%s is a directory", file.RelPath)
			}
			continue
		}
		if !os.IsNotExist(err) {
			return statuslineSourceChangeSet{}, err
		}
		out.sourcesToCreate = append(out.sourcesToCreate, file)
	}
	return out, nil
}

func selectedStatuslineSourceFiles(choices *Choices) []install.StatuslineSourceTemplate {
	files := make([]install.StatuslineSourceTemplate, 0, 2)
	for _, source := range install.StatuslineSourceTemplates() {
		switch source.RelPath {
		case claudeStatuslinePath:
			if choices.ClaudeStatusline && claudeToggleVisible(choices) {
				files = append(files, source)
			}
		case ".agent-layer/codex-statusline.toml":
			if choices.CodexStatusline && codexStatuslineToggleVisible(choices) {
				files = append(files, source)
			}
		}
	}
	return files
}

func applyStatuslineSourceChanges(root string, changes statuslineSourceChangeSet) error {
	for _, file := range changes.sourcesToCreate {
		data, err := install.StatuslineSourceSeedBytes(root, file)
		if err != nil {
			return err
		}
		path := filepath.Join(root, filepath.FromSlash(file.RelPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		if err := fsutil.WriteFileAtomic(path, data, file.Perm); err != nil {
			return err
		}
	}
	return nil
}

func buildStatuslineSourcePreview(changes statuslineSourceChangeSet) string {
	if len(changes.sourcesToCreate) == 0 {
		return ""
	}
	lines := make([]string, 0, len(changes.sourcesToCreate)+1)
	lines = append(lines, "Statusline source changes:")
	for _, file := range changes.sourcesToCreate {
		lines = append(lines, "  + "+file.RelPath+"  (seed-once source)")
	}
	return strings.Join(lines, "\n")
}
