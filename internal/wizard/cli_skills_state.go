package wizard

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/templates"
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
// The minimal placeholder overrides live memory evidence because edited memory
// files may be preserved when the user opts out. The function defaults to true
// when scans fail or the root is unset; an empty `.agent-layer/skills/` directory
// with no managed bundle files maps to false so a wizard rerun on a minimal
// install does not flip Q1 to "yes" by accident.
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
	if instructionPlaceholderExists(root) {
		return hasAnyTemplateMatchingStandardInstructionFile(root)
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
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func hasAnyTemplateMemoryFile(root string) bool {
	for _, name := range memoryFileBasenames {
		path := filepath.Join(root, ".agent-layer", "templates", "docs", name)
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func hasAnyStandardInstructionFile(root string) bool {
	for _, name := range standardInstructionBasenames {
		path := filepath.Join(root, ".agent-layer", "instructions", name)
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func hasAnyTemplateMatchingStandardInstructionFile(root string) bool {
	for _, name := range standardInstructionBasenames {
		path := filepath.Join(root, ".agent-layer", "instructions", name)
		data, err := os.ReadFile(path) // #nosec G304 -- path is root/.agent-layer/instructions/<standard instruction file>, where the basename comes from standardInstructionBasenames.
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return true
		}
		template, err := templates.Read(filepath.ToSlash(filepath.Join("instructions", name)))
		if err != nil {
			return true
		}
		if bytes.Equal(data, template) {
			return true
		}
	}
	return false
}
