package sync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestWriteGrokChimeHookWritesManagedStopHook(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
	}
	if err := writeGrokChimeHook(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeGrokChimeHook: %v", err)
	}
	data, err := os.ReadFile(grokChimeHookPath(root)) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, agentLayerGrokChimeCommand) {
		t.Fatalf("expected managed grok chime command, got:\n%s", got)
	}
	if !strings.Contains(got, `"Stop"`) {
		t.Fatalf("expected Stop hook event, got:\n%s", got)
	}
	if err := writeGrokChimeHook(RealSystem{}, root, project); err != nil {
		t.Fatalf("idempotent writeGrokChimeHook: %v", err)
	}
}

func TestGrokChimeHookRejectsUnsafePathsAndCleanupConflicts(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}}}

	t.Run("symlinked hooks directory", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".grok"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), filepath.Join(root, ".grok", "hooks")); err != nil {
			t.Fatal(err)
		}
		if err := writeGrokChimeHook(RealSystem{}, root, project); err == nil {
			t.Fatal("expected symlink conflict")
		}
	})

	t.Run("cleanup preserves conflict by failing", func(t *testing.T) {
		root := t.TempDir()
		path := grokChimeHookPath(root)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"hooks":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := cleanGrokChimeHook(RealSystem{}, root); err == nil {
			t.Fatal("expected user-owned cleanup conflict")
		}
	})
}

func TestGrokChimeHookSurfacesFilesystemFailures(t *testing.T) {
	injected := errors.New("injected")
	enabled := true
	project := &config.ProjectConfig{Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}}}
	root := t.TempDir()

	mkdirSystem := &MockSystem{Fallback: RealSystem{}, MkdirAllFunc: func(string, os.FileMode) error { return injected }}
	if err := writeGrokChimeHook(mkdirSystem, root, project); !errors.Is(err, injected) {
		t.Fatalf("mkdir error = %v", err)
	}

	writeSystem := &MockSystem{Fallback: RealSystem{}, WriteFileAtomicFunc: func(string, []byte, os.FileMode) error { return injected }}
	if err := writeGrokChimeHook(writeSystem, root, project); !errors.Is(err, injected) {
		t.Fatalf("write error = %v", err)
	}

	if err := ensureGrokChimePathContained(RealSystem{}, root, filepath.Join(root, "outside")); err == nil {
		t.Fatal("expected containment error")
	}
	statSystem := &MockSystem{LstatFunc: func(string) (os.FileInfo, error) { return nil, injected }}
	if err := ensureGrokChimePathContained(statSystem, root, filepath.Join(root, ".grok", "hooks")); !errors.Is(err, injected) {
		t.Fatalf("lstat error = %v", err)
	}
}

func TestCleanGrokChimeHookSurfacesFilesystemFailures(t *testing.T) {
	injected := errors.New("injected")
	seed := func(t *testing.T) (string, string) {
		t.Helper()
		root := t.TempDir()
		path := grokChimeHookPath(root)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, grokChimeHookContents(), 0o600); err != nil {
			t.Fatal(err)
		}
		return root, path
	}

	t.Run("read hook", func(t *testing.T) {
		root, _ := seed(t)
		sys := &MockSystem{Fallback: RealSystem{}, ReadFileFunc: func(string) ([]byte, error) { return nil, injected }}
		if err := cleanGrokChimeHook(sys, root); !errors.Is(err, injected) {
			t.Fatalf("read error = %v", err)
		}
	})
	t.Run("remove hook", func(t *testing.T) {
		root, _ := seed(t)
		sys := &MockSystem{Fallback: RealSystem{}, RemoveFunc: func(string) error { return injected }}
		if err := cleanGrokChimeHook(sys, root); !errors.Is(err, injected) {
			t.Fatalf("remove error = %v", err)
		}
	})
	t.Run("read hooks directory", func(t *testing.T) {
		root, _ := seed(t)
		sys := &MockSystem{Fallback: RealSystem{}, ReadDirFunc: func(string) ([]os.DirEntry, error) { return nil, injected }}
		if err := cleanGrokChimeHook(sys, root); !errors.Is(err, injected) {
			t.Fatalf("read directory error = %v", err)
		}
	})
	t.Run("remove hooks directory", func(t *testing.T) {
		root, path := seed(t)
		dir := filepath.Dir(path)
		sys := &MockSystem{Fallback: RealSystem{}, RemoveFunc: func(name string) error {
			if name == dir {
				return injected
			}
			return RealSystem{}.Remove(name)
		}}
		if err := cleanGrokChimeHook(sys, root); !errors.Is(err, injected) {
			t.Fatalf("remove directory error = %v", err)
		}
	})
}

