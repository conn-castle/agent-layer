package organizescratch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/gitenv"
)

func runOrganize(t *testing.T, opts Options) (stdout, stderr string, err error) {
	t.Helper()
	if opts.MinGroup == 0 {
		opts.MinGroup = DefaultMinGroup
	}
	var out, errOut bytes.Buffer
	err = Run(t.Context(), opts, &out, &errOut)
	return out.String(), errOut.String(), err
}

func readReviewDoc(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, reviewDocName)) // #nosec G304 -- test fixture.
	if err != nil {
		t.Fatalf("read review doc: %v", err)
	}
	return string(data)
}

func requireNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Fatalf("%s exists but should not", path)
	}
}

func requireFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- test-owned fixture.
	command.Env = append(gitenv.WithoutDiscovery(),
		"LC_ALL=C",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init", "--initial-branch=main")
	writeFileAt(t, filepath.Join(repo, "main.go"), "package main\n")
	git(t, repo, "add", "main.go")
	git(t, repo, "commit", "-m", "initial")
	return repo
}

func newRepoWithScratch(t *testing.T) (repo, scratch string) {
	t.Helper()
	repo = newRepo(t)
	writeFileAt(t, filepath.Join(repo, ".gitignore"), "scratch/\n")
	git(t, repo, "add", ".gitignore")
	git(t, repo, "commit", "-m", "ignore scratch")
	return repo, mkdirAt(t, filepath.Join(repo, "scratch"))
}

func truncateFile(t *testing.T, path string, size int64) {
	t.Helper()
	writeFileAt(t, path, "")
	if err := os.Truncate(path, size); err != nil {
		t.Fatalf("truncate %s: %v", path, err)
	}
}

func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	var snapshot []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		extra := ""
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			extra = " -> " + target
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path) // #nosec G122,G304 -- isolated immutable test fixture during snapshot.
			if err != nil {
				return err
			}
			extra = fmt.Sprintf(" sha256=%x", sha256.Sum256(data))
		}
		snapshot = append(snapshot, fmt.Sprintf("%s %s %d%s", filepath.ToSlash(rel), info.Mode(), info.Size(), extra))
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return snapshot
}

