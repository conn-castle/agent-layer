package wizard

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skillimport"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// cliSkillsCatalogTemplateRoot is the embedded directory holding catalog skill
// template trees. Each <id>/ subdirectory mirrors the structure that should
// land under .agent-layer/skills/<id>/.
const cliSkillsCatalogTemplateRoot = "skills-catalog"
const memoryInstructionName = "memory.md"

// skillsChangeSet describes what the apply path needs to do to bring
// `.agent-layer/skills/` and `docs/agent-layer/` in line with the user's wizard
// answers. Changes are summarized at the directory level for both the apply
// step and the rewrite preview.
type skillsChangeSet struct {
	importSelectors []string
	adoptLegacy     []string
	removeSelectors []string
	importPreview   []string
	// catalogSkillsToAdd holds catalog ids that are selected but missing on disk.
	catalogSkillsToAdd []string
	// catalogSkillsToRepair holds selected catalog ids whose directory exists but
	// one or more embedded files are missing.
	catalogSkillsToRepair []string
	// catalogSkillsToRemove holds catalog directory ids that are deselected but
	// exist on disk. A directory id may be a legacy pre-migration id.
	catalogSkillsToRemove []string
	// memoryFilesToCreate holds missing docs/agent-layer/*.md relative paths to
	// create because the instruction set includes memory.
	memoryFilesToCreate []string
	// templateMemoryFilesToCreate holds missing .agent-layer/templates/docs/*.md
	// relative paths to create because the selected instruction set includes memory.
	templateMemoryFilesToCreate []string
	// instructionImports holds exact external file selections for source-only apply.
	instructionImports []string
}

// memoryFileBasenames is the canonical set of agent-managed memory files that
// the rules-and-memory instruction choice can create when missing. Other files
// under docs/agent-layer/ (e.g. user-added notes) are left alone.
var memoryFileBasenames = []string{
	"ISSUES.md",
	"BACKLOG.md",
	"DECISIONS.md",
	"COMMANDS.md",
	"CONTEXT.md",
}

// standardInstructionBasenames identifies the fixed pre-import adoption slots.
var standardInstructionBasenames = []string{
	"00_rules.md",
	"01_memory.md",
}

// legacyInstructionBasenames are instruction files shipped by releases before
// the 0.16.0 consolidation. They are no longer seeded, but their presence still
// counts as instruction evidence so a repo that has not run `al upgrade`
// yet is not re-prompted to seed instruction files it already has.
var legacyInstructionBasenames = []string{
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
	var out skillsChangeSet

	for _, entry := range choices.CLISkillsCatalog {
		selected := choices.EnabledCLISkills[entry.ID]
		if entry.Repository != "" {
			if err := appendRemoteCatalogChanges(root, entry, selected, choices.InitialCLISkills, &out); err != nil {
				return skillsChangeSet{}, err
			}
			continue
		}
		stateID := catalogSkillStateIDOnDisk(root, entry)
		exists := catalogSkillExistsOnDisk(root, stateID)
		managed := catalogSkillIsManagedOnDisk(root, entry)
		missingFiles := false
		if selected && exists && !managed {
			return skillsChangeSet{}, fmt.Errorf(
				"cannot install catalog skill %s: .agent-layer/skills/%s/ already exists and is not catalog-managed; remove or rename it first",
				entry.ID,
				stateID,
			)
		}
		if selected && managed && stateID == entry.ID {
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
		case selected && managed && missingFiles:
			out.catalogSkillsToRepair = append(out.catalogSkillsToRepair, entry.ID)
		case !selected && managed:
			out.catalogSkillsToRemove = append(out.catalogSkillsToRemove, stateID)
		}
	}

	// Removing a selector reconciles remaining members of that same default
	// block. Name missing/conflicted siblings before the user approves the edit.
	if len(out.removeSelectors) > 0 {
		removed := map[string]bool{}
		for _, selector := range out.removeSelectors {
			removed[selector] = true
		}
		catalog, err := templates.LoadCLISkillCatalog()
		if err != nil {
			return skillsChangeSet{}, err
		}
		for _, entry := range catalog {
			if entry.Repository != templates.GeneralSkillsRepository {
				continue
			}
			members, err := skillimport.CatalogState(root, entry)
			if err != nil {
				return skillsChangeSet{}, err
			}
			for _, member := range members {
				if member.DefaultExact && !removed[member.Selector] && (!member.Imported || member.Problem != "") {
					note := "Warning: removal also reconciles configured sibling " + member.Selector + " in the same default import block; it may materialize or block the removal."
					if member.Problem != "" {
						note += " " + member.Problem
					}
					if member.Legacy {
						note += " Preserve the legacy tree; reconcile selector coverage with al skills commands before wizard legacy adoption."
					}
					out.importPreview = append(out.importPreview, note)
				}
			}
		}
	}

	if choices.InstructionSetTouched {
		if err := appendInstructionChanges(root, choices.InstructionSet, &out); err != nil {
			return skillsChangeSet{}, err
		}
	}

	sort.Strings(out.catalogSkillsToAdd)
	sort.Strings(out.catalogSkillsToRepair)
	sort.Strings(out.catalogSkillsToRemove)
	sort.Strings(out.memoryFilesToCreate)
	sort.Strings(out.templateMemoryFilesToCreate)
	sort.Strings(out.instructionImports)
	return out, nil
}

