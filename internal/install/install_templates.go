package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// templateGitignoreBlock is the template name for the gitignore managed block.
const templateGitignoreBlock = "gitignore.block"

// managedTemplateFiles lists template-managed files under .agent-layer.
// These files are considered part of the upgradeable template surface area:
// they appear in `al upgrade plan`, are eligible for overwrite prompts, and are
// captured in managed baseline evidence.
func (inst templateManager) managedTemplateFiles() []templateFile {
	root := inst.root
	return []templateFile{
		{filepath.Join(root, ".agent-layer", commandsAllowName), commandsAllowName, 0o644},
		{filepath.Join(root, ".agent-layer", templateGitignoreBlock), templateGitignoreBlock, 0o644},
	}
}

// userOwnedSeedFiles lists user-owned files under .agent-layer that are required
// for Agent Layer to operate, but should never be overwritten during init or
// upgrade flows. They are seeded only when missing.
func (inst templateManager) userOwnedSeedFiles() []templateFile {
	root := inst.root
	return []templateFile{
		{filepath.Join(root, ".agent-layer", configFileName), configFileName, 0o644},
		{filepath.Join(root, ".agent-layer", ".env"), "env", 0o600},
	}
}

// agentOnlyFiles lists agent-owned files under .agent-layer that are safe to
// overwrite unconditionally and should not be surfaced as upgrade actions.
func (inst templateManager) agentOnlyFiles() []templateFile {
	root := inst.root
	return []templateFile{
		{filepath.Join(root, ".agent-layer", ".gitignore"), "agent-layer.gitignore", 0o644},
	}
}

// knownTemplateFiles returns all template-related file paths that should be
// treated as known (never "unknown") within .agent-layer.
func (inst templateManager) knownTemplateFiles() []templateFile {
	out := make([]templateFile, 0, len(inst.managedTemplateFiles())+len(inst.userOwnedSeedFiles())+len(inst.userOwnedStatuslineSourceFiles())+len(inst.agentOnlyFiles()))
	out = append(out, inst.managedTemplateFiles()...)
	out = append(out, inst.userOwnedSeedFiles()...)
	out = append(out, inst.userOwnedStatuslineSourceFiles()...)
	out = append(out, inst.agentOnlyFiles()...)
	return out
}

func (inst templateManager) userOwnedStatuslineSourceFiles() []templateFile {
	root := inst.root
	sources := StatuslineSourceTemplates()
	out := make([]templateFile, 0, len(sources)+1)
	for _, source := range sources {
		out = append(out, templateFile{
			filepath.Join(root, filepath.FromSlash(source.RelPath)),
			source.TemplatePath,
			source.Perm,
		})
		if source.LegacyRelPath != "" {
			out = append(out, templateFile{
				filepath.Join(root, filepath.FromSlash(source.LegacyRelPath)),
				source.TemplatePath,
				source.Perm,
			})
		}
	}
	return out
}

// managedTemplateDirs lists template-managed directories under .agent-layer.
func (inst templateManager) managedTemplateDirs() []templateDir {
	root := inst.root
	return []templateDir{
		{instructionsDirName, filepath.Join(root, ".agent-layer", instructionsDirName)},
		{skillDirectoryName, filepath.Join(root, ".agent-layer", skillDirectoryName)},
		{docsAgentLayerDir, filepath.Join(root, ".agent-layer", "templates", "docs")},
	}
}

// managedInstructionTemplateDirs lists the bundled instruction templates one
// file at a time so upgrade activation can preserve a Rules-only selection.
func (inst templateManager) managedInstructionTemplateDirs() []templateDir {
	root := inst.root
	return []templateDir{
		{templateRoot: "instructions/00_rules.md", destRoot: filepath.Join(root, ".agent-layer", instructionsDirName)},
		{templateRoot: "instructions/01_memory.md", destRoot: filepath.Join(root, ".agent-layer", instructionsDirName)},
	}
}

func (inst templateManager) managedMemoryTemplateDirs() []templateDir {
	return []templateDir{{
		templateRoot: docsAgentLayerDir,
		destRoot:     filepath.Join(inst.root, ".agent-layer", "templates", "docs"),
	}}
}

// memoryTemplateDirs lists template-managed memory directories under docs/agent-layer.
func (inst templateManager) memoryTemplateDirs() []templateDir {
	root := inst.root
	return []templateDir{
		{docsAgentLayerDir, filepath.Join(root, "docs", "agent-layer")},
	}
}

