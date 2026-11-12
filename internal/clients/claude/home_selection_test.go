package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/herdr"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestHomeSelectionPhysicalPathsAndDisabledCleanup(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "child"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for _, enabled := range []bool{true, false} {
		for _, tc := range []struct {
			value string
			local bool
		}{
			{filepath.Join(root, ".claude-config"), true},
			{".claude-config", true},
			{filepath.Join(alias, ".claude-config"), true},
			{"link/../.claude-config", false},
			{filepath.Join(outside, ".claude-config"), false},
		} {
			var warning strings.Builder
			cfg := config.ClaudeConfig{LocalConfigDir: &enabled}
			env := ConfigureEnvironment(root, []string{"CLAUDE_CONFIG_DIR=" + tc.value, "CLAUDE_CONFIG_DIR=" + filepath.Join(t.TempDir(), "second-home")}, cfg, &warning)
			got, present := clients.GetEnv(env, "CLAUDE_CONFIG_DIR")
			count := 0
			for _, entry := range env {
				if strings.HasPrefix(entry, "CLAUDE_CONFIG_DIR=") {
					count++
				}
			}
			wantCount := 1
			if !enabled && tc.local {
				wantCount = 0
			}
			if count != wantCount {
				t.Fatalf("provider home entries=%d, want %d: %v", count, wantCount, env)
			}
			if !enabled && tc.local {
				if present {
					t.Fatalf("disabled config retained project home %q", got)
				}
			} else {
				if !present || !filepath.IsAbs(got) {
					t.Fatalf("home %q was not absolute: %q", tc.value, got)
				}
				want := filepath.Join(root, ".claude-config")
				if !tc.local {
					want = filepath.Join(outside, ".claude-config")
				}
				if !clients.SamePath(got, want) {
					t.Fatalf("home %q became %q, want %q", tc.value, got, want)
				}
			}
			if (warning.Len() > 0) != (enabled && !tc.local) {
				t.Fatalf("home %q enabled=%t warning=%q", tc.value, enabled, warning.String())
			}
		}
	}
}

func TestLaunchCapturesAbsoluteEffectiveHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Chdir(root)
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "child"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "claude")
	t.Setenv("PATH", binDir)
	call := testutil.CaptureExec(t, &execFunc, nil)
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "fixture")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	env := []string{"CLAUDE_CONFIG_DIR=link/../relative-home", "CLAUDE_CONFIG_DIR=" + filepath.Join(t.TempDir(), "second-home"), herdr.EnvEnabled + "=1", herdr.EnvSocketPath + "=/fixture/socket", herdr.EnvPaneID + "=w1:p1"}
	enabled := true
	cfg := &config.ProjectConfig{Root: root}
	cfg.Config.Agents.Claude = config.ClaudeConfig{LocalConfigDir: &enabled}
	if err := Launch(cfg, &run.Info{Dir: runDir}, env, nil); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range call.Env {
		if strings.HasPrefix(entry, "CLAUDE_CONFIG_DIR=") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("exec received %d provider home entries: %v", count, call.Env)
	}
	if got, _ := clients.GetEnv(call.Env, "CLAUDE_CONFIG_DIR"); !call.Called || got != filepath.Join(outside, "relative-home") {
		t.Fatalf("child home %q", got)
	}
	paths, err := filepath.Glob(filepath.Join(runDir, "herdr-launch-*.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("records=%v err=%v", paths, err)
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		ProviderHome string `json:"provider_home"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.ProviderHome != filepath.Join(outside, "relative-home") {
		t.Fatalf("captured home %q", record.ProviderHome)
	}
}

func TestSubfolderHomeFallbackAndPhysicalOverrides(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		for _, callerHomeExists := range []bool{false, true} {
			for _, value := range []string{".claude-config", "./.claude-config", "././.claude-config", ".claude-config/", "./.claude-config/", ".//.claude-config", ".claude-config/.", "././/.claude-config//././", "link/../.claude-config", "sub/../.claude-config"} {
				t.Run(fmt.Sprintf("enabled=%t/existing=%t/%s", enabled, callerHomeExists, value), func(t *testing.T) {
					root := t.TempDir()
					t.Setenv("HOME", t.TempDir())
					caller := filepath.Join(root, "sub")
					outside := t.TempDir()
					for _, dir := range []string{caller, filepath.Join(caller, "sub"), filepath.Join(outside, "child"), filepath.Join(outside, ".claude-config")} {
						if err := os.Mkdir(dir, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					if callerHomeExists {
						if err := os.Mkdir(filepath.Join(caller, ".claude-config"), 0o700); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Symlink(filepath.Join(outside, "child"), filepath.Join(caller, "link")); err != nil {
						t.Fatal(err)
					}
					t.Chdir(caller)
					cfg := config.ClaudeConfig{LocalConfigDir: &enabled}
					var warning strings.Builder
					env := ConfigureEnvironment(root, []string{"CLAUDE_CONFIG_DIR=" + value}, cfg, &warning)
					projectHome := value != "link/../.claude-config" && value != "sub/../.claude-config" && !callerHomeExists
					want := filepath.Join(root, ".claude-config")
					if !projectHome {
						want = filepath.Join(caller, ".claude-config")
					}
					if value == "link/../.claude-config" {
						want = filepath.Join(outside, ".claude-config")
					}
					got, present := clients.GetEnv(env, "CLAUDE_CONFIG_DIR")
					if !enabled && projectHome {
						if present {
							t.Fatalf("disabled project home retained as %q", got)
						}
					} else if !present || got != want {
						t.Fatalf("home=%q, want %q (present=%t)", got, want, present)
					}
					if (warning.Len() > 0) != (enabled && !projectHome) {
						t.Fatalf("warning=%q", warning.String())
					}
					if !projectHome && (callerHomeExists || strings.HasPrefix(value, "link/")) {
						selected, err := os.Stat(got)
						if err != nil {
							t.Fatal(err)
						}
						original, err := os.Stat(value)
						if err != nil || !os.SameFile(selected, original) {
							t.Fatalf("home %q changed filesystem selection: %v", got, err)
						}
					}

				})
			}
		}
	}
}
