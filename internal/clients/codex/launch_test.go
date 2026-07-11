package codex

import (
	"errors"
	"fmt"
	"io"
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

type execCall struct {
	called bool
	path   string
	argv   []string
	env    []string
}

func captureExec(t *testing.T, err error) *execCall {
	t.Helper()
	original := execFunc
	call := &execCall{}
	execFunc = func(path string, argv []string, env []string) error {
		if call.called {
			t.Fatal("execFunc called more than once")
		}
		call.called = true
		call.path = path
		call.argv = append([]string(nil), argv...)
		call.env = append([]string(nil), env...)
		return err
	}
	t.Cleanup(func() { execFunc = original })
	return call
}

func forbidExec(t *testing.T) {
	t.Helper()
	original := execFunc
	execFunc = func(string, []string, []string) error {
		t.Fatal("execFunc should not be called")
		return nil
	}
	t.Cleanup(func() { execFunc = original })
}

func writeResolvableCodex(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "codex")
	t.Setenv("PATH", binDir)
	return filepath.Join(binDir, "codex")
}

func assertExecCalled(t *testing.T, call *execCall, wantPath string, wantArgv []string) {
	t.Helper()
	if !call.called {
		t.Fatal("expected execFunc to be called")
	}
	if call.path != wantPath {
		t.Fatalf("expected exec path %q, got %q", wantPath, call.path)
	}
	if !reflect.DeepEqual(call.argv, wantArgv) {
		t.Fatalf("unexpected argv: got %#v want %#v", call.argv, wantArgv)
	}
}

func TestConfigureCodexHomeSetsDefaultWhenEnabled(t *testing.T) {
	root := t.TempDir()
	env := []string{}
	localConfigDir := true

	env = configureCodexHome(root, env, config.CodexConfig{LocalConfigDir: &localConfigDir})

	expected := filepath.Join(root, ".codex")
	value, ok := clients.GetEnv(env, "CODEX_HOME")
	if !ok || value != expected {
		t.Fatalf("expected CODEX_HOME %s, got %s", expected, value)
	}
}

func TestConfigureCodexHomeNoopWhenDisabled(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name string
		env  []string
	}{
		{name: "CODEX_HOME unset stays unset", env: []string{"PATH=/usr/bin"}},
		{name: "CODEX_HOME elsewhere preserved", env: []string{"CODEX_HOME=" + filepath.Join(t.TempDir(), "other")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Capture stderr to prove the disabled branch neither rewrites
			// CODEX_HOME nor warns. If the `if !CodexLocalConfigDirEnabled(cfg)
			// { return env }` early-return were removed, the unset case would
			// gain CODEX_HOME=<root>/.codex (env assertion flips) and the
			// elsewhere case would emit the mismatch warning (stderr assertion
			// flips), so this test can fail on a real defect.
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("pipe: %v", err)
			}
			origStderr := os.Stderr
			os.Stderr = w
			t.Cleanup(func() { os.Stderr = origStderr })

			want := append([]string(nil), tc.env...)
			// Empty CodexConfig => LocalConfigDir nil => opt-in disabled.
			out := configureCodexHome(root, tc.env, config.CodexConfig{})

			if err := w.Close(); err != nil {
				t.Fatalf("close pipe writer: %v", err)
			}
			stderrBytes, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("read stderr: %v", err)
			}
			if len(stderrBytes) != 0 {
				t.Fatalf("expected no stderr output when disabled, got %q", string(stderrBytes))
			}

			if !reflect.DeepEqual(out, want) {
				t.Fatalf("expected env returned unchanged %#v, got %#v", want, out)
			}
		})
	}
}

func TestLaunchCodexExecHandoff(t *testing.T) {
	root := t.TempDir()
	codexPath := writeResolvableCodex(t)
	call := captureExec(t, nil)
	env := []string{"PATH=" + filepath.Dir(codexPath), "CODEX_HOME=" + filepath.Join(t.TempDir(), "other")}

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, []string{"--search"}); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	assertExecCalled(t, call, codexPath, []string{"codex", "--search"})
	if !reflect.DeepEqual(call.env, env) {
		t.Fatalf("expected env to pass through unchanged, got %#v want %#v", call.env, env)
	}
}

func TestLaunchCodexSetsCodexHomeWhenEnabled(t *testing.T) {
	root := t.TempDir()
	codexPath := writeResolvableCodex(t)
	call := captureExec(t, nil)
	localConfigDir := true

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{LocalConfigDir: &localConfigDir},
			},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{"PATH=" + filepath.Dir(codexPath)}, nil); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	expected := filepath.Join(root, ".codex")
	value, ok := clients.GetEnv(call.env, "CODEX_HOME")
	if !ok || value != expected {
		t.Fatalf("expected CODEX_HOME %s, got %#v", expected, call.env)
	}
}

func TestLaunchCodexExecError(t *testing.T) {
	root := t.TempDir()
	writeResolvableCodex(t)
	wantErr := errors.New("exec failed")
	captureExec(t, wantErr)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{},
			},
		},
		Root: root,
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected exec error to wrap %v, got %v", wantErr, err)
	}
	if !strings.Contains(err.Error(), "codex exec handoff failed") {
		t.Fatalf("expected exec handoff context, got %v", err)
	}
}

func TestLaunchCodexMissingBinary(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	forbidExec(t)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{},
			},
		},
		Root: root,
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil)
	if err == nil {
		t.Fatal("expected missing binary error")
	}
	if !strings.Contains(err.Error(), "codex launcher requires `codex` on PATH") {
		t.Fatalf("expected lookup error to name codex, got %v", err)
	}
}

func TestConfigureCodexHomeWarnsOnMismatchWhenEnabled(t *testing.T) {
	root := t.TempDir()
	current := filepath.Join(t.TempDir(), "other")
	env := []string{"CODEX_HOME=" + current}
	localConfigDir := true

	// Capture stderr to verify the warning is emitted.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = origStderr })

	out := configureCodexHome(root, env, config.CodexConfig{LocalConfigDir: &localConfigDir})
	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	stderrBytes, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	stderr := string(stderrBytes)

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