// activeManagedTemplateDirs returns .agent-layer template dirs that should
// participate in install, upgrade, diff, and baseline operations for the current
// on-disk layout.
func (inst templateManager) activeManagedTemplateDirs() ([]templateDir, error) {
	dirs := []templateDir{}
	instructionDirs, err := inst.activeInstructionTemplateDirs()
	if err != nil {
		return nil, err
	}
	dirs = append(dirs, instructionDirs...)
	if hasMemory, err := inst.memoryEvidenceOnDisk(); err != nil {
		return nil, err
	} else if hasMemory {
		dirs = append(dirs, inst.managedMemoryTemplateDirs()...)
	}
	catalogDirs, err := inst.installedCatalogSkillTemplateDirs()
	if err != nil {
		return nil, err
	}
	dirs = append(dirs, catalogDirs...)
	developmentDirs, err := inst.installedDevelopmentSkillTemplateDirs()
	if err != nil {
		return nil, err
	}
	dirs = append(dirs, developmentDirs...)
	return dirs, nil
}

// activeMemoryTemplateDirs returns memory template dirs that should participate
// in install, upgrade, diff, and baseline operations for the current on-disk
// layout.
func (inst templateManager) activeMemoryTemplateDirs() ([]templateDir, error) {
	hasMemory, err := inst.memoryEvidenceOnDisk()
	if err != nil {
		return nil, err
	}
	if hasMemory {
		return inst.memoryTemplateDirs(), nil
	}
	return nil, nil
}

// activeAllTemplateDirs returns all template dirs that should participate in
// install, upgrade, diff, and baseline operations for the current on-disk layout.
func (inst templateManager) activeAllTemplateDirs() ([]templateDir, error) {
	managed, err := inst.activeManagedTemplateDirs()
	if err != nil {
		return nil, err
	}
	memory, err := inst.activeMemoryTemplateDirs()
	if err != nil {
		return nil, err
	}
	dirs := make([]templateDir, 0, len(managed)+len(memory))
	dirs = append(dirs, managed...)
	dirs = append(dirs, memory...)
	return dirs, nil
}