func TestRunRejectsUnusableOptions(t *testing.T) {
	file := writeFileAt(t, filepath.Join(t.TempDir(), "not-a-dir"), "x")
	cases := []struct {
		name string
		opts Options
		want string
	}{
		{"no root", Options{MinGroup: DefaultMinGroup}, "root directory is required"},
		{"missing root", Options{Root: filepath.Join(t.TempDir(), "absent"), MinGroup: DefaultMinGroup}, "read root"},
		{"root is a file", Options{Root: file, MinGroup: DefaultMinGroup}, "not a directory"},
		{"zero min group", Options{Root: t.TempDir(), MinGroup: 0}, "min group must be a positive integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			err := Run(t.Context(), tc.opts, &out, &errOut)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRunAllowsNonRepositoryAndUntrackedNonIgnoredRoots(t *testing.T) {
	t.Run("outside repository", func(t *testing.T) {
		root := t.TempDir()
		writeFileAt(t, filepath.Join(root, "notes.md"), "x")
		_, stderr, err := runOrganize(t, Options{Root: root})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !strings.Contains(stderr, "outside a git repository") {
			t.Fatalf("stderr = %q", stderr)
		}
	})

	t.Run("untracked and non-ignored", func(t *testing.T) {
		repo := newRepo(t)
		root := mkdirAt(t, filepath.Join(repo, "scratch-unignored"))
		writeFileAt(t, filepath.Join(root, "notes.md"), "x")
		if _, _, err := runOrganize(t, Options{Root: root}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		requireFile(t, filepath.Join(root, "notes.md"))
	})
}

func TestRunRefusesRepositoryRootAndTrackedSubtree(t *testing.T) {
	repo := newRepo(t)
	trackedDir := mkdirAt(t, filepath.Join(repo, "tracked"))
	writeFileAt(t, filepath.Join(trackedDir, "evidence.log"), "tracked")
	git(t, repo, "add", "tracked/evidence.log")
	git(t, repo, "commit", "-m", "tracked subtree")

	for name, root := range map[string]string{"repository root": repo, "tracked subtree": trackedDir} {
		t.Run(name, func(t *testing.T) {
			_, _, err := runOrganize(t, Options{Root: root, Apply: true})
			if err == nil || !strings.Contains(err.Error(), "git tracks content at or below") {
				t.Fatalf("err = %v", err)
			}
			requireFile(t, filepath.Join(trackedDir, "evidence.log"))
			requireNoFile(t, filepath.Join(root, reviewDocName))
		})
	}
}

func TestRunTreatsTrackedRootPathspecAsLiteral(t *testing.T) {
	repo := newRepo(t)
	rootName := "scratch[1]"
	root := mkdirAt(t, filepath.Join(repo, rootName))
	writeFileAt(t, filepath.Join(root, "tracked.log"), "tracked")
	git(t, repo, "add", "--", ":(literal)"+rootName+"/tracked.log")
	git(t, repo, "commit", "-m", "track metacharacter root")

	_, _, err := runOrganize(t, Options{Root: root, Apply: true})
	if err == nil || !strings.Contains(err.Error(), "git tracks content at or below") {
		t.Fatalf("err = %v", err)
	}
	requireFile(t, filepath.Join(root, "tracked.log"))
	requireNoFile(t, filepath.Join(root, reviewDocName))
}

func TestRunTreatsMissingOrBrokenGitAsFatal(t *testing.T) {
	t.Run("missing executable", func(t *testing.T) {
		root := t.TempDir()
		writeFileAt(t, filepath.Join(root, "notes.md"), "x")
		t.Setenv("PATH", t.TempDir())
		_, _, err := runOrganize(t, Options{Root: root})
		if err == nil || !strings.Contains(err.Error(), "executable file not found") {
			t.Fatalf("err = %v", err)
		}
		requireNoFile(t, filepath.Join(root, reviewDocName))
	})

	t.Run("non-repository diagnostic must be exact", func(t *testing.T) {
		root := t.TempDir()
		writeFileAt(t, filepath.Join(root, "notes.md"), "x")
		original := runGit
		runGit = func(context.Context, string, ...string) (string, error) {
			return "", &gitCommandError{cause: &exec.ExitError{}, stderr: "fatal: detected dubious ownership in repository"}
		}
		t.Cleanup(func() { runGit = original })
		_, _, err := runOrganize(t, Options{Root: root})
		if err == nil || !strings.Contains(err.Error(), "dubious ownership") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestGitFactsFailClosedAfterRepositoryRecognition(t *testing.T) {
	for _, failingCommand := range []string{"ls-files", "worktree"} {
		t.Run(failingCommand, func(t *testing.T) {
			_, scratch := newRepoWithScratch(t)
			writeFileAt(t, filepath.Join(scratch, "notes.md"), "x")
			original := runGit
			runGit = func(ctx context.Context, dir string, args ...string) (string, error) {
				if args[0] == failingCommand {
					if failingCommand != "ls-files" || !slices.Contains(args, "--") {
						return "", errors.New("injected git fact failure")
					}
				}
				return original(ctx, dir, args...)
			}
			t.Cleanup(func() { runGit = original })
			_, _, err := runOrganize(t, Options{Root: scratch})
			if err == nil || !strings.Contains(err.Error(), "injected git fact failure") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestGitOutputForcesStableEnglishLocale(t *testing.T) {
	bin := t.TempDir()
	script := writeFileAt(t, filepath.Join(bin, "git"), "#!/bin/sh\n"+
		"if [ \"$LC_ALL\" != C ]; then echo wrong-locale >&2; exit 9; fi\n"+
		"echo 'fatal: not a git repository (or any of the parent directories): .git' >&2\nexit 128\n")
	if err := os.Chmod(script, 0o700); err != nil { // #nosec G302 -- executable test fixture must have an execute bit.
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("LC_ALL", "fr_FR.UTF-8")
	_, err := gitOutput(t.Context(), t.TempDir(), "rev-parse", "--show-toplevel")
	if err == nil || !isNotGitRepository(err) || strings.Contains(err.Error(), "wrong-locale") {
		t.Fatalf("err = %v", err)
	}
}

func TestDryRunIsStrictlyReadOnlyAndPredictsDanglingCollision(t *testing.T) {
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "build.log"), "new")
	writeFileAt(t, filepath.Join(root, "logo.svg"), "<svg/>")
	target := filepath.Join(root, destArtifactsLogs, "build.log")
	mkdirAt(t, filepath.Dir(target))
	if err := os.Symlink("missing-target", target); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, root)

	stdout, _, err := runOrganize(t, Options{Root: root})
	if err == nil || !strings.Contains(err.Error(), "destination collision") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stdout, "LEFT IN PLACE  build.log") ||
		!strings.Contains(stdout, "Proposed review list") || !strings.Contains(stdout, "logo.svg") {
		t.Fatalf("stdout = %q", stdout)
	}
	after := snapshotTree(t, root)
	if !slices.Equal(before, after) {
		t.Fatalf("dry run changed filesystem\nbefore=%v\nafter=%v", before, after)
	}
	requireNoFile(t, filepath.Join(root, reviewDocName))
}

func TestApplyMovesEntriesAndReturnsNonZeroForCollision(t *testing.T) {
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "notes.md"), "x")
	writeFileAt(t, filepath.Join(root, "logo.svg"), "<svg/>")
	writeFileAt(t, filepath.Join(root, "build.log"), "new")
	writeFileAt(t, filepath.Join(root, destArtifactsLogs, "build.log"), "earlier")

	stdout, stderr, err := runOrganize(t, Options{Root: root, Apply: true})
	if err == nil || !strings.Contains(err.Error(), "destination collision") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(stderr, "COLLISION, left in place: build.log") || !strings.Contains(stdout, "moved 2/3") {
		t.Fatalf("stdout=%q stderr=%q", stdout, stderr)
	}
	requireFile(t, filepath.Join(root, "build.log"))
	requireFile(t, filepath.Join(root, reportsAdhocPrefix, adhocFallbackFolder, "notes.md"))
	requireFile(t, filepath.Join(root, destReviewUniqueAssets, "logo.svg"))
	doc := readReviewDoc(t, root)
	if !strings.Contains(doc, "**collision**") || !strings.Contains(doc, "logo.svg") {
		t.Fatalf("review doc = %q", doc)
	}
}

func TestApplyPreservesPriorUnresolvedReviewEntriesAndReasons(t *testing.T) {
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, destReviewUniqueAssets, "old-logo.svg"), "<svg/>")
	writeFileAt(t, filepath.Join(root, reviewDocName), "# Scratch organization review\n\n## Needs review\n\n### `review/unique-assets` (1)\n\n- `old-logo.svg` — bespoke logo from client\n")
	writeFileAt(t, filepath.Join(root, "build.log"), "x")

	if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	doc := readReviewDoc(t, root)
	if !strings.Contains(doc, "old-logo.svg") || !strings.Contains(doc, "bespoke logo from client") {
		t.Fatalf("review doc = %q", doc)
	}

	writeFileAt(t, filepath.Join(root, reviewDocName), "not safely parseable")
	writeFileAt(t, filepath.Join(root, "next.log"), "x")
	if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if doc := readReviewDoc(t, root); !strings.Contains(doc, "filed by an earlier run; reason not recorded") {
		t.Fatalf("review doc = %q", doc)
	}
}

func TestApplyRecordsActualPartialOutcomeAndUnattemptedEntries(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.log", "b.log", "c.log"} {
		writeFileAt(t, filepath.Join(root, name), name)
	}
	var output bytes.Buffer
	w := &mutatingWriter{Buffer: &output, mutate: func() { _ = os.Remove(filepath.Join(root, "b.log")) }}
	var stderr bytes.Buffer
	err := Run(t.Context(), Options{Root: root, Apply: true, MinGroup: DefaultMinGroup}, w, &stderr)
	if err == nil || !strings.Contains(err.Error(), "move b.log") {
		t.Fatalf("err = %v", err)
	}
	requireFile(t, filepath.Join(root, destArtifactsLogs, "a.log"))
	requireFile(t, filepath.Join(root, "c.log"))
	doc := readReviewDoc(t, root)
	for _, want := range []string{"`b.log` — **failed**", "`c.log` — **unattempted**"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("review doc = %q, want %q", doc, want)
		}
	}
	if strings.Contains(doc, "Entries have been moved") || strings.Contains(doc, "all entries") {
		t.Fatalf("review doc made a global success claim: %q", doc)
	}
}

func TestApplyPreservesPredictedCollisionAfterEarlierFailure(t *testing.T) {
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "a.log"), "fails")
	writeFileAt(t, filepath.Join(root, "z.log"), "collides")
	writeFileAt(t, filepath.Join(root, destArtifactsLogs, "z.log"), "existing")
	var output bytes.Buffer
	w := &mutatingWriter{Buffer: &output, mutate: func() { _ = os.Remove(filepath.Join(root, "a.log")) }}
	var stderr bytes.Buffer
	err := Run(t.Context(), Options{Root: root, Apply: true, MinGroup: DefaultMinGroup}, w, &stderr)
	if err == nil || !strings.Contains(err.Error(), "move a.log") {
		t.Fatalf("err = %v", err)
	}
	doc := readReviewDoc(t, root)
	for _, want := range []string{"`a.log` — **failed**", "`z.log` — **collision**", "already exists"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("review doc = %q, want %q", doc, want)
		}
	}
}

