package wizard

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
)

const (
	dispatchAgentCatalogID       = "dispatch-agent"
	legacyDispatchAgentCatalogID = "agent-dispatch"
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

// catalogSkillStateIDOnDisk returns the directory id that currently represents
// a catalog entry. Repositories installed before 0.16 keep the dispatch skill
// under its legacy id until the upgrade migration renames it. The wizard must
// preserve that state instead of creating the renamed directory alongside it.
func catalogSkillStateIDOnDisk(root string, entry CLISkillCatalogEntry) string {
	if entry.ID == dispatchAgentCatalogID && catalogSkillExistsOnDisk(root, legacyDispatchAgentCatalogID) {
		return legacyDispatchAgentCatalogID
	}
	return entry.ID
}

// catalogSkillIsManagedOnDisk reports whether the directory at a catalog
// entry's path is safe for the wizard to claim and remove. Entries without an
// ownership marker retain the legacy directory-presence behavior. Marked
// entries must contain their marker in SKILL.md so a user-authored same-name
// skill is not mistaken for catalog-installed content.
func catalogSkillIsManagedOnDisk(root string, entry CLISkillCatalogEntry) bool {
	stateID := catalogSkillStateIDOnDisk(root, entry)
	if !catalogSkillExistsOnDisk(root, stateID) {
		return false
	}
	if entry.OwnershipMarker == "" {
		return true
	}
	data, err := os.ReadFile(filepath.Join(root, ".agent-layer", "skills", stateID, "SKILL.md")) // #nosec G304 -- root is the project root and catalogSkillStateIDOnDisk returns a validated catalog id.
	if err != nil {
		return false
	}
	return bytes.Contains(data, []byte(entry.OwnershipMarker))
}

// detectAgentLayerEnabledFromDisk returns true when the workflow bundle appears
// present in the project — any embedded workflow-bundle skill directory,
// standard instruction file, managed memory template, or live memory file exists.
// The function defaults to true when scans fail or the root is unset; an empty
// `.agent-layer/skills/` directory with no managed bundle files maps to false.
//
// The result controls whether the install-only workflow-bundle prompt is shown.
// Existing bundle evidence suppresses the prompt because the wizard no longer
// performs workflow-bundle refreshes.
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
	for _, names := range [][]string{standardInstructionBasenames, legacyInstructionBasenames} {
		for _, name := range names {
			path := filepath.Join(root, ".agent-layer", "instructions", name)
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				return true
			}
		}
	}
	return false
}