// activeInstructionTemplateDirs returns only the managed instruction files
// already on disk. This keeps Rules-only and Rules-and-memory selections
// independent during later upgrades.
func (inst templateManager) activeInstructionTemplateDirs() ([]templateDir, error) {
	dirs := []templateDir{}
	for _, dir := range inst.managedInstructionTemplateDirs() {
		found, err := inst.anyExistingTemplateDirFile(dir)
		if err != nil {
			return nil, err
		}
		if found {
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

// memoryEvidenceOnDisk reports whether either half of the Rules-and-memory
// selection is present. Memory templates and live memory docs are managed as a
// pair because the wizard seeds them together.
func (inst templateManager) memoryEvidenceOnDisk() (bool, error) {
	for _, dir := range inst.managedMemoryTemplateDirs() {
		found, err := inst.anyExistingTemplateDirFile(dir)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	for _, dir := range inst.memoryTemplateDirs() {
		found, err := inst.anyExistingTemplateDirFile(dir)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// anyExistingTemplateDirFile reports whether any embedded file for dir already
// exists at its destination.
func (inst templateManager) anyExistingTemplateDirFile(dir templateDir) (bool, error) {
	entries, err := inst.templateDirEntries(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if _, statErr := inst.sys.Stat(entry.destPath); statErr == nil {
			return true, nil
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return false, fmt.Errorf(messages.InstallFailedStatFmt, entry.destPath, statErr)
		}
	}
	return false, nil
}

// installedCatalogSkillTemplateDirs returns catalog template dirs only for
// catalog skills already materialized under .agent-layer/skills/.
func (inst templateManager) installedCatalogSkillTemplateDirs() ([]templateDir, error) {
	return inst.installedSkillTemplateDirs("skills-catalog")
}

// installedDevelopmentSkillTemplateDirs returns grouped development-skill
// template dirs only for members already materialized under
// .agent-layer/skills/. They are catalog selections, not evidence that every
// development skill should be activated.
func (inst templateManager) installedDevelopmentSkillTemplateDirs() ([]templateDir, error) {
	return inst.installedSkillTemplateDirs("skills")
}

func (inst templateManager) skillTemplateDirs(templateRoot string) ([]templateDir, error) {
	ids := make(map[string]struct{})
	if err := templates.Walk(templateRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, templateRoot+"/")
		if rel == path {
			return fmt.Errorf(messages.InstallUnexpectedTemplatePathFmt, path)
		}
		parts := strings.SplitN(rel, "/", 2)
		if len(parts) != 2 || parts[0] == "" {
			return fmt.Errorf(messages.InstallUnexpectedTemplatePathFmt, path)
		}
		ids[parts[0]] = struct{}{}
		return nil
	}); err != nil {
		return nil, err
	}

	sortedIDs := make([]string, 0, len(ids))
	for id := range ids {
		sortedIDs = append(sortedIDs, id)
	}
	sort.Strings(sortedIDs)
	out := make([]templateDir, 0, len(sortedIDs))
	for _, id := range sortedIDs {
		destRoot := filepath.Join(inst.root, ".agent-layer", "skills", id)
		out = append(out, templateDir{
			templateRoot: templateRoot + "/" + id,
			destRoot:     destRoot,
		})
	}
	return out, nil
}

func (inst templateManager) installedSkillTemplateDirs(templateRoot string) ([]templateDir, error) {
	dirs, err := inst.skillTemplateDirs(templateRoot)
	if err != nil {
		return nil, err
	}
	out := make([]templateDir, 0, len(dirs))
	for _, dir := range dirs {
		info, err := inst.sys.Stat(dir.destRoot)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf(messages.InstallFailedStatFmt, dir.destRoot, err)
		}
		if info.IsDir() {
			out = append(out, dir)
		}
	}
	return out, nil
}

// ungatedTemplatePathByRel includes destinations that migrations may activate.
func (inst templateManager) ungatedTemplatePathByRel() (map[string]string, error) {
	dirs := inst.managedInstructionTemplateDirs()
	dirs = append(dirs, inst.managedMemoryTemplateDirs()...)
	for _, root := range []string{"skills-catalog", "skills"} {
		skillDirs, err := inst.skillTemplateDirs(root)
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, skillDirs...)
	}
	dirs = append(dirs, inst.memoryTemplateDirs()...)
	return inst.templatePathByRel(dirs, true)
}

// listManagedDiffs returns relative paths for managed files that differ from templates.
func (inst templateManager) listManagedDiffs() ([]string, error) {
	diffs := make(map[string]struct{})
	if err := inst.appendTemplateFileDiffs(diffs, inst.managedTemplateFiles()); err != nil {
		return nil, err
	}
	dirs, err := inst.activeManagedTemplateDirs()
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		if err := inst.appendTemplateDirDiffs(diffs, dir); err != nil {
			return nil, err
		}
	}
	paths := sortedKeys(diffs)
	templatePathByRel, err := inst.managedTemplatePathByRel()
	if err != nil {
		return nil, err
	}
	if err := inst.checkDiffEvidence(paths, templatePathByRel); err != nil {
		return nil, err
	}
	return paths, nil
}

// listMemoryDiffs returns relative paths for memory files that differ from templates.
func (inst templateManager) listMemoryDiffs() ([]string, error) {
	diffs := make(map[string]struct{})
	dirs, err := inst.activeMemoryTemplateDirs()
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		if err := inst.appendTemplateDirDiffs(diffs, dir); err != nil {
			return nil, err
		}
	}
	paths := sortedKeys(diffs)
	templatePathByRel, err := inst.memoryTemplatePathByRel()
	if err != nil {
		return nil, err
	}
	if err := inst.checkDiffEvidence(paths, templatePathByRel); err != nil {
		return nil, err
	}
	return paths, nil
}

// checkDiffEvidence validates the recorded evidence for each differing path
// that has a template mapping, in sorted order.
func (inst templateManager) checkDiffEvidence(paths []string, templatePathByRel map[string]string) error {
	for _, path := range paths {
		relPath := normalizeRelPath(path)
		if templatePath := templatePathByRel[relPath]; templatePath != "" {
			if err := inst.checkTemplateDiffEvidence(relPath, templatePath); err != nil {
				return err
			}
		}
	}
	return nil
}

// appendTemplateFileDiffs adds relative paths for files that differ from templates.
func (inst templateManager) appendTemplateFileDiffs(diffs map[string]struct{}, files []templateFile) error {
	sys := inst.sys
	for _, file := range files {
		info, err := sys.Stat(file.path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf(messages.InstallFailedStatFmt, file.path, err)
		}
		matches, err := inst.matchTemplate(file.path, file.template, info)
		if err != nil {
			return err
		}
		if !matches {
			diffs[normalizeRelPath(inst.relativePath(file.path))] = struct{}{}
		}
	}
	return nil
}

