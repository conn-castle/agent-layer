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
	logical, physical, err := resolveStartPaths(start)
	if err != nil {
		return "", false, err
	}
	for _, candidate := range distinctStartPaths(logical, physical) {
		root, found, err := findAgentLayerRoot(candidate)
		if err != nil || !found {
			if err != nil {
				return "", false, err
			}
			continue
		}
		resolved, err := resolveFoundRoot(start, root)
		return resolved, true, err
	}
	return "", false, nil
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
	logical, physical, err := resolveStartPaths(start)
	if err != nil {
		return "", err
	}
	starts := distinctStartPaths(logical, physical)

	for _, candidate := range starts {
		root, found, err := findAgentLayerRoot(candidate)
		if err != nil {
			return "", err
		}
		if found {
			return resolveFoundRoot(start, root)
		}
	}

	for _, candidate := range starts {
		root, found, err := findGitRoot(candidate)
		if err != nil {
			return "", err
		}
		if found {
			return resolveFoundRoot(start, root)
		}
	}
	return physical, nil
}

func findGitRoot(start string) (string, bool, error) {
	dir := start
	for {
		candidate := filepath.Join(dir, gitDir)
		info, err := os.Stat(candidate)
		if err == nil {
			if info.IsDir() || info.Mode().IsRegular() {
				return dir, true, nil
			}
			return "", false, fmt.Errorf(messages.RootPathNotDirOrFileFmt, candidate)
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

func resolveStartPaths(start string) (string, string, error) {
	if start == "" {
		return "", "", fmt.Errorf(messages.RootStartPathRequired)
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", "", fmt.Errorf(messages.RootResolvePathFmt, start, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", "", fmt.Errorf(messages.RootResolvePathFmt, start, err)
	}
	return abs, resolved, nil
}

func distinctStartPaths(logical string, physical string) []string {
	if logical == physical {
		return []string{logical}
	}
	return []string{logical, physical}
}

func resolveFoundRoot(start string, root string) (string, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf(messages.RootResolvePathFmt, start, err)
	}
	return resolved, nil
}
