package clients

import (
	"fmt"
	"strings"

	"github.com/nicholasjconn/agent-layer/internal/run"
)

// BuildEnv merges base env with project env and run metadata.
func BuildEnv(base []string, projectEnv map[string]string, runInfo *run.Info) []string {
	env := mergeEnvFillMissing(base, projectEnv)
	if runInfo != nil {
		env = mergeEnv(env, map[string]string{
			"AL_RUN_DIR": runInfo.Dir,
			"AL_RUN_ID":  runInfo.ID,
		})
	}
	return env
}

// GetEnv returns the value for the key from an env slice.
func GetEnv(env []string, key string) (string, bool) {
	for _, entry := range env {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 && parts[0] == key {
			return parts[1], true
		}
	}
	return "", false
}

// SetEnv sets or appends a key=value entry in an env slice.
func SetEnv(env []string, key string, value string) []string {
	entry := fmt.Sprintf("%s=%s", key, value)
	for i, existing := range env {
		if strings.HasPrefix(existing, key+"=") {
			env[i] = entry
			return env
		}
	}
	return append(env, entry)
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}
	for key, value := range overrides {
		base = SetEnv(base, key, value)
	}
	return base
}

func mergeEnvFillMissing(base []string, additions map[string]string) []string {
	if len(additions) == 0 {
		return base
	}
	for key, value := range additions {
		if value == "" {
			continue
		}
		if _, ok := GetEnv(base, key); ok {
			continue
		}
		base = SetEnv(base, key, value)
	}
	return base
}