func appendRemoteCatalogChanges(root string, entry templates.CLISkillCatalogEntry, selected bool, initial map[string]bool, out *skillsChangeSet) error {
	members, err := skillimport.CatalogState(root, entry)
	if err != nil {
		return err
	}
	wasSelected, initialized := initial[entry.ID]
	unchanged := initialized && wasSelected == selected
	// A conflicted bundle row is a note on an unchanged run, including its
	// otherwise valid legacy siblings. Do not request a partial conversion.
	preserveRow := false
	if unchanged {
		for _, member := range members {
			preserveRow = preserveRow || member.Problem != "" || (member.Legacy && (member.Configured || member.AddBlocked != ""))
		}
	}
	for _, member := range members {
		if member.Problem != "" {
			if unchanged {
				out.importPreview = append(out.importPreview, "Catalog note: "+member.Name+": "+member.Problem+" (wizard leaves it unchanged)")
				continue
			}
			return fmt.Errorf("catalog %s: %s", member.Name, member.Problem)
		}
		if selected {
			if preserveRow && member.Legacy {
				out.importPreview = append(out.importPreview, "Catalog note: preserve legacy "+member.Name+"; resolve this row's catalog conflicts with al skills commands before adoption (wizard leaves it unchanged)")
				continue
			}
			if member.Configured {
				if member.Legacy {
					return fmt.Errorf("catalog %s has configured import coverage and a legacy local copy; resolve with al skills commands before adoption", member.Name)
				}
				if !member.Imported {
					out.importPreview = append(out.importPreview, "Missing configured import "+member.Selector+"; use al skills pull/status to repair (wizard leaves it unchanged)")
				}
				if !member.DefaultExact {
					out.importPreview = append(out.importPreview, "Manually managed import "+member.Selector+"; existing ref/policy/selectors preserved")
				}
				continue
			}
			if unchanged && !member.Legacy {
				out.importPreview = append(out.importPreview, fmt.Sprintf("Missing bundle member %s; add explicitly with al skills add %s %s (wizard leaves it unchanged)", member.Name, entry.Repository, member.Selector))
				if member.AddBlocked != "" {
					out.importPreview = append(out.importPreview, "Catalog note: "+member.AddBlocked)
				}
				continue
			}
			if member.AddBlocked != "" {
				return fmt.Errorf("catalog %s: %s", member.Name, member.AddBlocked)
			}
			out.importSelectors = append(out.importSelectors, member.Selector)
			if member.Legacy {
				out.adoptLegacy = append(out.adoptLegacy, member.Selector)
				out.importPreview = append(out.importPreview, "Adopt legacy .agent-layer/skills/"+member.Name+" into .agent-layer/skills-imported/"+member.Name+" from "+entry.Repository+" path "+member.Selector+" (network required); preserve the complete local tree, including older defaults, as modified when it differs from fetched upstream; retire the local slot atomically")
			} else {
				out.importPreview = append(out.importPreview, "Import missing "+member.Selector+" from "+entry.Repository+" (network required)")
			}
		} else {
			if member.Legacy {
				out.importPreview = append(out.importPreview, "Preserve legacy local "+member.Name+" (deselection never deletes it)")
			}
			if member.DefaultExact {
				out.removeSelectors = append(out.removeSelectors, member.Selector)
				out.importPreview = append(out.importPreview, "Remove exact import selector "+member.Selector+"; local modifications block the whole removal; remaining selectors may require a repository fetch")
			} else if member.Configured {
				out.importPreview = append(out.importPreview, "Preserve manually managed "+member.Selector+"; change it through al skills commands")
			}
		}
	}
	if selected && entry.ID == "development-skills" {
		out.importPreview = append(out.importPreview, "Prerequisites: implement, ship-pr, and auto-skill-loop require retained dispatch-agent and configured named provider targets (implementer, plan_reviewers, code_reviewer, pr_worker; loop additionally operator, planner, rote_worker), plus the dispatch MCP start/inspect/wait/output/continue/cancel contract. This warning does not enforce co-selection.")
	}
	return nil
}