type mutatingWriter struct {
	*bytes.Buffer
	mutate func()
	done   bool
}

func (w *mutatingWriter) Write(p []byte) (int, error) {
	if !w.done {
		w.done = true
		w.mutate()
	}
	return w.Buffer.Write(p)
}

func TestRunFindsCompleteHazardSetThroughFilesystemBoundary(t *testing.T) {
	t.Run("hazard after 4000 files", func(t *testing.T) {
		root := t.TempDir()
		tree := mkdirAt(t, filepath.Join(root, "evidence"))
		for i := 0; i < 4001; i++ {
			writeFileAt(t, filepath.Join(tree, fmt.Sprintf("a%04d.json", i)), "{}")
		}
		writeFileAt(t, filepath.Join(tree, "z", "jwt-signing-key"), encodedPrivateKeyFixture())
		if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		requireFile(t, filepath.Join(root, destReviewSecrets, "evidence"))
	})

	t.Run("git metadata excluded but marker retained", func(t *testing.T) {
		root := t.TempDir()
		tree := mkdirAt(t, filepath.Join(root, "checkout-copy"))
		for i := 0; i < 500; i++ {
			writeFileAt(t, filepath.Join(tree, ".git", "objects", fmt.Sprintf("%04d", i)), "metadata")
		}
		writeFileAt(t, filepath.Join(tree, "notes.md"), "x")
		if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		requireFile(t, filepath.Join(root, destReviewCheckouts, "checkout-copy"))
		if doc := readReviewDoc(t, root); strings.Contains(doc, "directory has 501 files") {
			t.Fatalf(".git metadata consumed count: %q", doc)
		}
	})

	t.Run("authored SVG outranks generated JS", func(t *testing.T) {
		root := t.TempDir()
		tree := mkdirAt(t, filepath.Join(root, "generated"))
		for i := 0; i < vendoredMinFiles+1; i++ {
			writeFileAt(t, filepath.Join(tree, fmt.Sprintf("%04d.js", i)), "generated")
		}
		writeFileAt(t, filepath.Join(tree, "unique.svg"), "<svg/>")
		if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		requireFile(t, filepath.Join(root, destReviewUniqueAssets, "generated"))
	})

	t.Run("nested checkout marker", func(t *testing.T) {
		root := t.TempDir()
		tree := mkdirAt(t, filepath.Join(root, "collection"))
		mkdirAt(t, filepath.Join(tree, "foreign", ".git"))
		if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		requireFile(t, filepath.Join(root, destReviewCheckouts, "collection"))
	})
}

