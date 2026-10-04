package launchers

import (
	"os"

	"github.com/conn-castle/agent-layer/internal/fsutil"
)

// testSystem implements System using actual system calls.
type testSystem struct{}

// MkdirAll creates a directory and all parent directories.
func (testSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// WriteFileAtomic writes data to path atomically.
func (testSystem) WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	return fsutil.WriteFileAtomic(path, data, perm)
}
