package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/launchers"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/version"
)

// PromptOverwriteFunc asks whether to overwrite a given path.
type PromptOverwriteFunc func(path string) (bool, error)

// PromptOverwriteAllFunc asks whether to overwrite all managed files.
type PromptOverwriteAllFunc func() (bool, error)

// PromptDeleteUnknownAllFunc asks whether to delete all unknown paths.
type PromptDeleteUnknownAllFunc func(paths []string) (bool, error)

// PromptDeleteUnknownFunc asks whether to delete a specific unknown path.
type PromptDeleteUnknownFunc func(path string) (bool, error)

// Options controls installer behavior.
type Options struct {
	Overwrite              bool
	Force                  bool
	PromptOverwriteAll     PromptOverwriteAllFunc
	PromptOverwrite        PromptOverwriteFunc
	PromptDeleteUnknownAll PromptDeleteUnknownAllFunc
	PromptDeleteUnknown    PromptDeleteUnknownFunc
	PinVersion             string
}

type installer struct {
	root                   string
	overwrite              bool
	overwriteAll           bool
	overwriteAllDecided    bool
	force                  bool
	promptOverwriteAll     PromptOverwriteAllFunc
	promptOverwrite        PromptOverwriteFunc
	promptDeleteUnknownAll PromptDeleteUnknownAllFunc
	promptDeleteUnknown    PromptDeleteUnknownFunc
	diffs                  []string
	unknowns               []string
	pinVersion             string
}

