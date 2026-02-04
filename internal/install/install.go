package install

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/version"
)

// PromptOverwriteFunc asks whether to overwrite a given path.
type PromptOverwriteFunc func(path string) (bool, error)

// PromptOverwriteAllFunc asks whether to overwrite all managed files.
// paths contains the relative files that differ from templates.
type PromptOverwriteAllFunc func(paths []string) (bool, error)

// PromptDeleteUnknownAllFunc asks whether to delete all unknown paths.
type PromptDeleteUnknownAllFunc func(paths []string) (bool, error)

// PromptDeleteUnknownFunc asks whether to delete a specific unknown path.
type PromptDeleteUnknownFunc func(path string) (bool, error)

// Options controls installer behavior.
type Options struct {
	Overwrite  bool
	Force      bool
	Prompter   Prompter
	WarnWriter io.Writer
	PinVersion string
	System     System
}

type installer struct {
	root                      string
	overwrite                 bool
	overwriteAll              bool
	overwriteAllDecided       bool
	overwriteMemoryAll        bool
	overwriteMemoryAllDecided bool
	force                     bool
	prompter                  Prompter
	warnWriter                io.Writer
	diffs                     []string
	unknowns                  []string
	pinVersion                string
	templateEntries           map[string][]templateEntry
	templateMatchCache        map[string]matchCacheEntry
	sys                       System
}

type templateFile struct {
	path     string
	template string
	perm     fs.FileMode
}

type templateEntry struct {
	templatePath string
	destPath     string
	perm         fs.FileMode
}

type templateDir struct {
	templateRoot string
	destRoot     string
}

type matchCacheEntry struct {
	matches bool
	size    int64
	modTime int64
}

