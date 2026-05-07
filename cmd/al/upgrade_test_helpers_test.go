package main

import (
	"path/filepath"
	"testing"

	alsync "github.com/conn-castle/agent-layer/internal/sync"
)

func canonicalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// stubSyncRunNoop replaces the package-level syncRun stub with a no-op for the
// duration of the test. Used when a test mocks installRun to skip real install
// work and therefore has no real `.agent-layer/config.toml` for sync to load.
func stubSyncRunNoop(t *testing.T) {
	t.Helper()
	orig := syncRun
	syncRun = func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }
	t.Cleanup(func() { syncRun = orig })
}
