package wizard

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/install"
)

// catalogSkillExistsOnDisk reports whether a CLI catalog skill directory is
// present at .agent-layer/skills/<id>/. It is used both for default-from-state
// in initializeChoices and for the apply path's add/remove diff.
func catalogSkillExistsOnDisk(root string, id string) bool {
	if root == "" || !isSafeCLISkillCatalogID(id) {
		return false
	}
	dir := filepath.Join(root, ".agent-layer", "skills", id)
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// detectAgentLayerEnabledFromDisk returns true when the workflow bundle appears
// present in the project — any embedded workflow-bundle skill directory,
// standard instruction file, managed memory template, or live memory file exists.
// The function defaults to true when scans fail or the root is unset; an empty
// `.agent-layer/skills/` directory with no managed bundle files maps to false.
//
// The result is a hint for the default value of the Q1 prompt and is overridden
// by the user's actual selection.
func detectAgentLayerEnabledFromDisk(root string) bool {
	if root == "" {
		return true
	}
	if hasNonCatalogWorkflowSkill(root) {
		return true
	}
	if hasAnyTemplateMemoryFile(root) {
		return true
	}
	if hasAnyStandardInstructionFile(root) {
		return true
	}
	if hasAnyMemoryFile(root) {
		return true
	}
	return false
}

func hasNonCatalogWorkflowSkill(root string) bool {
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false
		}
		// On any other read error, fall back to true to bias toward keeping the
		// bundle rather than silently pruning user data.
		return true
	}
	workflowIDs, err := embeddedWorkflowSkillIDs()
	if err != nil || len(workflowIDs) == 0 {
		return len(entries) > 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, ok := workflowIDs[entry.Name()]; ok {
			return true
		}
	}
	return false
}

func hasAnyMemoryFile(root string) bool {
	for _, name := range memoryFileBasenames {
		path := filepath.Join(root, "docs", "agent-layer", name)
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			return true
		}
	}
	return false
}

func hasAnyTemplateMemoryFile(root string) bool {
	for _, name := range memoryFileBasenames {
		path := filepath.Join(root, ".agent-layer", "templates", "docs", name)
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			return true
		}
	}
	return false
}

func hasAnyStandardInstructionFile(root string) bool {
	for _, name := range standardInstructionBasenames {
		path := filepath.Join(root, ".agent-layer", "instructions", name)
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			return true
		}
	}
	return false
}

func isUserOwnedStandardInstructionFile(name string) bool {
	return install.IsUserOwnedInstructionFile(name)
}
