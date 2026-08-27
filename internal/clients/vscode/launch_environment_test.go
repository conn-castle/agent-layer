package vscode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
)

func TestLaunchAppliesOnlyProviderOwnedEnvironmentChanges(t *testing.T) {
	for _, test := range []struct {
		name             string
		vscodeEnabled    bool
		codexLocal       bool
		claudeEnabled    bool
		claudeLocal      bool
		grokEnabled      bool
		inherited        func(string) map[string]string
		want             func(string) map[string]string
		absent           []string
		wantGrokHome0700 bool
	}{
		{
			name:          "enabled providers receive repository homes",
			vscodeEnabled: true, codexLocal: true, claudeEnabled: true, claudeLocal: true, grokEnabled: true,
			inherited: func(string) map[string]string {
				return map[string]string{"CODEX_HOME": "/outside/codex", "CLAUDE_CONFIG_DIR": "/outside/claude", "GROK_HOME": "/outside/grok"}
			},
			want: func(root string) map[string]string {
				return map[string]string{
					"CODEX_HOME":        filepath.Join(root, ".codex"),
					"CLAUDE_CONFIG_DIR": filepath.Join(root, ".claude-config"),
					"GROK_HOME":         filepath.Join(root, ".grok-config"),
				}
			},
			wantGrokHome0700: true,
		},
		{
			name:          "disabled providers clear only stale repository homes",
			vscodeEnabled: true,
			inherited: func(root string) map[string]string {
				return map[string]string{
					"CODEX_HOME":        "/outside/codex",
					"CLAUDE_CONFIG_DIR": filepath.Join(root, ".claude-config"),
					"GROK_HOME":         filepath.Join(root, ".grok-config"),
				}
			},
			want:   func(string) map[string]string { return map[string]string{"CODEX_HOME": "/outside/codex"} },
			absent: []string{"CLAUDE_CONFIG_DIR", "GROK_HOME"},
		},
		{
			name:          "Claude VS Code without local config clears only its stale repository home",
			vscodeEnabled: true,
			claudeEnabled: true,
			inherited: func(root string) map[string]string {
				return map[string]string{"CLAUDE_CONFIG_DIR": filepath.Join(root, ".claude-config")}
			},
			want:   func(string) map[string]string { return nil },
			absent: []string{"CLAUDE_CONFIG_DIR"},
		},
		{
			name:          "disabled providers preserve user homes",
			vscodeEnabled: true,
			inherited: func(string) map[string]string {
				return map[string]string{"CODEX_HOME": "/outside/codex", "CLAUDE_CONFIG_DIR": "/outside/claude", "GROK_HOME": "/outside/grok"}
			},
			want: func(string) map[string]string {
				return map[string]string{"CODEX_HOME": "/outside/codex", "CLAUDE_CONFIG_DIR": "/outside/claude", "GROK_HOME": "/outside/grok"}
			},
		},
		{
			name:       "disabled VS Code does not opt Codex into a repository home",
			codexLocal: true,
			inherited:  func(string) map[string]string { return nil },
			want:       func(string) map[string]string { return nil },
			absent:     []string{"CODEX_HOME"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			binDir := t.TempDir()
			envPath := filepath.Join(t.TempDir(), "environment")
			codePath := filepath.Join(binDir, "code")
			if err := os.WriteFile(codePath, []byte("#!/bin/sh\n/usr/bin/env > \"$AL_TEST_ENV_FILE\"\n"), 0o755); err != nil { // #nosec G306 -- test-owned executable.
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

			cfg := &config.ProjectConfig{Root: root}
			cfg.Config.Agents.VSCode.Enabled = &test.vscodeEnabled
			cfg.Config.Agents.Codex.LocalConfigDir = &test.codexLocal
			cfg.Config.Agents.ClaudeVSCode.Enabled = &test.claudeEnabled
			cfg.Config.Agents.Claude.LocalConfigDir = &test.claudeLocal
			cfg.Config.Agents.Grok.Enabled = &test.grokEnabled

			env := []string{"PATH=" + os.Getenv("PATH"), "AL_TEST_ENV_FILE=" + envPath}
			for key, value := range test.inherited(root) {
				env = clients.SetEnv(env, key, value)
			}
			if err := Launch(cfg, &run.Info{ID: "test", Dir: root}, env, nil); err != nil {
				t.Fatalf("launch: %v", err)
			}
			data, err := os.ReadFile(envPath) // #nosec G304 -- test-owned path.
			if err != nil {
				t.Fatal(err)
			}
			launched := splitEnvironment(string(data))
			for key, value := range test.want(root) {
				if launched[key] != value {
					t.Fatalf("%s = %q, want %q", key, launched[key], value)
				}
			}
			for _, key := range test.absent {
				if _, present := launched[key]; present {
					t.Fatalf("%s unexpectedly present as %q", key, launched[key])
				}
			}
			if test.wantGrokHome0700 {
				info, err := os.Lstat(filepath.Join(root, ".grok-config"))
				if err != nil || info.Mode().Perm() != 0o700 {
					t.Fatalf("Grok home mode = %v, %v; want 0700", info, err)
				}
			}
		})
	}
}

func splitEnvironment(raw string) map[string]string {
	result := make(map[string]string)
	for _, entry := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}
