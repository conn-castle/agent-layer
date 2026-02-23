package vscode

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
)

func TestLaunchVSCode_NoCODEXHOMEWhenVSCodeDisabled(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	// Write a stub that dumps env to a file so we can inspect it.
	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	vscodeDisabled := false
	claudeVSCodeEnabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				VSCode:       config.EnableOnlyConfig{Enabled: &vscodeDisabled},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: &claudeVSCodeEnabled},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	// Filter out CODEX_HOME from the environment so the test only detects
	// additions made by the launcher itself.
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CODEX_HOME=") {
			env = append(env, e)
		}
	}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if strings.Contains(string(got), "CODEX_HOME=") {
		t.Fatal("expected CODEX_HOME to NOT be set when agents.vscode is disabled")
	}
}

func TestLaunchVSCode_ClearsInheritedCODEXHOMEWhenVSCodeDisabled(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	vscodeDisabled := false
	claudeVSCodeEnabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				VSCode:       config.EnableOnlyConfig{Enabled: &vscodeDisabled},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: &claudeVSCodeEnabled},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	// Inject a stale CODEX_HOME into the environment to simulate inheritance.
	env := append(os.Environ(), "CODEX_HOME=/stale/path")
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if strings.Contains(string(got), "CODEX_HOME=") {
		t.Fatal("expected inherited CODEX_HOME to be cleared when agents.vscode is disabled")
	}
}

func TestLaunchVSCode_SetsCODEXHOMEWhenVSCodeEnabled(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	// Write a stub that dumps env to a file so we can inspect it.
	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	vscodeEnabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				VSCode: config.EnableOnlyConfig{Enabled: &vscodeEnabled},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	// Filter out CODEX_HOME from the environment.
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CODEX_HOME=") {
			env = append(env, e)
		}
	}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	expected := "CODEX_HOME=" + filepath.Join(root, ".codex")
	if !strings.Contains(string(got), expected) {
		t.Fatalf("expected CODEX_HOME to be set to %s, got env:\n%s", filepath.Join(root, ".codex"), string(got))
	}
}

func TestLaunchVSCode_SetsCLAUDECONFIGDIRWhenClaudeVSCodeEnabled(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	claudeVSCodeEnabled := true
	localConfigDir := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude:       config.ClaudeConfig{LocalConfigDir: &localConfigDir},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: &claudeVSCodeEnabled},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDE_CONFIG_DIR=") && !strings.HasPrefix(e, "CODEX_HOME=") {
			env = append(env, e)
		}
	}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	expected := "CLAUDE_CONFIG_DIR=" + filepath.Join(root, ".claude-config")
	if !strings.Contains(string(got), expected) {
		t.Fatalf("expected CLAUDE_CONFIG_DIR to be set to %s, got env:\n%s", filepath.Join(root, ".claude-config"), string(got))
	}
}

func TestLaunchVSCode_ClearsInheritedCLAUDECONFIGDIRWhenClaudeVSCodeDisabled(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	claudeVSCodeDisabled := false
	vscodeEnabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				VSCode:       config.EnableOnlyConfig{Enabled: &vscodeEnabled},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: &claudeVSCodeDisabled},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	// Inject a stale repo-local CLAUDE_CONFIG_DIR to simulate inheritance.
	env := append(os.Environ(), "CLAUDE_CONFIG_DIR="+filepath.Join(root, ".claude-config"))
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if strings.Contains(string(got), "CLAUDE_CONFIG_DIR=") {
		t.Fatal("expected inherited CLAUDE_CONFIG_DIR to be cleared when agents.claude-vscode is disabled")
	}
}

func TestLaunchVSCode_PreservesInheritedCLAUDECONFIGDIRWhenClaudeVSCodeDisabled(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	claudeVSCodeDisabled := false
	vscodeEnabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				VSCode:       config.EnableOnlyConfig{Enabled: &vscodeEnabled},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: &claudeVSCodeDisabled},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	userDir := filepath.Join(t.TempDir(), "global-claude")
	env := append(os.Environ(), "CLAUDE_CONFIG_DIR="+userDir)
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if !strings.Contains(string(got), "CLAUDE_CONFIG_DIR="+userDir) {
		t.Fatalf("expected user CLAUDE_CONFIG_DIR to be preserved, got env:\n%s", string(got))
	}
}

func TestLaunchVSCode_BothVarsWhenBothEnabled(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	vscodeEnabled := true
	claudeVSCodeEnabled := true
	localConfigDir := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				VSCode:       config.EnableOnlyConfig{Enabled: &vscodeEnabled},
				Claude:       config.ClaudeConfig{LocalConfigDir: &localConfigDir},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: &claudeVSCodeEnabled},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CODEX_HOME=") && !strings.HasPrefix(e, "CLAUDE_CONFIG_DIR=") {
			env = append(env, e)
		}
	}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	envStr := string(got)
	expectedCodexHome := "CODEX_HOME=" + filepath.Join(root, ".codex")
	if !strings.Contains(envStr, expectedCodexHome) {
		t.Fatalf("expected CODEX_HOME to be set to %s, got env:\n%s", filepath.Join(root, ".codex"), envStr)
	}
	expectedClaudeConfigDir := "CLAUDE_CONFIG_DIR=" + filepath.Join(root, ".claude-config")
	if !strings.Contains(envStr, expectedClaudeConfigDir) {
		t.Fatalf("expected CLAUDE_CONFIG_DIR to be set to %s, got env:\n%s", filepath.Join(root, ".claude-config"), envStr)
	}
}

func TestLaunchVSCode_ClearsCLAUDECONFIGDIRWhenLocalConfigDirDisabled(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	// claude-vscode enabled but local_config_dir not set (nil) — should clear.
	claudeVSCodeEnabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: &claudeVSCodeEnabled},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	// Inject stale repo-local CLAUDE_CONFIG_DIR to simulate inheritance.
	env := append(os.Environ(), "CLAUDE_CONFIG_DIR="+filepath.Join(root, ".claude-config"))
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if strings.Contains(string(got), "CLAUDE_CONFIG_DIR=") {
		t.Fatal("expected CLAUDE_CONFIG_DIR to be cleared when local_config_dir is nil")
	}
}

func TestLaunchVSCode_PreservesInheritedCLAUDECONFIGDIRWhenLocalConfigDirDisabled(t *testing.T) {
	origLookPath := lookPath
	origReadFile := readFile
	t.Cleanup(func() {
		lookPath = origLookPath
		readFile = origReadFile
	})

	root := t.TempDir()
	binDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "code")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	claudeVSCodeEnabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: &claudeVSCodeEnabled},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	userDir := filepath.Join(t.TempDir(), "global-claude")
	env := append(os.Environ(), "CLAUDE_CONFIG_DIR="+userDir)
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if !strings.Contains(string(got), "CLAUDE_CONFIG_DIR="+userDir) {
		t.Fatalf("expected user CLAUDE_CONFIG_DIR to be preserved, got env:\n%s", string(got))
	}
}
