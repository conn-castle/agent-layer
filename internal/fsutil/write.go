package fsutil

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/messages"
)

var (
	createTemp    = os.CreateTemp
	chmodTempFile = func(file *os.File, perm os.FileMode) error { return file.Chmod(perm) }
	writeTempFile = func(file *os.File, data []byte) (int, error) { return file.Write(data) }
	syncTempFile  = func(file *os.File) error { return file.Sync() }
	closeTempFile = func(file *os.File) error { return file.Close() }
	renameFile    = os.Rename
	syncDirFunc   = syncDir
)

// WriteFileAtomicIfChanged preserves an existing regular file when both its
// bytes and mode already match. Avoiding an otherwise-identical rename keeps
// file watchers from treating a no-op generation pass as a configuration
// change. Missing files, symlinks, directories, and mode changes still flow
// through WriteFileAtomic.
func WriteFileAtomicIfChanged(path string, data []byte, perm os.FileMode) error {
	info, err := os.Lstat(path)
	if err == nil && info.Mode().IsRegular() && info.Mode() == perm {
		existing, readErr := os.ReadFile(path) // #nosec G304 -- path is an internal write target supplied by the caller.
		if readErr != nil {
			return fmt.Errorf(messages.FsutilReadExistingFileFmt, path, readErr)
		}
		if bytes.Equal(existing, data) {
			return nil
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf(messages.FsutilCheckExistingFileFmt, path, err)
	}
	return WriteFileAtomic(path, data, perm)
}

// WriteFileAtomic writes data to path using a temp file and atomic rename.
// perm sets the file mode applied to the final file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	tmp, err := createTemp(dir, base+".tmp-*")
	if err != nil {
		return fmt.Errorf(messages.FsutilCreateTempFileFmt, path, err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	if err := chmodTempFile(tmp, perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf(messages.FsutilSetPermissionsFmt, tmpName, err)
	}
	if _, err := writeTempFile(tmp, data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf(messages.FsutilWriteTempFileFmt, path, err)
	}
	if err := syncTempFile(tmp); err != nil {
		_ = tmp.Close()
		return fmt.Errorf(messages.FsutilSyncTempFileFmt, path, err)
	}
	if err := closeTempFile(tmp); err != nil {
		return fmt.Errorf(messages.FsutilCloseTempFileFmt, path, err)
	}

	if err := renameFile(tmpName, path); err != nil {
		return fmt.Errorf(messages.FsutilRenameTempFileFmt, path, err)
	}
	committed = true

	if err := syncDirFunc(dir); err != nil {
		return err
	}

	return nil
}

// syncDir fsyncs a directory to ensure rename durability.
func syncDir(dir string) error {
	d, err := os.Open(dir) // #nosec G304 -- dir is the parent of a write target chosen by an internal caller of WriteFileAtomic; not user input.
	if err != nil {
		return fmt.Errorf(messages.FsutilOpenDirFmt, dir, err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf(messages.FsutilSyncDirFmt, dir, err)
	}
	return nil
}