func TestRunDetectsCredentialBearingContent(t *testing.T) {
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "late-key.pem"), strings.Repeat("certificate chain\n", 400)+privateKeyPEM)
	encodedPrivateKey := base64.StdEncoding.EncodeToString([]byte(privateKeyPEM))
	writeFileAt(t, filepath.Join(root, "jwt-signing-key"), encodedPrivateKey[:12]+"\n"+encodedPrivateKey[12:])
	writeFileAt(t, filepath.Join(root, "jwt-public-certificate.pem"), base64.StdEncoding.EncodeToString([]byte(publicCertPEM)))
	writeFileAt(t, filepath.Join(root, ".env"), "EMPTY=\nPASSWORD=not-empty\n")
	writeFileAt(t, filepath.Join(root, "production.env"), "API_TOKEN=not-empty\n")
	writeFileAt(t, filepath.Join(root, ".env.example"), "API_TOKEN=replace-me\n")
	writeFileAt(t, filepath.Join(root, "storage-state.json"), `{"cookies":[],"origins":[]}`)
	writeFileAt(t, filepath.Join(root, "unfamiliar.json"), `{"cookies":[{"name":"session"}]}`)
	profile := mkdirAt(t, filepath.Join(root, "ChromeProfile"))
	for _, name := range []string{"Cookies", "Login Data", "Local State"} {
		writeFileAt(t, filepath.Join(profile, name), "browser data")
	}
	mixed := mkdirAt(t, filepath.Join(root, "secret-with-unreadable-sibling"))
	writeFileAt(t, filepath.Join(mixed, "jwt-signing-key"), encodedPrivateKey)
	locked := writeFileAt(t, filepath.Join(mixed, "locked.txt"), "authored but unreadable")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })
	unreadableTop := writeFileAt(t, filepath.Join(root, "unreadable.log"), "unknown")
	if err := os.Chmod(unreadableTop, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadableTop, 0o600) })

	if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, name := range []string{"late-key.pem", "jwt-signing-key", ".env", "production.env", ".env.example", "storage-state.json", "unfamiliar.json", "ChromeProfile", "secret-with-unreadable-sibling"} {
		requireFile(t, filepath.Join(root, destReviewSecrets, name))
	}
	requireFile(t, filepath.Join(root, destArtifactsData, "jwt-public-certificate.pem"))
	requireFile(t, filepath.Join(root, destReviewUnknown, "unreadable.log"))
	doc := readReviewDoc(t, root)
	for _, evidence := range []string{"PEM PRIVATE KEY", "decodes to a PEM PRIVATE KEY", "environment assignments", "cookies array", "browser profile"} {
		if !strings.Contains(doc, evidence) {
			t.Errorf("review doc missing %q: %s", evidence, doc)
		}
	}
}

func TestRunDisclosesCleanlyTruncatedCredentialCandidatesWithoutEscalatingThem(t *testing.T) {
	root := t.TempDir()
	truncateFile(t, filepath.Join(root, "large-session.json"), privateKeyProbeBytes+1)
	directory := mkdirAt(t, filepath.Join(root, "private-data"))
	truncateFile(t, filepath.Join(directory, "session.json"), privateKeyProbeBytes+1)

	if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireFile(t, filepath.Join(root, destArtifactsData, "large-session.json"))
	requireFile(t, filepath.Join(root, destArtifactsEvidence, "private-data"))
	doc := readReviewDoc(t, root)
	for _, name := range []string{"large-session.json", "private-data"} {
		if !strings.Contains(doc, name) || !strings.Contains(doc, "remaining content was not scanned") {
			t.Fatalf("review document lacks bounded disclosure for %s: %q", name, doc)
		}
	}
	if strings.Contains(doc, "### `review/secrets`") || strings.Contains(doc, "### `review/unknown`") {
		t.Fatalf("clean truncation was escalated into a review bucket: %q", doc)
	}
}