// Run initializes the repository with the required Agent Layer structure.
func Run(root string, opts Options) error {
	if root == "" {
		return fmt.Errorf(messages.InstallRootRequired)
	}

	overwrite := opts.Overwrite || opts.Force
	if overwrite && !opts.Force && opts.PromptOverwriteAll == nil {
		return fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	if overwrite && !opts.Force && opts.PromptDeleteUnknownAll == nil {
		return fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}

	inst := &installer{
		root:                   root,
		overwrite:              overwrite,
		force:                  opts.Force,
		promptOverwriteAll:     opts.PromptOverwriteAll,
		promptOverwrite:        opts.PromptOverwrite,
		promptDeleteUnknownAll: opts.PromptDeleteUnknownAll,
		promptDeleteUnknown:    opts.PromptDeleteUnknown,
	}
	if strings.TrimSpace(opts.PinVersion) != "" {
		normalized, err := version.Normalize(opts.PinVersion)
		if err != nil {
			return fmt.Errorf(messages.InstallInvalidPinVersionFmt, err)
		}
		inst.pinVersion = normalized
	}
	steps := []func() error{
		inst.createDirs,
		inst.writeVersionFile,
		inst.writeTemplateFiles,
		inst.writeTemplateDirs,
		inst.updateGitignore,
		inst.scanUnknowns,
		inst.handleUnknowns,
	}

	if err := runSteps(steps); err != nil {
		return err
	}

	inst.warnDifferences()
	inst.warnUnknowns()
	return nil
}

func runSteps(steps []func() error) error {
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	return nil
}

func (inst *installer) createDirs() error {
	root := inst.root
	dirs := []string{
		filepath.Join(root, ".agent-layer", "instructions"),
		filepath.Join(root, ".agent-layer", "slash-commands"),
		filepath.Join(root, ".agent-layer", "templates", "docs"),
		filepath.Join(root, ".agent-layer", "tmp", "runs"),
		filepath.Join(root, "docs", "agent-layer"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf(messages.InstallCreateDirFailedFmt, dir, err)
		}
	}
	return nil
}

func (inst *installer) writeTemplateFiles() error {
	root := inst.root
	files := []struct {
		path     string
		template string
		perm     fs.FileMode
	}{
		{filepath.Join(root, ".agent-layer", "config.toml"), "config.toml", 0o644},
		{filepath.Join(root, ".agent-layer", "commands.allow"), "commands.allow", 0o644},
		{filepath.Join(root, ".agent-layer", ".env"), "env", 0o600},
		{filepath.Join(root, ".agent-layer", ".gitignore"), "agent-layer.gitignore", 0o644},
		{filepath.Join(root, ".agent-layer", "gitignore.block"), "gitignore.block", 0o644},
	}
	for _, file := range files {
		if file.template == "gitignore.block" {
			if err := writeGitignoreBlock(file.path, file.template, file.perm, inst.shouldOverwrite, inst.recordDiff); err != nil {
				return err
			}
			continue
		}
		if err := writeTemplateFile(file.path, file.template, file.perm, inst.shouldOverwrite, inst.recordDiff); err != nil {
			return err
		}
	}
	return nil
}

// writeVersionFile writes .agent-layer/al.version when pinning is enabled.
func (inst *installer) writeVersionFile() error {
	if inst.pinVersion == "" {
		return nil
	}
	path := filepath.Join(inst.root, ".agent-layer", "al.version")
	existingBytes, err := os.ReadFile(path)
	if err == nil {
		existing := strings.TrimSpace(string(existingBytes))
		if existing == "" {
			return fmt.Errorf(messages.InstallExistingPinFileEmptyFmt, path)
		}
		normalized, err := version.Normalize(existing)
		if err != nil {
			normalized = ""
		}
		if normalized == inst.pinVersion {
			return nil
		}
		overwrite, err := inst.shouldOverwrite(path)
		if err != nil {
			return err
		}
		if !overwrite {
			inst.recordDiff(path)
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(messages.InstallFailedReadFmt, path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf(messages.InstallFailedCreateDirForFmt, path, err)
	}
	content := []byte(inst.pinVersion + "\n")
	if err := fsutil.WriteFileAtomic(path, content, 0o644); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
	}
	return nil
}

func (inst *installer) writeTemplateDirs() error {
	root := inst.root
	dirs := []struct {
		templateRoot string
		destRoot     string
	}{
		{"instructions", filepath.Join(root, ".agent-layer", "instructions")},
		{"slash-commands", filepath.Join(root, ".agent-layer", "slash-commands")},
		{"docs/agent-layer", filepath.Join(root, "docs", "agent-layer")},
		{"docs/agent-layer", filepath.Join(root, ".agent-layer", "templates", "docs")},
	}
	for _, dir := range dirs {
		if err := writeTemplateDir(dir.templateRoot, dir.destRoot, inst.shouldOverwrite, inst.recordDiff); err != nil {
			return err
		}
	}
	return nil
}

func (inst *installer) updateGitignore() error {
	root := inst.root
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	blockBytes, err := os.ReadFile(blockPath)
	if err != nil {
		return fmt.Errorf(messages.InstallFailedReadGitignoreBlockFmt, blockPath, err)
	}
	return ensureGitignore(filepath.Join(root, ".gitignore"), string(blockBytes))
}

func (inst *installer) recordDiff(path string) {
	inst.diffs = append(inst.diffs, path)
}

func (inst *installer) recordUnknown(path string) {
	inst.unknowns = append(inst.unknowns, path)
}

// shouldOverwrite decides whether to overwrite the given path.
// It returns true to overwrite, false to keep existing content, or an error.
func (inst *installer) shouldOverwrite(path string) (bool, error) {
	if !inst.overwrite {
		return false, nil
	}
	if inst.force {
		return true, nil
	}
	if !inst.overwriteAllDecided {
		if inst.promptOverwriteAll == nil {
			return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
		}
		overwriteAll, err := inst.promptOverwriteAll()
		if err != nil {
			return false, err
		}
		inst.overwriteAll = overwriteAll
		inst.overwriteAllDecided = true
	}
	if inst.overwriteAll {
		return true, nil
	}
	if inst.promptOverwrite == nil {
		return false, fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	rel := path
	if inst.root != "" {
		if candidate, err := filepath.Rel(inst.root, path); err == nil {
			rel = candidate
		}
	}
	return inst.promptOverwrite(rel)
}

func (inst *installer) warnDifferences() {
	if inst.overwrite || len(inst.diffs) == 0 {
		return
	}

	sort.Strings(inst.diffs)
	_, _ = fmt.Fprintln(os.Stderr, messages.InstallDiffHeader)
	for _, path := range inst.diffs {
		rel, err := filepath.Rel(inst.root, path)
		if err != nil {
			rel = path
		}
		_, _ = fmt.Fprintf(os.Stderr, messages.InstallDiffLineFmt, rel)
	}
	_, _ = fmt.Fprintln(os.Stderr)
	_, _ = fmt.Fprintln(os.Stderr, messages.InstallDiffFooter)
	_, _ = fmt.Fprintln(os.Stderr)
}

func (inst *installer) warnUnknowns() {
	if inst.overwrite || len(inst.unknowns) == 0 {
		return
	}

	inst.sortUnknowns()
	_, _ = fmt.Fprintln(os.Stderr, messages.InstallUnknownHeader)
	for _, path := range inst.unknowns {
		rel := inst.relativePath(path)
		_, _ = fmt.Fprintf(os.Stderr, messages.InstallDiffLineFmt, rel)
	}
	_, _ = fmt.Fprintln(os.Stderr)
	_, _ = fmt.Fprintln(os.Stderr, messages.InstallUnknownFooter)
	_, _ = fmt.Fprintln(os.Stderr)
}

func (inst *installer) scanUnknowns() error {
	known, err := inst.buildKnownPaths()
	if err != nil {
		return err
	}

	root := filepath.Join(inst.root, ".agent-layer")
	if err := inst.scanUnknownRoot(root, known); err != nil {
		return err
	}
	inst.sortUnknowns()
	return nil
}

func (inst *installer) scanUnknownRoot(root string, known map[string]struct{}) error {
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf(messages.InstallFailedStatFmt, root, err)
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		clean := filepath.Clean(path)
		if clean == filepath.Clean(root) {
			return nil
		}
		if _, ok := known[clean]; ok {
			return nil
		}
		inst.recordUnknown(clean)
		if entry.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
}

func (inst *installer) handleUnknowns() error {
	if len(inst.unknowns) == 0 || !inst.overwrite {
		return nil
	}
	if inst.force {
		return inst.deleteUnknowns(inst.unknowns)
	}
	if inst.promptDeleteUnknownAll == nil {
		return fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	rel := inst.relativeUnknowns()
	deleteAll, err := inst.promptDeleteUnknownAll(rel)
	if err != nil {
		return err
	}
	if deleteAll {
		return inst.deleteUnknowns(inst.unknowns)
	}
	if inst.promptDeleteUnknown == nil {
		return fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
	}
	for _, path := range inst.unknowns {
		relPath := inst.relativePath(path)
		deletePath, err := inst.promptDeleteUnknown(relPath)
		if err != nil {
			return err
		}
		if deletePath {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf(messages.InstallDeleteUnknownFailedFmt, relPath, err)
			}
		}
	}
	return nil
}

func (inst *installer) deleteUnknowns(paths []string) error {
	for _, path := range paths {
		rel := inst.relativePath(path)
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf(messages.InstallDeleteUnknownFailedFmt, rel, err)
		}
	}
	return nil
}

func (inst *installer) sortUnknowns() {
	sort.Slice(inst.unknowns, func(i, j int) bool {
		return inst.relativePath(inst.unknowns[i]) < inst.relativePath(inst.unknowns[j])
	})
}

func (inst *installer) relativeUnknowns() []string {
	if len(inst.unknowns) == 0 {
		return nil
	}
	rel := make([]string, 0, len(inst.unknowns))
	for _, path := range inst.unknowns {
		rel = append(rel, inst.relativePath(path))
	}
	sort.Strings(rel)
	return rel
}

func (inst *installer) relativePath(path string) string {
	rel := path
	if inst.root != "" {
		if candidate, err := filepath.Rel(inst.root, path); err == nil {
			rel = candidate
		}
	}
	return rel
}

func (inst *installer) buildKnownPaths() (map[string]struct{}, error) {
	known := make(map[string]struct{})
	add := func(path string) {
		known[filepath.Clean(path)] = struct{}{}
	}

	root := inst.root
	add(filepath.Join(root, ".agent-layer"))
	add(filepath.Join(root, ".agent-layer", "instructions"))
	add(filepath.Join(root, ".agent-layer", "slash-commands"))
	add(filepath.Join(root, ".agent-layer", "templates"))
	add(filepath.Join(root, ".agent-layer", "templates", "docs"))
	add(filepath.Join(root, ".agent-layer", "tmp"))
	add(filepath.Join(root, ".agent-layer", "tmp", "runs"))

	// Root-level managed files.
	add(filepath.Join(root, ".agent-layer", "config.toml"))
	add(filepath.Join(root, ".agent-layer", "commands.allow"))
	add(filepath.Join(root, ".agent-layer", ".env"))
	add(filepath.Join(root, ".agent-layer", ".gitignore"))
	add(filepath.Join(root, ".agent-layer", "gitignore.block"))
	add(filepath.Join(root, ".agent-layer", "al.version"))

	// VS Code launchers generated by sync.
	for _, path := range launchers.VSCodePaths(root).All() {
		add(path)
	}

	addTemplatePaths := func(templateRoot string, destRoot string) error {
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
			add(filepath.Join(destRoot, rel))
			return nil
		})
	}

	if err := addTemplatePaths("instructions", filepath.Join(root, ".agent-layer", "instructions")); err != nil {
		return nil, err
	}
	if err := addTemplatePaths("slash-commands", filepath.Join(root, ".agent-layer", "slash-commands")); err != nil {
		return nil, err
	}
	if err := addTemplatePaths("docs/agent-layer", filepath.Join(root, ".agent-layer", "templates", "docs")); err != nil {
		return nil, err
	}

	return known, nil
}