// appendTemplateDirDiffs adds relative paths for directory template diffs.
func (inst templateManager) appendTemplateDirDiffs(diffs map[string]struct{}, dir templateDir) error {
	entries, err := inst.templateDirEntries(dir)
	if err != nil {
		return err
	}
	sys := inst.sys
	for _, entry := range entries {
		relPath := normalizeRelPath(inst.relativePath(entry.destPath))
		info, err := sys.Stat(entry.destPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf(messages.InstallFailedStatFmt, entry.destPath, err)
		}
		if _, ok := sectionAwareMarkerForPath(relPath); ok {
			matches, matchErr := inst.sectionAwareTemplateMatch(relPath, entry.destPath, entry.templatePath)
			if matchErr != nil {
				return matchErr
			}
			if matches {
				continue
			}
			// If the marker is missing or malformed, the write path will fail loudly.
			diffs[relPath] = struct{}{}
			continue
		}
		matches, err := inst.matchTemplate(entry.destPath, entry.templatePath, info)
		if err != nil {
			return err
		}
		if !matches {
			diffs[relPath] = struct{}{}
		}
	}
	return nil
}

// sortedKeys returns sorted keys for a set.
func sortedKeys(entries map[string]struct{}) []string {
	if len(entries) == 0 {
		return nil
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (inst templateManager) managedTemplatePathByRel() (map[string]string, error) {
	dirs := inst.managedTemplateDirs()
	catalogDirs, err := inst.installedCatalogSkillTemplateDirs()
	if err != nil {
		return nil, err
	}
	dirs = append(dirs, catalogDirs...)
	return inst.templatePathByRel(dirs, true)
}

func (inst templateManager) memoryTemplatePathByRel() (map[string]string, error) {
	return inst.templatePathByRel(inst.memoryTemplateDirs(), false)
}

func (inst templateManager) templatePathByRel(dirs []templateDir, includeManagedFiles bool) (map[string]string, error) {
	m := make(map[string]string)
	if includeManagedFiles {
		for _, file := range inst.managedTemplateFiles() {
			rel := normalizeRelPath(inst.relativePath(file.path))
			m[rel] = file.template
		}
	}
	for _, dir := range dirs {
		entries, err := inst.templateDirEntries(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			rel := normalizeRelPath(inst.relativePath(entry.destPath))
			m[rel] = entry.templatePath
		}
	}
	return m, nil
}

func (inst templateManager) writeTemplateDirCached(dir templateDir) error {
	entries, err := inst.templateDirEntries(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		relPath := normalizeRelPath(inst.relativePath(entry.destPath))
		if marker, ok := sectionAwareMarkerForPath(relPath); ok {
			if err := inst.writeSectionAwareTemplateFile(entry.destPath, entry.templatePath, entry.perm, relPath, marker); err != nil {
				return err
			}
			continue
		}
		if err := inst.writeTemplateFile(entry.destPath, entry.templatePath, entry.perm, inst.shouldOverwrite, inst.recordDiff); err != nil {
			return err
		}
	}
	return nil
}

func (inst templateManager) writeSectionAwareTemplateFile(path string, templatePath string, perm fs.FileMode, relPath string, marker string) error {
	_, err := inst.sys.Stat(path)
	if err == nil {
		localBytes, readErr := inst.sys.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf(messages.InstallFailedReadFmt, path, readErr)
		}
		templateBytes, templateErr := templates.Read(templatePath)
		if templateErr != nil {
			return fmt.Errorf(messages.InstallFailedReadTemplateFmt, templatePath, templateErr)
		}

		localManaged, localUser, splitErr := splitSectionAwareContent(relPath, marker, localBytes)
		if splitErr != nil {
			return splitErr
		}
		templateManaged, _, templateSplitErr := splitSectionAwareContent(relPath, marker, templateBytes)
		if templateSplitErr != nil {
			return templateSplitErr
		}

		if normalizeTemplateContent(localManaged) == normalizeTemplateContent(templateManaged) {
			return nil
		}

		overwrite, overwriteErr := inst.shouldOverwrite(path)
		if overwriteErr != nil {
			return overwriteErr
		}
		if !overwrite {
			inst.recordDiff(path)
			return nil
		}

		merged := []byte(templateManaged + localUser)
		if err := inst.sys.WriteFileAtomic(path, merged, perm); err != nil {
			return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(messages.InstallFailedStatFmt, path, err)
	}
	return inst.writeTemplateFile(path, templatePath, perm, inst.shouldOverwrite, inst.recordDiff)
}

func (inst templateManager) templateDirEntries(dir templateDir) ([]templateEntry, error) {
	if inst.templateEntries == nil {
		inst.templateEntries = make(map[string][]templateEntry)
	}
	key := dir.templateRoot + "|" + dir.destRoot
	if cached, ok := inst.templateEntries[key]; ok {
		return cached, nil
	}
	entries := []templateEntry{}
	err := templates.Walk(dir.templateRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(path, dir.templateRoot+"/")
		if rel == path {
			if path != dir.templateRoot {
				return fmt.Errorf(messages.InstallUnexpectedTemplatePathFmt, path)
			}
			rel = filepath.Base(path)
		}
		destPath := filepath.Join(dir.destRoot, rel)
		entries = append(entries, templateEntry{
			templatePath: path,
			destPath:     destPath,
			perm:         0o644,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	inst.templateEntries[key] = entries
	return entries, nil
}

func normalizeRelPath(path string) string {
	return strings.ReplaceAll(filepath.ToSlash(path), "\\", "/")
}

func (inst templateManager) matchTemplate(path string, templatePath string, info fs.FileInfo) (bool, error) {
	key := inst.matchCacheKey(path, templatePath)
	if cached, ok := inst.templateMatchCache[key]; ok && cached.size == info.Size() && cached.modTime == info.ModTime().UnixNano() {
		return cached.matches, nil
	}
	matches, err := fileMatchesTemplate(inst.sys, path, templatePath)
	if err != nil {
		return false, err
	}
	if inst.templateMatchCache == nil {
		inst.templateMatchCache = make(map[string]matchCacheEntry)
	}
	inst.templateMatchCache[key] = matchCacheEntry{
		matches: matches,
		size:    info.Size(),
		modTime: info.ModTime().UnixNano(),
	}
	return matches, nil
}

func (inst templateManager) matchCacheKey(path string, templatePath string) string {
	return path + "\n" + templatePath
}

// writeTemplateFile writes templatePath to path when the file is missing. An
// existing file that differs from the template is overwritten only when
// shouldOverwrite approves; otherwise it is reported to recordDiff.
func (inst templateManager) writeTemplateFile(path string, templatePath string, perm fs.FileMode, shouldOverwrite PromptOverwriteFunc, recordDiff func(string)) error {
	info, err := inst.sys.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(messages.InstallFailedStatFmt, path, err)
	}
	// An overwritten gitignore.block keeps the existing file's tracking choices.
	mergeTracking := false
	if err == nil {
		matches, err := inst.matchTemplate(path, templatePath, info)
		if err != nil {
			return err
		}
		if matches {
			return nil
		}
		overwrite := false
		if shouldOverwrite != nil {
			overwrite, err = shouldOverwrite(path)
			if err != nil {
				return err
			}
		}
		if !overwrite {
			if recordDiff != nil {
				recordDiff(path)
			}
			return nil
		}
		mergeTracking = templatePath == templateGitignoreBlock
	}

	var data []byte
	if mergeTracking {
		existing, readErr := inst.sys.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf(messages.InstallFailedReadFmt, path, readErr)
		}
		data, err = templateTarget(path, templatePath, existing)
		if err != nil {
			return err
		}
	} else {
		data, err = templates.Read(templatePath)
		if err != nil {
			return fmt.Errorf(messages.InstallFailedReadTemplateFmt, templatePath, err)
		}
	}
	if err := inst.sys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf(messages.InstallFailedCreateDirForFmt, path, err)
	}
	if err := inst.sys.WriteFileAtomic(path, data, perm); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
	}
	// Replacement may preserve size and mtime, so discard the pre-write match.
	delete(inst.templateMatchCache, inst.matchCacheKey(path, templatePath))
	return nil
}

func fileMatchesTemplate(sys System, path string, templatePath string) (bool, error) {
	existing, err := sys.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf(messages.InstallFailedReadFmt, path, err)
	}
	target, err := templateTarget(path, templatePath, existing)
	if err != nil {
		return false, err
	}
	return normalizeTemplateContent(string(existing)) == normalizeTemplateContent(string(target)), nil
}

// templateTarget returns the template content an existing file at path is
// compared against and overwritten with. gitignore.block keeps the existing
// file's tracking choices.
func templateTarget(path string, templatePath string, existing []byte) ([]byte, error) {
	template, err := templates.Read(templatePath)
	if err != nil {
		return nil, fmt.Errorf(messages.InstallFailedReadTemplateFmt, templatePath, err)
	}
	if templatePath == templateGitignoreBlock {
		merged, mergeErr := mergeGitignoreBlockTemplate(existing, template)
		if mergeErr != nil {
			return nil, fmt.Errorf(messages.InstallGitignoreMergeTrackingFmt, path, mergeErr)
		}
		return merged, nil
	}
	return template, nil
}

func normalizeTemplateContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.TrimRight(content, "\n") + "\n"
}