func TestRunRetainsSampledRepoCopyHeuristicWithDisclosure(t *testing.T) {
	repo, scratch := newRepoWithScratch(t)
	copyDir := mkdirAt(t, filepath.Join(scratch, "repo-copy"))
	for i := 0; i < scanNameSample+25; i++ {
		name := fmt.Sprintf("tracked%04d.go", i)
		writeFileAt(t, filepath.Join(repo, name), "package main\n")
		writeFileAt(t, filepath.Join(copyDir, name), "package main\n")
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "tracked copy sources")

	if _, _, err := runOrganize(t, Options{Root: scratch, Apply: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireFile(t, filepath.Join(scratch, destReviewRegenerable, "repo-copy"))
	if doc := readReviewDoc(t, scratch); !strings.Contains(doc, "bounded sample") {
		t.Fatalf("review doc lacks sampling disclosure: %q", doc)
	}
}

func TestRunSizeOverridesParentChildAndTopLevelFile(t *testing.T) {
	root := t.TempDir()
	many := mkdirAt(t, filepath.Join(root, "many-files"))
	for i := 0; i <= maxUnreviewedFiles; i++ {
		writeFileAt(t, filepath.Join(many, fmt.Sprintf("%03d.txt", i)), "x")
	}
	hugeChild := mkdirAt(t, filepath.Join(root, "parent-with-huge-child", "child"))
	truncateFile(t, filepath.Join(hugeChild, "blob.bin"), maxUnreviewedBytes+1)
	truncateFile(t, filepath.Join(root, "archive.zip"), maxUnreviewedBytes+1)

	if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, name := range []string{"many-files", "parent-with-huge-child", "archive.zip"} {
		requireFile(t, filepath.Join(root, destReviewOversized, name))
	}
	doc := readReviewDoc(t, root)
	if !strings.Contains(doc, "child (250.0 MiB)") || !strings.Contains(doc, "101 files") || !strings.Contains(doc, "file apparent size") {
		t.Fatalf("review doc lacks size evidence: %q", doc)
	}
}

func TestRunRepairsEntryAndNestedRegisteredWorktrees(t *testing.T) {
	repo, scratch := newRepoWithScratch(t)
	parent := filepath.Join(scratch, "parent")
	git(t, repo, "worktree", "add", parent, "-b", "parent")
	git(t, repo, "worktree", "add", filepath.Join(parent, "nested"), "-b", "nested")

	if _, stderr, err := runOrganize(t, Options{Root: scratch, Apply: true, MoveWorktrees: true}); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, stderr)
	}
	movedParent := filepath.Join(scratch, destReviewCheckouts, "parent")
	for _, target := range []string{movedParent, filepath.Join(movedParent, "nested")} {
		git(t, target, "status", "--porcelain")
	}
	list := git(t, repo, "worktree", "list")
	if !strings.Contains(list, canonicalPath(movedParent)) || !strings.Contains(list, canonicalPath(filepath.Join(movedParent, "nested"))) || strings.Contains(list, "prunable") {
		t.Fatalf("worktree list = %q", list)
	}
}

func TestRunProtectsAndRepairsForeignLinkedWorktree(t *testing.T) {
	foreignRepo := newRepo(t)
	_, scratch := newRepoWithScratch(t)
	linked := filepath.Join(scratch, "foreign-linked")
	git(t, foreignRepo, "worktree", "add", linked, "-b", "foreign")

	stdout, _, err := runOrganize(t, Options{Root: scratch, Apply: true})
	if err != nil {
		t.Fatalf("default Run: %v", err)
	}
	if !strings.Contains(stdout, "LEFT IN PLACE  foreign-linked") {
		t.Fatalf("stdout = %q", stdout)
	}
	requireFile(t, filepath.Join(linked, ".git"))

	if _, stderr, err := runOrganize(t, Options{Root: scratch, Apply: true, MoveWorktrees: true}); err != nil {
		t.Fatalf("move Run: %v\nstderr=%s", err, stderr)
	}
	moved := filepath.Join(scratch, destReviewCheckouts, "foreign-linked")
	git(t, moved, "status", "--porcelain")
	list := git(t, foreignRepo, "worktree", "list")
	if !strings.Contains(list, canonicalPath(moved)) || strings.Contains(list, "prunable") {
		t.Fatalf("foreign worktree list = %q", list)
	}
}

