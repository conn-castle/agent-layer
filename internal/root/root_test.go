package root

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestMain(m *testing.M) {
	tempRoot, err := rootTestTempRoot()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "prepare isolated root-test temp directory: %v\n", err)
		os.Exit(1)
	}

	// Root discovery intentionally walks to the filesystem root. Keep these
	// tests independent of legitimate .agent-layer or .git markers that may
	// exist above the process-wide OS temp directory.
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		if err := os.Setenv(name, tempRoot); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "set %s for root tests: %v\n", name, err)
			_ = os.RemoveAll(tempRoot)
			os.Exit(1)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(tempRoot)
	os.Exit(code)
}

func rootTestTempRoot() (string, error) {
	candidates := []string{os.TempDir()}
	if os.PathSeparator == '/' {
		// /var/tmp is the standard fallback when the shorter-lived temp root is
		// itself inside an Agent Layer project.
		candidates = append(candidates, "/var/tmp")
	}
	for _, lookup := range []func() (string, error){os.UserCacheDir, os.UserConfigDir, os.UserHomeDir} {
		if dir, err := lookup(); err == nil && dir != "" {
			candidates = append(candidates, dir)
		}
	}
	for _, parent := range candidates {
		if err := os.MkdirAll(parent, 0o700); err != nil {
			continue
		}
		dir, err := os.MkdirTemp(parent, "agent-layer-root-tests-")
		if err != nil {
			continue
		}
		_, found, findErr := FindAgentLayerRoot(dir)
		if findErr == nil && !found {
			return dir, nil
		}
		_ = os.RemoveAll(dir)
	}
	return "", fmt.Errorf("no writable temp location without an .agent-layer ancestor")
}

func TestFindAgentLayerRootFound(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	got, found, err := FindAgentLayerRoot(sub)
	if err != nil {
		t.Fatalf("FindAgentLayerRoot error: %v", err)
	}
	if !found {
		t.Fatalf("expected root to be found")
	}
	want := resolvedTestPath(t, root)
	if got != want {
		t.Fatalf("expected root %s, got %s", want, got)
	}
}

func TestFindAgentLayerRootResolvesSymlinkedDescendant(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	descendant := filepath.Join(repo, "packages", "service")
	if err := os.MkdirAll(descendant, 0o700); err != nil {
		t.Fatalf("mkdir descendant: %v", err)
	}

	logicalParent := t.TempDir()
	linkedRepo := filepath.Join(logicalParent, "linked-repo")
	if err := os.Symlink(repo, linkedRepo); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}
	logicalDescendant := filepath.Join(linkedRepo, "packages", "service")

	got, found, err := FindAgentLayerRoot(logicalDescendant)
	if err != nil {
		t.Fatalf("FindAgentLayerRoot error: %v", err)
	}
	if !found {
		t.Fatal("expected root to be found through symlinked descendant")
	}
	want := resolvedTestPath(t, repo)
	if got != want {
		t.Fatalf("expected real root %s, got %s", want, got)
	}
}

func TestFindAgentLayerRootPreservesLogicalAncestorOfSymlinkedSubdirectory(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	target := t.TempDir()
	linkedSubdirectory := filepath.Join(repo, "service")
	if err := os.Symlink(target, linkedSubdirectory); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	got, found, err := FindAgentLayerRoot(linkedSubdirectory)
	if err != nil {
		t.Fatalf("FindAgentLayerRoot error: %v", err)
	}
	if !found {
		t.Fatal("expected logical repository root above symlinked subdirectory")
	}
	if want := resolvedTestPath(t, repo); got != want {
		t.Fatalf("expected logical ancestor root %s, got %s", want, got)
	}
}

func TestFindAgentLayerRootMissing(t *testing.T) {
	root := t.TempDir()
	got, found, err := FindAgentLayerRoot(root)
	if err != nil {
		t.Fatalf("FindAgentLayerRoot error: %v", err)
	}
	if found {
		t.Fatalf("expected not found, got %s", got)
	}
}

func TestFindAgentLayerRootFileError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".agent-layer"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, _, err := FindAgentLayerRoot(root)
	if err == nil {
		t.Fatalf("expected error for file .agent-layer")
	}
}

func TestRootDiscoveryFailsOnMarkerSymlinkLoops(t *testing.T) {
	for _, test := range []struct {
		marker string
		find   func(string) error
	}{
		{marker: ".agent-layer", find: func(root string) error { _, _, err := FindAgentLayerRoot(root); return err }},
		{marker: ".git", find: func(root string) error { _, err := FindRepoRoot(root); return err }},
	} {
		t.Run(test.marker, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Symlink(test.marker, filepath.Join(root, test.marker)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := test.find(root); err == nil || !strings.Contains(strings.ToLower(err.Error()), "check") {
				t.Fatalf("marker symlink-loop error = %v", err)
			}
		})
	}
}

