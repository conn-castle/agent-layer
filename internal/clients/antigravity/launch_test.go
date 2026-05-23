package antigravity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestLaunchAntigravity(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	writeLoggingStub(t, binDir, "agy", 0)

	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Explicitly clear AGY_CLI_DISABLE_AUTO_UPDATE before Launch so the log
	// assertion below proves Launch is what set it (not the test process's
	// pre-existing environment).
	t.Setenv("AGY_CLI_DISABLE_AUTO_UPDATE", "")
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, []string{"--debug"}); err != nil {
		t.Fatalf("Launch error: %v", err)
	}
	expectedGeminiDir := filepath.Join(root, ".agy")
	if info, err := os.Stat(expectedGeminiDir); err != nil {
		t.Fatalf("expected repo-local gemini dir: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("expected %s to be a directory", expectedGeminiDir)
	}

	logData, err := os.ReadFile(filepath.Join(binDir, "agy.log")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read agy log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "--gemini_dir="+expectedGeminiDir) {
		t.Fatalf("expected --gemini_dir arg in log, got:\n%s", log)
	}
	if !strings.Contains(log, "--debug") {
		t.Fatalf("expected pass-through arg in log, got:\n%s", log)
	}
	if !strings.Contains(log, "AGY_CLI_DISABLE_AUTO_UPDATE=1") {
		t.Fatalf("expected auto-update env disable in log, got:\n%s", log)
	}
	// Regression guard: default approvals mode must NOT pass
	// --dangerously-skip-permissions to agy. Only Approvals.Mode == YOLO
	// should opt the user out of permission prompts.
	if strings.Contains(log, "--dangerously-skip-permissions") {
		t.Fatalf("did not expect --dangerously-skip-permissions for default approvals, got:\n%s", log)
	}
}

func TestLaunchAntigravityYOLO(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	writeLoggingStub(t, binDir, "agy", 0)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AGY_CLI_DISABLE_AUTO_UPDATE", "")
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, []string{"--debug"}); err != nil {
		t.Fatalf("Launch error: %v", err)
	}

	logData, err := os.ReadFile(filepath.Join(binDir, "agy.log")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read agy log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "--dangerously-skip-permissions") {
		t.Fatalf("expected --dangerously-skip-permissions arg when Approvals.Mode=yolo, got:\n%s", log)
	}
	// passArgs must still be forwarded after the YOLO flag so user-supplied
	// args are preserved.
	if !strings.Contains(log, "--debug") {
		t.Fatalf("expected pass-through arg in log, got:\n%s", log)
	}
}

func TestLaunchAntigravityError(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	testutil.WriteStubWithExit(t, binDir, "agy", 1)

	cfg := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()
	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env, nil); err == nil {
		t.Fatalf("expected error")
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

// TestLaunchAntigravityMissingBinary covers the new LookPath preflight: when
// `agy` is not discoverable, the user must receive a targeted install hint
// instead of a generic "exited with error" message that wrongly implies the
// binary ran. Without the preflight (F-D-3) the failure mode is confusing.
func TestLaunchAntigravityMissingBinary(t *testing.T) {
	originalLookPath := lookPathFunc
	lookPathFunc = func(string) (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { lookPathFunc = originalLookPath })

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
	// F-B2-5 regression guard: missing-binary failures must NOT pollute the
	// user's repo with a stray .agy/ directory. The preflight now runs
	// before MkdirAll.
	if _, statErr := os.Stat(filepath.Join(root, ".agy")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no .agy/ directory after missing-binary failure, got stat err = %v", statErr)
	}
}

func writeLoggingStub(t *testing.T, dir string, name string, exitCode int) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := []byte(fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > \"$0.log\"\nenv >> \"$0.log\"\nexit %d\n", exitCode))
	if err := os.WriteFile(path, content, 0o700); err != nil { // #nosec G306 -- test writes an executable shell stub (PATH-shadowed) for subprocess invocation.
		t.Fatalf("write logging stub: %v", err)
	}
}