func TestRunProtectsNestedForeignLinkedWorktree(t *testing.T) {
	foreignRepo := newRepo(t)
	_, scratch := newRepoWithScratch(t)
	container := mkdirAt(t, filepath.Join(scratch, "container"))
	linked := filepath.Join(container, "foreign-linked")
	git(t, foreignRepo, "worktree", "add", linked, "-b", "foreign-nested")

	stdout, _, err := runOrganize(t, Options{Root: scratch, Apply: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout, "LEFT IN PLACE  container") {
		t.Fatalf("stdout = %q", stdout)
	}
	requireFile(t, filepath.Join(linked, ".git"))

	if _, stderr, err := runOrganize(t, Options{Root: scratch, Apply: true, MoveWorktrees: true}); err != nil {
		t.Fatalf("move Run: %v\nstderr=%s", err, stderr)
	}
	moved := filepath.Join(scratch, destReviewCheckouts, "container", "foreign-linked")
	git(t, moved, "status", "--porcelain")
	list := git(t, foreignRepo, "worktree", "list")
	if !strings.Contains(list, canonicalPath(moved)) || strings.Contains(list, "prunable") {
		t.Fatalf("nested foreign worktree list = %q", list)
	}
}

func TestRunProtectsAndRepairsForeignMainCheckoutWithExternalWorktree(t *testing.T) {
	source := newRepo(t)
	root := t.TempDir()
	mainCheckout := filepath.Join(root, "mainrepo")
	git(t, root, "clone", source, mainCheckout)
	external := filepath.Join(t.TempDir(), "outside-wt")
	git(t, mainCheckout, "worktree", "add", external, "-b", "outside")

	stdout, _, err := runOrganize(t, Options{Root: root, Apply: true})
	if err != nil {
		t.Fatalf("default Run: %v", err)
	}
	if !strings.Contains(stdout, "LEFT IN PLACE  mainrepo") || !strings.Contains(stdout, "registered main checkout") || !strings.Contains(stdout, "externally registered linked worktree") {
		t.Fatalf("stdout = %q", stdout)
	}
	if doc := readReviewDoc(t, root); !strings.Contains(doc, "outside-wt") {
		t.Fatalf("review doc does not disclose external registration %q: %q", external, doc)
	}
	git(t, external, "status", "--porcelain")

	if _, stderr, err := runOrganize(t, Options{Root: root, Apply: true, MoveWorktrees: true}); err != nil {
		t.Fatalf("move Run: %v\nstderr=%s", err, stderr)
	}
	movedMain := filepath.Join(root, destReviewCheckouts, "mainrepo")
	git(t, movedMain, "status", "--porcelain")
	git(t, external, "status", "--porcelain")
	list := git(t, movedMain, "worktree", "list", "--porcelain")
	if !strings.Contains(list, canonicalPath(movedMain)) || !strings.Contains(list, canonicalPath(external)) || strings.Contains(list, "prunable") {
		t.Fatalf("foreign main worktree list = %q", list)
	}
}

func TestRunRepairsNestedMainCheckoutAndItsLinkedWorktreeFromMainContext(t *testing.T) {
	source := newRepo(t)
	root := t.TempDir()
	container := mkdirAt(t, filepath.Join(root, "container"))
	mainCheckout := filepath.Join(container, "mainrepo")
	git(t, container, "clone", source, mainCheckout)
	linked := filepath.Join(container, "linked")
	git(t, mainCheckout, "worktree", "add", linked, "-b", "nested-linked")

	if _, stderr, err := runOrganize(t, Options{Root: root, Apply: true, MoveWorktrees: true}); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, stderr)
	}
	movedContainer := filepath.Join(root, destReviewCheckouts, "container")
	movedMain := filepath.Join(movedContainer, "mainrepo")
	movedLinked := filepath.Join(movedContainer, "linked")
	git(t, movedMain, "status", "--porcelain")
	git(t, movedLinked, "status", "--porcelain")
	list := git(t, movedMain, "worktree", "list", "--porcelain")
	if !strings.Contains(list, canonicalPath(movedMain)) || !strings.Contains(list, canonicalPath(movedLinked)) || strings.Contains(list, "prunable") {
		t.Fatalf("nested main worktree list = %q", list)
	}
}

