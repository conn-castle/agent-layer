package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mitchellh/go-homedir"

	"github.com/conn-castle/agent-layer/internal/messages"
)

var envVarPattern = regexp.MustCompile(`\$\{([A-Z0-9_]+)\}`)

// EnvVarReplacer returns a replacement string for a resolved env var.
type EnvVarReplacer func(name string, value string) string

// ExtractEnvVarNames returns env var names referenced by ${VAR} placeholders.
// input is a string that may contain placeholders; returns names in scan order.
func ExtractEnvVarNames(input string) []string {
	matches := envVarPattern.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return nil
	}
	names := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 && match[1] != "" {
			names = append(names, match[1])
		}
	}
	return names
}

// SubstituteEnvVars replaces ${VAR} placeholders using env values.
func SubstituteEnvVars(input string, env map[string]string) (string, error) {
	return SubstituteEnvVarsWith(input, env, nil)
}

// SubstituteEnvVarsWith replaces ${VAR} placeholders using env values and a replacer.
func SubstituteEnvVarsWith(input string, env map[string]string, replacer EnvVarReplacer) (string, error) {
	if replacer == nil {
		replacer = func(_ string, value string) string {
			return value
		}
	}
	missing := make(map[string]struct{})
	result := envVarPattern.ReplaceAllStringFunc(input, func(match string) string {
		varName := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		value, ok := env[varName]
		if !ok || value == "" {
			missing[varName] = struct{}{}
			return match
		}
		return replacer(varName, value)
	})

	if len(missing) > 0 {
		var names []string
		for name := range missing {
			names = append(names, name)
		}
		sort.Strings(names)
		return "", fmt.Errorf(messages.ConfigMissingEnvVarsFmt, strings.Join(names, ", "))
	}

	return result, nil
}

// BuiltinRepoRootEnvVar is the built-in placeholder for the repo root path.
const BuiltinRepoRootEnvVar = "AL_REPO_ROOT"

// WithBuiltInEnv returns a copy of env with built-in values added.
// env is the parsed .env map; repoRoot should be the absolute repo root path.
func WithBuiltInEnv(env map[string]string, repoRoot string) map[string]string {
	merged := make(map[string]string, len(env)+1)
	for key, value := range env {
		merged[key] = value
	}
	if repoRoot != "" {
		merged[BuiltinRepoRootEnvVar] = repoRoot
	}
	return merged
}

// IsBuiltInEnvVar reports whether name is reserved for built-in placeholders.
func IsBuiltInEnvVar(name string) bool {
	return name == BuiltinRepoRootEnvVar
}

// ShouldExpandPath reports whether value requests path expansion.
// Path expansion is enabled when the value starts with "~" or "${AL_REPO_ROOT}".
func ShouldExpandPath(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "~") || strings.HasPrefix(trimmed, "${"+BuiltinRepoRootEnvVar+"}")
}

// ExpandPath expands "~" and resolves relative paths against repoRoot.
// value is the resolved string (placeholders already substituted).
func ExpandPath(value string, repoRoot string) (string, error) {
	expanded, err := homedir.Expand(value)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(expanded) {
		return filepath.Clean(expanded), nil
	}
	if repoRoot == "" {
		return "", fmt.Errorf("repo root required for path expansion")
	}
	return filepath.Clean(filepath.Join(repoRoot, expanded)), nil
}

// ExpandPathIfNeeded expands value when the raw input signals a path placeholder.
func ExpandPathIfNeeded(raw string, value string, repoRoot string) (string, error) {
	if !ShouldExpandPath(raw) {
		return value, nil
	}
	return ExpandPath(value, repoRoot)
}
