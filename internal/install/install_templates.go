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
	"github.com/conn-castle/agent-layer/internal/version"
)

// templateGitignoreBlock is the template name for the gitignore managed block.
const templateGitignoreBlock = "gitignore.block"

// managedTemplateFiles lists template-managed files under .agent-layer.
func (inst *installer) managedTemplateFiles() []templateFile {
	root := inst.root
	return []templateFile{
		{filepath.Join(root, ".agent-layer", "config.toml"), "config.toml", 0o644},
		{filepath.Join(root, ".agent-layer", "commands.allow"), "commands.allow", 0o644},
		{filepath.Join(root, ".agent-layer", ".env"), "env", 0o600},
		{filepath.Join(root, ".agent-layer", ".gitignore"), "agent-layer.gitignore", 0o644},
		{filepath.Join(root, ".agent-layer", templateGitignoreBlock), templateGitignoreBlock, 0o644},
	}
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
	diffs := make(map[string]struct{})
	if err := inst.appendTemplateFileDiffs(diffs, inst.managedTemplateFiles()); err != nil {
		return nil, err
	}
	for _, dir := range inst.managedTemplateDirs() {
		if err := inst.appendTemplateDirDiffs(diffs, dir); err != nil {
			return nil, err
		}
	}
	if err := inst.appendPinnedVersionDiff(diffs); err != nil {
		return nil, err
	}
	return sortedKeys(diffs), nil
}

// listMemoryDiffs returns relative paths for memory files that differ from templates.
func (inst *installer) listMemoryDiffs() ([]string, error) {
	diffs := make(map[string]struct{})
	for _, dir := range inst.memoryTemplateDirs() {
		if err := inst.appendTemplateDirDiffs(diffs, dir); err != nil {
			return nil, err
		}
	}
	return sortedKeys(diffs), nil
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
		matches, err := inst.templateFileMatches(file, info)
		if err != nil {
			return err
		}
		if !matches {
			diffs[inst.relativePath(file.path)] = struct{}{}
		}
	}
	return nil
}

// templateFileMatches reports whether a template file should be considered unchanged.
func (inst *installer) templateFileMatches(file templateFile, info fs.FileInfo) (bool, error) {
	sys := inst.sys
	if file.template != templateGitignoreBlock {
		return inst.matchTemplate(sys, file.path, file.template, info)
	}
	existingBytes, err := sys.ReadFile(file.path)
	if err != nil {
		return false, fmt.Errorf(messages.InstallFailedReadFmt, file.path, err)
	}
	templateBytes, err := templates.Read(file.template)
	if err != nil {
		return false, fmt.Errorf(messages.InstallFailedReadTemplateFmt, file.template, err)
	}
	templateBlock := normalizeGitignoreBlock(string(templateBytes))
	existing := normalizeGitignoreBlock(string(existingBytes))
	if existing == templateBlock || gitignoreBlockMatchesHash(existing) {
		return true, nil
	}
	return false, nil
}

// appendTemplateDirDiffs adds relative paths for directory template diffs.
func (inst *installer) appendTemplateDirDiffs(diffs map[string]struct{}, dir templateDir) error {
	entries, err := inst.templateDirEntries(dir)
	if err != nil {
		return err
	}
	sys := inst.sys
	for _, entry := range entries {
		info, err := sys.Stat(entry.destPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf(messages.InstallFailedStatFmt, entry.destPath, err)
		}
		matches, err := inst.matchTemplate(sys, entry.destPath, entry.templatePath, info)
		if err != nil {
			return err
		}
		if !matches {
			diffs[inst.relativePath(entry.destPath)] = struct{}{}
		}
	}
	return nil
}

// appendPinnedVersionDiff adds al.version when it differs from the requested pin.
func (inst *installer) appendPinnedVersionDiff(diffs map[string]struct{}) error {
	if inst.pinVersion == "" {
		return nil
	}
	sys := inst.sys
	path := filepath.Join(inst.root, ".agent-layer", "al.version")
	data, err := sys.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf(messages.InstallFailedReadFmt, path, err)
	}
	existing := strings.TrimSpace(string(data))
	if existing == "" {
		return nil
	}
	normalized, err := version.Normalize(existing)
	if err != nil {
		normalized = ""
	}
	if normalized != inst.pinVersion {
		diffs[inst.relativePath(path)] = struct{}{}
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

func (inst *installer) writeTemplateDirCached(dir templateDir) error {
	entries, err := inst.templateDirEntries(dir)
	if err != nil {
		return err
	}
	sys := inst.sys
	for _, entry := range entries {
		if err := writeTemplateFileWithMatch(sys, entry.destPath, entry.templatePath, entry.perm, inst.shouldOverwrite, inst.recordDiff, inst.matchTemplate); err != nil {
			return err
		}
	}
	return nil
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
