package main

import (
	"fmt"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/root"
)

// resolveRepoRoot returns the repo root that contains .agent-layer or fails if missing.
func resolveRepoRoot() (string, error) {
	cwd, err := getwd()
	if err != nil {
		return "", err
	}
	repoRoot, found, err := root.FindAgentLayerRoot(cwd)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf(messages.RootMissingAgentLayer)
	}
	return repoRoot, nil
}

// resolveInitRoot finds the best candidate root for initialization (prefers .agent-layer, then .git).
func resolveInitRoot() (string, error) {
	cwd, err := getwd()
	if err != nil {
		return "", err
	}
	return root.FindRepoRoot(cwd)
}