func appendInstructionChanges(root string, set InstructionSet, out *skillsChangeSet) error {
	switch set {
	case InstructionSetNone, "":
		return nil
	case InstructionSetRules:
		return appendMissingInstructionFiles(root, []string{"rules.md"}, out)
	case InstructionSetRulesAndMemory:
		if err := appendMissingInstructionFiles(root, []string{"rules.md", memoryInstructionName}, out); err != nil {
			return err
		}
		memoryAdds, err := listMissingMemoryFiles(root, filepath.Join(root, "docs", "agent-layer"))
		if err != nil {
			return err
		}
		templateMemoryAdds, err := listMissingMemoryFiles(root, filepath.Join(root, ".agent-layer", "templates", "docs"))
		if err != nil {
			return err
		}
		out.memoryFilesToCreate = memoryAdds
		out.templateMemoryFilesToCreate = templateMemoryAdds
		return nil
	default:
		return fmt.Errorf(messages.WizardUnknownInstructionSetFmt, set)
	}
}

func appendMissingInstructionFiles(root string, names []string, out *skillsChangeSet) error {
	raw, err := os.ReadFile(config.DefaultPaths(root).ConfigPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	preview, err := config.MigrateInstructionOrderFS(os.DirFS(root), root, string(raw))
	if err != nil {
		return err
	}
	cfg, err := config.ParseConfigLenient([]byte(preview), "instruction preview")
	if err != nil {
		return err
	}
	for _, name := range names {
		configured := false
		for _, imp := range cfg.Instructions.Imports {
			configured = configured || filepath.Base(imp.Selectors[0]) == name
		}
		if configured {
			continue
		}
		legacy := skilljournal.LegacyInstruction(name)
		legacyPath := filepath.Join(config.DefaultPaths(root).InstructionsDir, legacy)
		if info, err := os.Lstat(legacyPath); err == nil {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("instruction %s exists but is not a regular file; use an unlinked source before adoption", legacyPath)
			}
		} else if errors.Is(err, os.ErrNotExist) {
			legacy = ""
		} else {
			return err
		}
		opts := instructionAddOptions(name)
		preview, err = config.AddInstructionImport(preview, config.SkillImport{Repository: opts.Repository, Selectors: opts.Selectors}, opts.Order, legacy)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate instruction order") {
				return fmt.Errorf("%w; choose precedence explicitly with al instructions add --order N or edit instruction orders in .agent-layer/config.toml", err)
			}
			return err
		}
		out.instructionImports = append(out.instructionImports, filepath.ToSlash(filepath.Join(".agent-layer", "instructions-imported", name)))
		out.importPreview = append(out.importPreview, "Import/adopt "+name+" from "+templates.GeneralSkillsRepository+" instructions/"+name+" (network required); preserve legacy bytes and order, retire the old slot atomically")
	}

	return nil
}

func instructionAddOptions(name string) skillimport.AddOptions {
	order := 0
	if name == memoryInstructionName {
		order = 10
	}
	return skillimport.AddOptions{Repository: templates.GeneralSkillsRepository, Selectors: []string{"instructions/" + name}, Order: &order, AdoptLegacy: true}
}

