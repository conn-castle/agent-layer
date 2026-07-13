package claude

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func writeResolvableClaude(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "claude")
	t.Setenv("PATH", binDir)
	return filepath.Join(binDir, "claude")
}

func TestLaunchClaudeExecHandoff(t *testing.T) {
	root := t.TempDir()
	claudePath := writeResolvableClaude(t)
	call := testutil.CaptureExec(t, &execFunc, nil)
	env := []string{"PATH=" + filepath.Dir(claudePath), "CUSTOM=1"}

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, []string{"--print", "hello"}); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	call.AssertCalled(t, claudePath, []string{"claude", "--model", "test-model", "--print", "hello"})
	if !reflect.DeepEqual(call.Env, env) {
		t.Fatalf("expected env to pass through unchanged, got %#v want %#v", call.Env, env)
	}
}

func TestLaunchClaudeDoesNotApplyDispatchBackgroundWaitCeiling(t *testing.T) {
	const backgroundWaitCeilingEnv = "CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS"
	root := t.TempDir()
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{},
			},
		},
		Root: root,
	}

	for _, tt := range []struct {
		name string
		env  []string
	}{
		{name: "does not inject absent value"},
		{name: "does not replace caller value", env: []string{backgroundWaitCeilingEnv + "=600000"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			claudePath := writeResolvableClaude(t)
			call := testutil.CaptureExec(t, &execFunc, nil)
			env := append([]string{"PATH=" + filepath.Dir(claudePath)}, tt.env...)

			if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
				t.Fatalf("Launch error: %v", err)
			}
			call.AssertCalled(t, claudePath, []string{"claude"})
			if !reflect.DeepEqual(call.Env, env) {
				t.Fatalf("interactive environment changed: got %#v want %#v", call.Env, env)
			}
		})
	}
}

func TestLaunchClaudeExecError(t *testing.T) {
	root := t.TempDir()
	writeResolvableClaude(t)
	wantErr := errors.New("exec failed")
	testutil.CaptureExec(t, &execFunc, wantErr)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected exec error to wrap %v, got %v", wantErr, err)
	}
	if !strings.Contains(err.Error(), "claude exec handoff failed") {
		t.Fatalf("expected exec handoff context, got %v", err)
	}
}

func TestLaunchClaudeMissingBinary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	testutil.ForbidExec(t, &execFunc)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil)
	if err == nil {
		t.Fatal("expected missing binary error")
	}
	if !strings.Contains(err.Error(), "claude launcher requires `claude` on PATH") {
		t.Fatalf("expected lookup error to name claude, got %v", err)
	}
}

func TestLaunchClaudeEffortArgs(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name  string
		agent config.ClaudeConfig
		argv  []string
	}{
		{
			name: "max effort is passed",
			agent: config.ClaudeConfig{
				Model:           "opus",
				ReasoningEffort: "max",
			},
			argv: []string{"claude", "--model", "opus", "--effort", "max"},
		},
		{
			name: "non-max effort is passed",
			agent: config.ClaudeConfig{
				Model:           "opus",
				ReasoningEffort: "high",
			},
			argv: []string{"claude", "--model", "opus", "--effort", "high"},
		},
		{
			name:  "empty effort is omitted",
			agent: config.ClaudeConfig{Model: "opus"},
			argv:  []string{"claude", "--model", "opus"},
		},
		{
			name: "agent_specific effortLevel suppresses CLI effort",
			agent: config.ClaudeConfig{
				Model:           "opus",
				ReasoningEffort: "high",
				AgentSpecific: map[string]any{
					"effortLevel": "low",
				},
			},
			argv: []string{"claude", "--model", "opus"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claudePath := writeResolvableClaude(t)
			call := testutil.CaptureExec(t, &execFunc, nil)
			cfg := &config.ProjectConfig{
				Config: config.Config{
					Agents: config.AgentsConfig{Claude: tc.agent},
				},
				Root: root,
			}

			if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil); err != nil {
				t.Fatalf("Launch error: %v", err)
			}

			call.AssertCalled(t, claudePath, tc.argv)
		})
	}
}

func TestLaunchClaudeYOLO(t *testing.T) {
	root := t.TempDir()
	claudePath := writeResolvableClaude(t)
	call := testutil.CaptureExec(t, &execFunc, nil)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	call.AssertCalled(t, claudePath, []string{"claude", "--model", "test-model", "--dangerously-skip-permissions"})
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

func TestLaunchClaudeNoLocalConfigDir(t *testing.T) {
	root := t.TempDir()
	claudePath := writeResolvableClaude(t)
	call := testutil.CaptureExec(t, &execFunc, nil)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{"PATH=" + filepath.Dir(claudePath)}, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	if _, ok := clients.GetEnv(call.Env, "CLAUDE_CONFIG_DIR"); ok {
		t.Fatal("expected CLAUDE_CONFIG_DIR to NOT be set when local_config_dir is nil")
	}
}

func TestLaunchClaudeSetsClaudeConfigDirWhenEnabled(t *testing.T) {
	root := t.TempDir()
	claudePath := writeResolvableClaude(t)
	call := testutil.CaptureExec(t, &execFunc, nil)

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

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{"PATH=" + filepath.Dir(claudePath)}, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	expected := filepath.Join(root, ".claude-config")
	value, ok := clients.GetEnv(call.Env, "CLAUDE_CONFIG_DIR")
	if !ok || value != expected {
		t.Fatalf("expected CLAUDE_CONFIG_DIR=%s, got %#v", expected, call.Env)
	}
}

func TestLaunchClaudeClearsStaleClaudeConfigDir(t *testing.T) {
	root := t.TempDir()
	claudePath := writeResolvableClaude(t)
	call := testutil.CaptureExec(t, &execFunc, nil)

	stale := filepath.Join(root, ".claude-config")
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	env := []string{"PATH=" + filepath.Dir(claudePath), "CLAUDE_CONFIG_DIR=" + stale}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	if _, ok := clients.GetEnv(call.Env, "CLAUDE_CONFIG_DIR"); ok {
		t.Fatal("expected stale CLAUDE_CONFIG_DIR to be cleared when local_config_dir is disabled")
	}
}

func TestLaunchClaudePreservesUserClaudeConfigDir(t *testing.T) {
	root := t.TempDir()
	claudePath := writeResolvableClaude(t)
	call := testutil.CaptureExec(t, &execFunc, nil)

	userDir := filepath.Join(t.TempDir(), "my-custom-claude")
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Model: "test-model"},
			},
		},
		Root: root,
	}

	env := []string{"PATH=" + filepath.Dir(claudePath), "CLAUDE_CONFIG_DIR=" + userDir}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	value, ok := clients.GetEnv(call.Env, "CLAUDE_CONFIG_DIR")
	if !ok || value != userDir {
		t.Fatalf("expected user CLAUDE_CONFIG_DIR to be preserved, got %#v", call.Env)
	}
}
