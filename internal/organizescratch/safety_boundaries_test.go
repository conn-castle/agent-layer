package organizescratch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestClassifierConservativeBoundaryBranches(t *testing.T) {
	t.Run("bounded credential probe", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "session-token")
		truncateFile(t, path, privateKeyProbeBytes+1)
		data, truncated, err := readProbe(path)
		if err != nil || !truncated || len(data) != privateKeyProbeBytes {
			t.Fatalf("len=%d truncated=%v err=%v", len(data), truncated, err)
		}
		if _, _, err := readProbe(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("missing probe file unexpectedly succeeded")
		}
		got := classify(topLevel(filepath.Dir(path), filepath.Base(path)), emptyContext())
		if got.dest != destArtifactsData || !strings.Contains(got.reason, "remaining content was not scanned") {
			t.Fatalf("dest=%q reason=%q", got.dest, got.reason)
		}
	})

	t.Run("environment assignment grammar", func(t *testing.T) {
		if envHasNonEmptyAssignment([]byte("\n# comment\nNO_EQUALS\nEMPTY=\nQUOTED=\"\"\nSINGLE=''\n")) {
			t.Fatal("empty assignments were treated as credential-bearing")
		}
		if !envHasNonEmptyAssignment([]byte("export TOKEN = value\r\n")) {
			t.Fatal("export assignment was missed")
		}
	})

	t.Run("vendored heuristic boundaries", func(t *testing.T) {
		if matched, sampled := looksVendored("node_modules", treeScan{}); !matched || sampled {
			t.Fatalf("explicit dependency = %v,%v", matched, sampled)
		}
		for _, scan := range []treeScan{
			{files: 10, sampleFiles: 10, byExt: map[string]int{"js": 10}},
			{files: 1000, sampleFiles: 0, byExt: map[string]int{}},
		} {
			if matched, _ := looksVendored("ordinary", scan); matched {
				t.Fatalf("small/empty sample matched: %+v", scan)
			}
		}
		if matched, sampled := looksVendored("ordinary", treeScan{files: 600, sampleFiles: 400, byExt: map[string]int{"js": 400}}); !matched || !sampled {
			t.Fatalf("JS statistical dependency = %v,%v", matched, sampled)
		}
		if matched, _ := looksVendored("archive.zip", treeScan{files: 600, sampleFiles: 400, byExt: map[string]int{"js": 400}}); matched {
			t.Fatal("archive matched a dependency-tree heuristic")
		}
	})

	t.Run("tracked and tool assets excluded", func(t *testing.T) {
		scan := treeScan{assets: []string{"wordmark.svg", "tracked.svg", "playwright-logo.svg"}}
		assets := uniqueAssetNames(scan, newSet("tracked.svg"))
		if fmt.Sprint(assets) != "[wordmark.svg]" {
			t.Fatalf("assets = %v", assets)
		}
	})

	t.Run("special nodes and vanished entries require review", func(t *testing.T) {
		root := t.TempDir()
		fifo := filepath.Join(root, "evidence", "pipe")
		mkdirAt(t, filepath.Dir(fifo))
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Skipf("mkfifo unavailable: %v", err)
		}
		if got := classify(topLevel(root, "evidence"), emptyContext()); got.dest != destReviewUnknown {
			t.Fatalf("directory special-node dest=%q", got.dest)
		}
		topFIFO := filepath.Join(root, "top.pipe")
		if err := syscall.Mkfifo(topFIFO, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := classify(topLevel(root, "top.pipe"), emptyContext()); got.dest != destReviewUnknown {
			t.Fatalf("top special-node dest=%q", got.dest)
		}
		vanished := fileEntry(t, "vanished.log", "x")
		if err := os.Remove(vanished.abs); err != nil {
			t.Fatal(err)
		}
		if got := classify(vanished, emptyContext()); got.dest != destReviewUnknown {
			t.Fatalf("vanished dest=%q", got.dest)
		}
	})

	t.Run("bulk and ordinary directories remain distinct", func(t *testing.T) {
		root := t.TempDir()
		bulk := dirEntry(t, root, "bulk")
		for i := 0; i <= bulkSamplesMinFiles; i++ {
			writeFileAt(t, filepath.Join(bulk.abs, fmt.Sprintf("%03d.sample", i)), "x")
		}
		if got := classify(bulk, emptyContext()); got.dest != destReviewBulkSamples {
			t.Fatalf("bulk dest=%q reason=%q", got.dest, got.reason)
		}
		ordinary := dirEntry(t, root, "ordinary")
		writeFileAt(t, filepath.Join(ordinary.abs, "notes.md"), "x")
		writeFileAt(t, filepath.Join(ordinary.abs, "trace.log"), "x")
		if got := classify(ordinary, emptyContext()); got.dest != destArtifactsEvidence {
			t.Fatalf("ordinary dest=%q", got.dest)
		}
	})
}

