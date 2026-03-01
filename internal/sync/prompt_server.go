package sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

const agentLayerGoModuleLine = "module github.com/conn-castle/agent-layer"

// ResolvePromptServerCommand returns the command and args used to run the internal MCP prompt server.
// Args: sys provides filesystem/exec access; root is the repo root.
// Returns: the resolved command/args or an error when resolution fails.
func ResolvePromptServerCommand(sys System, root string) (string, []string, error) {
	if sys == nil {
		return "", nil, errors.New("system is required")
	}
	value := reflect.ValueOf(sys)
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		if value.IsNil() {
			return "", nil, errors.New("system is required")
		}
	}
	return resolvePromptServerCommand(sys, root)
}

// ResolvePromptServerEnv returns the environment variables for the internal MCP prompt server.
// Args: root is the repo root.
// Returns: ordered env vars or an error when resolution fails.
func ResolvePromptServerEnv(root string) (OrderedMap[string], error) {
	return resolvePromptServerEnv(root)
}

// resolvePromptServerCommand returns the command and args used to run the internal MCP prompt server.
// It prefers "go run <root>/cmd/al mcp-prompts" when local source is present,
// then falls back to globally installed "al mcp-prompts".
// It returns an error when it cannot resolve a runnable command.
func resolvePromptServerCommand(sys System, root string) (string, []string, error) {
	sourcePath := filepath.Join(root, "cmd", "al")
	sourcePresent := false
	if strings.TrimSpace(root) != "" {
		info, err := sys.Stat(sourcePath)
		if err == nil {
			if !info.IsDir() {
				return "", nil, fmt.Errorf(messages.SyncPromptServerNotDirFmt, sourcePath)
			}
			sourcePresent = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf(messages.SyncCheckPathFmt, sourcePath, err)
		}
	}

	sourceRootMatches := false
	if sourcePresent {
		match, err := isAgentLayerSourceRoot(sys, root)
		if err != nil {
			return "", nil, err
		}
		sourceRootMatches = match
		if sourceRootMatches {
			if _, err := sys.LookPath("go"); err == nil {
				return "go", []string{"run", sourcePath, "mcp-prompts"}, nil
			}
		}
	}

	if _, err := sys.LookPath("al"); err == nil {
		return "al", []string{"mcp-prompts"}, nil
	}

	if root == "" {
		return "", nil, fmt.Errorf(messages.SyncMissingPromptServerNoRoot)
	}
	if !sourcePresent {
		return "", nil, fmt.Errorf(messages.SyncMissingPromptServerSourceFmt, sourcePath)
	}
	if !sourceRootMatches {
		return "", nil, fmt.Errorf(messages.SyncPromptServerModuleMismatchFmt, root)
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

func isAgentLayerSourceRoot(sys System, root string) (bool, error) {
	goModPath := filepath.Join(root, "go.mod")
	content, err := sys.ReadFile(goModPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf(messages.SyncReadFailedFmt, goModPath, err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(line) == agentLayerGoModuleLine {
			return true, nil
		}
	}
	return false, nil
}
