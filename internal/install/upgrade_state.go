package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// validateUpgradeStateDirs inspects AL-owned storage without following its
// directory links. Missing directories are valid; planning must not create them.
// Project-root and OS ancestors may be links, as may unrelated user-owned paths.
func validateUpgradeStateDirs(root string, sys System) error {
	for _, rel := range []string{".agent-layer", ".agent-layer/state", upgradeSnapshotDirRelPath} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := sys.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect upgrade state directory %s: %w", rel, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("upgrade state directory %s is not a real directory", rel)
		}
	}
	return nil
}
