package wizard

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// catalogSkillExistsOnDisk reports whether a CLI catalog skill directory is
// present at .agent-layer/skills/<id>/. It is used both for default-from-state
// in initializeChoices and for the apply path's add/remove diff.
func catalogSkillExistsOnDisk(root string, id string) bool {
	if root == "" || !templates.IsSafeCLISkillCatalogID(id) {
		return false
	}
	dir := filepath.Join(root, ".agent-layer", "skills", id)
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// catalogSkillIsManagedOnDisk reports whether the directory at a catalog
// embedded entry's path is safe for the wizard to claim and remove. Entries without an ownership marker
// retain the legacy directory-presence behavior. Marked entries must contain
// their marker in SKILL.md so a user-authored same-name skill is not mistaken
// for catalog-installed content.
func catalogSkillIsManagedOnDisk(root string, entry templates.CLISkillCatalogEntry) bool {
	stateID := entry.ID
	if !catalogSkillExistsOnDisk(root, stateID) {
		return false
	}
	if entry.OwnershipMarker == "" {
		return true
	}
	data, err := os.ReadFile(filepath.Join(root, ".agent-layer", "skills", stateID, "SKILL.md")) // #nosec G304 -- root is the project root and entry.ID is a validated catalog id.
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(entry.OwnershipMarker))
}

// detectInstructionEvidenceFromDisk returns true when Agent Layer-managed
// instruction or canonical memory files already exist. The function defaults
// to true when scans fail or the root is unset. An empty
// `.agent-layer/instructions/` directory with no memory files maps to false.
//
// The result controls whether the install-only instruction prompt is shown.
// Standard local instruction files, configured imports and memory files suppress
// repeated selection. Other local fragments and skills are configured separately.
func detectInstructionEvidenceFromDisk(root string) bool {
	if root == "" {
		return true
	}

	cfg, err := config.LoadConfigLenient(config.DefaultPaths(root).ConfigPath)
	if err == nil && len(cfg.Instructions.Imports) > 0 {
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