// applySkillsChanges materializes the change set on disk. Each catalog skill
// addition is copied from its embedded template tree; deletions are recursive
// removes scoped to the targeted directory.
func applySkillsChanges(root string, changes skillsChangeSet) (err error) {
	var committedInstructions []string
	defer func() {
		if err != nil && len(committedInstructions) > 0 {
			err = fmt.Errorf("instruction imports committed (%s); projection was not completed; run al sync: %w", strings.Join(committedInstructions, ", "), err)
		}
	}()
	for _, id := range changes.catalogSkillsToAdd {
		if err := copySkillDirToDisk(root, cliSkillsCatalogTemplateRoot+"/"+id, id); err != nil {
			return fmt.Errorf("add catalog skill %s: %w", id, err)
		}
	}
	for _, id := range changes.catalogSkillsToRepair {
		if err := copySkillDirMissingFiles(root, cliSkillsCatalogTemplateRoot+"/"+id, id); err != nil {
			return fmt.Errorf("repair catalog skill %s: %w", id, err)
		}
	}
	for _, id := range changes.catalogSkillsToRemove {
		dir := filepath.Join(root, ".agent-layer", "skills", id)
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("remove catalog skill %s: %w", id, err)
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
	for _, rel := range changes.instructionImports {
		name := filepath.Base(rel)
		report, err := skillimport.NewInstructionsSourceOnly(root).Add(context.Background(), instructionAddOptions(name))
		if err != nil {
			return fmt.Errorf("instruction import failed for %s: %w%s", name, err, catalogFailureReport(report, "instruction import"))
		}
		if report.Failed() {
			return fmt.Errorf("instruction import failed for %s: %s", name, report.Render("instruction import"))
		}
		committedInstructions = append(committedInstructions, name)
	}

	if len(changes.importSelectors) > 0 {
		report, err := skillimport.NewSourceOnly(root).InstallCatalog(context.Background(), changes.importSelectors, changes.adoptLegacy)
		if err != nil {
			return fmt.Errorf("catalog installation failed; prior wizard config/env writes may already be applied; import source state remains coherent: %w%s", err, catalogFailureReport(report, "catalog installation"))
		}
	}
	if len(changes.removeSelectors) > 0 {
		report, err := skillimport.NewSourceOnly(root).RemoveCatalogSelectors(context.Background(), changes.removeSelectors)
		if err != nil {
			if len(changes.importSelectors) > 0 {
				return fmt.Errorf("catalog installation committed; selector removal was not applied; prior wizard config/env writes remain: %w%s", err, catalogFailureReport(report, "catalog removal"))
			}
			return fmt.Errorf("catalog removal failed without import changes; prior wizard config/env writes may already be applied: %w%s", err, catalogFailureReport(report, "catalog removal"))
		}
	}
	return nil
}

// catalogFailureReport omits an empty report when preflight failed before any
// scoped outcome existed; rendering it would claim the failed operation succeeded.
func catalogFailureReport(report *skillimport.Report, operation string) string {
	if report == nil || (len(report.Sources) == 0 && len(report.Skills) == 0) {
		return ""
	}
	return "\n" + report.Render(operation)
}

// copySkillDirToDisk copies the embedded template directory for destID to
// .agent-layer/skills/<destID>/. Errors when the embedded directory is missing.
func copySkillDirToDisk(root string, templateRoot string, destID string) error {
	if !templates.IsSafeCLISkillCatalogID(destID) {
		return fmt.Errorf("invalid catalog skill id %q", destID)
	}
	destRoot := filepath.Join(root, ".agent-layer", "skills", destID)
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
		return fmt.Errorf("catalog skill %q has no embedded files", destID)
	}
	return nil
}

// copySkillDirMissingFiles copies only absent embedded files for destID into
// .agent-layer/skills/<destID>/, preserving any existing skill files.
func copySkillDirMissingFiles(root string, templateRoot string, destID string) error {
	if !templates.IsSafeCLISkillCatalogID(destID) {
		return fmt.Errorf("invalid catalog skill id %q", destID)
	}
	return copyTemplateDirMissingWithMode(
		templateRoot,
		filepath.Join(root, ".agent-layer", "skills", destID),
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
		exists, statErr := regularFileExists(destPath)
		if statErr != nil {
			return statErr
		}
		if exists {
			wroteOrSkipped = true
			return nil
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

func regularFileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s exists but is not a regular file", path)
	}
	return true, nil
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
		exists, statErr := regularFileExists(destPath)
		if statErr != nil {
			return statErr
		}
		if exists {
			return nil
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
		exists, err := regularFileExists(path)
		if err != nil {
			return nil, err
		}
		if exists {
			continue
		}
		out = append(out, filepath.ToSlash(filepath.Join(relRoot, name)))
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
		len(changes.memoryFilesToCreate) +
		len(changes.templateMemoryFilesToCreate) +
		len(changes.instructionImports)
	lines := make([]string, 0, lineCapacity)
	lines = append(lines, changes.importPreview...)
	for _, id := range changes.catalogSkillsToAdd {
		lines = append(lines, fmt.Sprintf("  + .agent-layer/skills/%s/", id))
	}
	for _, id := range changes.catalogSkillsToRepair {
		lines = append(lines, fmt.Sprintf("  + .agent-layer/skills/%s/  (missing catalog skill files)", id))
	}
	for _, id := range changes.catalogSkillsToRemove {
		lines = append(lines, fmt.Sprintf("  - .agent-layer/skills/%s/", id))
	}
	for _, rel := range changes.memoryFilesToCreate {
		lines = append(lines, fmt.Sprintf("  + %s  (memory file)", rel))
	}
	for _, rel := range changes.templateMemoryFilesToCreate {
		lines = append(lines, fmt.Sprintf("  + %s  (memory template)", rel))
	}
	for _, rel := range changes.instructionImports {
		lines = append(lines, fmt.Sprintf("  + %s  (Git instruction import)", rel))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(append([]string{"Skills changes (directory summary):"}, lines...), "\n")
}
