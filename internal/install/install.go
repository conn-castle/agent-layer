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

	"github.com/conn-castle/agent-layer/internal/launchers"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/version"
)

// PromptOverwriteFunc asks whether to overwrite a given path.
type PromptOverwriteFunc func(path string) (bool, error)

// PromptDeleteUnknownAllFunc asks whether to delete all unknown paths.
type PromptDeleteUnknownAllFunc func(paths []string) (bool, error)

// PromptDeleteUnknownFunc asks whether to delete a specific unknown path.
type PromptDeleteUnknownFunc func(path string) (bool, error)

// Options controls installer behavior.
type Options struct {
	Overwrite    bool
	Force        bool
	Prompter     Prompter
	WarnWriter   io.Writer
	PinVersion   string
	DiffMaxLines int
	System       System
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
	diffMaxLines              int
	managedDiffPreviews       map[string]DiffPreview
	memoryDiffPreviews        map[string]DiffPreview
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
		root:         root,
		overwrite:    overwrite,
		force:        opts.Force,
		prompter:     opts.Prompter,
		warnWriter:   warnWriter,
		diffMaxLines: normalizeDiffMaxLines(opts.DiffMaxLines),
		sys:          sys,
	}
	if strings.TrimSpace(opts.PinVersion) != "" {
		normalized, err := version.Normalize(opts.PinVersion)
		if err != nil {
			return fmt.Errorf(messages.InstallInvalidPinVersionFmt, err)
		}
		inst.pinVersion = normalized
	}
	preTransactionSteps := []func() error{
		inst.createDirs,
		inst.scanUnknowns,
	}
	if err := runSteps(preTransactionSteps); err != nil {
		return err
	}
	if overwrite {
		snapshot, err := inst.createUpgradeSnapshot()
		if err != nil {
			return err
		}
		if err := inst.runUpgradeTransaction(&snapshot); err != nil {
			return err
		}
	} else {
		steps := []func() error{
			inst.writeVersionFile,
			inst.writeTemplateFiles,
			inst.writeTemplateDirs,
			inst.updateGitignore,
			inst.writeVSCodeLaunchers,
			inst.handleUnknowns,
		}
		if err := runSteps(steps); err != nil {
			return err
		}
	}
	baselineSource := BaselineStateSourceWrittenByInit
	if overwrite {
		baselineSource = BaselineStateSourceWrittenByUpgrade
	}
	if err := inst.writeManagedBaselineIfConsistent(baselineSource); err != nil {
		return err
	}

	inst.warnDifferences()
	inst.warnUnknowns()
	return nil
}

type transactionStep struct {
	name            string
	run             func() error
	rollbackTargets func() []string
}

