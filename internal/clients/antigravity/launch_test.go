package antigravity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func writeResolvableAgy(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "agy")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return filepath.Join(binDir, "agy")
}

func TestLaunchAntigravity(t *testing.T) {
	root := t.TempDir()
	agyPath := writeResolvableAgy(t)
	call := testutil.CaptureExec(t, &execFunc, nil)

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

	call.AssertCalled(t, agyPath, []string{"agy", "--gemini_dir=" + expectedGeminiDir, "--debug"})
	value, ok := clients.GetEnv(call.Env, disableAutoUpdateEnv)
	if !ok || value != "1" {
		t.Fatalf("expected %s=1 in exec env, got %#v", disableAutoUpdateEnv, call.Env)
	}
}

func TestLaunchAntigravityYOLO(t *testing.T) {
	root := t.TempDir()
	agyPath := writeResolvableAgy(t)
	call := testutil.CaptureExec(t, &execFunc, nil)

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
	call.AssertCalled(t, agyPath, []string{"agy", "--gemini_dir=" + expectedGeminiDir, "--dangerously-skip-permissions", "--debug"})
}

func TestLaunchAntigravityExecError(t *testing.T) {
	root := t.TempDir()
	writeResolvableAgy(t)
	wantErr := errors.New("exec failed")
	testutil.CaptureExec(t, &execFunc, wantErr)

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
	testutil.ForbidExec(t, &execFunc)

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

func TestLaunchExplicitOptionsReplaceDefaults(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "agy")
	t.Setenv("PATH", binDir)
	call := testutil.CaptureExec(t, &execFunc, nil)
	cfg := &config.ProjectConfig{Root: root, Config: config.Config{
		Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
		Agents:    config.AgentsConfig{},
	}}
	if err := Launch(cfg, nil, nil, []string{"--gemini_dir=custom", "--dangerously-skip-permissions"}); err != nil {
		t.Fatal(err)
	}
	call.AssertCalled(t, filepath.Join(binDir, "agy"), []string{"agy", "--gemini_dir=custom", "--dangerously-skip-permissions"})
}

func TestBaseArgsKeepsAgyOwnerOnly(t *testing.T) {
	root := t.TempDir()
	geminiDir := filepath.Join(root, ".agy")
	if _, err := BaseArgs(root, config.Config{}); err != nil {
		t.Fatalf("BaseArgs fresh: %v", err)
	}
	assertMode(t, geminiDir, 0o700)

	if err := os.Chmod(geminiDir, 0o755); err != nil { // #nosec G302 -- fixture starts too open so production can tighten it.
		t.Fatal(err)
	}
	if _, err := BaseArgs(root, config.Config{}); err != nil {
		t.Fatalf("BaseArgs existing: %v", err)
	}
	assertMode(t, geminiDir, 0o700)
}

func TestBaseArgsRejectsSymlinkedAgy(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Chmod(target, 0o755); err != nil { // #nosec G302 -- fixture verifies the symlink target is not tightened.
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, ".agy")); err != nil {
		t.Fatal(err)
	}
	_, err := BaseArgs(root, config.Config{})
	if err == nil || !strings.Contains(err.Error(), "must be a real directory") {
		t.Fatalf("expected symlinked .agy to fail, got %v", err)
	}
	assertMode(t, target, 0o755)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
