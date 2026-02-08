package dispatch

import (
	"errors"
	"fmt"
	"io"
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
