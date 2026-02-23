package sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// resolvePromptServerCommand returns the command and args used to run the internal MCP prompt server.
// It prefers "go run <root>/cmd/al mcp-prompts" when local source is present,
// then falls back to globally installed "al mcp-prompts".
// It returns an error when it cannot resolve a runnable command.
func resolvePromptServerCommand(sys System, root string) (string, []string, error) {
	if root != "" {
		sourcePath := filepath.Join(root, "cmd", "al")
		info, err := sys.Stat(sourcePath)
		if err == nil {
			if !info.IsDir() {
				return "", nil, fmt.Errorf(messages.SyncPromptServerNotDirFmt, sourcePath)
			}
			if _, err := sys.LookPath("go"); err == nil {
				return "go", []string{"run", sourcePath, "mcp-prompts"}, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf(messages.SyncCheckPathFmt, sourcePath, err)
		}
	}

	if _, err := sys.LookPath("al"); err == nil {
		return "al", []string{"mcp-prompts"}, nil
	}

	if root == "" {
		return "", nil, fmt.Errorf(messages.SyncMissingPromptServerNoRoot)
	}

	sourcePath := filepath.Join(root, "cmd", "al")
	info, err := sys.Stat(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf(messages.SyncMissingPromptServerSourceFmt, sourcePath)
		}
		return "", nil, fmt.Errorf(messages.SyncCheckPathFmt, sourcePath, err)
	}
	if !info.IsDir() {
		return "", nil, fmt.Errorf(messages.SyncPromptServerNotDirFmt, sourcePath)
	}

	if _, err := sys.LookPath("go"); err != nil {
		return "", nil, fmt.Errorf(messages.SyncMissingGoForPromptServerFmt, err)
	}

	return "go", []string{"run", sourcePath, "mcp-prompts"}, nil
}

// resolvePromptServerEnv returns deterministic env vars for the internal MCP prompt server.
func resolvePromptServerEnv(root string) (OrderedMap[string], error) {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return nil, errors.New("repo root is required for internal MCP prompt server")
	}
	return OrderedMap[string]{
		config.BuiltinRepoRootEnvVar: trimmedRoot,
	}, nil
}
