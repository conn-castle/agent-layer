package clients

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/update"
	"github.com/conn-castle/agent-layer/internal/updatewarn"
)

func TestRunPipeline(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)

	var gotRun *run.Info
	var gotEnv []string
	var gotArgs []string
	launch := func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		gotRun = runInfo
		gotEnv = env
		gotArgs = args
		return nil
	}

	passArgs := []string{"--debug", "true"}
	err := Run(context.Background(), root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, launch, passArgs, "v1.0.0")
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
	if strings.Join(gotArgs, ",") != strings.Join(passArgs, ",") {
		t.Fatalf("expected args %v, got %v", passArgs, gotArgs)
	}
	if _, err := os.Stat(filepath.Join(root, ".gemini", "settings.json")); err != nil {
		t.Fatalf("expected gemini settings: %v", err)
	}
}

func TestRunDisabled(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)

	disabled := false
	err := Run(context.Background(), root, "gemini", func(cfg *config.Config) *bool {
		return &disabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return nil
	}, nil, "v1.0.0")
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func TestRunMissingConfig(t *testing.T) {
	err := Run(context.Background(), t.TempDir(), "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return nil
	}, nil, "v1.0.0")
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

	err := Run(context.Background(), root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return nil
	}, nil, "v1.0.0")
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

	err := Run(context.Background(), root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return nil
	}, nil, "v1.0.0")
	if err == nil {
		t.Fatalf("expected run create error")
	}
}

func TestRunLaunchError(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)

	err := Run(context.Background(), root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return fmt.Errorf("launch failed")
	}, nil, "v1.0.0")
	if err == nil || !strings.Contains(err.Error(), "launch failed") {
		t.Fatalf("expected launch error, got %v", err)
	}
}

func TestRunWarnsOnUpdateWhenEnabled(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)

	paths := config.DefaultPaths(root)
	configToml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = false

[agents.claude-vscode]
enabled = false

[agents.codex]
enabled = false

[agents.vscode]
enabled = false

[agents.antigravity]
enabled = false

[warnings]
version_update_on_sync = true
instruction_token_threshold = 50000
mcp_server_threshold = 5
`
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origCheck := updatewarn.CheckForUpdate
	calls := 0
	updatewarn.CheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
		calls++
		return update.CheckResult{Current: "1.0.0", Latest: "2.0.0", Outdated: true}, nil
	}
	t.Cleanup(func() { updatewarn.CheckForUpdate = origCheck })

	var stderr bytes.Buffer
	launch := func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return nil
	}

	err := RunWithStderr(context.Background(), root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, launch, nil, "v1.0.0", &stderr)
	if err != nil {
		t.Fatalf("RunWithStderr error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected update check to run once, got %d", calls)
	}
	if !strings.Contains(stderr.String(), "update available") {
		t.Fatalf("expected update warning, got %q", stderr.String())
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

[agents.claude-vscode]
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
	gitignoreBlock := "# test gitignore content\n"
	if err := os.WriteFile(filepath.Join(paths.Root, ".agent-layer", "gitignore.block"), []byte(gitignoreBlock), 0o644); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
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

func writeMinimalRepoWithMode(t *testing.T, root string, mode string) {
	t.Helper()
	paths := config.DefaultPaths(root)
	dirs := []string{paths.InstructionsDir, paths.SlashCommandsDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	configToml := fmt.Sprintf(`
[approvals]
mode = %q

[agents.gemini]
enabled = true

[agents.claude]
enabled = false