func TestRunRepairsMovedWorktreeAfterLaterMoveFailure(t *testing.T) {
	repo, scratch := newRepoWithScratch(t)
	worktree := filepath.Join(scratch, "aaa-wt")
	git(t, repo, "worktree", "add", worktree, "-b", "partial-repair")
	writeFileAt(t, filepath.Join(scratch, "bbb.log"), "fails later")
	var output bytes.Buffer
	w := &mutatingWriter{Buffer: &output, mutate: func() { _ = os.Remove(filepath.Join(scratch, "bbb.log")) }}
	var stderr bytes.Buffer
	err := Run(t.Context(), Options{Root: scratch, Apply: true, MoveWorktrees: true, MinGroup: DefaultMinGroup}, w, &stderr)
	if err == nil || !strings.Contains(err.Error(), "move bbb.log") {
		t.Fatalf("err = %v\nstderr=%s", err, stderr.String())
	}
	moved := filepath.Join(scratch, destReviewCheckouts, "aaa-wt")
	git(t, moved, "status", "--porcelain")
	list := git(t, repo, "worktree", "list")
	if !strings.Contains(list, canonicalPath(moved)) || strings.Contains(list, "prunable") {
		t.Fatalf("worktree list = %q", list)
	}
	doc := readReviewDoc(t, scratch)
	if !strings.Contains(doc, "`aaa-wt` →") || !strings.Contains(doc, "`bbb.log` — **failed**") || !strings.Contains(doc, "move bbb.log") {
		t.Fatalf("review doc = %q", doc)
	}
}

func TestRunPersistsRepairFailureAlongsideLaterMoveFailure(t *testing.T) {
	repo, scratch := newRepoWithScratch(t)
	git(t, repo, "worktree", "add", filepath.Join(scratch, "aaa-wt"), "-b", "partial-repair-failure")
	writeFileAt(t, filepath.Join(scratch, "bbb.log"), "fails later")
	original := runGit
	runGit = func(ctx context.Context, dir string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "worktree" && args[1] == "repair" {
			return "", errors.New("injected repair failure after partial move")
		}
		return original(ctx, dir, args...)
	}
	t.Cleanup(func() { runGit = original })
	var output bytes.Buffer
	w := &mutatingWriter{Buffer: &output, mutate: func() { _ = os.Remove(filepath.Join(scratch, "bbb.log")) }}
	var stderr bytes.Buffer
	err := Run(t.Context(), Options{Root: scratch, Apply: true, MoveWorktrees: true, MinGroup: DefaultMinGroup}, w, &stderr)
	if err == nil || !strings.Contains(err.Error(), "move bbb.log") || !strings.Contains(err.Error(), "injected repair failure after partial move") {
		t.Fatalf("err = %v", err)
	}
	doc := readReviewDoc(t, scratch)
	for _, want := range []string{"move bbb.log", "injected repair failure after partial move", "Operational failures"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("review doc = %q, want %q", doc, want)
		}
	}
}

func TestRunIgnoresUnrelatedPrunableRegistrationWhenMovedTargetIsHealthy(t *testing.T) {
	repo, scratch := newRepoWithScratch(t)
	stale := filepath.Join(repo, "unrelated-stale")
	git(t, repo, "worktree", "add", stale, "-b", "unrelated-stale")
	if err := os.Rename(stale, stale+".moved-aside"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(scratch, "target-wt")
	git(t, repo, "worktree", "add", target, "-b", "target-repair")
	if before := git(t, repo, "worktree", "list", "--porcelain"); !strings.Contains(before, "prunable") {
		t.Fatalf("fixture did not create a prunable registration: %q", before)
	}

	if _, stderr, err := runOrganize(t, Options{Root: scratch, Apply: true, MoveWorktrees: true}); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, stderr)
	}
	moved := filepath.Join(scratch, destReviewCheckouts, "target-wt")
	git(t, moved, "status", "--porcelain")
	after := git(t, repo, "worktree", "list", "--porcelain")
	if !strings.Contains(after, canonicalPath(moved)) || !strings.Contains(after, "prunable") {
		t.Fatalf("worktree list = %q", after)
	}
}

func TestRunReturnsNonZeroForFailedAndStaleWorktreeRepair(t *testing.T) {
	for _, mode := range []string{"failed", "stale"} {
		t.Run(mode, func(t *testing.T) {
			repo, scratch := newRepoWithScratch(t)
			git(t, repo, "worktree", "add", filepath.Join(scratch, "feature"), "-b", "feature")
			original := runGit
			runGit = func(ctx context.Context, dir string, args ...string) (string, error) {
				if len(args) >= 2 && args[0] == "worktree" && args[1] == "repair" {
					if mode == "failed" {
						return "", errors.New("injected repair failure")
					}
					return "", nil
				}
				return original(ctx, dir, args...)
			}
			t.Cleanup(func() { runGit = original })

			_, stderr, err := runOrganize(t, Options{Root: scratch, Apply: true, MoveWorktrees: true})
			if err == nil {
				t.Fatal("Run unexpectedly succeeded")
			}
			if mode == "failed" && !strings.Contains(err.Error(), "injected repair failure") {
				t.Fatalf("err = %v", err)
			}
			if mode == "stale" && !strings.Contains(err.Error(), "after repair") {
				t.Fatalf("err = %v", err)
			}
			if !strings.Contains(stderr, "ERROR:") || !strings.Contains(readReviewDoc(t, scratch), "Operational failures") {
				t.Fatalf("stderr=%q doc=%q", stderr, readReviewDoc(t, scratch))
			}
		})
	}
}