func TestFindRepoRootPrefersAgentLayer(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	sub := filepath.Join(root, "nested")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	got, err := FindRepoRoot(sub)
	if err != nil {
		t.Fatalf("FindRepoRoot error: %v", err)
	}
	want := resolvedTestPath(t, root)
	if got != want {
		t.Fatalf("expected root %s, got %s", want, got)
	}
}

func TestFindRepoRootUsesGit(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	sub := filepath.Join(root, "nested")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	got, err := FindRepoRoot(sub)
	if err != nil {
		t.Fatalf("FindRepoRoot error: %v", err)
	}
	want := resolvedTestPath(t, root)
	if got != want {
		t.Fatalf("expected root %s, got %s", want, got)
	}
}

func TestFindRepoRootResolvesSymlinkedDescendant(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	descendant := filepath.Join(repo, "packages", "service")
	if err := os.MkdirAll(descendant, 0o700); err != nil {
		t.Fatalf("mkdir descendant: %v", err)
	}

	logicalParent := t.TempDir()
	linkedRepo := filepath.Join(logicalParent, "linked-repo")
	if err := os.Symlink(repo, linkedRepo); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	got, err := FindRepoRoot(filepath.Join(linkedRepo, "packages", "service"))
	if err != nil {
		t.Fatalf("FindRepoRoot error: %v", err)
	}
	want := resolvedTestPath(t, repo)
	if got != want {
		t.Fatalf("expected real Git root %s, got %s", want, got)
	}
}

func TestFindRepoRootPreservesLogicalGitAncestorOfSymlinkedSubdirectory(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	target := t.TempDir()
	linkedSubdirectory := filepath.Join(repo, "service")
	if err := os.Symlink(target, linkedSubdirectory); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	got, err := FindRepoRoot(linkedSubdirectory)
	if err != nil {
		t.Fatalf("FindRepoRoot error: %v", err)
	}
	if want := resolvedTestPath(t, repo); got != want {
		t.Fatalf("expected logical Git ancestor %s, got %s", want, got)
	}
}

func TestFindRepoRootFallsBackToStart(t *testing.T) {
	root := t.TempDir()
	got, err := FindRepoRoot(root)
	if err != nil {
		t.Fatalf("FindRepoRoot error: %v", err)
	}
	want := resolvedTestPath(t, root)
	if got != want {
		t.Fatalf("expected root %s, got %s", want, got)
	}
}

func TestFindRootsRequireStartPath(t *testing.T) {
	if _, _, err := FindAgentLayerRoot(""); err == nil {
		t.Fatal("expected FindAgentLayerRoot to reject empty start")
	}
	if _, err := FindRepoRoot(""); err == nil {
		t.Fatal("expected FindRepoRoot to reject empty start")
	}
}

func TestFindRootsRejectBrokenSymlinkStart(t *testing.T) {
	parent := t.TempDir()
	broken := filepath.Join(parent, "broken")
	if err := os.Symlink(filepath.Join(parent, "missing-target"), broken); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	tests := []struct {
		name string
		find func() error
	}{
		{
			name: "agent layer root",
			find: func() error {
				_, _, err := FindAgentLayerRoot(broken)
				return err
			},
		},
		{
			name: "repository root",
			find: func() error {
				_, err := FindRepoRoot(broken)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.find()
			if err == nil {
				t.Fatal("expected broken symlink to fail")
			}
			if !strings.Contains(err.Error(), "resolve path ") || !strings.Contains(err.Error(), broken) {
				t.Fatalf("expected actionable path-resolution error for %s, got %v", broken, err)
			}
		})
	}
}

func TestFindRepoRootUsesGitFile(t *testing.T) {
	root := t.TempDir()
	gitPath := filepath.Join(root, ".git")
	if err := os.WriteFile(gitPath, []byte("gitdir: .git/worktrees/x\n"), 0o600); err != nil {
		t.Fatalf("write .git file: %v", err)
	}

	got, err := FindRepoRoot(root)
	if err != nil {
		t.Fatalf("FindRepoRoot error: %v", err)
	}
	want := resolvedTestPath(t, root)
	if got != want {
		t.Fatalf("expected root %s, got %s", want, got)
	}
}

func TestFindRepoRootGitSpecialFileErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mkfifo is not supported on windows")
	}

	root := t.TempDir()
	gitPath := filepath.Join(root, ".git")
	if err := syscall.Mkfifo(gitPath, 0o644); err != nil {
		t.Fatalf("mkfifo .git: %v", err)
	}

	if _, err := FindRepoRoot(root); err == nil {
		t.Fatal("expected error when .git is neither directory nor regular file")
	}
}

func resolvedTestPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("resolve test path %s: %v", path, err)
	}
	return resolved
}
