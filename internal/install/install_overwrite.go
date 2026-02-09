package install

import (
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
	return inst.prompter.Overwrite(rel)
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
	overwriteAll, err := inst.prompter.OverwriteAll(formatLabeledPaths(diffs))
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
	overwriteAll, err := inst.prompter.OverwriteAllMemory(formatLabeledPaths(diffs))
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
