package testutil_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/gitenv"
	projectroot "github.com/conn-castle/agent-layer/internal/root"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

// TestGitIsolationBoundary exercises the same test-process entry point used by
// direct go test, Make's test/coverage targets, and Git's actual hook runner.
func TestGitIsolationBoundary(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required")
	}
	parent := filepath.Join(t.TempDir(), "repository with spaces")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, parent, "init", "--quiet", "--initial-branch=main")
	writeIsolationFile(t, filepath.Join(parent, "sentinel"), "preserve me\n", 0o600)
	gitFixture(t, parent, "add", "sentinel")
	gitFixture(t, parent, "-c", "user.name=Sentinel", "-c", "user.email=sentinel@example.invalid", "commit", "--quiet", "-m", "seed")
	linked := filepath.Join(parent, "linked")
	gitFixture(t, parent, "worktree", "add", "--quiet", "-b", "linked", linked)
	linkedGit := strings.TrimSpace(gitFixture(t, linked, "rev-parse", "--absolute-git-dir"))
	bareParent := filepath.Join(filepath.Dir(parent), "bare-sentinel.git")
	if err := os.Mkdir(bareParent, 0o700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, bareParent, "init", "--bare", "--quiet")
	// A temp symlink's logical ancestors must not become product roots.
	if err := os.Mkdir(filepath.Join(parent, ".agent-layer"), 0o700); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	link := filepath.Join(parent, "external-temp")
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}
	// Make only the disposable index stat cache stale, without changing content.
	stale := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(parent, "sentinel"), stale, stale); err != nil {
		t.Fatal(err)
	}
	protected := make(map[string][]byte)
	for _, path := range []string{
		filepath.Join(parent, ".git", "config"), filepath.Join(parent, ".git", "index"), filepath.Join(parent, ".git", "HEAD"),
		filepath.Join(linkedGit, "index"), filepath.Join(linkedGit, "HEAD"),
		filepath.Join(bareParent, "config"), filepath.Join(bareParent, "HEAD"),
		filepath.Join(parent, ".git", "refs", "heads", "main"), filepath.Join(parent, ".git", "refs", "heads", "linked"),
	} {
		data, err := os.ReadFile(path) // #nosec G304 -- explicit disposable sentinel metadata paths.
		if err != nil {
			t.Fatal(err)
		}
		protected[path] = data
	}
	assertProtected := func(t *testing.T) {
		t.Helper()
		for path, before := range protected {
			after, err := os.ReadFile(path) // #nosec G304 -- same disposable sentinel paths captured above.
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("sentinel metadata changed: %s (%v)", path, err)
			}
		}
	}
	hostileHooks := filepath.Join(parent, "hostile-hooks")
	marker := filepath.Join(parent, "host-hook-ran")
	writeIsolationFile(t, filepath.Join(hostileHooks, "pre-commit"), "#!/bin/sh\nprintf leaked > "+shellQuote(marker)+"\nexit 27\n", 0o700)
	global := filepath.Join(parent, "host.gitconfig")
	writeIsolationFile(t, global, "[core]\n\thooksPath = "+hostileHooks+"\n", 0o600)

	for _, mode := range []string{"direct", "hook", "bare-temp", "relative-temp", "symlink-temp", "shell"} {
		t.Run(mode, func(t *testing.T) {
			tempParent := parent
			if mode == "bare-temp" {
				tempParent = bareParent
			}
			if mode == "symlink-temp" {
				tempParent = link
			}
			requestedTemp := filepath.Join(tempParent, "requested-temp-"+mode)
			if err := os.Mkdir(requestedTemp, 0o700); err != nil {
				t.Fatal(err)
			}
			probe := filepath.Join(parent, "fixtures-"+mode)
			cwd := linked
			if mode == "relative-temp" {
				cwd = filepath.Join(linked, "nested")
				if err := os.Mkdir(cwd, 0o700); err != nil {
					t.Fatal(err)
				}
				requestedTemp = filepath.Join(cwd, "requested-temp")
				if err := os.Mkdir(requestedTemp, 0o700); err != nil {
					t.Fatal(err)
				}
				relative, err := filepath.Rel(cwd, requestedTemp)
				if err != nil {
					t.Fatal(err)
				}
				requestedTemp = relative
			}
			env := append(os.Environ(),
				"AL_TEST_GIT_ISOLATION_PROBE="+probe, "AL_TEST_STATUS_REPO="+parent, "LC_ALL=C",
				"GIT_OPTIONAL_LOCKS=1",
				"TMPDIR="+requestedTemp, "TMP="+requestedTemp, "TEMP="+requestedTemp,
				"GIT_DIR="+linkedGit, "GIT_WORK_TREE="+linked,
				"GIT_COMMON_DIR="+filepath.Join(parent, ".git"), "GIT_INDEX_FILE="+filepath.Join(linkedGit, "index"),
				"GIT_OBJECT_DIRECTORY="+filepath.Join(parent, ".git", "objects"),
				"GIT_ALTERNATE_OBJECT_DIRECTORIES="+filepath.Join(parent, ".git", "objects"),
				"GIT_IMPLICIT_WORK_TREE=0", "GIT_NAMESPACE=foreign",
				"GIT_CONFIG="+filepath.Join(parent, ".git", "config"),
				"GIT_CONFIG_GLOBAL="+global, "GIT_CONFIG_SYSTEM="+global,
				"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.hooksPath", "GIT_CONFIG_VALUE_0="+hostileHooks,
				"GIT_CONFIG_KEY_93=core.bare", "GIT_CONFIG_VALUE_93=true",
				"GIT_CONFIG_PARAMETERS="+shellQuote("core.hooksPath="+hostileHooks),
			)
			args := []string{os.Args[0], "-test.run=^TestGitIsolationProbe$", "-test.count=1"}
			if mode == "hook" {
				hooks := filepath.Join(parent, "test-hooks")
				writeIsolationFile(t, filepath.Join(hooks, "pre-commit"), "#!/bin/sh\nexec "+shellQuote(os.Args[0])+" -test.run=^TestGitIsolationProbe$ -test.count=1\n", 0o700)
				args = []string{"git", "-c", "core.hooksPath=" + hooks, "hook", "run", "pre-commit"}
			}
			if mode == "shell" {
				bootstrap, err := filepath.Abs(filepath.Join("..", "..", "scripts", "test-git-env.sh"))
				if err != nil {
					t.Fatal(err)
				}
				script := filepath.Join(parent, "shell-probe.sh")
				writeIsolationFile(t, script, `#!/bin/bash
set -euo pipefail
source "$1"
isolate_test_git
git -C "$AL_TEST_STATUS_REPO" status --porcelain -- sentinel >/dev/null
mkdir -p "$2/regular" "$2/bare.git"
git -C "$2/regular" init --quiet --initial-branch=main
git -C "$2/bare.git" init --quiet --bare
git -C "$2/regular" config user.name Fixture
git -C "$2/regular" config user.email fixture@example.invalid
printf fixture > "$2/regular/file"
git -C "$2/regular" add file
git -C "$2/regular" read-tree --empty
[[ -z "$(git -C "$2/regular" ls-files)" ]]
git -C "$2/regular" add file
git -C "$2/regular" commit --quiet -m fixture
git -C "$2/regular" push --quiet "$2/bare.git" main
[[ "$(git -C "$2/bare.git" show main:file)" == fixture ]]
export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0="url.$2/regular.insteadOf" GIT_CONFIG_VALUE_0=https://fixture.invalid/source
[[ -n "$(git ls-remote https://fixture.invalid/source HEAD)" ]]
`, 0o700)
				args = []string{"bash", script, bootstrap, probe}
			}
			cmd := exec.Command(args[0], args[1:]...) // #nosec G204 G702 -- fixed test executable, Git hook runner, or shell probe; every argument targets disposable fixtures.
			cmd.Dir, cmd.Env = cwd, env
			out, err := cmd.CombinedOutput()
			assertProtected(t) // Check even when the subprocess fails.
			if err != nil {
				t.Fatalf("%s test subprocess: %v\n%s", mode, err, out)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("fixture inherited the invoking hook: %v", err)
			}
		})
	}
}

