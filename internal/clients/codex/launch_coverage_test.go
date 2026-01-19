package codex

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicholasjconn/agent-layer/internal/config"
	"github.com/nicholasjconn/agent-layer/internal/run"
)

func TestLaunch_NoArgs(t *testing.T) {
	root := t.TempDir()
	binDir := t.TempDir()
	writeStubWithExit(t, binDir, "codex", 0)

	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Codex: config.CodexConfig{
					// Empty model/reasoning
				},
			},
		},
		Root: root,
	}

	t.Setenv("PATH", binDir)
	env := os.Environ()

	if err := Launch(cfg, &run.Info{ID: "id", Dir: root}, env); err != nil {
		t.Fatalf("Launch error: %v", err)
	}
}

func TestResolvePath_EvalSymlinksSuccess(t *testing.T) {
	// Create a real file so EvalSymlinks succeeds
	dir := t.TempDir()
	path := filepath.Join(dir, "real")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	resolved := resolvePath(path)
	// On Mac/Linux tmp might be symlinked, so we expect EvalSymlinks to do something or return abs.
	// Just ensuring no panic and coverage hits.
	if resolved == "" {
		t.Fatalf("expected resolved path")
	}
}

func TestResolvePath_EvalSymlinksFailure(t *testing.T) {
	// Path that does not exist -> EvalSymlinks fails
	path := "/non-existent/path/xyz"
	resolved := resolvePath(path)
	if resolved == "" {
		t.Fatalf("expected resolved path")
	}
	// Should be abs path
	if !filepath.IsAbs(resolved) {
		t.Fatalf("expected absolute path")
	}
}