func TestRunReportsBothSymlinkBreakMechanismsAndPreservesInternalLinks(t *testing.T) {
	base := t.TempDir()
	root := mkdirAt(t, filepath.Join(base, "root"))
	writeFileAt(t, filepath.Join(base, "outside.txt"), "outside")

	relativeBundle := mkdirAt(t, filepath.Join(root, "relative-bundle", "sub"))
	writeFileAt(t, filepath.Join(filepath.Dir(relativeBundle), "notes.md"), "x")
	if err := os.Symlink("../../../outside.txt", filepath.Join(relativeBundle, "external-link")); err != nil {
		t.Fatal(err)
	}

	writeFileAt(t, filepath.Join(root, "source.txt"), "source")
	absoluteBundle := mkdirAt(t, filepath.Join(root, "absolute-bundle"))
	writeFileAt(t, filepath.Join(absoluteBundle, "notes.md"), "x")
	if err := os.Symlink(filepath.Join(root, "source.txt"), filepath.Join(absoluteBundle, "source-link")); err != nil {
		t.Fatal(err)
	}

	internalBundle := mkdirAt(t, filepath.Join(root, "internal-bundle"))
	writeFileAt(t, filepath.Join(internalBundle, "target.txt"), "target")
	if err := os.Symlink("target.txt", filepath.Join(internalBundle, "valid-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(root, "top-dangling")); err != nil {
		t.Fatal(err)
	}

	if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, name := range []string{"relative-bundle", "absolute-bundle", "top-dangling"} {
		requireFile(t, filepath.Join(root, destReviewSymlinks, name))
	}
	requireFile(t, filepath.Join(root, destArtifactsEvidence, "internal-bundle"))
	doc := readReviewDoc(t, root)
	if !strings.Contains(doc, "external-link") || !strings.Contains(doc, "source-link") || strings.Contains(doc, "valid-link") {
		t.Fatalf("review doc = %q", doc)
	}
}

func TestRunCanonicalizesEquivalentRootSpellingsForSymlinkPlanning(t *testing.T) {
	base := t.TempDir()
	realRoot := mkdirAt(t, filepath.Join(base, "real-root"))
	aliasRoot := filepath.Join(base, "alias-root")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatal(err)
	}
	writeFileAt(t, filepath.Join(realRoot, "source.txt"), "source")
	owner := mkdirAt(t, filepath.Join(realRoot, "bundle"))
	writeFileAt(t, filepath.Join(owner, "notes.md"), "notes")
	if err := os.Symlink(filepath.Join(canonicalPath(realRoot), "source.txt"), filepath.Join(owner, "source-link")); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, realRoot)

	stdout, _, err := runOrganize(t, Options{Root: aliasRoot})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout, "source-link") || !strings.Contains(stdout, "planned moves would break") {
		t.Fatalf("stdout = %q", stdout)
	}
	if after := snapshotTree(t, realRoot); !slices.Equal(before, after) {
		t.Fatalf("dry run changed canonical root\nbefore=%v\nafter=%v", before, after)
	}
}

func TestRunReportsKeptStationarySymlinkOwnerInDryRunAndApply(t *testing.T) {
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "source.txt"), "source")
	runs := mkdirAt(t, filepath.Join(root, "runs"))
	if err := os.Symlink("../source.txt", filepath.Join(runs, "source-link")); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runOrganize(t, Options{Root: root, Keep: []string{"runs"}})
	if err != nil {
		t.Fatalf("dry Run: %v", err)
	}
	for _, want := range []string{"STAYS IN PLACE  runs", "source-link", "planned moves would break"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("dry-run stdout = %q, want %q", stdout, want)
		}
	}

	if _, _, err := runOrganize(t, Options{Root: root, Keep: []string{"runs"}, Apply: true}); err != nil {
		t.Fatalf("apply Run: %v", err)
	}
	requireFile(t, filepath.Join(root, "runs", "source-link"))
	requireFile(t, filepath.Join(root, destArtifactsData, "source.txt"))
	doc := readReviewDoc(t, root)
	if !strings.Contains(doc, "`runs`") || !strings.Contains(doc, "actual outcomes broke or may have broken") || !strings.Contains(doc, "source-link") {
		t.Fatalf("review doc = %q", doc)
	}
}

func TestReservedNamesIncludeMetadataAndCallerKeeps(t *testing.T) {
	reserved := reservedNames([]string{" custom ", ""})
	for _, name := range []string{"reports", "artifacts", "review", reviewDocName, ".git", ".gitignore", "README.md", ".DS_Store", "custom"} {
		if !inSet(reserved, name) {
			t.Errorf("%q was not reserved", name)
		}
	}
}

func TestDisplayPathPrefersRelativeForm(t *testing.T) {
	root := t.TempDir()
	if got := displayPath(root, filepath.Join(root, "a", "b")); got != filepath.Join("a", "b") {
		t.Fatalf("displayPath = %q", got)
	}
}