func TestRunAdditionalOutcomeAndFilesystemBoundaries(t *testing.T) {
	t.Run("nothing to do preserves document", func(t *testing.T) {
		root := t.TempDir()
		writeFileAt(t, filepath.Join(root, reviewDocName), "prior")
		mkdirAt(t, filepath.Join(root, "reports"))
		stdout, _, err := runOrganize(t, Options{Root: root, Apply: true})
		if err != nil || !strings.Contains(stdout, "nothing to organize") || readReviewDoc(t, root) != "prior" {
			t.Fatalf("stdout=%q err=%v doc=%q", stdout, err, readReviewDoc(t, root))
		}
	})

	t.Run("stdout failure stops before mutation", func(t *testing.T) {
		root := t.TempDir()
		writeFileAt(t, filepath.Join(root, "notes.md"), "x")
		err := Run(t.Context(), Options{Root: root, Apply: true, MinGroup: DefaultMinGroup}, failingWriter{}, &bytes.Buffer{})
		if err == nil {
			t.Fatal("closed stdout unexpectedly succeeded")
		}
		requireFile(t, filepath.Join(root, "notes.md"))
		requireNoFile(t, filepath.Join(root, reviewDocName))
	})

	t.Run("destination appearing after preview becomes collision", func(t *testing.T) {
		root := t.TempDir()
		writeFileAt(t, filepath.Join(root, "build.log"), "new")
		var stdout, stderr bytes.Buffer
		writer := &mutatingWriter{Buffer: &stdout, mutate: func() {
			writeFileAt(t, filepath.Join(root, destArtifactsLogs, "build.log"), "raced")
		}}
		err := Run(t.Context(), Options{Root: root, Apply: true, MinGroup: DefaultMinGroup}, writer, &stderr)
		if err == nil || !strings.Contains(err.Error(), "collision") {
			t.Fatalf("err=%v", err)
		}
		requireFile(t, filepath.Join(root, "build.log"))
	})

	t.Run("destination link appearing after preview stops mutation", func(t *testing.T) {
		base := t.TempDir()
		root := mkdirAt(t, filepath.Join(base, "root"))
		writeFileAt(t, filepath.Join(root, "build.log"), "new")
		var stdout, stderr bytes.Buffer
		writer := &mutatingWriter{Buffer: &stdout, mutate: func() {
			if err := os.Symlink(filepath.Join(base, "outside"), filepath.Join(root, "artifacts")); err != nil {
				t.Fatalf("symlink: %v", err)
			}
		}}
		err := Run(t.Context(), Options{Root: root, Apply: true, MinGroup: DefaultMinGroup}, writer, &stderr)
		if err == nil || !strings.Contains(err.Error(), "not a real directory") {
			t.Fatalf("err=%v", err)
		}
		requireFile(t, filepath.Join(root, "build.log"))
	})

	t.Run("existing review path must be a real directory", func(t *testing.T) {
		root := t.TempDir()
		writeFileAt(t, filepath.Join(root, "review"), "not a directory")
		writeFileAt(t, filepath.Join(root, "build.log"), "x")
		_, _, err := runOrganize(t, Options{Root: root, Apply: true})
		if err == nil || !strings.Contains(err.Error(), "not a real directory") {
			t.Fatalf("err=%v", err)
		}
		requireFile(t, filepath.Join(root, "build.log"))
	})

	t.Run("direct prior review entry is preserved", func(t *testing.T) {
		root := t.TempDir()
		writeFileAt(t, filepath.Join(root, "review", "loose.txt"), "old")
		writeFileAt(t, filepath.Join(root, "build.log"), "x")
		if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if doc := readReviewDoc(t, root); !strings.Contains(doc, "`review` (1)") || !strings.Contains(doc, "loose.txt") {
			t.Fatalf("doc=%q", doc)
		}
	})
}

func TestGitAndRepairAdditionalFailureBoundaries(t *testing.T) {
	t.Run("recognized repository with empty top is fatal", func(t *testing.T) {
		root := t.TempDir()
		writeFileAt(t, filepath.Join(root, "notes.md"), "x")
		original := runGit
		runGit = func(context.Context, string, ...string) (string, error) { return "\n", nil }
		t.Cleanup(func() { runGit = original })
		_, _, err := runOrganize(t, Options{Root: root})
		if err == nil || !strings.Contains(err.Error(), "empty top level") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("linked marker worktree-list failure is fatal", func(t *testing.T) {
		foreignRepo := newRepo(t)
		_, scratch := newRepoWithScratch(t)
		linked := filepath.Join(scratch, "foreign")
		git(t, foreignRepo, "worktree", "add", linked, "-b", "foreign-list-failure")
		original := runGit
		runGit = func(ctx context.Context, dir string, args ...string) (string, error) {
			if dir == linked && len(args) >= 2 && args[0] == "worktree" && args[1] == "list" {
				return "", errors.New("injected linked list failure")
			}
			return original(ctx, dir, args...)
		}
		t.Cleanup(func() { runGit = original })
		_, _, err := runOrganize(t, Options{Root: scratch, Apply: true, MoveWorktrees: true})
		if err == nil || !strings.Contains(err.Error(), "injected linked list failure") {
			t.Fatalf("err=%v", err)
		}
		requireFile(t, filepath.Join(linked, ".git"))
	})

	t.Run("post-repair verification command failure is fatal", func(t *testing.T) {
		repo, scratch := newRepoWithScratch(t)
		git(t, repo, "worktree", "add", filepath.Join(scratch, "feature"), "-b", "verify-failure")
		original := runGit
		repaired := false
		runGit = func(ctx context.Context, dir string, args ...string) (string, error) {
			if len(args) >= 2 && args[0] == "worktree" && args[1] == "repair" {
				result, err := original(ctx, dir, args...)
				repaired = true
				return result, err
			}
			if repaired && len(args) >= 2 && args[0] == "worktree" && args[1] == "list" {
				return "", errors.New("injected verification failure")
			}
			return original(ctx, dir, args...)
		}
		t.Cleanup(func() { runGit = original })
		_, _, err := runOrganize(t, Options{Root: scratch, Apply: true, MoveWorktrees: true})
		if err == nil || !strings.Contains(err.Error(), "injected verification failure") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("git command error unwraps its cause", func(t *testing.T) {
		cause := errors.New("cause")
		err := &gitCommandError{cause: cause, stderr: "detail"}
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "detail") {
			t.Fatalf("err=%v", err)
		}
	})
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }
