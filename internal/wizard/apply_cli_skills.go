package wizard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// cliSkillsCatalogTemplateRoot is the embedded directory holding catalog skill
// template trees. Each <id>/ subdirectory mirrors the structure that should
// land under .agent-layer/skills/<id>/.
const cliSkillsCatalogTemplateRoot = "skills-catalog"

// skillsChangeSet describes what the apply path needs to do to bring
// `.agent-layer/skills/` and `docs/agent-layer/` in line with the user's Q1/Q2
// answers. Changes are summarized at the directory level for both the apply
// step and the rewrite preview.
type skillsChangeSet struct {
	// catalogSkillsToAdd holds catalog ids that are selected but missing on disk.
	catalogSkillsToAdd []string
	// catalogSkillsToRepair holds selected catalog ids whose directory exists but
	// one or more embedded files are missing.
	catalogSkillsToRepair []string
	// catalogSkillsToRemove holds catalog ids that are deselected but exist on disk.
	catalogSkillsToRemove []string
	// workflowSkillsToRefresh holds embedded workflow skill directory names that
	// should be replaced because Q1 is yes.
	workflowSkillsToRefresh []string
	// memoryFilesToCreate holds missing docs/agent-layer/*.md relative paths to
	// create because Q1 is yes.
	memoryFilesToCreate []string
	// templateMemoryFilesToCreate holds missing .agent-layer/templates/docs/*.md
	// relative paths to create because Q1 is yes.
	templateMemoryFilesToCreate []string
	// managedInstructionFilesToRefresh holds bundled managed instruction files
	// that should be overwritten because Q1 is yes.
	managedInstructionFilesToRefresh []string
	// userInstructionFilesToCreate holds user-owned instruction files that should
	// be created only when missing because Q1 is yes.
	userInstructionFilesToCreate []string
}

// memoryFileBasenames is the canonical set of agent-managed memory files that
// the workflow-bundle step can create when missing. Other files under
// docs/agent-layer/ (e.g. user-added notes) are left alone.
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

	if choices.InstallWorkflowBundleTouched && choices.InstallWorkflowBundle {
		workflowIDs, err := embeddedWorkflowSkillIDs()
		if err != nil {
			return skillsChangeSet{}, err
		}
		workflowSkills := make([]string, 0, len(workflowIDs))
		for id := range workflowIDs {
			workflowSkills = append(workflowSkills, id)
		}
		out.workflowSkillsToRefresh = workflowSkills
		memoryAdds, err := listMissingMemoryFiles(root, filepath.Join(root, "docs", "agent-layer"))
		if err != nil {
			return skillsChangeSet{}, err
		}
		templateMemoryAdds, err := listMissingMemoryFiles(root, filepath.Join(root, ".agent-layer", "templates", "docs"))
		if err != nil {
			return skillsChangeSet{}, err
		}
		out.memoryFilesToCreate = memoryAdds
		out.templateMemoryFilesToCreate = templateMemoryAdds
		userInstructionAdds, err := listMissingUserOwnedInstructionFiles(root)
		if err != nil {
			return skillsChangeSet{}, err
		}
		out.userInstructionFilesToCreate = userInstructionAdds
		out.managedInstructionFilesToRefresh = managedInstructionFiles()
	}

	sort.Strings(out.catalogSkillsToAdd)
	sort.Strings(out.catalogSkillsToRepair)
	sort.Strings(out.catalogSkillsToRemove)
	sort.Strings(out.workflowSkillsToRefresh)
	sort.Strings(out.memoryFilesToCreate)
	sort.Strings(out.templateMemoryFilesToCreate)
	sort.Strings(out.managedInstructionFilesToRefresh)
	sort.Strings(out.userInstructionFilesToCreate)
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
	for _, id := range changes.workflowSkillsToRefresh {
		if err := copyTemplateDirReplace("skills/"+id, filepath.Join(root, ".agent-layer", "skills", id)); err != nil {
			return fmt.Errorf("refresh workflow skill %s: %w", id, err)
		}
	}
	if len(changes.memoryFilesToCreate) > 0 {
		if err := copyTemplateDirMissing("docs/agent-layer", filepath.Join(root, "docs", "agent-layer")); err != nil {
			return fmt.Errorf("create memory files: %w", err)
		}
	}
	if len(changes.templateMemoryFilesToCreate) > 0 {
		if err := copyTemplateDirMissing("docs/agent-layer", filepath.Join(root, ".agent-layer", "templates", "docs")); err != nil {
			return fmt.Errorf("create memory templates: %w", err)
		}
	}
	for _, rel := range changes.managedInstructionFilesToRefresh {
		name := filepath.Base(rel)
		if err := copyTemplateFile("instructions/"+name, filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("refresh instruction file %s: %w", rel, err)
		}
	}
	for _, rel := range changes.userInstructionFilesToCreate {
		name := filepath.Base(rel)
		if err := copyTemplateFileIfMissing("instructions/"+name, filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("create instruction file %s: %w", rel, err)
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

func copyTemplateDirReplace(templateRoot string, destRoot string) error {
	if err := os.RemoveAll(destRoot); err != nil {
		return err
	}
	wrote := false
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
		data, readErr := templates.Read(path)
		if readErr != nil {
			return readErr
		}
		destPath := filepath.Join(destRoot, rel)
		if mkErr := os.MkdirAll(filepath.Dir(destPath), 0o750); mkErr != nil {
			return mkErr
		}
		if writeErr := os.WriteFile(destPath, data, 0o600); writeErr != nil {
			return writeErr
		}
		wrote = true
		return nil
	})
	if err != nil {
		return err
	}
	if !wrote {
		return fmt.Errorf("template directory %q has no embedded files", templateRoot)
	}
	return nil
}

