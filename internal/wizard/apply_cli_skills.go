package wizard

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// cliSkillsCatalogTemplateRoot is the embedded directory holding catalog skill
// template trees. Each <id>/ subdirectory mirrors the structure that should
// land under .agent-layer/skills/<id>/.
const cliSkillsCatalogTemplateRoot = "skills-catalog"

// skillsChangeSet describes what the apply path needs to do to bring
// `.agent-layer/skills/` and `docs/agent-layer/` in line with the user's Q1/Q2
// answers. Adds and removes are summarized at the directory level for both
// the apply step and the rewrite preview.
type skillsChangeSet struct {
	// catalogSkillsToAdd holds catalog ids that are selected but missing on disk.
	catalogSkillsToAdd []string
	// catalogSkillsToRepair holds selected catalog ids whose directory exists but
	// one or more embedded files are missing.
	catalogSkillsToRepair []string
	// catalogSkillsToRemove holds catalog ids that are deselected but exist on disk.
	catalogSkillsToRemove []string
	// workflowSkillsToRemove holds non-catalog skill directory names to remove
	// because Q1 is no on an existing repo.
	workflowSkillsToRemove []string
	// workflowSkillsToAdd holds embedded workflow skill directory names that are
	// missing files and should be restored because Q1 is yes.
	workflowSkillsToAdd []string
	// memoryFilesToRemove holds docs/agent-layer/*.md relative paths to remove
	// because Q1 is no on an existing repo.
	memoryFilesToRemove []string
	// memoryFilesToAdd holds missing docs/agent-layer/*.md relative paths to
	// restore because Q1 is yes.
	memoryFilesToAdd []string
	// templateMemoryFilesToRemove holds .agent-layer/templates/docs/*.md relative
	// paths to remove because Q1 is no on an existing repo.
	templateMemoryFilesToRemove []string
	// templateMemoryFilesToAdd holds missing .agent-layer/templates/docs/*.md
	// relative paths to restore because Q1 is yes.
	templateMemoryFilesToAdd []string
	// instructionFilesToRemove holds .agent-layer/instructions/*.md relative paths
	// to remove because Q1 is no on an existing repo.
	instructionFilesToRemove []string
	// instructionFilesToAdd holds missing .agent-layer/instructions/*.md relative
	// paths to restore because Q1 is yes.
	instructionFilesToAdd []string
	// addInstructionPlaceholder is true when Q1=no should leave a minimal
	// zero-byte instruction placeholder behind.
	addInstructionPlaceholder bool
}

// memoryFileBasenames is the canonical set of agent-managed memory files that
// the Q1-prune step is allowed to remove. Other files under docs/agent-layer/
// (e.g. user-added notes) are left alone.
var memoryFileBasenames = []string{
	"ISSUES.md",
	"BACKLOG.md",
	"ROADMAP.md",
	"DECISIONS.md",
	"COMMANDS.md",
	"CONTEXT.md",
}

var standardInstructionBasenames = []string{
	"00_rules.md",
	"01_base.md",
	"02_memory.md",
	"03_tools.md",
	"04_conventions.md",
}

