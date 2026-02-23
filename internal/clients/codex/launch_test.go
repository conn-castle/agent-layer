package codex

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

func TestEnsureCodexHomeSetsDefault(t *testing.T) {
	root := t.TempDir()
	env := []string{}

	env = ensureCodexHome(root, env)

	expected := filepath.Join(root, ".codex")
	value, ok := clients.GetEnv(env, "CODEX_HOME")
	if !ok || value != expected {
		t.Fatalf("expected CODEX_HOME %s, got %s", expected, value)
	}
}

func TestEnsureCodexHomeKeepsMatching(t *testing.T) {
	root := t.TempDir()
	expected := filepath.Join(root, ".codex")
	env := []string{"CODEX_HOME=" + expected}

	env = ensureCodexHome(root, env)

	value, ok := clients.GetEnv(env, "CODEX_HOME")
	if !ok || value != expected {
		t.Fatalf("expected CODEX_HOME %s, got %s", expected, value)
	}
}

func TestLaunchCodex(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStubWithExit(t, binDir, "codex", 0)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					Model:           "model",
					ReasoningEffort: "low",
				},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "other"))
	env := os.Environ()

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}
}

func TestLaunchCodexError(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStubWithExit(t, binDir, "codex", 1)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					Model:           "model",
					ReasoningEffort: "low",
				},
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

func TestEnsureCodexHomeWarnsOnMismatch(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(t.TempDir(), "other")
	env := []string{"CODEX_HOME=" + current}

	// Capture stderr to verify the warning is emitted.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	out := ensureCodexHome(root, env)
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	var buf [4096]byte
	n, _ := r.Read(buf[:])
	stderr := string(buf[:n])

	// Warn-and-preserve: the original value must be kept.
	value, ok := clients.GetEnv(out, "CODEX_HOME")
	if !ok || value != current {
		t.Fatalf("expected CODEX_HOME to remain %s, got %s", current, value)
	}

	// Verify warning was actually emitted to stderr.
	expected := filepath.Join(root, ".codex")
	wantWarning := fmt.Sprintf(messages.ClientsCodexHomeWarningFmt, current, expected)
	if !strings.Contains(stderr, wantWarning) {
		t.Fatalf("expected stderr to contain warning %q, got %q", wantWarning, stderr)
	}
}

func TestEnsureCodexHome_WarningWriteFailureLeavesEnvUnchanged(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(t.TempDir(), "other")
	env := []string{"CODEX_HOME=" + current}

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

	out := ensureCodexHome(root, env)
	value, ok := clients.GetEnv(out, "CODEX_HOME")
	if !ok || value != current {
		t.Fatalf("expected CODEX_HOME to remain unchanged, got %q", value)
	}
}
