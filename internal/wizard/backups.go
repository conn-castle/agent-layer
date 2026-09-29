package wizard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/conn-castle/agent-layer/internal/fsutil"
)

const (
	configBackupName = "config.toml.bak"
	envBackupName    = ".env.bak"
)

func backupPath(root, name string) string {
	return filepath.Join(root, ".agent-layer", "state", "wizard-backups", name)
}

func isBackupName(name string) bool {
	for _, base := range []string{configBackupName, envBackupName} {
		if name == base {
			return true
		}
		if suffix, ok := strings.CutPrefix(name, base+".legacy-"); ok {
			n, err := strconv.Atoi(suffix)
			if err == nil && n > 0 && strconv.Itoa(n) == suffix {
				return true
			}
		}
	}
	return false
}

// MigrateBackups relocates legacy wizard backups without overwriting existing
// recovery files. Copying exclusively works across filesystems; the source is
// removed only after the destination has been written, synced, and closed.
func MigrateBackups(root string) error {
	for _, name := range []string{configBackupName, envBackupName} {
		source := filepath.Join(root, ".agent-layer", name)
		info, err := os.Lstat(source)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect wizard backup %s: %w", source, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("wizard backup %s must be a regular file", source)
		}

		data, err := os.ReadFile(source) // #nosec G304 -- source is a fixed legacy backup path within the repository.
		if err != nil {
			return fmt.Errorf("read wizard backup %s: %w", source, err)
		}
		target := backupPath(root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return fmt.Errorf("create wizard backup directory: %w", err)
		}
		for n := 0; ; n++ {
			destination := target
			if n > 0 {
				destination = fmt.Sprintf("%s.legacy-%d", target, n)
			}
			if err := copyBackupExclusive(destination, data, info.Mode().Perm()); err != nil {
				if os.IsExist(err) {
					continue
				}
				return fmt.Errorf("migrate wizard backup %s to %s: %w", source, destination, err)
			}
			if err := os.Remove(source); err != nil {
				return fmt.Errorf("remove migrated wizard backup %s (preserved at %s): %w", source, destination, err)
			}
			break
		}
	}
	return nil
}

// copyBackupExclusive never replaces an existing destination. A failed copy
// removes only the file it created, leaving the source available for retry.
func copyBackupExclusive(destination string, data []byte, perm os.FileMode) (err error) {
	// #nosec G304 -- destination is a generated backup path within the repository.
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			if removeErr := os.Remove(destination); removeErr != nil {
				err = errors.Join(err, fmt.Errorf("remove incomplete backup %s: %w", destination, removeErr))
			}
		}
	}()
	if err = file.Chmod(perm); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	complete = true
	return fsutil.SyncDir(filepath.Dir(destination))
}
