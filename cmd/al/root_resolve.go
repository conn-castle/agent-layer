package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
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

// resolveRepoRootForPromptServer resolves the repo root for mcp-prompts.
// It prefers AL_REPO_ROOT when set so MCP clients do not depend on their launch cwd.
func resolveRepoRootForPromptServer() (string, error) {
	if hint := strings.TrimSpace(os.Getenv(config.BuiltinRepoRootEnvVar)); hint != "" {
		repoRoot, found, err := root.FindAgentLayerRoot(hint)
		if err != nil {
			return "", err
		}
		if !found {
			return "", fmt.Errorf(messages.RootMissingAgentLayer)
		}
		return repoRoot, nil
	}
	return resolveRepoRoot()
}

// resolveInitRoot finds the best candidate root for initialization (prefers .agent-layer, then .git).
func resolveInitRoot() (string, error) {
	cwd, err := getwd()
	if err != nil {
		return "", err
	}
	return root.FindRepoRoot(cwd)
}
