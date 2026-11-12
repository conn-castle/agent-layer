package codex

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
			{filepath.Join(root, ".codex"), true},
			{".codex", true},
			{filepath.Join(alias, ".codex"), true},
			{"link/../.codex", false},
			{filepath.Join(outside, ".codex"), false},
		} {
			var warning strings.Builder
			cfg := config.CodexConfig{LocalConfigDir: &enabled}
			env := ConfigureEnvironment(root, []string{"CODEX_HOME=" + tc.value, "CODEX_HOME=" + filepath.Join(t.TempDir(), "second-home")}, cfg, &warning)
			got, present := clients.GetEnv(env, "CODEX_HOME")
			count := 0
			for _, entry := range env {
				if strings.HasPrefix(entry, "CODEX_HOME=") {
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
				want := filepath.Join(root, ".codex")
				if !tc.local {
					want = filepath.Join(outside, ".codex")
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
	testutil.WriteStub(t, binDir, "codex")
	t.Setenv("PATH", binDir)
	call := testutil.CaptureExec(t, &execFunc, nil)
	runDir := filepath.Join(root, ".agent-layer", "tmp", "runs", "fixture")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	env := []string{"CODEX_HOME=link/../relative-home", "CODEX_HOME=" + filepath.Join(t.TempDir(), "second-home"), herdr.EnvEnabled + "=1", herdr.EnvSocketPath + "=/fixture/socket", herdr.EnvPaneID + "=w1:p1"}
	enabled := true
	cfg := &config.ProjectConfig{Root: root}
	cfg.Config.Agents.Codex = config.CodexConfig{LocalConfigDir: &enabled}
	if err := Launch(cfg, &run.Info{Dir: runDir}, env, nil); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range call.Env {
		if strings.HasPrefix(entry, "CODEX_HOME=") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("exec received %d provider home entries: %v", count, call.Env)
	}
	if got, _ := clients.GetEnv(call.Env, "CODEX_HOME"); !call.Called || got != filepath.Join(outside, "relative-home") {
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

func TestWarningAndEnsureAgreeForAliasesAndDifferentHomes(t *testing.T) {
	for _, local := range []bool{true, false} {
		root := t.TempDir()
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(root, alias); err != nil {
			t.Fatal(err)
		}
		selected := filepath.Join(alias, ".codex")
		if !local {
			selected = filepath.Join(t.TempDir(), ".codex")
		}
		enabled := true
		cfg := config.CodexConfig{LocalConfigDir: &enabled}
		var warning strings.Builder
		env := ConfigureEnvironment(root, []string{"CODEX_HOME=" + selected}, cfg, &warning)
		if (warning.Len() == 0) != local {
			t.Fatalf("local=%t warning=%q", local, warning.String())
		}
		if err := EnsureSelectedHome(root, env, cfg); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(selected)
		if local {
			if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
				t.Fatalf("home=%v err=%v", info, err)
			}
		} else if !os.IsNotExist(err) {
			t.Fatalf("foreign home was created: %v", err)
		}
	}
}

func TestCaseAliasSelectionUsesFilesystemIdentity(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, ".codex")
	alias := filepath.Join(root, ".Codex")
	if err := os.Mkdir(alias, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(home)
	same := err == nil
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	enabled := true
	cfg := config.CodexConfig{LocalConfigDir: &enabled}
	var warning strings.Builder
	env := ConfigureEnvironment(root, []string{"CODEX_HOME=" + alias}, cfg, &warning)
	if (warning.Len() == 0) != same {
		t.Fatalf("same=%t warning=%q", same, warning.String())
	}
	if err := EnsureSelectedHome(root, env, cfg); err != nil {
		t.Fatal(err)
	}
	disabled := ConfigureEnvironment(root, env, config.CodexConfig{}, nil)
	if _, present := clients.GetEnv(disabled, "CODEX_HOME"); present == same {
		t.Fatalf("case alias cleanup same=%t env=%v", same, disabled)
	}
	if !same {
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatalf("case-sensitive alias created project home: %v", err)
		}
	}
}

func TestLaunchCreatesPrivateSelectedProjectHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeResolvableCodex(t)
	testutil.CaptureExec(t, &execFunc, nil)
	enabled := true
	cfg := &config.ProjectConfig{Root: root}
	cfg.Config.Agents.Codex.LocalConfigDir = &enabled
	if err := Launch(cfg, nil, []string{}, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(root, ".codex"))
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("home=%v err=%v", info, err)
	}
}

func TestSubfolderHomeFallbackAndPhysicalOverrides(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		for _, callerHomeExists := range []bool{false, true} {
			for _, value := range []string{".codex", "./.codex", "././.codex", ".codex/", "./.codex/", ".//.codex", ".codex/.", "././/.codex//././", "link/../.codex", "sub/../.codex"} {
				t.Run(fmt.Sprintf("enabled=%t/existing=%t/%s", enabled, callerHomeExists, value), func(t *testing.T) {
					root := t.TempDir()
					t.Setenv("HOME", t.TempDir())
					caller := filepath.Join(root, "sub")
					outside := t.TempDir()
					for _, dir := range []string{caller, filepath.Join(caller, "sub"), filepath.Join(outside, "child"), filepath.Join(outside, ".codex")} {
						if err := os.Mkdir(dir, 0o700); err != nil {
							t.Fatal(err)
						}
					}
					if callerHomeExists {
						if err := os.Mkdir(filepath.Join(caller, ".codex"), 0o700); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.Symlink(filepath.Join(outside, "child"), filepath.Join(caller, "link")); err != nil {
						t.Fatal(err)
					}
					t.Chdir(caller)
					cfg := config.CodexConfig{LocalConfigDir: &enabled}
					var warning strings.Builder
					env := ConfigureEnvironment(root, []string{"CODEX_HOME=" + value}, cfg, &warning)
					projectHome := value != "link/../.codex" && value != "sub/../.codex" && !callerHomeExists
					want := filepath.Join(root, ".codex")
					if !projectHome {
						want = filepath.Join(caller, ".codex")
					}
					if value == "link/../.codex" {
						want = filepath.Join(outside, ".codex")
					}
					got, present := clients.GetEnv(env, "CODEX_HOME")
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
					if err := EnsureSelectedHome(root, env, cfg); err != nil {
						t.Fatal(err)
					}
					info, err := os.Stat(filepath.Join(root, ".codex"))
					if enabled && projectHome {
						if err != nil || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
							t.Fatalf("project home=%v err=%v", info, err)
						}
					} else if !os.IsNotExist(err) {
						t.Fatalf("unselected project home was created: %v", err)
					}
				})
			}
		}
	}
}