func copyTemplateFile(templatePath string, destPath string) error {
	data, err := templates.Read(templatePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o750); err != nil {
		return err
	}
	return os.WriteFile(destPath, data, 0o644) // #nosec G306 -- workflow instructions are non-secret project files.
}

func copyTemplateFileIfMissing(templatePath string, destPath string) error {
	if _, err := os.Stat(destPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return copyTemplateFile(templatePath, destPath)
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

// managedInstructionFiles returns managed bundled instruction file paths.
func managedInstructionFiles() []string {
	out := make([]string, 0, len(standardInstructionBasenames))
	for _, name := range standardInstructionBasenames {
		if isUserOwnedStandardInstructionFile(name) {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Join(".agent-layer", "instructions", name)))
	}
	return out
}

func listMissingUserOwnedInstructionFiles(root string) ([]string, error) {
	out := make([]string, 0, len(standardInstructionBasenames))
	for _, name := range standardInstructionBasenames {
		if !isUserOwnedStandardInstructionFile(name) {
			continue
		}
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

// buildSkillsPreview returns a directory-summary preview for the rewrite
// preview's Skills section. Returns empty string when no skill changes are
// scheduled.
func buildSkillsPreview(changes skillsChangeSet) string {
	lineCapacity := len(changes.catalogSkillsToAdd) +
		len(changes.catalogSkillsToRepair) +
		len(changes.catalogSkillsToRemove) +
		len(changes.workflowSkillsToRefresh) +
		len(changes.memoryFilesToCreate) +
		len(changes.templateMemoryFilesToCreate) +
		len(changes.managedInstructionFilesToRefresh) +
		len(changes.userInstructionFilesToCreate)
	lines := make([]string, 0, lineCapacity)
	for _, id := range changes.catalogSkillsToAdd {
		lines = append(lines, fmt.Sprintf("  + .agent-layer/skills/%s/", id))
	}
	for _, id := range changes.catalogSkillsToRepair {
		lines = append(lines, fmt.Sprintf("  + .agent-layer/skills/%s/  (missing catalog skill files)", id))
	}
	for _, id := range changes.catalogSkillsToRemove {
		lines = append(lines, fmt.Sprintf("  - .agent-layer/skills/%s/", id))
	}
	for _, name := range changes.workflowSkillsToRefresh {
		lines = append(lines, fmt.Sprintf("  ~ .agent-layer/skills/%s/  (workflow bundle refresh)", name))
	}
	for _, rel := range changes.memoryFilesToCreate {
		lines = append(lines, fmt.Sprintf("  + %s  (memory file)", rel))
	}
	for _, rel := range changes.templateMemoryFilesToCreate {
		lines = append(lines, fmt.Sprintf("  + %s  (memory template)", rel))
	}
	for _, rel := range changes.managedInstructionFilesToRefresh {
		lines = append(lines, fmt.Sprintf("  ~ %s  (managed instruction refresh)", rel))
	}
	for _, rel := range changes.userInstructionFilesToCreate {
		lines = append(lines, fmt.Sprintf("  + %s  (user-owned instruction seed)", rel))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(append([]string{"Skills changes (directory summary):"}, lines...), "\n")
}
