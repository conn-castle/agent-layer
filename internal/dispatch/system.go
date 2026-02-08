package dispatch

import (
	"io"
	"os"

	"github.com/conn-castle/agent-layer/internal/root"
)

// System abstracts OS operations needed by version dispatch.
// This interface is intentionally package-local per Decision edefea6 to enable
// parallel-safe unit tests without shared global state. Other packages (install,
// sync) define their own System interfaces with operations specific to their needs.
type System interface {
	UserCacheDir() (string, error)
	ReadFile(name string) ([]byte, error)
	Getenv(key string) string
	Environ() []string
	ExecBinary(path string, args []string, env []string, exit func(int)) error
	FindAgentLayerRoot(start string) (string, bool, error)
	Stderr() io.Writer
}

// RealSystem implements System using the OS and root finder.
type RealSystem struct{}

// UserCacheDir returns the default user cache directory.
func (RealSystem) UserCacheDir() (string, error) {
	return os.UserCacheDir()
}

// ReadFile reads the named file and returns the contents.
func (RealSystem) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

// Getenv returns the value of the environment variable named by key.
func (RealSystem) Getenv(key string) string {
	return os.Getenv(key)
}

// Environ returns a copy of strings representing the environment.
func (RealSystem) Environ() []string {
	return os.Environ()
}

// ExecBinary replaces the current process with the provided binary.
func (RealSystem) ExecBinary(path string, args []string, env []string, exit func(int)) error {
	return execBinary(path, args, env, exit)
}

// FindAgentLayerRoot searches upwards from start for an .agent-layer directory.
func (RealSystem) FindAgentLayerRoot(start string) (string, bool, error) {
	return root.FindAgentLayerRoot(start)
}

// Stderr returns the standard error writer.
func (RealSystem) Stderr() io.Writer {
	return os.Stderr
}
