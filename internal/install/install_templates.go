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
func (inst *installer) managedTemplateFiles() []templateFile {
	root := inst.root
	return []templateFile{
		{filepath.Join(root, ".agent-layer", "commands.allow"), "commands.allow", 0o644},
		{filepath.Join(root, ".agent-layer", templateGitignoreBlock), templateGitignoreBlock, 0o644},
	}
}

// userOwnedSeedFiles lists user-owned files under .agent-layer that are required
// for Agent Layer to operate, but should never be overwritten during init or
// upgrade flows. They are seeded only when missing.
func (inst *installer) userOwnedSeedFiles() []templateFile {
	root := inst.root
	return []templateFile{
		{filepath.Join(root, ".agent-layer", "config.toml"), "config.toml", 0o644},
		{filepath.Join(root, ".agent-layer", ".env"), "env", 0o600},
	}
}

// agentOnlyFiles lists agent-owned files under .agent-layer that are safe to
// overwrite unconditionally and should not be surfaced as upgrade actions.
func (inst *installer) agentOnlyFiles() []templateFile {
	root := inst.root
	return []templateFile{
		{filepath.Join(root, ".agent-layer", ".gitignore"), "agent-layer.gitignore", 0o644},
	}
}

// knownTemplateFiles returns all template-related file paths that should be
// treated as known (never "unknown") within .agent-layer.
func (inst *installer) knownTemplateFiles() []templateFile {
	out := make([]templateFile, 0, len(inst.managedTemplateFiles())+len(inst.userOwnedSeedFiles())+len(inst.agentOnlyFiles()))
	out = append(out, inst.managedTemplateFiles()...)
	out = append(out, inst.userOwnedSeedFiles()...)
	out = append(out, inst.agentOnlyFiles()...)
	return out
}

// managedTemplateDirs lists template-managed directories under .agent-layer.
func (inst *installer) managedTemplateDirs() []templateDir {
	root := inst.root
	return []templateDir{
		{"instructions", filepath.Join(root, ".agent-layer", "instructions")},
		{"slash-commands", filepath.Join(root, ".agent-layer", "slash-commands")},
		{"docs/agent-layer", filepath.Join(root, ".agent-layer", "templates", "docs")},
	}
}

// memoryTemplateDirs lists template-managed memory directories under docs/agent-layer.
func (inst *installer) memoryTemplateDirs() []templateDir {
	root := inst.root
	return []templateDir{
		{"docs/agent-layer", filepath.Join(root, "docs", "agent-layer")},
	}
}

// allTemplateDirs returns managed and memory template directories.
func (inst *installer) allTemplateDirs() []templateDir {
	managed := inst.managedTemplateDirs()
	memory := inst.memoryTemplateDirs()
	dirs := make([]templateDir, 0, len(managed)+len(memory))
	dirs = append(dirs, managed...)
	dirs = append(dirs, memory...)
	return dirs
}

// listManagedDiffs returns relative paths for managed files that differ from templates.
func (inst *installer) listManagedDiffs() ([]string, error) {
	labeled, err := inst.listManagedLabeledDiffs()
	if err != nil {
		return nil, err
	}
	return labeledDiffPaths(labeled), nil
}

// listManagedLabeledDiffs returns managed diffs with ownership labels.
func (inst *installer) listManagedLabeledDiffs() ([]LabeledPath, error) {
	diffs := make(map[string]struct{})
	if err := inst.appendTemplateFileDiffs(diffs, inst.managedTemplateFiles()); err != nil {
		return nil, err
	}
	for _, dir := range inst.managedTemplateDirs() {
		if err := inst.appendTemplateDirDiffs(diffs, dir); err != nil {
			return nil, err
		}
	}
	templatePathByRel, err := inst.managedTemplatePathByRel()
	if err != nil {
		return nil, err
	}
	return inst.buildLabeledDiffs(sortedKeys(diffs), templatePathByRel)
}