func TestWriteGrokChimeHookCleansWhenDisabled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	disabled := false
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
	}
	if err := writeGrokChimeHook(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeGrokChimeHook: %v", err)
	}
	project.Config.Notifications.Chime = &disabled
	if err := writeGrokChimeHook(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeGrokChimeHook cleanup: %v", err)
	}
	if _, err := os.Stat(grokChimeHookPath(root)); !os.IsNotExist(err) {
		t.Fatalf("expected managed hook removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(grokChimeHookPath(root))); !os.IsNotExist(err) {
		t.Fatalf("expected empty hooks dir removed, stat err = %v", err)
	}
}

func TestWriteGrokChimeHookPreservesSiblingHooks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	hooksDir := filepath.Join(root, ".grok", "hooks")
	if err := os.MkdirAll(hooksDir, 0o700); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	sibling := filepath.Join(hooksDir, "user.json")
	if err := os.WriteFile(sibling, []byte(`{"hooks":{}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write sibling: %v", err)
	}
	enabled := true
	disabled := false
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
	}
	if err := writeGrokChimeHook(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeGrokChimeHook: %v", err)
	}
	project.Config.Notifications.Chime = &disabled
	if err := writeGrokChimeHook(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeGrokChimeHook cleanup: %v", err)
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatalf("expected sibling hook preserved: %v", err)
	}
	if _, err := os.Stat(grokChimeHookPath(root)); !os.IsNotExist(err) {
		t.Fatalf("expected managed hook removed, stat err = %v", err)
	}
}

func TestWriteGrokChimeHookRewritesPriorManagedContents(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := grokChimeHookPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	legacy := []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"al hook chime grok # agent-layer-chime","timeout":1}]}]}}` + "\n")
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy hook: %v", err)
	}
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
	}
	if err := writeGrokChimeHook(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeGrokChimeHook rewrite: %v", err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if !bytes.Equal(data, grokChimeHookContents()) {
		t.Fatalf("expected current managed hook contents, got:\n%s", data)
	}
}

func TestWriteGrokChimeHookRejectsUserOwnedFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := grokChimeHookPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"hooks":{}}`+"\n"), 0o600); err != nil {
		t.Fatalf("write user hook: %v", err)
	}
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
	}
	err := writeGrokChimeHook(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "not the Agent Layer-managed Grok chime hook") {
		t.Fatalf("error = %v, want managed-hook conflict", err)
	}
}

func TestWriteGrokChimeHookRejectsLegacyAgentSpecific(t *testing.T) {
	t.Parallel()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
			Agents: config.AgentsConfig{
				Grok: config.GrokConfig{
					AgentSpecific: map[string]any{
						"hooks": map[string]any{
							"Stop": []any{map[string]any{
								"hooks": []any{chimeHandler(agentLayerGrokChimeCommand)},
							}},
						},
					},
				},
			},
		},
	}
	err := writeGrokChimeHook(RealSystem{}, t.TempDir(), project)
	if err == nil || !strings.Contains(err.Error(), "agents.grok.agent_specific.hooks") {
		t.Fatalf("error = %v, want legacy agent_specific chime", err)
	}
}

func TestCleanGrokOutputsRemovesManagedChimeHook(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Notifications: config.NotificationsConfig{Chime: &enabled},
		},
	}
	if err := writeGrokChimeHook(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeGrokChimeHook: %v", err)
	}
	if err := cleanGrokOutputs(RealSystem{}, root); err != nil {
		t.Fatalf("cleanGrokOutputs: %v", err)
	}
	if _, err := os.Stat(grokChimeHookPath(root)); !os.IsNotExist(err) {
		t.Fatalf("expected managed hook removed, stat err = %v", err)
	}
}
