package fsutil

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// ErrNotRegularFile identifies document paths that resolve to special files or directories.
var ErrNotRegularFile = errors.New(messages.FsutilNotRegularFile)

// RequireRegularFile checks the resolved file kind and names the path on failure.
func RequireRegularFile(info fs.FileInfo, path string) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf(messages.FsutilRequireRegularFileFmt, path, ErrNotRegularFile)
	}
	return nil
}

// OpenRegularFile follows symlinks and refuses non-regular targets before reading.
func OpenRegularFile(path string) (*os.File, error) {
	file, err := openRegularFile(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil {
		err = RequireRegularFile(info, path)
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// ReadRegularFile reads a regular file without blocking on a FIFO's open.
func ReadRegularFile(path string) ([]byte, error) {
	file, err := OpenRegularFile(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(file)
}
