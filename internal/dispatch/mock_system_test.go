package dispatch

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// errNotMocked is returned when a testSystem method is called without a mock function set.
var errNotMocked = errors.New("testSystem: method not mocked")

// testSystem provides a mock System for unit tests.
//
// Fallback behavior:
//   - UserCacheDir, ExecBinary: Return errNotMocked (fail-fast). These operations
//     have side effects or return values that should never come from the real OS
//     in tests.
//   - ReadFile, Getenv, Environ, FindAgentLayerRoot: Fall back to RealSystem.
//     This enables tests to use t.TempDir() for filesystem fixtures and t.Setenv()
//     for environment variables without requiring explicit mocks for every call.
//   - Stat, Chmod, Rename, CreateTemp, FileSync, PlatformStrings: Fall back to
//     RealSystem. These commonly use real test fixtures (t.TempDir).
//   - Sleep: Defaults to no-op in tests to avoid slowing down test runs.
//   - HTTPClient: Falls back to RealSystem (returns the shared default client).
//   - Flock: Falls back to RealSystem (uses real unix.Flock).
//
// When adding new methods, prefer fail-fast unless the method is commonly used
// with real test fixtures (t.TempDir, t.Setenv).
type testSystem struct {
	// RealSystem is embedded for fallback behavior on methods that commonly
	// use real test fixtures. Override-able methods check their Func field
	// first and delegate to RealSystem if nil.
	RealSystem

	UserCacheDirFunc       func() (string, error)
	ReadFileFunc           func(name string) ([]byte, error)
	GetenvFunc             func(key string) string
	EnvironFunc            func() []string
	ExecBinaryFunc         func(path string, args []string, env []string, exit func(int)) error
	FindAgentLayerRootFunc func(start string) (string, bool, error)
	StderrFunc             func() io.Writer
	StatFunc               func(name string) (os.FileInfo, error)
	ChmodFunc              func(name string, mode os.FileMode) error
	RenameFunc             func(oldpath, newpath string) error
	CreateTempFunc         func(dir, pattern string) (*os.File, error)
	FileSyncFunc           func(f *os.File) error
	PlatformStringsFunc    func() (string, string, error)
	SleepFunc              func(d time.Duration)
	HTTPClientFunc         func() *http.Client
	FlockFunc              func(fd int, how int) error
}

func (s *testSystem) UserCacheDir() (string, error) {
	if s.UserCacheDirFunc != nil {
		return s.UserCacheDirFunc()
	}
	return "", fmt.Errorf("%w: UserCacheDir", errNotMocked)
}

func (s *testSystem) ReadFile(name string) ([]byte, error) {
	if s.ReadFileFunc != nil {
		return s.ReadFileFunc(name)
	}
	return s.RealSystem.ReadFile(name)
}

func (s *testSystem) Getenv(key string) string {
	if s.GetenvFunc != nil {
		return s.GetenvFunc(key)
	}
	return s.RealSystem.Getenv(key)
}

func (s *testSystem) Environ() []string {
	if s.EnvironFunc != nil {
		return s.EnvironFunc()
	}
	return s.RealSystem.Environ()
}

func (s *testSystem) ExecBinary(path string, args []string, env []string, exit func(int)) error {
	if s.ExecBinaryFunc != nil {
		return s.ExecBinaryFunc(path, args, env, exit)
	}
	return fmt.Errorf("%w: ExecBinary", errNotMocked)
}

func (s *testSystem) FindAgentLayerRoot(start string) (string, bool, error) {
	if s.FindAgentLayerRootFunc != nil {
		return s.FindAgentLayerRootFunc(start)
	}
	return s.RealSystem.FindAgentLayerRoot(start)
}

func (s *testSystem) Stderr() io.Writer {
	if s.StderrFunc != nil {
		return s.StderrFunc()
	}
	return io.Discard
}

func (s *testSystem) Stat(name string) (os.FileInfo, error) {
	if s.StatFunc != nil {
		return s.StatFunc(name)
	}
	return s.RealSystem.Stat(name)
}

func (s *testSystem) Chmod(name string, mode os.FileMode) error {
	if s.ChmodFunc != nil {
		return s.ChmodFunc(name, mode)
	}
	return s.RealSystem.Chmod(name, mode)
}

func (s *testSystem) Rename(oldpath, newpath string) error {
	if s.RenameFunc != nil {
		return s.RenameFunc(oldpath, newpath)
	}
	return s.RealSystem.Rename(oldpath, newpath)
}

func (s *testSystem) CreateTemp(dir, pattern string) (*os.File, error) {
	if s.CreateTempFunc != nil {
		return s.CreateTempFunc(dir, pattern)
	}
	return s.RealSystem.CreateTemp(dir, pattern)
}

func (s *testSystem) FileSync(f *os.File) error {
	if s.FileSyncFunc != nil {
		return s.FileSyncFunc(f)
	}
	return s.RealSystem.FileSync(f)
}

func (s *testSystem) PlatformStrings() (string, string, error) {
	if s.PlatformStringsFunc != nil {
		return s.PlatformStringsFunc()
	}
	return s.RealSystem.PlatformStrings()
}

func (s *testSystem) Sleep(d time.Duration) {
	if s.SleepFunc != nil {
		s.SleepFunc(d)
		return
	}
	// Default to no-op in tests to avoid slowing down test runs.
}

func (s *testSystem) HTTPClient() *http.Client {
	if s.HTTPClientFunc != nil {
		return s.HTTPClientFunc()
	}
	return s.RealSystem.HTTPClient()
}

func (s *testSystem) Flock(fd int, how int) error {
	if s.FlockFunc != nil {
		return s.FlockFunc(fd, how)
	}
	return s.RealSystem.Flock(fd, how)
}
