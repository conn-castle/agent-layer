package clients

import (
	"testing"

	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/run"
)

func TestBuildEnv(t *testing.T) {
	base := []string{"PATH=/bin", "AL_RUN_DIR=/old"}
	projectEnv := map[string]string{"TOKEN": "abc"}
	runInfo := &run.Info{ID: "run1", Dir: "/tmp/run1"}

	env := BuildEnv(base, projectEnv, runInfo)

	if value, ok := GetEnv(env, "TOKEN"); !ok || value != "abc" {
		t.Fatalf("expected TOKEN in env, got %v", value)
	}
	if value, ok := GetEnv(env, "AL_RUN_DIR"); !ok || value != "/tmp/run1" {
		t.Fatalf("expected AL_RUN_DIR in env, got %v", value)
	}
	if value, ok := GetEnv(env, "AL_RUN_ID"); !ok || value != "run1" {
		t.Fatalf("expected AL_RUN_ID in env, got %v", value)
	}
}

func TestBuildEnvDoesNotOverrideBase(t *testing.T) {
	base := []string{"TOKEN=real"}
	projectEnv := map[string]string{"TOKEN": "abc"}

	env := BuildEnv(base, projectEnv, nil)

	if value, ok := GetEnv(env, "TOKEN"); !ok || value != "real" {
		t.Fatalf("expected TOKEN to remain from base env, got %v", value)
	}
}

func TestBuildEnvDoesNotOverrideBaseWithEmptyProjectValue(t *testing.T) {
	base := []string{"TOKEN=real"}
	projectEnv := map[string]string{"TOKEN": ""}

	env := BuildEnv(base, projectEnv, nil)

	if value, ok := GetEnv(env, "TOKEN"); !ok || value != "real" {
		t.Fatalf("expected TOKEN to remain from base env, got %v", value)
	}
}

func TestSetEnvUpdatesExisting(t *testing.T) {
	env := []string{"KEY=old"}
	env = SetEnv(env, "KEY", "new")
	if value, ok := GetEnv(env, "KEY"); !ok || value != "new" {
		t.Fatalf("expected KEY=new, got %v", value)
	}
}

func TestGetEnvMissing(t *testing.T) {
	env := []string{"KEY=value", "NOVAL"}
	if _, ok := GetEnv(env, "MISSING"); ok {
		t.Fatalf("expected missing key to return false")
	}
}

func TestBuildEnvEmptyProjectEnv(t *testing.T) {
	base := []string{"PATH=/bin"}
	env := BuildEnv(base, map[string]string{}, nil)

	if value, ok := GetEnv(env, "PATH"); !ok || value != "/bin" {
		t.Fatalf("expected PATH in env, got %v", value)
	}
}

func TestBuildEnvNilProjectEnv(t *testing.T) {
	base := []string{"PATH=/bin"}
	env := BuildEnv(base, nil, nil)

	if value, ok := GetEnv(env, "PATH"); !ok || value != "/bin" {
		t.Fatalf("expected PATH in env, got %v", value)
	}
}

func TestMergeEnvEmptyOverrides(t *testing.T) {
	base := []string{"PATH=/bin"}
	result := mergeEnv(base, map[string]string{})
	if len(result) != 1 || result[0] != "PATH=/bin" {
		t.Fatalf("expected unchanged base, got %v", result)
	}
}

func TestMergeEnvNilOverrides(t *testing.T) {
	base := []string{"PATH=/bin"}
	result := mergeEnv(base, nil)
	if len(result) != 1 || result[0] != "PATH=/bin" {
		t.Fatalf("expected unchanged base, got %v", result)
	}
}

func TestUnsetEnv(t *testing.T) {
	env := []string{"PATH=/bin", "KEY=value", "OTHER=data"}
	result := UnsetEnv(env, "KEY")
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if _, ok := GetEnv(result, "KEY"); ok {
		t.Fatalf("expected KEY to be removed")
	}
	if value, ok := GetEnv(result, "PATH"); !ok || value != "/bin" {
		t.Fatalf("expected PATH to remain, got %v", value)
	}
	if value, ok := GetEnv(result, "OTHER"); !ok || value != "data" {
		t.Fatalf("expected OTHER to remain, got %v", value)
	}
}

func TestUnsetEnvMissingKey(t *testing.T) {
	env := []string{"PATH=/bin", "OTHER=data"}
	result := UnsetEnv(env, "MISSING")
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
}

func TestUnsetEnvEmptyKey(t *testing.T) {
	env := []string{"PATH=/bin", "KEY=value"}
	result := UnsetEnv(env, "")
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if value, ok := GetEnv(result, "PATH"); !ok || value != "/bin" {
		t.Fatalf("expected PATH to remain, got %v", value)
	}
	if value, ok := GetEnv(result, "KEY"); !ok || value != "value" {
		t.Fatalf("expected KEY to remain, got %v", value)
	}
}

func TestUnsetEnvEmptySlice(t *testing.T) {
	result := UnsetEnv([]string{}, "KEY")
	if len(result) != 0 {
		t.Fatalf("expected empty slice, got %v", result)
	}
}

func TestBuildEnvStripsShimActive(t *testing.T) {
	base := []string{"PATH=/bin", dispatch.EnvShimActive + "=1", "OTHER=data"}
	env := BuildEnv(base, nil, nil)

	if _, ok := GetEnv(env, dispatch.EnvShimActive); ok {
		t.Fatalf("expected %s to be stripped from environment", dispatch.EnvShimActive)
	}
	if value, ok := GetEnv(env, "PATH"); !ok || value != "/bin" {
		t.Fatalf("expected PATH to remain, got %v", value)
	}
	if value, ok := GetEnv(env, "OTHER"); !ok || value != "data" {
		t.Fatalf("expected OTHER to remain, got %v", value)
	}
}