[agents.claude-vscode]
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
`, mode)
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	gitignoreBlock := "# test gitignore content\n"
	if err := os.WriteFile(filepath.Join(paths.Root, ".agent-layer", "gitignore.block"), []byte(gitignoreBlock), 0o644); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
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

func TestRunNoSync_Success(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)

	called := false
	err := RunNoSync(root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		called = true
		if runInfo == nil || runInfo.Dir == "" || runInfo.ID == "" {
			t.Fatalf("expected run info")
		}
		return nil
	}, []string{"--arg"})
	if err != nil {
		t.Fatalf("RunNoSync: %v", err)
	}
	if !called {
		t.Fatal("expected launch to be called")
	}
}

func TestRunNoSync_YOLOWarning(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepoWithMode(t, root, "yolo")

	var stderr bytes.Buffer
	err := RunNoSyncWithStderr(root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return nil
	}, nil, &stderr)
	if err != nil {
		t.Fatalf("RunNoSyncWithStderr: %v", err)
	}
	output := stderr.String()
	if !strings.Contains(output, "[yolo]") {
		t.Fatalf("expected [yolo] acknowledgement in stderr, got %q", output)
	}
	if strings.Contains(output, "WARNING") {
		t.Fatalf("expected single-line ack, not structured warning, got %q", output)
	}
}

func TestRunNoSync_Disabled(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)
	disabled := false
	err := RunNoSync(root, "gemini", func(cfg *config.Config) *bool {
		return &disabled
	}, func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return nil
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func TestRunWithStderr_NilWriter(t *testing.T) {
	root := t.TempDir()
	writeMinimalRepo(t, root)
	launch := func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return nil
	}
	err := RunWithStderr(context.Background(), root, "gemini", func(cfg *config.Config) *bool {
		return cfg.Agents.Gemini.Enabled
	}, launch, nil, "v1.0.0", nil)
	if err != nil {
		t.Fatalf("RunWithStderr nil writer: %v", err)
	}
}

func TestRunWithStderr_NilWriterVSCodeAutoApprove(t *testing.T) {
	root := t.TempDir()
	writeAutoApproveRepo(t, root)
	launch := func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return nil
	}
	// Should not panic with nil stderr when name is "vscode" and auto-approved skills exist.
	err := RunWithStderr(context.Background(), root, "vscode", func(cfg *config.Config) *bool {
		v := (cfg.Agents.VSCode.Enabled != nil && *cfg.Agents.VSCode.Enabled) ||
			(cfg.Agents.ClaudeVSCode.Enabled != nil && *cfg.Agents.ClaudeVSCode.Enabled)
		return &v
	}, launch, nil, "v1.0.0", nil)
	if err != nil {
		t.Fatalf("RunWithStderr nil writer vscode: %v", err)
	}
}

func TestRunWithStderr_AutoApproveInfoLine(t *testing.T) {
	root := t.TempDir()
	writeAutoApproveRepo(t, root)
	var stderr bytes.Buffer
	launch := func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return nil
	}
	err := RunWithStderr(context.Background(), root, "vscode", func(cfg *config.Config) *bool {
		v := (cfg.Agents.VSCode.Enabled != nil && *cfg.Agents.VSCode.Enabled) ||
			(cfg.Agents.ClaudeVSCode.Enabled != nil && *cfg.Agents.ClaudeVSCode.Enabled)
		return &v
	}, launch, nil, "v1.0.0", &stderr)
	if err != nil {
		t.Fatalf("RunWithStderr: %v", err)
	}
	output := stderr.String()
	if !strings.Contains(output, "[auto-approve] skills: approved") {
		t.Fatalf("expected auto-approve info line, got %q", output)
	}
}

func TestRunWithStderr_AutoApproveNotPrintedWhenClaudeVSCodeDisabled(t *testing.T) {
	root := t.TempDir()
	// Config: claude=true (CLI auto-approve relevant), claude-vscode=false, vscode=true.
	// Auto-approve should NOT be printed for name="vscode" because claude-vscode is disabled.
	writeAutoApproveRepoCustom(t, root, true, false, true)
	var stderr bytes.Buffer
	launch := func(project *config.ProjectConfig, runInfo *run.Info, env []string, args []string) error {
		return nil
	}
	err := RunWithStderr(context.Background(), root, "vscode", func(cfg *config.Config) *bool {
		v := (cfg.Agents.VSCode.Enabled != nil && *cfg.Agents.VSCode.Enabled) ||
			(cfg.Agents.ClaudeVSCode.Enabled != nil && *cfg.Agents.ClaudeVSCode.Enabled)
		return &v
	}, launch, nil, "v1.0.0", &stderr)
	if err != nil {
		t.Fatalf("RunWithStderr: %v", err)
	}
	output := stderr.String()
	if strings.Contains(output, "[auto-approve]") {
		t.Fatalf("expected no auto-approve info line for vscode without claude-vscode, got %q", output)
	}
}

func writeAutoApproveRepo(t *testing.T, root string) {
	t.Helper()
	writeAutoApproveRepoCustom(t, root, false, true, false)
}

func writeAutoApproveRepoCustom(t *testing.T, root string, claude, claudeVSCode, vscode bool) {
	t.Helper()
	paths := config.DefaultPaths(root)
	dirs := []string{paths.InstructionsDir, paths.SlashCommandsDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	configToml := fmt.Sprintf(`
[approvals]
mode = "all"

[agents.gemini]
enabled = false

[agents.claude]
enabled = %t

[agents.claude-vscode]
enabled = %t

[agents.codex]
enabled = false

[agents.vscode]
enabled = %t

[agents.antigravity]
enabled = false

[warnings]
instruction_token_threshold = 50000
mcp_server_threshold = 5
`, claude, claudeVSCode, vscode)
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	gitignoreBlock := "# test gitignore content\n"
	if err := os.WriteFile(filepath.Join(paths.Root, ".agent-layer", "gitignore.block"), []byte(gitignoreBlock), 0o644); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_base.md"), []byte("base"), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	command := "---\ndescription: Auto-approved skill\nauto-approve: true\n---\n\nDo it."
	if err := os.WriteFile(filepath.Join(paths.SlashCommandsDir, "approved.md"), []byte(command), 0o644); err != nil {
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
