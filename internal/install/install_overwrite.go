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
	if inst.force {
		return true, nil
	}
	if inst.isMemoryPath(path) {
		overwriteAll, err := inst.shouldOverwriteAllMemory()
		if err != nil {
			return false, err
		}
		if overwriteAll {
			return true, nil
		}
	} else {
		overwriteAll, err := inst.shouldOverwriteAllManaged()
		if err != nil {
			return false, err
		}
		if overwriteAll {
			return true, nil
		}
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
	return inst.prompter.Overwrite(preview)
}

// shouldOverwriteAllManaged resolves the "overwrite all managed files" decision.
func (inst *installer) shouldOverwriteAllManaged() (bool, error) {
	if inst.overwriteAllDecided {
		return inst.overwriteAll, nil
	}
	if inst.prompter == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	diffs, err := inst.listManagedLabeledDiffs()
	if err != nil {
		return false, err
	}
	previews, index, err := inst.buildManagedDiffPreviews(diffs)
	if err != nil {
		return false, err
	}
	inst.managedDiffPreviews = index
	overwriteAll, err := inst.prompter.OverwriteAll(previews)
	if err != nil {
		return false, err
	}
	inst.overwriteAll = overwriteAll
	inst.overwriteAllDecided = true
	return overwriteAll, nil
}

// shouldOverwriteAllMemory resolves the "overwrite all memory files" decision.
func (inst *installer) shouldOverwriteAllMemory() (bool, error) {
	if inst.overwriteMemoryAllDecided {
		return inst.overwriteMemoryAll, nil
	}
	if inst.prompter == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	diffs, err := inst.listMemoryLabeledDiffs()
	if err != nil {
		return false, err
	}
	previews, index, err := inst.buildMemoryDiffPreviews(diffs)
	if err != nil {
		return false, err
	}
	inst.memoryDiffPreviews = index
	overwriteAll, err := inst.prompter.OverwriteAllMemory(previews)
	if err != nil {
		return false, err
	}
	inst.overwriteMemoryAll = overwriteAll
	inst.overwriteMemoryAllDecided = true
	return overwriteAll, nil
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
	templatePathByRel, err := inst.managedTemplatePathByRel()
	if err != nil {
		return DiffPreview{}, err
	}
	if inst.isMemoryPath(absPath) {
		templatePathByRel, err = inst.memoryTemplatePathByRel()
		if err != nil {
			return DiffPreview{}, err
		}
	}
	entry, err := inst.diffPreviewEntry(relPath, templatePathByRel)
	if err != nil {
		return DiffPreview{}, err
	}
	preview, err := inst.buildSingleDiffPreview(entry, templatePathByRel)
	if err == nil {
		return preview, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return DiffPreview{}, err
	}
	return DiffPreview{
		Path:      relPath,
		Ownership: entry.Ownership,
	}, nil
}

func (inst *installer) diffPreviewEntry(relPath string, templatePathByRel map[string]string) (LabeledPath, error) {
	if relPath == pinVersionRelPath {
		return LabeledPath{
			Path:      relPath,
			Ownership: OwnershipUpstreamTemplateDelta,
		}, nil
	}
	templatePath := templatePathByRel[relPath]
	if strings.TrimSpace(templatePath) == "" {
		return LabeledPath{}, fmt.Errorf(messages.InstallMissingTemplatePathMappingFmt, relPath)
	}
	ownership, err := inst.classifyOwnership(relPath, templatePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return LabeledPath{}, err
		}
		ownership = OwnershipLocalCustomization
	}
	return LabeledPath{
		Path:      relPath,
		Ownership: ownership,
	}, nil
}