func writeTemplateIfMissing(path string, templatePath string, perm fs.FileMode) error {
	return writeTemplateFile(path, templatePath, perm, nil, nil)
}

func writeTemplateDir(templateRoot string, destRoot string, shouldOverwrite PromptOverwriteFunc, recordDiff func(string)) error {
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
		return writeTemplateFile(destPath, path, 0o644, shouldOverwrite, recordDiff)
	})
}

func ensureGitignore(path string, block string) error {
	block = normalizeGitignoreBlock(block)
	contentBytes, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(messages.InstallFailedReadFmt, path, err)
	}

	if errors.Is(err, os.ErrNotExist) {
		if err := fsutil.WriteFileAtomic(path, []byte(block), 0o644); err != nil {
			return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
		}
		return nil
	}

	content := normalizeGitignoreBlock(string(contentBytes))
	updated := updateGitignoreContent(content, block)
	if err := fsutil.WriteFileAtomic(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
	}
	return nil
}

func writeGitignoreBlock(path string, templatePath string, perm fs.FileMode, shouldOverwrite PromptOverwriteFunc, recordDiff func(string)) error {
	templateBytes, err := templates.Read(templatePath)
	if err != nil {
		return fmt.Errorf(messages.InstallFailedReadTemplateFmt, templatePath, err)
	}
	templateBlock := normalizeGitignoreBlock(string(templateBytes))
	rendered := renderGitignoreBlock(templateBlock)

	existingBytes, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(messages.InstallFailedReadFmt, path, err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf(messages.InstallFailedCreateDirForFmt, path, err)
		}
		if err := fsutil.WriteFileAtomic(path, []byte(rendered), perm); err != nil {
			return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
		}
		return nil
	}

	existing := normalizeGitignoreBlock(string(existingBytes))
	if existing == templateBlock || gitignoreBlockMatchesHash(existing) {
		if err := fsutil.WriteFileAtomic(path, []byte(rendered), perm); err != nil {
			return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
		}
		return nil
	}

	if shouldOverwrite != nil {
		overwrite, err := shouldOverwrite(path)
		if err != nil {
			return err
		}
		if overwrite {
			if err := fsutil.WriteFileAtomic(path, []byte(rendered), perm); err != nil {
				return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
			}
			return nil
		}
	}

	if recordDiff != nil {
		recordDiff(path)
	}
	return nil
}

func writeTemplateFile(path string, templatePath string, perm fs.FileMode, shouldOverwrite PromptOverwriteFunc, recordDiff func(string)) error {
	_, err := os.Stat(path)
	if err == nil {
		matches, err := fileMatchesTemplate(path, templatePath)
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf(messages.InstallFailedCreateDirForFmt, path, err)
	}
	if err := fsutil.WriteFileAtomic(path, data, perm); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
	}
	return nil
}

func fileMatchesTemplate(path string, templatePath string) (bool, error) {
	existing, err := os.ReadFile(path)
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
