package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

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
