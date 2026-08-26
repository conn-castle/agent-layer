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
	if len(entry.Members) > 0 {
		for _, member := range entry.Members {
			if catalogSkillExistsOnDisk(root, member) {
				return true
			}
		}
		return false
	}
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

// detectInstructionEvidenceFromDisk returns true when Agent Layer-managed
// instruction or canonical memory files already exist. The function defaults
// to true when scans fail or the root is unset. An empty
// `.agent-layer/instructions/` directory with no memory files maps to false.
//
// The result controls whether the install-only instruction prompt is shown.
// Standard or legacy managed instruction files, live memory docs, and memory
// templates suppress the prompt because the wizard does not refresh them.
// User-authored extra instruction files do not count, so a repo that only
// has custom fragments can still seed 00_rules.md. Development skills on
// disk do not count; they are selected independently on the skills catalog
// screen.
func detectInstructionEvidenceFromDisk(root string) bool {
	if root == "" {
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
