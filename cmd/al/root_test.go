package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/nicholasjconn/agent-layer/internal/config"
)

func TestRootVersionFlag(t *testing.T) {
	cmd := newRootCmd()
	cmd.Version = "v1.2.3"
	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.SetArgs([]string{"--version"})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if strings.TrimSpace(out.String()) != "v1.2.3" {
		t.Fatalf("unexpected version output: %q", out.String())
	}
}

func TestRootHelp(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !strings.Contains(out.String(), "Agent Layer vNext") {
		t.Fatalf("expected help output, got %q", out.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestRootVersionFlagWriteError(t *testing.T) {
	cmd := newRootCmd()
	cmd.Version = "v1.2.3"
	cmd.SetArgs([]string{"--version"})
	cmd.SetOut(failingWriter{})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error when output fails")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStubCmd(t *testing.T) {
	cmd := newStubCmd("doctor")
	err := cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected not implemented error, got %v", err)
	}
}

func TestInstallAndSyncCommands(t *testing.T) {
	root := t.TempDir()
	withWorkingDir(t, root, func() {
		cmd := newInstallCmd()
		cmd.SetArgs([]string{"--no-wizard"})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(bytes.NewBufferString(""))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("install error: %v", err)
		}
		writeStub(t, root, "al")

		if err := newSyncCmd().RunE(nil, nil); err != nil {
			t.Fatalf("sync error: %v", err)
		}

		if _, err := os.Stat(filepath.Join(root, ".agent-layer", "config.toml")); err != nil {
			t.Fatalf("expected config.toml to exist: %v", err)
		}
	})
}

func TestInstallCommandNoWizardSkipsWizard(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	originalRunWizard := runWizard
	wizardCalled := false
	runWizard = func(_ string) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizard = originalRunWizard })

	cmd := newInstallCmd()
	cmd.SetArgs([]string{"--no-wizard"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("y\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install error: %v", err)
	}
	if wizardCalled {
		t.Fatalf("expected wizard to be skipped with --no-wizard")
	}
}

func TestInstallCommandPromptYesRunsWizard(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	originalRunWizard := runWizard
	wizardCalled := false
	runWizard = func(_ string) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizard = originalRunWizard })

	cmd := newInstallCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("y\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install error: %v", err)
	}
	if !wizardCalled {
		t.Fatalf("expected wizard to run after confirmation")
	}
}

func TestInstallCommandNonInteractiveSkipsWizard(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	originalRunWizard := runWizard
	wizardCalled := false
	runWizard = func(_ string) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizard = originalRunWizard })

	cmd := newInstallCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("install error: %v", err)
	}
	if wizardCalled {
		t.Fatalf("expected wizard to be skipped in non-interactive mode")
	}
}

func TestClientCommandsMissingConfig(t *testing.T) {
	root := t.TempDir()
	withWorkingDir(t, root, func() {
		commands := []*cobra.Command{
			newGeminiCmd(),
			newClaudeCmd(),
			newCodexCmd(),
			newVSCodeCmd(),
			newAntigravityCmd(),
			newMcpPromptsCmd(),
		}
		for _, cmd := range commands {
			err := cmd.RunE(cmd, nil)
			if err == nil {
				t.Fatalf("expected error for %s", cmd.Use)
			}
		}
	})
}

func TestClientCommandsSuccess(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	binDir := t.TempDir()
	writeStub(t, binDir, "gemini")
	writeStub(t, binDir, "claude")
	writeStub(t, binDir, "codex")
	writeStub(t, binDir, "code")
	writeStub(t, binDir, "antigravity")

	t.Setenv("PATH", binDir)

	original := runPromptServer
	t.Cleanup(func() { runPromptServer = original })
	runPromptServer = func(ctx context.Context, version string, commands []config.SlashCommand) error {
		return nil
	}

	withWorkingDir(t, root, func() {
		commands := []*cobra.Command{
			newGeminiCmd(),
			newClaudeCmd(),
			newCodexCmd(),
			newVSCodeCmd(),
			newAntigravityCmd(),
			newMcpPromptsCmd(),
		}
		for _, cmd := range commands {
			if err := cmd.RunE(cmd, nil); err != nil {
				t.Fatalf("command %s failed: %v", cmd.Use, err)
			}
		}
	})
}

func TestDoctorCommand(t *testing.T) {
	root := t.TempDir()

	// Test failure (no repo)
	withWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected doctor failure in empty dir")
		}
	})

	// Test success
	writeTestRepo(t, root)
	withWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		// Capture output? doctor prints to stdout.
		// We just care about return code for now.
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("doctor failed in valid repo: %v", err)
		}
	})
}

func TestWizardCommand(t *testing.T) {
	originalIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	root := t.TempDir()
	withWorkingDir(t, root, func() {
		// Force the non-interactive path to keep tests deterministic.
		cmd := newWizardCmd()
		err := cmd.RunE(cmd, nil)
		// Should fail because not interactive
		if err == nil {
			t.Fatal("expected wizard to fail in non-interactive test")
		}
		if !strings.Contains(err.Error(), "interactive terminal") {
			t.Logf("got error: %v", err)
		}
	})
}

func TestCommandsGetwdError(t *testing.T) {
	original := getwd
	getwd = func() (string, error) {
		return "", errors.New("boom")
	}
	t.Cleanup(func() { getwd = original })

	commands := []*cobra.Command{
		newInstallCmd(),
		newSyncCmd(),
		newMcpPromptsCmd(),
		newGeminiCmd(),
		newClaudeCmd(),
		newCodexCmd(),
		newVSCodeCmd(),
		newAntigravityCmd(),
	}
	for _, cmd := range commands {
		if err := cmd.RunE(cmd, nil); err == nil {
			t.Fatalf("expected error for %s", cmd.Use)
		}
	}
}

func writeTestRepo(t *testing.T, root string) {
	t.Helper()
	paths := config.DefaultPaths(root)
	if err := os.MkdirAll(paths.InstructionsDir, 0o755); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SlashCommandsDir, 0o755); err != nil {
		t.Fatalf("mkdir slash commands: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	configToml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true
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
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o644); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}
	writeStub(t, root, "al")
}

func writeStub(t *testing.T, dir string, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore chdir: %v", err)
		}
	}()
	fn()
}
