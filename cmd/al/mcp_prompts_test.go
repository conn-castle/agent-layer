package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestMCPPromptsCmdRunE_Success(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)
	t.Setenv(config.BuiltinRepoRootEnvVar, "")

	original := runPromptServer
	t.Cleanup(func() { runPromptServer = original })

	called := false
	runPromptServer = func(ctx context.Context, version string, commands []config.SlashCommand) error {
		called = true
		if ctx == nil {
			t.Fatal("expected non-nil context")
		}
		if version != Version {
			t.Fatalf("expected version %q, got %q", Version, version)
		}
		if len(commands) != 1 || commands[0].Name != "alpha" {
			t.Fatalf("unexpected slash commands payload: %#v", commands)
		}
		return nil
	}

	testutil.WithWorkingDir(t, root, func() {
		cmd := newMcpPromptsCmd()
		cmd.SetContext(context.Background())
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("RunE error: %v", err)
		}
	})

	if !called {
		t.Fatal("expected runPromptServer to be called")
	}
}

func TestMCPPromptsCmdRunE_PropagatesRunnerError(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)
	t.Setenv(config.BuiltinRepoRootEnvVar, "")

	original := runPromptServer
	t.Cleanup(func() { runPromptServer = original })

	wantErr := errors.New("prompt server failed")
	runPromptServer = func(context.Context, string, []config.SlashCommand) error {
		return wantErr
	}

	testutil.WithWorkingDir(t, root, func() {
		cmd := newMcpPromptsCmd()
		cmd.SetContext(context.Background())
		err := cmd.RunE(cmd, nil)
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
	})
}

func TestMCPPromptsCmdRunE_ResolveRootError(t *testing.T) {
	t.Setenv(config.BuiltinRepoRootEnvVar, "")

	originalGetwd := getwd
	getwd = func() (string, error) { return "", errors.New("getwd failed") }
	t.Cleanup(func() { getwd = originalGetwd })

	cmd := newMcpPromptsCmd()
	cmd.SetContext(context.Background())
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected resolve root error")
	}
	if !strings.Contains(err.Error(), "getwd failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMCPPromptsCmdRunE_LoadProjectConfigError(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	t.Setenv(config.BuiltinRepoRootEnvVar, root)

	cmd := newMcpPromptsCmd()
	cmd.SetContext(context.Background())
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected load config error")
	}
	if !strings.Contains(err.Error(), "config.toml") {
		t.Fatalf("expected config path error, got: %v", err)
	}
}
