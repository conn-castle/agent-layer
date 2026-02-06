package clients

import (
	"fmt"
	"strings"

	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/run"
)

// BuildEnv merges base env with project env and run metadata.
// It strips AL_SHIM_ACTIVE from the environment because child processes
// are new execution contexts that should be free to dispatch independently.
// The dispatch guard is only meaningful for the exec replacement chain,
// not for spawned subprocesses.
func BuildEnv(base []string, projectEnv map[string]string, runInfo *run.Info) []string {
	env := UnsetEnv(base, dispatch.EnvShimActive)
	env = mergeEnvFillMissing(env, projectEnv)
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

// UnsetEnv removes all entries for the given key from an env slice.
// If key is empty, it returns env unchanged.
func UnsetEnv(env []string, key string) []string {
	if key == "" {
		return env
	}
	prefix := key + "="
	result := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return result
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
