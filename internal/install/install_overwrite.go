package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// shouldOverwrite decides whether to overwrite the given path.
// It returns true to overwrite, false to keep existing content, or an error.
func (inst *installer) shouldOverwrite(path string) (bool, error) {
	if !inst.overwrite {
		return false, nil
	}
	if err := inst.resolveOverwriteAllDecisions(); err != nil {
		return false, err
	}
	if inst.isMemoryPath(path) {
		if inst.overwriteMemoryAll {
			return true, nil
		}
	} else if inst.overwriteAll {
		return true, nil
	}
	if inst.prompter == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	rel := path
	if inst.root != "" {
		if candidate, err := filepath.Rel(inst.root, path); err == nil {
			rel = candidate
		}
	}
	preview, err := inst.lookupDiffPreview(rel)
	if err != nil {
		return false, err
	}
	return inst.prompter.overwrite(preview)
}

func (inst *installer) resolveOverwriteAllDecisions() error {
	if inst.overwriteAllDecided {
		return nil
	}
	if inst.prompter == nil || inst.prompter.OverwriteAllUnifiedPreviewFunc == nil {
		return fmt.Errorf(messages.InstallOverwritePromptRequired)
	}

	managedDiffs, err := inst.templates().listManagedDiffs()
	if err != nil {
		return err
	}
	managedPreviews, managedIndex, err := inst.buildManagedDiffPreviews(managedDiffs)
	if err != nil {
		return err
	}
	inst.managedDiffPreviews = managedIndex

	memoryDiffs, err := inst.templates().listMemoryDiffs()
	if err != nil {
		return err
	}
	memoryPreviews, memoryIndex, err := inst.buildMemoryDiffPreviews(memoryDiffs)
	if err != nil {
		return err
	}
	inst.memoryDiffPreviews = memoryIndex

	if len(managedPreviews) == 0 && len(memoryPreviews) == 0 {
		inst.overwriteAll = false
		inst.overwriteMemoryAll = false
		inst.overwriteAllDecided = true
		return nil
	}

	managed, memory, err := inst.prompter.OverwriteAllUnifiedPreviewFunc(managedPreviews, memoryPreviews)
	if err != nil {
		return err
	}
	inst.overwriteAll = managed
	inst.overwriteMemoryAll = memory
	inst.overwriteAllDecided = true
	return nil
}

// isMemoryPath reports whether the path is under docs/agent-layer.
func (inst *installer) isMemoryPath(path string) bool {
	if inst.root == "" {
		return false
	}
	rel, err := filepath.Rel(inst.root, path)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	memoryRoot := filepath.Join("docs", "agent-layer")
	if rel == memoryRoot {
		return true
	}
	return strings.HasPrefix(rel, memoryRoot+string(os.PathSeparator))
}

func (inst *installer) relativePath(path string) string {
	rel := path
	if inst.root != "" {
		if candidate, err := filepath.Rel(inst.root, path); err == nil {
			rel = candidate
		}
	}
	return rel
}

func (inst *installer) lookupDiffPreview(relPath string) (DiffPreview, error) {
	relPath = filepath.ToSlash(relPath)
	if relPath == "" {
		return DiffPreview{}, fmt.Errorf(messages.InstallDiffPreviewPathRequired)
	}
	if preview, ok := inst.managedDiffPreviews[relPath]; ok {
		return preview, nil
	}
	if preview, ok := inst.memoryDiffPreviews[relPath]; ok {
		return preview, nil
	}
	absPath := filepath.Join(inst.root, filepath.FromSlash(relPath))
	templatePathByRel, err := inst.templates().managedTemplatePathByRel()
	if err != nil {
		return DiffPreview{}, err
	}
	if inst.isMemoryPath(absPath) {
		templatePathByRel, err = inst.templates().memoryTemplatePathByRel()
		if err != nil {
			return DiffPreview{}, err
		}
	}
	if relPath != pinVersionRelPath {
		templatePath := templatePathByRel[relPath]
		if strings.TrimSpace(templatePath) == "" {
			return DiffPreview{}, fmt.Errorf(messages.InstallMissingTemplatePathMappingFmt, relPath)
		}
		if err := inst.checkTemplateDiffEvidence(relPath, templatePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return DiffPreview{}, err
		}
	}
	preview, err := inst.buildSingleDiffPreview(relPath, templatePathByRel)
	if err == nil {
		return preview, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return DiffPreview{}, err
	}
	return DiffPreview{
		Path: relPath,
	}, nil
}
