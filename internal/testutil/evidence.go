package testutil

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// SnapshotEvidence records bytes, modes and link targets without following links.
// Fixtures must have no concurrent writers while their evidence is captured.
func SnapshotEvidence(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, root := range roots {
		require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			value := info.Mode().String()
			switch {
			case info.Mode().IsRegular():
				data, err := os.ReadFile(path) // #nosec G304 G122 -- stable test-owned fixture; WalkDir skips links.
				if err != nil {
					return err
				}
				value += ":" + string(data)
			case info.Mode()&os.ModeSymlink != 0:
				target, err := os.Readlink(path)
				if err != nil {
					return err
				}
				value += ":" + target
			}
			result[path] = value
			return nil
		}))
	}
	return result
}
