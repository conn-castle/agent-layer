package root

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	agentLayerDir = ".agent-layer"
	gitDir        = ".git"
)

// FindAgentLayerRoot walks upward from start until it finds a directory containing .agent-layer/.
// It returns the root path, whether it was found, and any error encountered.
func FindAgentLayerRoot(start string) (string, bool, error) {
	resolved, err := resolveStartPath(start)
	if err != nil {
		return "", false, err
	}
	return findAgentLayerRoot(resolved)
}

func findAgentLayerRoot(start string) (string, bool, error) {
	dir := start
	for {
		candidate := filepath.Join(dir, agentLayerDir)
		info, err := os.Stat(candidate)
		if err == nil {
			if !info.IsDir() {
				return "", false, fmt.Errorf(messages.RootPathNotDirFmt, candidate)
			}
			return dir, true, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", false, fmt.Errorf(messages.RootCheckPathFmt, candidate, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false, nil
		}
		dir = parent
	}
}

// FindRepoRoot returns the repo root for initialization.
// It prefers an existing .agent-layer directory, then a .git directory or file, and falls back to start.
func FindRepoRoot(start string) (string, error) {
	resolved, err := resolveStartPath(start)
	if err != nil {
		return "", err
	}

	if root, found, err := findAgentLayerRoot(resolved); err != nil {
		return "", err
	} else if found {
		return root, nil
	}

	dir := resolved
	for {
		candidate := filepath.Join(dir, gitDir)
		info, err := os.Stat(candidate)
		if err == nil {
			if info.IsDir() || info.Mode().IsRegular() {
				return dir, nil
			}
			return "", fmt.Errorf(messages.RootPathNotDirOrFileFmt, candidate)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf(messages.RootCheckPathFmt, candidate, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return resolved, nil
		}
		dir = parent
	}
}

func resolveStartPath(start string) (string, error) {
	if start == "" {
		return "", fmt.Errorf(messages.RootStartPathRequired)
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf(messages.RootResolvePathFmt, start, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf(messages.RootResolvePathFmt, start, err)
	}
	return resolved, nil
}
