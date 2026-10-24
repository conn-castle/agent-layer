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

func writeResolvableCodex(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "codex")
	t.Setenv("PATH", binDir)
	return filepath.Join(binDir, "codex")
}

func TestConfigureEnvironmentCodexHomeSetsDefaultWhenEnabled(t *testing.T) {
	root := t.TempDir()
	env := []string{}
	localConfigDir := true

	env = ConfigureEnvironment(root, env, config.CodexConfig{LocalConfigDir: &localConfigDir}, os.Stderr)

	expected := filepath.Join(root, ".codex")
	value, ok := clients.GetEnv(env, "CODEX_HOME")
	if !ok || value != expected {
		t.Fatalf("expected CODEX_HOME %s, got %s", expected, value)
	}
}

func TestConfigureEnvironmentCodexHomeNoopWhenDisabled(t *testing.T) {
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
			out := ConfigureEnvironment(root, tc.env, config.CodexConfig{}, os.Stderr)

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
	call := testutil.CaptureExec(t, &execFunc, nil)
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

	call.AssertCalled(t, codexPath, []string{"codex", "--search"})
	if !reflect.DeepEqual(call.Env, env) {
		t.Fatalf("expected env to pass through unchanged, got %#v want %#v", call.Env, env)
	}
}

func TestLaunchCodexSetsCodexHomeWhenEnabled(t *testing.T) {
	root := t.TempDir()
	codexPath := writeResolvableCodex(t)
	call := testutil.CaptureExec(t, &execFunc, nil)
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
	value, ok := clients.GetEnv(call.Env, "CODEX_HOME")
	if !ok || value != expected {
		t.Fatalf("expected CODEX_HOME %s, got %#v", expected, call.Env)
	}
}

func TestLaunchCodexExecError(t *testing.T) {
	root := t.TempDir()
	writeResolvableCodex(t)
	wantErr := errors.New("exec failed")
	testutil.CaptureExec(t, &execFunc, wantErr)

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
	testutil.ForbidExec(t, &execFunc)

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

func TestConfigureEnvironmentCodexHomeWarnsOnMismatchWhenEnabled(t *testing.T) {
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

	out := ConfigureEnvironment(root, env, config.CodexConfig{LocalConfigDir: &localConfigDir}, os.Stderr)
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

func TestEnsureSelectedHome(t *testing.T) {
	enabled, disabled := true, false
	for _, tc := range []struct {
		name     string
		enabled  *bool
		external bool
		alias    bool
		existing os.FileMode
		create   bool
	}{
		{name: "creates missing selected home", enabled: &enabled, create: true},
		{name: "keeps existing selected home", enabled: &enabled, existing: 0o755},
		{name: "disabled does not create inherited project home", enabled: &disabled},
		{name: "external inherited home is not created", enabled: &enabled, external: true},
		{name: "creates home selected through a root alias", enabled: &enabled, alias: true, create: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, ".codex")
			selected := home
			if tc.external {
				selected = filepath.Join(t.TempDir(), "external")
			}
			if tc.alias {
				link := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(root, link); err != nil {
					t.Fatal(err)
				}
				selected = filepath.Join(link, ".codex")
			}
			if tc.existing != 0 {
				if err := os.Mkdir(home, tc.existing); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(home, tc.existing); err != nil {
					t.Fatal(err)
				}
			}

			if err := EnsureSelectedHome(root, []string{"CODEX_HOME=" + selected}, config.CodexConfig{LocalConfigDir: tc.enabled}); err != nil {
				t.Fatal(err)
			}

			info, err := os.Lstat(home)
			switch {
			case tc.create:
				if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
					t.Fatalf("home=%v err=%v, want 0700 directory", info, err)
				}
			case tc.existing != 0:
				if err != nil || info.Mode().Perm() != tc.existing {
					t.Fatalf("existing home=%v err=%v, want mode %v unchanged", info, err, tc.existing)
				}
			default:
				if !os.IsNotExist(err) {
					t.Fatalf("home was created: %v", err)
				}
			}
			if tc.external {
				if _, err := os.Lstat(selected); !os.IsNotExist(err) {
					t.Fatalf("external home was created: %v", err)
				}
			}
		})
	}
}

func TestEnsureSelectedHomeRejectsBlockingEntry(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(home string) error
	}{
		{name: "regular file", plant: func(home string) error { return os.WriteFile(home, []byte("user bytes"), 0o600) }},
		{name: "dangling symlink", plant: func(home string) error { return os.Symlink(filepath.Join(filepath.Dir(home), "missing"), home) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, ".codex")
			if err := tc.plant(home); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(home)
			if err != nil {
				t.Fatal(err)
			}
			enabled := true

			err = EnsureSelectedHome(root, []string{"CODEX_HOME=" + home}, config.CodexConfig{LocalConfigDir: &enabled})
			if err == nil || err.Error() != fmt.Sprintf(messages.ClientsCodexHomeNotDirectoryFmt, home) {
				t.Fatalf("error=%v", err)
			}
			after, err := os.Lstat(home)
			if err != nil || after.Mode() != before.Mode() || after.Size() != before.Size() {
				t.Fatalf("blocking entry changed: %v %v", after, err)
			}
		})
	}
}
