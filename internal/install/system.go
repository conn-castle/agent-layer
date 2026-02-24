package install

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/fsutil"
)

// System abstracts filesystem operations needed by the installer.
// This interface is intentionally package-local per Decision edefea6 to enable
// parallel-safe unit tests without shared global state. Other packages (dispatch,
// sync) define their own System interfaces with operations specific to their needs.
type System interface {
	Lstat(name string) (os.FileInfo, error)
	Stat(name string) (os.FileInfo, error)
	ReadFile(name string) ([]byte, error)
	Readlink(name string) (string, error)
	LookupEnv(key string) (string, bool)
	MkdirAll(path string, perm os.FileMode) error
	RemoveAll(path string) error
	Rename(oldpath string, newpath string) error
	Symlink(oldname string, newname string) error
	WalkDir(root string, fn fs.WalkDirFunc) error
	WriteFileAtomic(filename string, data []byte, perm os.FileMode) error
}

// RealSystem implements System using the OS filesystem.
type RealSystem struct{}

// Lstat returns a FileInfo describing the named file without following symlinks.
func (RealSystem) Lstat(name string) (os.FileInfo, error) {
	return os.Lstat(name)
}

// Stat returns a FileInfo describing the named file.
func (RealSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// ReadFile reads the named file and returns the contents.
func (RealSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// Readlink returns the destination of a symbolic link.
func (RealSystem) Readlink(name string) (string, error) {
	return os.Readlink(name)
}

// LookupEnv returns the value and presence of an environment variable.
func (RealSystem) LookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}

// MkdirAll creates a directory named path, along with any necessary parents.
func (RealSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// RemoveAll removes path and any children it contains.
func (RealSystem) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// Rename renames (moves) oldpath to newpath.
func (RealSystem) Rename(oldpath string, newpath string) error {
	return os.Rename(oldpath, newpath)
}

// Symlink creates newname as a symbolic link to oldname.
func (RealSystem) Symlink(oldname string, newname string) error {
	return os.Symlink(oldname, newname)
}

// WalkDir walks the file tree rooted at root.
func (RealSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, fn)
}

// WriteFileAtomic writes data to a file atomically by writing to a temp file and renaming.
func (RealSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	return fsutil.WriteFileAtomic(filename, data, perm)
}
