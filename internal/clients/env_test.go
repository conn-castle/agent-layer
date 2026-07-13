package clients

import (
	"testing"

	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
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

func TestMergeEnvEmptyOverrides(t *testing.T) {
	base := []string{"PATH=/bin"}
	result := mergeEnv(base, map[string]string{})
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
	base := []string{"PATH=/bin", versiondispatch.EnvShimActive + "=1", "OTHER=data"}
	env := BuildEnv(base, nil, nil)

	if _, ok := GetEnv(env, versiondispatch.EnvShimActive); ok {
		t.Fatalf("expected %s to be stripped from environment", versiondispatch.EnvShimActive)
	}
	if value, ok := GetEnv(env, "PATH"); !ok || value != "/bin" {
		t.Fatalf("expected PATH to remain, got %v", value)
	}
	if value, ok := GetEnv(env, "OTHER"); !ok || value != "data" {
		t.Fatalf("expected OTHER to remain, got %v", value)
	}
}

// TestBuildEnvStripsDispatchActive exercises F8: a leaked
// AL_DISPATCH_ACTIVE in the parent shell must not survive a normal launch.
// Dispatch sets the marker explicitly on dispatched children, but a normal
// `al claude` launched from a shell that exported the marker should run
// the child without it.
func TestBuildEnvStripsDispatchActive(t *testing.T) {
	base := []string{"PATH=/bin", EnvDispatchActive + "=1", "OTHER=data"}
	env := BuildEnv(base, nil, nil)

	if _, ok := GetEnv(env, EnvDispatchActive); ok {
		t.Fatalf("expected %s to be stripped from environment", EnvDispatchActive)
	}
	if value, ok := GetEnv(env, "PATH"); !ok || value != "/bin" {
		t.Fatalf("expected PATH to remain, got %v", value)
	}
	if value, ok := GetEnv(env, "OTHER"); !ok || value != "data" {
		t.Fatalf("expected OTHER to remain, got %v", value)
	}
}

// TestBuildEnvForAgentStripsDispatchActive verifies that the normal
// agent-launch helper also drops the dispatch-active marker (since it
// composes BuildEnv).
func TestBuildEnvForAgentStripsDispatchActive(t *testing.T) {
	base := []string{"PATH=/bin", EnvDispatchActive + "=1"}
	env := BuildEnvForAgent(base, nil, nil, "claude")

	if _, ok := GetEnv(env, EnvDispatchActive); ok {
		t.Fatalf("expected %s to be stripped from BuildEnvForAgent output", EnvDispatchActive)
	}
}

func TestBuildEnvForAgentPreservesDevelopmentVersionDispatchBypass(t *testing.T) {
	base := []string{
		"PATH=/repo/.agent-layer/tmp/dev-bin:/bin",
		versiondispatch.EnvDevelopmentBypassVersionDispatch + "=1",
	}
	env := BuildEnvForAgent(base, nil, nil, "codex")

	if value, ok := GetEnv(env, versiondispatch.EnvDevelopmentBypassVersionDispatch); !ok || value != "1" {
		t.Fatalf("expected %s=1 to reach the launched agent, got %q ok=%v", versiondispatch.EnvDevelopmentBypassVersionDispatch, value, ok)
	}
	if value, ok := GetEnv(env, "PATH"); !ok || value != "/repo/.agent-layer/tmp/dev-bin:/bin" {
		t.Fatalf("expected development PATH to reach the launched agent, got %q ok=%v", value, ok)
	}
}

func TestBuildEnvForAgentSetsSupportedCallerMarkers(t *testing.T) {
	tests := map[string]string{
		"antigravity": "antigravity",
		"claude":      "claude",
		"codex":       "codex",
	}
	for agent, want := range tests {
		t.Run(agent, func(t *testing.T) {
			env := BuildEnvForAgent([]string{"PATH=/bin"}, nil, nil, agent)
			if got, ok := GetEnv(env, EnvDispatchCallerAgent); !ok || got != want {
				t.Fatalf("expected %s=%s, got %q ok=%v", EnvDispatchCallerAgent, want, got, ok)
			}
		})
	}
}

func TestBuildEnvForAgentStripsUnsupportedCallerMarkers(t *testing.T) {
	tests := []string{"", "vscode", "claude_vscode", "copilot"}
	for _, agent := range tests {
		t.Run(agent, func(t *testing.T) {
			env := BuildEnvForAgent([]string{"PATH=/bin", EnvDispatchCallerAgent + "=claude"}, nil, nil, agent)
			if got, ok := GetEnv(env, EnvDispatchCallerAgent); ok {
				t.Fatalf("expected %s to be stripped for %q, got %q", EnvDispatchCallerAgent, agent, got)
			}
		})
	}
}
