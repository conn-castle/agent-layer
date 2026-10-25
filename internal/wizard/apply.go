package wizard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/fatih/color"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/envfile"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skillmigration"
	"github.com/conn-castle/agent-layer/internal/sync"
)

type syncer func(ctx context.Context, root string) (*sync.Result, error)

var writeFileAtomic = fsutil.WriteFileAtomic

// applyChanges writes config/env updates and runs sync.
// root/configPath/envPath identify files; c holds wizard selections; runSync is the sync function to call; returns an error on failure.
func applyChanges(ctx context.Context, root, configPath, envPath string, c *Choices, runSync syncer, out io.Writer) (err error) {
	sourcesApplied := false
	defer func() {
		if err != nil && sourcesApplied {
			err = fmt.Errorf("import source changes were committed; projection was not completed: %w", err)
		}
	}()
	if out == nil {
		out = os.Stdout
	}

	if err := skillmigration.WithRecoveredContext(ctx, root, func() error {
		return writeConfigEnv(root, configPath, envPath, c)
	}); err != nil {
		return err
	}

	// Skills, instruction imports, and memory file seeds.
	// Run before sync so sync sees the final on-disk layout.
	skillsChangeSet, err := computeSkillsChangeSet(ctx, root, c)
	if err != nil {
		return fmt.Errorf(messages.WizardApplySkillsFailedFmt+"; config/env writes were applied; gitignore block updates, statusline source updates, and sync were not run", err)
	}
	if c.previewedSkills != nil {
		skillsChangeSet.adoptLegacy = c.previewedSkills.adoptLegacy
	}
	if err := applySkillsChanges(ctx, root, skillsChangeSet); err != nil {
		return fmt.Errorf(messages.WizardApplySkillsFailedFmt+"; config/env writes were applied; gitignore block updates, statusline source updates, and sync were not run", err)
	}
	sourcesApplied = len(skillsChangeSet.importSelectors)+len(skillsChangeSet.removeSelectors)+len(skillsChangeSet.instructionImports) > 0

	gitignoreChangeSet, err := computeGitignoreBlockChangeSet(root, c)
	if err != nil {
		return err
	}
	if err := applyGitignoreBlockChanges(root, gitignoreChangeSet); err != nil {
		return err
	}

	statuslineSourceChangeSet, err := computeStatuslineSourceChangeSet(root, c)
	if err != nil {
		return err
	}
	if err := applyStatuslineSourceChanges(root, statuslineSourceChangeSet); err != nil {
		return err
	}

	// Sync
	_, _ = fmt.Fprintln(out, messages.WizardRunningSync)
	result, err := runSync(ctx, root)
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf(messages.WizardSyncCanceledAfterWritesFmt, err)
	}
	if err != nil {
		return err
	}
	// Print any warnings from sync
	if len(result.Warnings) > 0 {
		_, _ = fmt.Fprintln(out)
		warnColor := color.New(color.FgYellow)
		for _, w := range result.Warnings {
			_, _ = warnColor.Fprintf(out, messages.WizardWarningFmt, w.Message)
		}
		_, _ = fmt.Fprintln(out)
	}
	return nil
}

