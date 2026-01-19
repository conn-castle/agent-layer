package sync

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

var lookPath = exec.LookPath

// resolvePromptServerCommand returns the command and args used to run the internal MCP prompt server.
// The root argument is the repo root; it prefers "./al mcp-prompts" and falls back to "go run <root>/cmd/al mcp-prompts".
// It returns an error when it cannot resolve a runnable command.
func resolvePromptServerCommand(root string) (string, []string, error) {
	if root == "" {
		return "./al", []string{"mcp-prompts"}, nil
	}

	alPath := filepath.Join(root, "al")
	if _, err := os.Stat(alPath); err == nil {
		return "./al", []string{"mcp-prompts"}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", nil, fmt.Errorf("check %s: %w", alPath, err)
	}

	sourcePath := filepath.Join(root, "cmd", "al")
	info, err := os.Stat(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf("missing prompt server source at %s", sourcePath)
		}
		return "", nil, fmt.Errorf("check %s: %w", sourcePath, err)
	}
	if !info.IsDir() {
		return "", nil, fmt.Errorf("prompt server source path %s is not a directory", sourcePath)
	}

	if _, err := lookPath("go"); err != nil {
		return "", nil, fmt.Errorf("missing go on PATH for prompt server: %w", err)
	}

	return "go", []string{"run", sourcePath, "mcp-prompts"}, nil
}
