package clients

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
)

func TestRunPipeline(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)

	var gotRun *run.Info
	var gotEnv []string
	launch := func(project *config.ProjectConfig, runInfo *run.Info, env []string) error {
		gotRun = runInfo
		gotEnv = env
		return nil
	}

	err := Run(root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, launch)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if gotRun == nil || gotRun.Dir == "" || gotRun.ID == "" {
		t.Fatalf("expected run info to be populated")
	}
	if _, err := os.Stat(gotRun.Dir); err != nil {
		t.Fatalf("expected run dir to exist: %v", err)
	}
	if value, ok := GetEnv(gotEnv, "AL_RUN_DIR"); !ok || value == "" {
		t.Fatalf("expected AL_RUN_DIR to be set")
	}
	if value, ok := GetEnv(gotEnv, "AL_RUN_ID"); !ok || value == "" {
		t.Fatalf("expected AL_RUN_ID to be set")
	}
	if _, err := os.Stat(filepath.Join(root, ".gemini", "settings.json")); err != nil {
		t.Fatalf("expected gemini settings: %v", err)
	}
}

func TestRunDisabled(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)

	disabled := false
	err := Run(root, "gemini", func(cfg *config.Config) *bool {
		return &disabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func TestRunMissingConfig(t *testing.T) {
	err := Run(t.TempDir(), "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "missing config file") {
		t.Fatalf("expected missing config error, got %v", err)
	}
}

func TestRunSyncError(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)

	if err := os.Chmod(root, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(root, 0o700)
	})

	err := Run(root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected sync error")
	}
}

func TestRunCreateError(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)

	blockPath := filepath.Join(root, ".agent-layer", "tmp")
	if err := os.WriteFile(blockPath, []byte("block"), 0o644); err != nil {
		t.Fatalf("write tmp file: %v", err)
	}

	err := Run(root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string) error {
		return nil
	})
	if err == nil {
		t.Fatalf("expected run create error")
	}
}

func TestRunLaunchError(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)

	err := Run(root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string) error {
		return fmt.Errorf("launch failed")
	})
	if err == nil || !strings.Contains(err.Error(), "launch failed") {
		t.Fatalf("expected launch error, got %v", err)
	}
}

func writeMinimalRepo(t *testing.T, root string) {
	t.Helper()
	paths := config.DefaultPaths(root)
	dirs := []string{paths.InstructionsDir, paths.SlashCommandsDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	configToml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = false

[agents.codex]
enabled = false

[agents.vscode]
enabled = false

[agents.antigravity]
enabled = false

[warnings]
instruction_token_threshold = 50000
mcp_server_threshold = 5
`
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_base.md"), []byte("base"), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	command := `---
name: alpha
description: test
---

Do it.`
	if err := os.WriteFile(filepath.Join(paths.SlashCommandsDir, "alpha.md"), []byte(command), 0o644); err != nil {
		t.Fatalf("write slash command: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte(""), 0o644); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "al"), []byte("stub"), 0o755); err != nil {
		t.Fatalf("write al stub: %v", err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
}
