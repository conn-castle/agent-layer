// Package projectpaths defines the local storage layout for Agent Layer projects.
package projectpaths

//go:generate go run -tags tools ../tools/projectlayout -root ../..

import (
	"path/filepath"
	"strings"
)

// Project-relative storage directories.
const (
	Working    = ".agent-layer/working"
	Cache      = ".agent-layer/cache"
	Worktrees  = ".agent-layer/worktrees"
	Runs       = ".agent-layer/state/runs"
	LegacyTmp  = ".agent-layer/tmp"
	LegacyRuns = LegacyTmp + "/runs"
)

// Contains reports whether a path equals or descends from the specified directory.
func Contains(dir, path string) bool {
	dir = filepath.Clean(dir)
	path = filepath.Clean(path)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}
