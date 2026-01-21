package clients

import (
	"testing"

	"github.com/nicholasjconn/agent-layer/internal/run"
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
