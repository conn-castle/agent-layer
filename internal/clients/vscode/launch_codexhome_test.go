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
	if !strings.Contains(string(got), "CODEX_HOME=") {
		t.Fatal("expected CODEX_HOME to be set when agents.vscode is enabled")
	}
}