// computeSkillsChangeSet inspects the on-disk layout and the wizard's choices
// to derive the set of directory adds, directory removes, and memory file
// changes the apply path will perform. Order in each slice is sorted for
// deterministic preview output.
func computeSkillsChangeSet(root string, choices *Choices) (skillsChangeSet, error) {
	out := skillsChangeSet{}

	for _, entry := range choices.CLISkillsCatalog {
		exists := catalogSkillExistsOnDisk(root, entry.ID)
		selected := choices.EnabledCLISkills[entry.ID]
		missingFiles := false
		if selected && exists {
			var err error
			missingFiles, err = templateDirHasMissingFiles(
				cliSkillsCatalogTemplateRoot+"/"+entry.ID,
				filepath.Join(root, ".agent-layer", "skills", entry.ID),
			)
			if err != nil {
				return skillsChangeSet{}, err
			}
		}
		switch {
		case selected && !exists:
			out.catalogSkillsToAdd = append(out.catalogSkillsToAdd, entry.ID)
		case selected && missingFiles:
			out.catalogSkillsToRepair = append(out.catalogSkillsToRepair, entry.ID)
		case !selected && exists:
			out.catalogSkillsToRemove = append(out.catalogSkillsToRemove, entry.ID)
		}
	}

	if choices.EnableAgentLayerTouched && !choices.EnableAgentLayer {
		workflowIDs, err := embeddedWorkflowSkillIDs()
		if err != nil {
			return skillsChangeSet{}, err
		}
		out.workflowSkillsToRemove = listWorkflowSkillDirs(root, workflowIDs)
		memoryRemovals, err := listRemovableMemoryFiles(root)
		if err != nil {
			return skillsChangeSet{}, err
		}
		out.memoryFilesToRemove = memoryRemovals
		out.templateMemoryFilesToRemove = listExistingTemplateMemoryFiles(root)
		instructionRemovals, err := listRemovableInstructionFiles(root)
		if err != nil {
			return skillsChangeSet{}, err
		}
		out.instructionFilesToRemove = instructionRemovals
		out.addInstructionPlaceholder = !instructionPlaceholderExists(root)
	} else if choices.EnableAgentLayerTouched && choices.EnableAgentLayer {
		workflowIDs, err := embeddedWorkflowSkillIDs()
		if err != nil {
			return skillsChangeSet{}, err
		}
		workflowAdds, err := listMissingWorkflowSkillDirs(root, workflowIDs)
		if err != nil {
			return skillsChangeSet{}, err
		}
		out.workflowSkillsToAdd = workflowAdds
		memoryAdds, err := listMissingMemoryFiles(root, filepath.Join(root, "docs", "agent-layer"))
		if err != nil {
			return skillsChangeSet{}, err
		}
		templateMemoryAdds, err := listMissingMemoryFiles(root, filepath.Join(root, ".agent-layer", "templates", "docs"))
		if err != nil {
			return skillsChangeSet{}, err
		}
		out.memoryFilesToAdd = memoryAdds
		out.templateMemoryFilesToAdd = templateMemoryAdds
		instructionAdds, err := listMissingInstructionFiles(root)
		if err != nil {
			return skillsChangeSet{}, err
		}
		out.instructionFilesToAdd = instructionAdds
	}

	sort.Strings(out.catalogSkillsToAdd)
	sort.Strings(out.catalogSkillsToRepair)
	sort.Strings(out.catalogSkillsToRemove)
	sort.Strings(out.workflowSkillsToRemove)
	sort.Strings(out.workflowSkillsToAdd)
	sort.Strings(out.memoryFilesToRemove)
	sort.Strings(out.memoryFilesToAdd)
	sort.Strings(out.templateMemoryFilesToRemove)
	sort.Strings(out.templateMemoryFilesToAdd)
	sort.Strings(out.instructionFilesToRemove)
	sort.Strings(out.instructionFilesToAdd)
	return out, nil
}