// Run initializes the repository with the required Agent Layer structure.
func Run(root string, opts Options) error {
	if root == "" {
		return fmt.Errorf(messages.InstallRootRequired)
	}

	overwrite := opts.Overwrite || opts.Force
	if err := validatePrompter(opts.Prompter, overwrite, opts.Force); err != nil {
		return err
	}

	sys := opts.System
	if sys == nil {
		return fmt.Errorf(messages.InstallSystemRequired)
	}
	warnWriter := opts.WarnWriter
	if warnWriter == nil {
		warnWriter = os.Stderr
	}
	inst := &installer{
		root:       root,
		overwrite:  overwrite,
		force:      opts.Force,
		prompter:   opts.Prompter,
		warnWriter: warnWriter,
		sys:        sys,
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
		inst.writeVSCodeLaunchers,
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

func validatePrompter(prompter Prompter, overwrite bool, force bool) error {
	if !overwrite || force {
		return nil
	}
	if prompter == nil {
		return fmt.Errorf(messages.InstallOverwritePromptRequired)
	}
	if validator, ok := prompter.(promptValidator); ok {
		if !validator.hasOverwriteAll() {
			return fmt.Errorf(messages.InstallOverwritePromptRequired)
		}
		if !validator.hasOverwriteAllMemory() {
			return fmt.Errorf(messages.InstallOverwritePromptRequired)
		}
		if !validator.hasOverwrite() {
			return fmt.Errorf(messages.InstallOverwritePromptRequired)
		}
		if !validator.hasDeleteUnknownAll() {
			return fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
		}
		if !validator.hasDeleteUnknown() {
			return fmt.Errorf(messages.InstallDeleteUnknownPromptRequired)
		}
	}
	return nil
}

func (inst *installer) createDirs() error {
	root := inst.root
	sys := inst.sys
	dirs := []string{
		filepath.Join(root, ".agent-layer", "instructions"),
		filepath.Join(root, ".agent-layer", "slash-commands"),
		filepath.Join(root, ".agent-layer", "templates", "docs"),
		filepath.Join(root, ".agent-layer", "tmp", "runs"),
		filepath.Join(root, "docs", "agent-layer"),
	}
	for _, dir := range dirs {
		if err := sys.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf(messages.InstallCreateDirFailedFmt, dir, err)
		}
	}
	return nil
}

func (inst *installer) writeTemplateFiles() error {
	for _, file := range inst.managedTemplateFiles() {
		if file.template == templateGitignoreBlock {
			if err := writeGitignoreBlock(inst.sys, file.path, file.template, file.perm, inst.shouldOverwrite, inst.recordDiff); err != nil {
				return err
			}
			continue
		}
		if err := writeTemplateFileWithMatch(inst.sys, file.path, file.template, file.perm, inst.shouldOverwrite, inst.recordDiff, inst.matchTemplate); err != nil {
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
	sys := inst.sys
	path := filepath.Join(inst.root, ".agent-layer", "al.version")
	existingBytes, err := sys.ReadFile(path)
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

	if err := sys.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf(messages.InstallFailedCreateDirForFmt, path, err)
	}
	content := []byte(inst.pinVersion + "\n")
	if err := sys.WriteFileAtomic(path, content, 0o644); err != nil {
		return fmt.Errorf(messages.InstallFailedWriteFmt, path, err)
	}
	return nil
}

func (inst *installer) writeTemplateDirs() error {
	for _, dir := range inst.allTemplateDirs() {
		if err := inst.writeTemplateDirCached(dir); err != nil {
			return err
		}
	}
	return nil
}

func (inst *installer) recordDiff(path string) {
	inst.diffs = append(inst.diffs, path)
}

func (inst *installer) recordUnknown(path string) {
	inst.unknowns = append(inst.unknowns, path)
}

func (inst *installer) warnOutput() io.Writer {
	if inst.warnWriter != nil {
		return inst.warnWriter
	}
	return os.Stderr
}

func (inst *installer) warnDifferences() {
	if inst.overwrite || len(inst.diffs) == 0 {
		return
	}

	sort.Strings(inst.diffs)
	out := inst.warnOutput()
	// NOTE: Errors from warning-output writes are intentionally discarded. These
	// are non-critical warning messages; failing to display a warning should not
	// abort the operation or propagate an error to the caller.
	_, _ = fmt.Fprintln(out, messages.InstallDiffHeader)
	for _, path := range inst.diffs {
		rel, err := filepath.Rel(inst.root, path)
		if err != nil {
			rel = path
		}
		_, _ = fmt.Fprintf(out, messages.InstallDiffLineFmt, rel)
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, messages.InstallDiffFooter)
	_, _ = fmt.Fprintln(out)
}

func (inst *installer) warnUnknowns() {
	if inst.overwrite || len(inst.unknowns) == 0 {
		return
	}

	inst.sortUnknowns()
	out := inst.warnOutput()
	_, _ = fmt.Fprintln(out, messages.InstallUnknownHeader)
	for _, path := range inst.unknowns {
		rel := inst.relativePath(path)
		_, _ = fmt.Fprintf(out, messages.InstallDiffLineFmt, rel)
	}
	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, messages.InstallUnknownFooter)
	_, _ = fmt.Fprintln(out)
}

// writeVSCodeLaunchers generates VS Code launcher files during installation.
// These launchers are always created because VS Code is enabled by default in the config template.
func (inst *installer) writeVSCodeLaunchers() error {
	adapter := &systemAdapter{sys: inst.sys}
	return sync.WriteVSCodeLaunchers(adapter, inst.root)
}

// systemAdapter adapts install.System to sync.System interface.
type systemAdapter struct {
	sys System
}

func (a *systemAdapter) LookPath(file string) (string, error) {
	// Not needed for WriteVSCodeLaunchers, but required by sync.System interface
	return "", fmt.Errorf("LookPath is not supported during installation")
}

func (a *systemAdapter) Stat(name string) (os.FileInfo, error) {
	return a.sys.Stat(name)
}

func (a *systemAdapter) MkdirAll(path string, perm os.FileMode) error {
	return a.sys.MkdirAll(path, perm)
}

func (a *systemAdapter) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	return a.sys.WriteFileAtomic(filename, data, perm)
}

func (a *systemAdapter) MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	// Not needed for WriteVSCodeLaunchers, but required by sync.System interface
	return nil, fmt.Errorf("MarshalIndent is not supported during installation")
}

func (a *systemAdapter) ReadFile(name string) ([]byte, error) {
	return a.sys.ReadFile(name)
}
