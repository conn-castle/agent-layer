package main

import (
	"fmt"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/root"
)

var (
	findAgentLayerRoot = root.FindAgentLayerRoot
	findRepoRoot       = root.FindRepoRoot
)

// resolveRepoRoot returns the repo root that contains .agent-layer or fails if missing.
func resolveRepoRoot() (string, error) {
	cwd, err := getwd()
	if err != nil {
		return "", err
	}
	repoRoot, found, err := findAgentLayerRoot(cwd)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf(messages.RootMissingAgentLayer)
	}
	return repoRoot, nil
}

// resolveInitRoot finds the candidate root for initialization and returns it alongside the absolute cwd.
// When here is true the absolute cwd is returned verbatim so users can install in a subfolder of an
// existing agent-layer or git repo; otherwise the closest ancestor with .agent-layer/ (preferred) or
// .git is returned, falling back to cwd.
func resolveInitRoot(here bool) (root string, cwd string, err error) {
	wd, err := getwd()
	if err != nil {
		return "", "", err
	}
	absCwd, err := filepath.Abs(wd)
	if err != nil {
		return "", "", fmt.Errorf(messages.RootResolvePathFmt, wd, err)
	}
	if here {
		return absCwd, absCwd, nil
	}
	root, err = findRepoRoot(wd)
	if err != nil {
		return "", "", err
	}
	return root, absCwd, nil
}