// listMemoryDiffs returns relative paths for memory files that differ from templates.
func (inst *installer) listMemoryDiffs() ([]string, error) {
	labeled, err := inst.listMemoryLabeledDiffs()
	if err != nil {
		return nil, err
	}
	return labeledDiffPaths(labeled), nil
}

func labeledDiffPaths(labeled []LabeledPath) []string {
	if len(labeled) == 0 {
		return nil
	}
	paths := make([]string, 0, len(labeled))
	for _, entry := range labeled {
		paths = append(paths, entry.Path)
	}
	return paths
}

// listMemoryLabeledDiffs returns memory diffs with ownership labels.
func (inst *installer) listMemoryLabeledDiffs() ([]LabeledPath, error) {
	diffs := make(map[string]struct{})
	for _, dir := range inst.memoryTemplateDirs() {
		if err := inst.appendTemplateDirDiffs(diffs, dir); err != nil {
			return nil, err
		}
	}
	templatePathByRel, err := inst.memoryTemplatePathByRel()
	if err != nil {
		return nil, err
	}
	return inst.buildLabeledDiffs(sortedKeys(diffs), templatePathByRel)
}

// appendTemplateFileDiffs adds relative paths for files that differ from templates.
func (inst *installer) appendTemplateFileDiffs(diffs map[string]struct{}, files []templateFile) error {
	sys := inst.sys
	for _, file := range files {
		info, err := sys.Stat(file.path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf(messages.InstallFailedStatFmt, file.path, err)
		}
		matches, err := inst.matchTemplate(sys, file.path, file.template, info)
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
func (inst *installer) appendTemplateDirDiffs(diffs map[string]struct{}, dir templateDir) error {
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
		matches, err := inst.matchTemplate(sys, entry.destPath, entry.templatePath, info)
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

func (inst *installer) buildLabeledDiffs(paths []string, templatePathByRel map[string]string) ([]LabeledPath, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]LabeledPath, 0, len(paths))
	for _, path := range paths {
		relPath := normalizeRelPath(path)
		templatePath := templatePathByRel[relPath]
		ownership := OwnershipLocalCustomization
		if templatePath != "" {
			classified, err := inst.classifyOwnership(relPath, templatePath)
			if err != nil {
				return nil, err
			}
			ownership = classified
		}
		out = append(out, LabeledPath{
			Path:      relPath,
			Ownership: ownership,
		})
	}
	return out, nil
}

func (inst *installer) managedTemplatePathByRel() (map[string]string, error) {
	return inst.templatePathByRel(inst.managedTemplateDirs(), true)
}

func (inst *installer) memoryTemplatePathByRel() (map[string]string, error) {
	return inst.templatePathByRel(inst.memoryTemplateDirs(), false)
}

func (inst *installer) templatePathByRel(dirs []templateDir, includeManagedFiles bool) (map[string]string, error) {
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

func (inst *installer) writeTemplateDirCached(dir templateDir) error {
	entries, err := inst.templateDirEntries(dir)
	if err != nil {
		return err
	}
	sys := inst.sys
	for _, entry := range entries {
		relPath := normalizeRelPath(inst.relativePath(entry.destPath))
		if marker, ok := sectionAwareMarkerForPath(relPath); ok {
			if err := inst.writeSectionAwareTemplateFile(entry.destPath, entry.templatePath, entry.perm, relPath, marker); err != nil {
				return err
			}
			continue
		}
		if err := writeTemplateFileWithMatch(sys, entry.destPath, entry.templatePath, entry.perm, inst.shouldOverwrite, inst.recordDiff, inst.matchTemplate); err != nil {
			return err
		}
	}
	return nil
}

func (inst *installer) writeSectionAwareTemplateFile(path string, templatePath string, perm fs.FileMode, relPath string, marker string) error {
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
	return writeTemplateFileWithMatch(inst.sys, path, templatePath, perm, inst.shouldOverwrite, inst.recordDiff, inst.matchTemplate)
}

func (inst *installer) templateDirEntries(dir templateDir) ([]templateEntry, error) {
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
			return fmt.Errorf(messages.InstallUnexpectedTemplatePathFmt, path)
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

func (inst *installer) matchTemplate(sys System, path string, templatePath string, info fs.FileInfo) (bool, error) {
	if sys == nil {
		sys = inst.sys
	}
	if info != nil && inst.templateMatchCache != nil {
		key := inst.matchCacheKey(path, templatePath)
		if cached, ok := inst.templateMatchCache[key]; ok && cached.size == info.Size() && cached.modTime == info.ModTime().UnixNano() {
			return cached.matches, nil
		}
	}
	matches, err := fileMatchesTemplate(sys, path, templatePath)
	if err != nil {
		return false, err
	}
	if info != nil {
		if inst.templateMatchCache == nil {
			inst.templateMatchCache = make(map[string]matchCacheEntry)
		}
		inst.templateMatchCache[inst.matchCacheKey(path, templatePath)] = matchCacheEntry{
			matches: matches,
			size:    info.Size(),
			modTime: info.ModTime().UnixNano(),
		}
	}
	return matches, nil
}

func (inst *installer) matchCacheKey(path string, templatePath string) string {
	return path + "\n" + templatePath
}

func writeTemplateIfMissing(sys System, path string, templatePath string, perm fs.FileMode) error {
	return writeTemplateFile(sys, path, templatePath, perm, nil, nil)
}

func writeTemplateDir(sys System, templateRoot string, destRoot string, shouldOverwrite PromptOverwriteFunc, recordDiff func(string)) error {
	return templates.Walk(templateRoot, func(path string, entry fs.DirEntry, err error) error {
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
		destPath := filepath.Join(destRoot, rel)
		return writeTemplateFile(sys, destPath, path, 0o644, shouldOverwrite, recordDiff)
	})
}

// MatchTemplateFunc compares a destination file to a template.
type MatchTemplateFunc func(sys System, path string, templatePath string, info fs.FileInfo) (bool, error)

func fileMatchesTemplateWithInfo(sys System, path string, templatePath string, _ fs.FileInfo) (bool, error) {
	return fileMatchesTemplate(sys, path, templatePath)
}

func writeTemplateFile(sys System, path string, templatePath string, perm fs.FileMode, shouldOverwrite PromptOverwriteFunc, recordDiff func(string)) error {
	return writeTemplateFileWithMatch(sys, path, templatePath, perm, shouldOverwrite, recordDiff, fileMatchesTemplateWithInfo)
}

func writeTemplateFileWithMatch(
	sys System,
	path string,
	templatePath string,
	perm fs.FileMode,
	shouldOverwrite PromptOverwriteFunc,
	recordDiff func(string),
	matchTemplate MatchTemplateFunc,
) error {
	if matchTemplate == nil {
		matchTemplate = fileMatchesTemplateWithInfo
	}
	info, err := sys.Stat(path)
	if err == nil {
		matches, err := matchTemplate(sys, path, templatePath, info)
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
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(messages.InstallFailedStatFmt, path, err)
	}

	data, err := templates.Read(templatePath)
	if err != nil {
		return fmt.Errorf(messages.InstallFailedReadTemplateFmt, templatePath, err)
	}
	if err := sys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf(messages.InstallFailedCreateDirForFmt, path, err)
	}
	if err := sys.WriteFileAtomic(path, data, perm); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
	}
	return nil
}

func fileMatchesTemplate(sys System, path string, templatePath string) (bool, error) {
	existing, err := sys.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf(messages.InstallFailedReadFmt, path, err)
	}
	template, err := templates.Read(templatePath)
	if err != nil {
		return false, fmt.Errorf(messages.InstallFailedReadTemplateFmt, templatePath, err)
	}
	return normalizeTemplateContent(string(existing)) == normalizeTemplateContent(string(template)), nil
}

func normalizeTemplateContent(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.TrimRight(content, "\n") + "\n"
}
