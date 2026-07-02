package antigravity

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

func writeResolvableAgy(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "agy")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return filepath.Join(binDir, "agy")
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

func TestLaunchAntigravity(t *testing.T) {
	root := t.TempDir()
	agyPath := writeResolvableAgy(t)
	call := captureExec(t, nil)

	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	env := []string{"PATH=" + filepath.Dir(agyPath), disableAutoUpdateEnv + "=0"}
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, []string{"--debug"}); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	expectedGeminiDir := filepath.Join(root, ".agy")
	if info, err := os.Stat(expectedGeminiDir); err != nil {
		t.Fatalf("expected repo-local gemini dir: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", expectedGeminiDir)
	}

	assertExecCalled(t, call, agyPath, []string{"agy", "--gemini_dir=" + expectedGeminiDir, "--debug"})
	value, ok := clients.GetEnv(call.env, disableAutoUpdateEnv)
	if !ok || value != "1" {
		t.Fatalf("expected %s=1 in exec env, got %#v", disableAutoUpdateEnv, call.env)
	}
}

func TestLaunchAntigravityYOLO(t *testing.T) {
	root := t.TempDir()
	agyPath := writeResolvableAgy(t)
	call := captureExec(t, nil)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
		},
		Root: root,
	}

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{"PATH=" + filepath.Dir(agyPath)}, []string{"--debug"}); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	expectedGeminiDir := filepath.Join(root, ".agy")
	assertExecCalled(t, call, agyPath, []string{"agy", "--gemini_dir=" + expectedGeminiDir, "--dangerously-skip-permissions", "--debug"})
}

func TestLaunchAntigravityExecError(t *testing.T) {
	root := t.TempDir()
	writeResolvableAgy(t)
	wantErr := errors.New("exec failed")
	captureExec(t, wantErr)

	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, []string{}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected exec error to wrap %v, got %v", wantErr, err)
	}
	if !strings.Contains(err.Error(), "antigravity exec handoff failed") {
		t.Fatalf("expected exec handoff context, got %v", err)
	}
}

func TestLaunchAntigravityRelativeRootFails(t *testing.T) {
	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   "relative",
	}

	err := Launch(cfg, &run.Info{ID: "id", Dir: "relative"}, nil, nil)
	if err == nil {
		t.Fatal("expected relative root error")
	}
	// Surface the root path in the error so the caller can fix the right
	// thing — F-A-10 explicitly called out that the prior message named
	// `.agy` instead of `cfg.Root`.
	if !strings.Contains(err.Error(), "relative") {
		t.Fatalf("expected error to name the relative root, got: %v", err)
	}
}

// TestLaunchAntigravityMissingBinary covers the LookPath preflight: when `agy`
// is not discoverable, the user must receive a targeted install hint instead of
// an exec handoff failure that wrongly implies the binary ran.
func TestLaunchAntigravityMissingBinary(t *testing.T) {
	originalLookPath := lookPathFunc
	lookPathFunc = func(string) (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { lookPathFunc = originalLookPath })
	forbidExec(t)

	root := t.TempDir()
	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}
	err := Launch(cfg, &run.Info{ID: "id", Dir: root}, os.Environ(), nil)
	if err == nil {
		t.Fatal("expected error when agy is missing from PATH")
	}
	if !strings.Contains(err.Error(), "agy") {
		t.Fatalf("expected error to mention agy, got: %v", err)
	}
	if !strings.Contains(err.Error(), "antigravity.google") {
		t.Fatalf("expected error to include install hint, got: %v", err)
	}
	// Missing-binary failures must NOT pollute the user's repo with a stray
	// .agy/ directory. The preflight runs before MkdirAll.
	if _, statErr := os.Stat(filepath.Join(root, ".agy")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no .agy/ directory after missing-binary failure, got stat err = %v", statErr)
	}
}
