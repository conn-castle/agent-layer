package fsutil

import (
	"io/fs"
	"os/exec"
	"path/filepath"
)

// RequireExecutable accepts a regular file this user can execute, without opening it.
// Resolve the spelling to an absolute path so a bare name cannot select a different PATH entry.
func RequireExecutable(path string, info fs.FileInfo) error {
	if err := RequireRegularFile(info, path); err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	_, err = exec.LookPath(absolute)
	return err
}
