package sync

import (
	"encoding/json"
	"os"
	"os/exec"

	"github.com/conn-castle/agent-layer/internal/fsutil"
)

// System abstracts system-level operations to enable dependency injection in sync logic.
type System interface {
	LookPath(file string) (string, error)
	Stat(name string) (os.FileInfo, error)
	Lstat(name string) (os.FileInfo, error)
	Readlink(name string) (string, error)
	MkdirAll(path string, perm os.FileMode) error
	WriteFileAtomic(filename string, data []byte, perm os.FileMode) error
	MarshalIndent(v any, prefix, indent string) ([]byte, error)
	ReadFile(name string) ([]byte, error)
	ReadDir(name string) ([]os.DirEntry, error)
	Remove(name string) error
	RemoveAll(path string) error
}

// RealSystem implements System using actual system calls.
type RealSystem struct{}

// LookPath searches for an executable named file in the directories named by the PATH environment variable.
func (RealSystem) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// Stat returns a FileInfo describing the named file.
func (RealSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// Lstat returns a FileInfo describing the named file without following symlinks.
func (RealSystem) Lstat(name string) (os.FileInfo, error) {
	return os.Lstat(name)
}

// Readlink returns the destination of a symbolic link.
func (RealSystem) Readlink(name string) (string, error) {
	return os.Readlink(name)
}

// MkdirAll creates a directory named path, along with any necessary parents.
func (RealSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// WriteFileAtomic writes data to a file atomically by writing to a temp file and renaming.
func (RealSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	return fsutil.WriteFileAtomic(filename, data, perm)
}

// MarshalIndent returns the JSON encoding of v with indentation.
func (RealSystem) MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(v, prefix, indent)
}

// ReadFile reads the named file and returns the contents.
func (RealSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name) // #nosec G304 -- pass-through wrapper for the System abstraction; callers (sync internals) are responsible for the path origin.
}

// ReadDir reads the named directory and returns all directory entries.
func (RealSystem) ReadDir(name string) ([]os.DirEntry, error) {
	return os.ReadDir(name)
}

// Remove removes the named file or empty directory.
func (RealSystem) Remove(name string) error {
	return os.Remove(name)
}

// RemoveAll removes path and any children it contains.
func (RealSystem) RemoveAll(path string) error {
	return os.RemoveAll(path)
}