func (inst *installer) runUpgradeTransaction(snapshot *upgradeSnapshot) error {
	steps := []transactionStep{
		{name: "writeVersionFile", run: inst.writeVersionFile, rollbackTargets: inst.writeVersionFileTargetPaths},
		{name: "writeTemplateFiles", run: inst.writeTemplateFiles, rollbackTargets: inst.writeTemplateFilesTargetPaths},
		{name: "writeTemplateDirs", run: inst.writeTemplateDirs, rollbackTargets: inst.writeTemplateDirsTargetPaths},
		{name: "updateGitignore", run: inst.updateGitignore, rollbackTargets: inst.updateGitignoreTargetPaths},
		{name: "writeVSCodeLaunchers", run: inst.writeVSCodeLaunchers, rollbackTargets: inst.writeVSCodeLaunchersTargetPaths},
		{name: "handleUnknowns", run: inst.handleUnknowns, rollbackTargets: inst.handleUnknownsTargetPaths},
	}
	completedTargets := make(map[string]struct{})
	for _, step := range steps {
		currentStepTargets := step.rollbackTargets()
		if err := step.run(); err != nil {
			snapshot.Status = upgradeSnapshotStatusAutoRolledBack
			snapshot.FailureStep = step.name
			snapshot.FailureError = err.Error()

			rollbackTargets := mergeRollbackTargets(completedTargets, currentStepTargets)
			rollbackErr := inst.rollbackUpgradeSnapshot(*snapshot, rollbackTargets)
			if rollbackErr != nil {
				snapshot.Status = upgradeSnapshotStatusRollbackFailed
				if writeErr := inst.writeUpgradeSnapshot(*snapshot, false); writeErr != nil {
					return fmt.Errorf("upgrade step %s failed: %w; rollback failed: %v; failed to write snapshot state: %v", step.name, err, rollbackErr, writeErr)
				}
				_, _ = fmt.Fprintf(inst.warnOutput(), messages.InstallUpgradeSnapshotRollbackFailedFmt, step.name, snapshot.SnapshotID, rollbackErr)
				return fmt.Errorf("upgrade step %s failed: %w; rollback failed: %v", step.name, err, rollbackErr)
			}
			if writeErr := inst.writeUpgradeSnapshot(*snapshot, false); writeErr != nil {
				return fmt.Errorf("upgrade step %s failed: %w; rollback succeeded; failed to write snapshot state: %v", step.name, err, writeErr)
			}
			_, _ = fmt.Fprintf(inst.warnOutput(), messages.InstallUpgradeSnapshotRolledBackFmt, step.name, snapshot.SnapshotID)
			return fmt.Errorf("upgrade step %s failed: %w", step.name, err)
		}
		for _, path := range currentStepTargets {
			completedTargets[filepath.Clean(path)] = struct{}{}
		}
	}

	snapshot.Status = upgradeSnapshotStatusApplied
	snapshot.FailureStep = ""
	snapshot.FailureError = ""
	if err := inst.writeUpgradeSnapshot(*snapshot, false); err != nil {
		return fmt.Errorf("mark upgrade snapshot %s as applied: %w", snapshot.SnapshotID, err)
	}
	return nil
}

func mergeRollbackTargets(completed map[string]struct{}, current []string) []string {
	targets := make(map[string]struct{}, len(completed)+len(current))
	for path := range completed {
		targets[path] = struct{}{}
	}
	for _, path := range current {
		targets[filepath.Clean(path)] = struct{}{}
	}
	out := make([]string, 0, len(targets))
	for path := range targets {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
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

func (inst *installer) writeVSCodeLaunchers() error {
	return launchers.WriteVSCodeLaunchers(inst.sys, inst.root)
}

func (inst *installer) writeTemplateFiles() error {
	// User-owned required files: seed only when missing; never overwrite.
	for _, file := range inst.userOwnedSeedFiles() {
		if err := writeTemplateIfMissing(inst.sys, file.path, file.template, file.perm); err != nil {
			return err
		}
	}

	// Agent-owned internal files: always overwrite to enforce safety invariants.
	alwaysOverwrite := func(string) (bool, error) { return true, nil }
	for _, file := range inst.agentOnlyFiles() {
		if err := writeTemplateFile(inst.sys, file.path, file.template, file.perm, alwaysOverwrite, nil); err != nil {
			return err
		}
	}

	// Upgrade-managed files: overwrite behavior is controlled by init/upgrade flags.
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
// Empty or corrupt (non-semver) pin files are auto-repaired without requiring --overwrite.
// Valid semver pins that differ from the target still require --overwrite.
func (inst *installer) writeVersionFile() error {
	if inst.pinVersion == "" {
		return nil
	}
	sys := inst.sys
	path := filepath.Join(inst.root, ".agent-layer", "al.version")
	existingBytes, err := sys.ReadFile(path)
	if err == nil {
		existing := strings.TrimSpace(string(existingBytes))
		normalized, normErr := version.Normalize(existing)
		switch {
		case existing == "" || normErr != nil:
			// Empty or corrupt pin file: auto-repair by falling through to write.
			_, _ = fmt.Fprintf(inst.warnOutput(), messages.InstallAutoRepairPinWarningFmt, path, existing, inst.pinVersion)
		case normalized == inst.pinVersion:
			return nil
		default:
			// Valid semver that differs: preserve current overwrite semantics.
			overwrite, err := inst.shouldOverwrite(path)
			if err != nil {
				return err
			}
			if !overwrite {
				inst.recordDiff(path)
				return nil
			}
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