// TestGitIsolationProbe runs only in a child whose TestMain receives the hostile
// environment. Assertions use real Git operations, rather than a filter list.
func TestGitIsolationProbe(t *testing.T) {
	root := os.Getenv("AL_TEST_GIT_ISOLATION_PROBE")
	if root == "" {
		t.Skip("subprocess probe")
	}
	outside := t.TempDir()
	if found, ok, err := projectroot.FindAgentLayerRoot(outside); err != nil || ok {
		t.Fatalf("temporary fixture discovered an Agent Layer parent: %q, %v, %v", found, ok, err)
	}
	physicalOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}
	if found, err := projectroot.FindRepoRoot(outside); err != nil || found != physicalOutside {
		t.Fatalf("temporary fixture discovered a repository parent: %q, %v", found, err)
	}
	// The benchmark's development-source probe uses this same public status
	// boundary after WithoutDiscovery. Its read must not refresh a source index.
	status := exec.Command("git", "status", "--porcelain", "--", "sentinel") // #nosec G204 -- fixed status probe targeting the disposable parent sentinel.
	status.Dir, status.Env = os.Getenv("AL_TEST_STATUS_REPO"), gitenv.WithoutDiscovery()
	if out, err := status.CombinedOutput(); err != nil {
		t.Fatalf("source-status probe: %v\n%s", err, out)
	}
	for _, args := range [][]string{{"rev-parse", "--show-toplevel"}, {"read-tree", "--empty"}} {
		cmd := exec.Command("git", args...) // #nosec G204 -- fixed commands in the test-owned nonrepository directory.
		cmd.Dir = outside
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), "not a git repository") {
			t.Fatalf("expected nonrepository behavior for %v: %v: %s", args, err, out)
		}
	}
	// Explicit fixtures can still be below a repository: initialization and
	// subsequent writes must target them rather than the inherited hook target.
	regular, bare := filepath.Join(root, "regular"), filepath.Join(root, "bare.git")
	for _, dir := range []string{regular, bare} {
		if err := os.MkdirAll(dir, 0o700); err != nil { // #nosec G703 -- root is passed only by the disposable parent probe.
			t.Fatal(err)
		}
	}
	gitFixture(t, regular, "init", "--quiet", "--initial-branch=main")
	gitFixture(t, bare, "init", "--quiet", "--bare")
	gitFixture(t, regular, "config", "user.name", "Fixture")
	gitFixture(t, regular, "config", "user.email", "fixture@example.invalid")
	gitFixture(t, regular, "config", "receive.denyCurrentBranch", "updateInstead")
	gitFixture(t, bare, "config", "user.name", "Bare Fixture")
	physicalRegular, err := filepath.EvalSymlinks(regular)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(gitFixture(t, regular, "rev-parse", "--show-toplevel")); got != physicalRegular {
		t.Fatalf("regular repository = %q, want %q", got, physicalRegular)
	}
	if got := strings.TrimSpace(gitFixture(t, bare, "config", "--local", "--get", "core.bare")); got != "true" {
		t.Fatalf("bare repository core.bare = %q", got)
	}
	writeIsolationFile(t, filepath.Join(regular, "fixture"), "fixture content\n", 0o600)
	gitFixture(t, regular, "add", "fixture")
	gitFixture(t, regular, "read-tree", "--empty")
	if got := gitFixture(t, regular, "ls-files"); got != "" {
		t.Fatalf("fixture index not cleared: %s", got)
	}
	gitFixture(t, regular, "add", "fixture")
	gitFixture(t, regular, "commit", "--quiet", "-m", "fixture")
	gitFixture(t, regular, "push", "--quiet", bare, "main")
	if got := gitFixture(t, bare, "show", "main:fixture"); got != "fixture content\n" {
		t.Fatalf("bare fixture content = %q", got)
	}
	// Exercise the original raw-Git catalog helper and its deliberate inherited
	// URL rewrite. Startup isolation must not scrub configuration added by tests.
	catalog := testutil.CatalogGitFixture(t)
	want := strings.TrimSpace(gitFixture(t, catalog, "rev-parse", "HEAD"))
	got := gitFixture(t, outside, "ls-remote", templates.GeneralSkillsRepository, "HEAD")
	if !strings.HasPrefix(got, want+"\t") {
		t.Fatalf("test-owned URL rewrite failed: %q", got)
	}
}

func gitFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 G702 -- fixed Git executable with explicit test-controlled arguments and disposable fixture cwd.
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func writeIsolationFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil { // #nosec G703 -- paths belong to this test's disposable sentinel or child fixture.
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil { // #nosec G306 G703 -- explicit private executable fixtures under the disposable test root.
		t.Fatal(err)
	}
}

func shellQuote(s string) string {
	return fmt.Sprintf("'%s'", strings.ReplaceAll(s, "'", "'\\''"))
}