func writeConfigEnv(root, configPath, envPath string, c *Choices) error {
	// Config
	rawConfig, err := os.ReadFile(configPath) // #nosec G304 -- configPath is the caller-resolved .agent-layer/config.toml path used by wizard apply.
	if err != nil {
		return err
	}
	configPerm, err := filePermOr(configPath, 0644)
	if err != nil {
		return err
	}
	// Prepare and validate the complete config before creating backups or writing.
	newConfig, err := PatchConfig(string(rawConfig), c)
	if err != nil {
		return fmt.Errorf(messages.WizardPatchConfigFailedFmt, err)
	}
	newConfig, err = config.MigrateInstructionOrderFS(os.DirFS(root), root, newConfig)
	if err != nil {
		return fmt.Errorf(messages.WizardPatchConfigFailedFmt, err)
	}
	// Env
	rawEnv, err := os.ReadFile(envPath) // #nosec G304 -- envPath is the caller-resolved .agent-layer/.env path used by wizard apply.
	envExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	envPerm, err := filePermOr(envPath, 0600)
	if err != nil {
		return err
	}
	newEnv := envfile.Patch(string(rawEnv), c.Secrets)

	// Back up and write only documents whose bytes change, so an unchanged
	// document keeps its previous recovery backup. A missing .env is created.
	// An aborted apply restores the previous backups of unchanged documents.
	configChanged := newConfig != string(rawConfig)
	envChanged := !envExists || newEnv != string(rawEnv)
	var restores []func() error
	abort := func(err error) error {
		for _, restore := range restores {
			err = errors.Join(err, restore())
		}
		return err
	}
	if configChanged {
		restore, err := backupDocument(root, configBackupName, configPath, rawConfig, configPerm)
		if err != nil {
			return fmt.Errorf(messages.WizardBackupConfigFailedFmt, err)
		}
		restores = append(restores, restore)
	}
	if envExists && envChanged {
		restore, err := backupDocument(root, envBackupName, envPath, rawEnv, envPerm)
		if err != nil {
			return abort(fmt.Errorf(messages.WizardBackupEnvFailedFmt, err))
		}
		restores = append(restores, restore)
	}
	// Patch
	if configChanged {
		if err := writeFileAtomic(configPath, []byte(newConfig), configPerm); err != nil {
			return abort(fmt.Errorf(messages.WizardWriteConfigFailedFmt, err))
		}
	}
	if envChanged {
		if err := writeFileAtomic(envPath, []byte(newEnv), envPerm); err != nil {
			if !configChanged {
				return abort(fmt.Errorf(messages.WizardWriteEnvFailedFmt, err))
			}
			if rollbackErr := writeFileAtomic(configPath, rawConfig, configPerm); rollbackErr != nil {
				return abort(fmt.Errorf(messages.WizardWriteEnvRollbackConfigFailedFmt, err, rollbackErr))
			}
			return abort(fmt.Errorf(messages.WizardWriteEnvFailedFmt, err))
		}
	}

	return nil
}

// filePermOr returns the file permission bits or a fallback when the file is missing.
// path is the file to inspect; fallback is the permission to use when the file does not exist.
func filePermOr(path string, fallback os.FileMode) (os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fallback, nil
		}
		return 0, err
	}
	return info.Mode().Perm(), nil
}

// writeBackup replaces the named wizard backup and returns a function that
// restores its previous state: the earlier regular file, or no backup.
// root is the repo root; name is the backup file name; data is the source content; perm is the file mode to apply.
func writeBackup(root, name string, data []byte, perm os.FileMode) (func() error, error) {
	if err := validateBackupStorage(root); err != nil {
		return nil, err
	}
	path := backupPath(root, name)
	if err := fsutil.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	restore := func() error {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("wizard backup %s must be a regular file", path)
		}
		previous, err := os.ReadFile(path) // #nosec G304 -- path is a fixed wizard backup path within validated backup storage.
		if err != nil {
			return nil, fmt.Errorf("read wizard backup %s: %w", path, err)
		}
		restore = func() error { return writeFileAtomic(path, previous, info.Mode().Perm()) }
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	restoreBackup := func() error {
		if err := restore(); err != nil {
			return fmt.Errorf("restore wizard backup %s: %w", path, err)
		}
		return nil
	}
	// A write can fail after its rename replaced the backup, so restore it.
	if err := writeFileAtomic(path, data, perm); err != nil {
		return nil, errors.Join(err, restoreBackup())
	}
	return restoreBackup, nil
}

// backupDocument backs up documentPath's original bytes and returns a function
// that restores the previous backup only while the document still holds those
// bytes; a document that may have changed keeps the backup of its original.
func backupDocument(root, name, documentPath string, original []byte, perm os.FileMode) (func() error, error) {
	restore, err := writeBackup(root, name, original, perm)
	if err != nil {
		return nil, err
	}
	return func() error {
		current, err := os.ReadFile(documentPath) // #nosec G304 -- documentPath is the caller-resolved wizard document being restored.
		if err == nil && bytes.Equal(current, original) {
			return restore()
		}
		return nil
	}, nil
}