// applySkillsChanges materializes the change set on disk. Each catalog skill
// addition is copied from the embedded skills-catalog/<id>/ tree; deletions are
// recursive removes scoped to the targeted directory.
func applySkillsChanges(root string, changes skillsChangeSet) error {
	for _, id := range changes.catalogSkillsToAdd {
		if err := copyCatalogSkillToDisk(root, id); err != nil {
			return fmt.Errorf("add catalog skill %s: %w", id, err)
		}
	}
	for _, id := range changes.catalogSkillsToRepair {
		if err := copyCatalogSkillMissingFiles(root, id); err != nil {
			return fmt.Errorf("repair catalog skill %s: %w", id, err)
		}
	}
	for _, id := range changes.catalogSkillsToRemove {
		dir := filepath.Join(root, ".agent-layer", "skills", id)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove catalog skill %s: %w", id, err)
		}
	}
	for _, name := range changes.workflowSkillsToRemove {
		dir := filepath.Join(root, ".agent-layer", "skills", name)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove workflow skill %s: %w", name, err)
		}
	}
	for _, id := range changes.workflowSkillsToAdd {
		if err := copyTemplateDirMissing("skills/"+id, filepath.Join(root, ".agent-layer", "skills", id)); err != nil {
			return fmt.Errorf("restore workflow skill %s: %w", id, err)
		}
	}
	for _, rel := range changes.memoryFilesToRemove {
		path := filepath.Join(root, rel)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove memory file %s: %w", rel, err)
		}
	}
	if len(changes.memoryFilesToAdd) > 0 {
		if err := copyTemplateDirMissing("docs/agent-layer", filepath.Join(root, "docs", "agent-layer")); err != nil {
			return fmt.Errorf("restore memory files: %w", err)
		}
	}
	for _, rel := range changes.templateMemoryFilesToRemove {
		path := filepath.Join(root, rel)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove memory template %s: %w", rel, err)
		}
	}
	if len(changes.templateMemoryFilesToAdd) > 0 {
		if err := copyTemplateDirMissing("docs/agent-layer", filepath.Join(root, ".agent-layer", "templates", "docs")); err != nil {
			return fmt.Errorf("restore memory templates: %w", err)
		}
	}
	for _, rel := range changes.instructionFilesToRemove {
		path := filepath.Join(root, rel)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove instruction file %s: %w", rel, err)
		}
	}
	if len(changes.instructionFilesToAdd) > 0 {
		if err := copyTemplateDirMissing("instructions", filepath.Join(root, ".agent-layer", "instructions")); err != nil {
			return fmt.Errorf("restore instruction files: %w", err)
		}
	}
	if changes.addInstructionPlaceholder {
		path := filepath.Join(root, ".agent-layer", "instructions", install.MinimalLayoutPlaceholderFile)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return fmt.Errorf("create instruction placeholder dir: %w", err)
		}
		if err := fsutil.WriteFileAtomic(path, nil, 0o600); err != nil {
			return fmt.Errorf("write instruction placeholder: %w", err)
		}
	}
	return nil
}

// copyCatalogSkillToDisk copies the embedded catalog skill directory for id to
// .agent-layer/skills/<id>/. Errors when the embedded directory is missing.
func copyCatalogSkillToDisk(root string, id string) error {
	if !isSafeCLISkillCatalogID(id) {
		return fmt.Errorf("invalid catalog skill id %q", id)
	}
	templateRoot := cliSkillsCatalogTemplateRoot + "/" + id
	destRoot := filepath.Join(root, ".agent-layer", "skills", id)
	wrote := false
	err := templates.Walk(templateRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, templateRoot+"/")
		data, readErr := templates.Read(path)
		if readErr != nil {
			return readErr
		}
		destPath := filepath.Join(destRoot, rel)
		if mkErr := os.MkdirAll(filepath.Dir(destPath), 0o750); mkErr != nil {
			return mkErr
		}
		if writeErr := fsutil.WriteFileAtomic(destPath, data, 0o600); writeErr != nil {
			return writeErr
		}
		wrote = true
		return nil
	})
	if err != nil {
		return err
	}
	if !wrote {
		return fmt.Errorf("catalog skill %q has no embedded files", id)
	}
	return nil
}

// copyCatalogSkillMissingFiles copies only absent embedded files for id into
// .agent-layer/skills/<id>/, preserving any existing catalog skill files.
func copyCatalogSkillMissingFiles(root string, id string) error {
	if !isSafeCLISkillCatalogID(id) {
		return fmt.Errorf("invalid catalog skill id %q", id)
	}
	return copyTemplateDirMissingWithMode(
		cliSkillsCatalogTemplateRoot+"/"+id,
		filepath.Join(root, ".agent-layer", "skills", id),
		0o600,
	)
}

// copyTemplateDirMissing copies missing files from an embedded template
// directory to destRoot without overwriting existing files.
func copyTemplateDirMissing(templateRoot string, destRoot string) error {
	return copyTemplateDirMissingWithMode(templateRoot, destRoot, 0o644)
}

// copyTemplateDirMissingWithMode copies missing embedded files using fileMode
// for any newly created destination file.
func copyTemplateDirMissingWithMode(templateRoot string, destRoot string, fileMode fs.FileMode) error {
	wroteOrSkipped := false
	err := templates.Walk(templateRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, templateRoot+"/")
		if rel == path {
			return fmt.Errorf("unexpected template path %s", path)
		}
		destPath := filepath.Join(destRoot, rel)
		if _, err := os.Stat(destPath); err == nil {
			wroteOrSkipped = true
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		data, readErr := templates.Read(path)
		if readErr != nil {
			return readErr
		}
		if mkErr := os.MkdirAll(filepath.Dir(destPath), 0o750); mkErr != nil {
			return mkErr
		}
		if writeErr := fsutil.WriteFileAtomic(destPath, data, fileMode); writeErr != nil {
			return writeErr
		}
		wroteOrSkipped = true
		return nil
	})
	if err != nil {
		return err
	}
	if !wroteOrSkipped {
		return fmt.Errorf("template directory %q has no embedded files", templateRoot)
	}
	return nil
}

