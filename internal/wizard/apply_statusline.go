package wizard

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/templates"
)

type statuslineSourceFile struct {
	relPath      string
	templatePath string
	perm         fs.FileMode
}

type statuslineSourceChangeSet struct {
	sourcesToCreate []statuslineSourceFile
}

func computeStatuslineSourceChangeSet(root string, choices *Choices) (statuslineSourceChangeSet, error) {
	var out statuslineSourceChangeSet
	for _, file := range selectedStatuslineSourceFiles(choices) {
		path := filepath.Join(root, filepath.FromSlash(file.relPath))
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				return statuslineSourceChangeSet{}, fmt.Errorf("%s is a directory", file.relPath)
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

func selectedStatuslineSourceFiles(choices *Choices) []statuslineSourceFile {
	files := make([]statuslineSourceFile, 0, 2)
	if choices.ClaudeStatuslineTouched && choices.ClaudeStatusline && claudeToggleVisible(choices) {
		files = append(files, statuslineSourceFile{
			relPath:      ".agent-layer/claude-statusline.sh",
			templatePath: "claude-statusline.sh",
			perm:         0o755,
		})
	}
	if choices.CodexStatuslineTouched && choices.CodexStatusline && codexToggleVisible(choices) {
		files = append(files, statuslineSourceFile{
			relPath:      ".agent-layer/codex-statusline.toml",
			templatePath: "codex-statusline.toml",
			perm:         0o644,
		})
	}
	return files
}

func applyStatuslineSourceChanges(root string, changes statuslineSourceChangeSet) error {
	for _, file := range changes.sourcesToCreate {
		data, err := templates.Read(file.templatePath)
		if err != nil {
			return err
		}
		path := filepath.Join(root, filepath.FromSlash(file.relPath))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		if err := fsutil.WriteFileAtomic(path, data, file.perm); err != nil {
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
		lines = append(lines, "  + "+file.relPath+"  (seed-once source)")
	}
	return strings.Join(lines, "\n")
}
