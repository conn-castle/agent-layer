package dispatch

import (
	"io"
	"net/http"
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/unix"

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
	Stat(name string) (os.FileInfo, error)
	Chmod(name string, mode os.FileMode) error
	Rename(oldpath, newpath string) error
	CreateTemp(dir, pattern string) (*os.File, error)
	FileSync(f *os.File) error
	PlatformStrings() (string, string, error)
	Sleep(d time.Duration)
	HTTPClient() *http.Client
	Flock(fd int, how int) error
}

// defaultHTTPClient is the shared HTTP client for production use.
// It is created once and reused across calls for connection pooling.
var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

// RealSystem implements System using the OS and root finder.
type RealSystem struct {
	StderrWriter io.Writer
}

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
func (r RealSystem) Stderr() io.Writer {
	if r.StderrWriter != nil {
		return r.StderrWriter
	}
	return os.Stderr
}

// Stat returns a FileInfo describing the named file.
func (RealSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// Chmod changes the mode of the named file.
func (RealSystem) Chmod(name string, mode os.FileMode) error {
	return os.Chmod(name, mode)
}

// Rename renames (moves) oldpath to newpath.
func (RealSystem) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

// CreateTemp creates a new temporary file in the directory dir.
func (RealSystem) CreateTemp(dir, pattern string) (*os.File, error) {
	return os.CreateTemp(dir, pattern)
}

// FileSync commits the current contents of the file to stable storage.
func (RealSystem) FileSync(f *os.File) error {
	return f.Sync()
}

// PlatformStrings returns the supported OS and architecture strings for release assets.
func (RealSystem) PlatformStrings() (string, string, error) {
	return checkPlatform(runtime.GOOS, runtime.GOARCH)
}

// Sleep pauses the current goroutine for at least the duration d.
func (RealSystem) Sleep(d time.Duration) {
	time.Sleep(d)
}

// HTTPClient returns the shared HTTP client for download operations.
func (RealSystem) HTTPClient() *http.Client {
	return defaultHTTPClient
}

// Flock applies or removes an advisory lock on the file represented by fd.
func (RealSystem) Flock(fd int, how int) error {
	return unix.Flock(fd, how)
}