// embeddedWorkflowSkillIDs returns the workflow-bundle skill ids from the
// embedded skills/ template tree.
func embeddedWorkflowSkillIDs() (map[string]struct{}, error) {
	ids := make(map[string]struct{})
	err := templates.Walk("skills", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		rel := strings.TrimPrefix(path, "skills/")
		id := strings.Split(rel, "/")[0]
		if id != "" && id != rel {
			ids[id] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// listWorkflowSkillDirs returns sorted directory names under .agent-layer/skills/
// that belong to the embedded workflow bundle. Catalog and user-created skill
// directories are left alone.
func listWorkflowSkillDirs(root string, workflowIDs map[string]struct{}) []string {
	entries, err := os.ReadDir(filepath.Join(root, ".agent-layer", "skills"))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, isWorkflow := workflowIDs[entry.Name()]; !isWorkflow {
			continue
		}
		out = append(out, entry.Name())
	}
	return out
}

// listMissingWorkflowSkillDirs returns workflow-bundle skill dirs that are
// missing one or more embedded files at their destination.
func listMissingWorkflowSkillDirs(root string, workflowIDs map[string]struct{}) ([]string, error) {
	ids := make([]string, 0, len(workflowIDs))
	for id := range workflowIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		missing, err := templateDirHasMissingFiles("skills/"+id, filepath.Join(root, ".agent-layer", "skills", id))
		if err != nil {
			return nil, err
		}
		if missing {
			out = append(out, id)
		}
	}
	return out, nil
}

// templateDirHasMissingFiles reports whether any embedded file in templateRoot
// is absent under destRoot.
func templateDirHasMissingFiles(templateRoot string, destRoot string) (bool, error) {
	found := false
	missing := false
	err := templates.Walk(templateRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		found = true
		rel := strings.TrimPrefix(path, templateRoot+"/")
		if rel == path {
			return fmt.Errorf("unexpected template path %s", path)
		}
		destPath := filepath.Join(destRoot, rel)
		if _, err := os.Stat(destPath); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("template directory %q has no embedded files", templateRoot)
	}
	return missing, nil
}

// listRemovableMemoryFiles returns canonical live memory files that still match
// their embedded templates exactly and can be pruned without deleting user
// entries.
func listRemovableMemoryFiles(root string) ([]string, error) {
	out := make([]string, 0, len(memoryFileBasenames))
	for _, name := range memoryFileBasenames {
		path := filepath.Join(root, "docs", "agent-layer", name)
		data, err := os.ReadFile(path) // #nosec G304 -- path is root/docs/agent-layer/<canonical memory file>, where the basename comes from memoryFileBasenames.
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		template, err := templates.Read(filepath.ToSlash(filepath.Join("docs", "agent-layer", name)))
		if err != nil {
			return nil, err
		}
		if bytes.Equal(data, template) {
			out = append(out, filepath.ToSlash(filepath.Join("docs", "agent-layer", name)))
		}
	}
	return out, nil
}

// listMissingMemoryFiles returns canonical memory file paths missing under
// destRoot, expressed relative to root for preview and apply summaries.
func listMissingMemoryFiles(root string, destRoot string) ([]string, error) {
	out := make([]string, 0, len(memoryFileBasenames))
	relRoot, err := filepath.Rel(root, destRoot)
	if err != nil {
		return nil, err
	}
	for _, name := range memoryFileBasenames {
		path := filepath.Join(destRoot, name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if errors.Is(err, os.ErrNotExist) {
			out = append(out, filepath.ToSlash(filepath.Join(relRoot, name)))
		} else {
			return nil, err
		}
	}
	return out, nil
}

// listExistingTemplateMemoryFiles returns .agent-layer/templates/docs/<basename>
// for each canonical memory template file that exists on disk.
func listExistingTemplateMemoryFiles(root string) []string {
	out := make([]string, 0, len(memoryFileBasenames))
	for _, name := range memoryFileBasenames {
		path := filepath.Join(root, ".agent-layer", "templates", "docs", name)
		if _, err := os.Stat(path); err == nil {
			out = append(out, filepath.ToSlash(filepath.Join(".agent-layer", "templates", "docs", name)))
		}
	}
	return out
}

// listMissingInstructionFiles returns standard instruction file paths missing
// from .agent-layer/instructions/.
func listMissingInstructionFiles(root string) ([]string, error) {
	out := make([]string, 0, len(standardInstructionBasenames))
	for _, name := range standardInstructionBasenames {
		path := filepath.Join(root, ".agent-layer", "instructions", name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if errors.Is(err, os.ErrNotExist) {
			out = append(out, filepath.ToSlash(filepath.Join(".agent-layer", "instructions", name)))
		} else {
			return nil, err
		}
	}
	return out, nil
}

// listRemovableInstructionFiles returns standard instruction files that still
// match their embedded templates exactly and can be pruned without deleting
// local instruction edits.
func listRemovableInstructionFiles(root string) ([]string, error) {
	out := make([]string, 0, len(standardInstructionBasenames))
	for _, name := range standardInstructionBasenames {
		path := filepath.Join(root, ".agent-layer", "instructions", name)
		data, err := os.ReadFile(path) // #nosec G304 -- path is root/.agent-layer/instructions/<standard instruction file>, where the basename comes from standardInstructionBasenames.
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		template, err := templates.Read(filepath.ToSlash(filepath.Join("instructions", name)))
		if err != nil {
			return nil, err
		}
		if bytes.Equal(data, template) {
			out = append(out, filepath.ToSlash(filepath.Join(".agent-layer", "instructions", name)))
		}
	}
	return out, nil
}

func instructionPlaceholderExists(root string) bool {
	path := filepath.Join(root, ".agent-layer", "instructions", install.MinimalLayoutPlaceholderFile)
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// buildSkillsPreview returns a directory-summary preview for the rewrite
// preview's Skills section. Returns empty string when no skill changes are
// scheduled.
func buildSkillsPreview(changes skillsChangeSet) string {
	var lines []string
	for _, id := range changes.catalogSkillsToAdd {
		lines = append(lines, fmt.Sprintf("  + .agent-layer/skills/%s/", id))
	}
	for _, id := range changes.catalogSkillsToRepair {
		lines = append(lines, fmt.Sprintf("  + .agent-layer/skills/%s/  (missing catalog skill files)", id))
	}
	for _, id := range changes.catalogSkillsToRemove {
		lines = append(lines, fmt.Sprintf("  - .agent-layer/skills/%s/", id))
	}
	for _, name := range changes.workflowSkillsToRemove {
		lines = append(lines, fmt.Sprintf("  - .agent-layer/skills/%s/  (workflow bundle)", name))
	}
	for _, name := range changes.workflowSkillsToAdd {
		lines = append(lines, fmt.Sprintf("  + .agent-layer/skills/%s/  (workflow bundle)", name))
	}
	for _, rel := range changes.memoryFilesToRemove {
		lines = append(lines, fmt.Sprintf("  - %s  (memory file)", rel))
	}
	for _, rel := range changes.memoryFilesToAdd {
		lines = append(lines, fmt.Sprintf("  + %s  (memory file)", rel))
	}
	for _, rel := range changes.templateMemoryFilesToRemove {
		lines = append(lines, fmt.Sprintf("  - %s  (memory template)", rel))
	}
	for _, rel := range changes.templateMemoryFilesToAdd {
		lines = append(lines, fmt.Sprintf("  + %s  (memory template)", rel))
	}
	for _, rel := range changes.instructionFilesToRemove {
		lines = append(lines, fmt.Sprintf("  - %s  (instruction file)", rel))
	}
	for _, rel := range changes.instructionFilesToAdd {
		lines = append(lines, fmt.Sprintf("  + %s  (instruction file)", rel))
	}
	if changes.addInstructionPlaceholder {
		lines = append(lines, fmt.Sprintf("  + .agent-layer/instructions/%s  (minimal placeholder)", install.MinimalLayoutPlaceholderFile))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(append([]string{"Skills changes (directory summary):"}, lines...), "\n")
}
