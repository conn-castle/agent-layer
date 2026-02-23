package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestLaunchClaude(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "claude")

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}
}

func TestLaunchClaudeError(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStubWithExit(t, binDir, "claude", 1)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestLaunchClaudeYOLO(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStubExpectArg(t, binDir, "claude", "--dangerously-skip-permissions")

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "yolo"},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}
}

func TestEnsureClaudeConfigDirSetsDefault(t *testing.T) {
	root := t.TempDir()
	env := []string{}

	env = ensureClaudeConfigDir(root, env)

	expected := filepath.Join(root, ".claude-config")
	value, ok := clients.GetEnv(env, "CLAUDE_CONFIG_DIR")
	if !ok || value != expected {
		t.Fatalf("expected CLAUDE_CONFIG_DIR %s, got %s", expected, value)
	}
}

func TestEnsureClaudeConfigDirKeepsMatching(t *testing.T) {
	root := t.TempDir()
	expected := filepath.Join(root, ".claude-config")
	env := []string{"CLAUDE_CONFIG_DIR=" + expected}

	env = ensureClaudeConfigDir(root, env)

	value, ok := clients.GetEnv(env, "CLAUDE_CONFIG_DIR")
	if !ok || value != expected {
		t.Fatalf("expected CLAUDE_CONFIG_DIR %s, got %s", expected, value)
	}
}

func TestEnsureClaudeConfigDirWarnsOnMismatch(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(t.TempDir(), "other")
	env := []string{"CLAUDE_CONFIG_DIR=" + current}

	// Capture stderr to verify the warning is emitted.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	out := ensureClaudeConfigDir(root, env)
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	var buf [4096]byte
	n, _ := r.Read(buf[:])
	stderr := string(buf[:n])

	// Warn-and-preserve: the original value must be kept.
	value, ok := clients.GetEnv(out, "CLAUDE_CONFIG_DIR")
	if !ok || value != current {
		t.Fatalf("expected CLAUDE_CONFIG_DIR to remain %s, got %s", current, value)
	}

	// Verify warning was actually emitted to stderr.
	expected := filepath.Join(root, ".claude-config")
	wantWarning := fmt.Sprintf(messages.ClientsClaudeConfigDirWarningFmt, current, expected)
	if !strings.Contains(stderr, wantWarning) {
		t.Fatalf("expected stderr to contain warning %q, got %q", wantWarning, stderr)
	}
}

func TestEnsureClaudeConfigDir_WarningWriteFailureLeavesEnvUnchanged(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(t.TempDir(), "other")
	env := []string{"CLAUDE_CONFIG_DIR=" + current}

	origStderr := os.Stderr
	_, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if err := stderrWriter.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	os.Stderr = stderrWriter
	t.Cleanup(func() { os.Stderr = origStderr })

	out := ensureClaudeConfigDir(root, env)
	value, ok := clients.GetEnv(out, "CLAUDE_CONFIG_DIR")
	if !ok || value != current {
		t.Fatalf("expected CLAUDE_CONFIG_DIR to remain unchanged, got %q", value)
	}
}

func TestLaunchClaude_NoLocalConfigDir(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "claude")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	// LocalConfigDir is nil (default) — CLAUDE_CONFIG_DIR should NOT be set.
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDE_CONFIG_DIR=") {
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
	if strings.Contains(string(got), "CLAUDE_CONFIG_DIR=") {
		t.Fatal("expected CLAUDE_CONFIG_DIR to NOT be set when local_config_dir is nil")
	}
}

func TestLaunchClaude_SetsClaudeConfigDirWhenEnabled(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "claude")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	localConfigDir := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{
					Model:          "test-model",
					LocalConfigDir: &localConfigDir,
				},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDE_CONFIG_DIR=") {
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
		t.Fatalf("expected CLAUDE_CONFIG_DIR=%s, got env:\n%s", filepath.Join(root, ".claude-config"), string(got))
	}
}

func TestLaunchClaude_ClearsStaleClaudeConfigDir(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "claude")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	// local_config_dir is nil (disabled). Simulate stale inherited
	// CLAUDE_CONFIG_DIR that matches the repo-local path Agent Layer
	// would have set — it must be cleared.
	stale := filepath.Join(root, ".claude-config")
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := []string{"PATH=" + binDir, "CLAUDE_CONFIG_DIR=" + stale}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if strings.Contains(string(got), "CLAUDE_CONFIG_DIR=") {
		t.Fatal("expected stale CLAUDE_CONFIG_DIR to be cleared when local_config_dir is disabled")
	}
}

func TestLaunchClaude_PreservesUserClaudeConfigDir(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()

	envFile := filepath.Join(t.TempDir(), "env.txt")
	stubPath := filepath.Join(binDir, "claude")
	stubContent := fmt.Sprintf("#!/bin/sh\n/usr/bin/env > %s\n", envFile)
	if err := os.WriteFile(stubPath, []byte(stubContent), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	// local_config_dir is nil (disabled). A user-set CLAUDE_CONFIG_DIR
	// pointing elsewhere (not the repo-local path) must be preserved.
	userDir := filepath.Join(t.TempDir(), "my-custom-claude")
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := []string{"PATH=" + binDir, "CLAUDE_CONFIG_DIR=" + userDir}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	got, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	want := "CLAUDE_CONFIG_DIR=" + userDir
	if !strings.Contains(string(got), want) {
		t.Fatalf("expected user CLAUDE_CONFIG_DIR to be preserved, got env:\n%s", string(got))
	}
}

func TestClearStaleClaudeConfigDir_MatchingPath(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, ".claude-config")
	env := []string{"CLAUDE_CONFIG_DIR=" + stale}

	out := clearStaleClaudeConfigDir(root, env)

	if _, ok := clients.GetEnv(out, "CLAUDE_CONFIG_DIR"); ok {
		t.Fatal("expected CLAUDE_CONFIG_DIR to be cleared for matching repo-local path")
	}
}

func TestClearStaleClaudeConfigDir_DifferentPath(t *testing.T) {
	root := t.TempDir()
	userDir := "/custom/claude"
	env := []string{"CLAUDE_CONFIG_DIR=" + userDir}

	out := clearStaleClaudeConfigDir(root, env)

	value, ok := clients.GetEnv(out, "CLAUDE_CONFIG_DIR")
	if !ok || value != userDir {
		t.Fatalf("expected CLAUDE_CONFIG_DIR to be preserved, got %q", value)
	}
}

func TestClearStaleClaudeConfigDir_NotSet(t *testing.T) {
	root := t.TempDir()
	env := []string{}

	out := clearStaleClaudeConfigDir(root, env)

	if _, ok := clients.GetEnv(out, "CLAUDE_CONFIG_DIR"); ok {
		t.Fatal("expected CLAUDE_CONFIG_DIR to remain unset")
	}
}
