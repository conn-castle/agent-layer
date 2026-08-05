package gitrepo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// testRepo is a hermetic local repository. No network or credential is used.
type testRepo struct {
	t   *testing.T
	dir string
}

func newTestRepo(t *testing.T, defaultBranch string) *testRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is required: %v", err)
	}
	configureTestGitIdentity(t)
	repo := &testRepo{t: t, dir: t.TempDir()}
	repo.git("init", "--quiet", "--initial-branch="+defaultBranch)
	repo.git("config", "user.name", "Agent Layer Test")
	repo.git("config", "user.email", "test@example.invalid")
	repo.git("config", "receive.denyCurrentBranch", "updateInstead")
	repo.write("README.md", "seed\n", 0o644)
	repo.commit("seed")
	return repo
}

// configureTestGitIdentity keeps commits created in isolated destination
// checkouts independent of the developer or CI runner's global Git config.
func configureTestGitIdentity(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Agent Layer Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Agent Layer Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
}

func (r *testRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- test-controlled arguments.
	cmd.Dir = r.dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func (r *testRepo) write(relative string, content string, mode os.FileMode) {
	r.t.Helper()
	path := filepath.Join(r.dir, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		r.t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil { // #nosec G306 -- mode is part of the fixture.
		r.t.Fatalf("write: %v", err)
	}
}

func (r *testRepo) commit(message string) string {
	r.t.Helper()
	r.git("add", "--all")
	r.git("commit", "--quiet", "--allow-empty", "--message", message)
	return r.git("rev-parse", "HEAD")
}

// literalRepository wraps a reference that carries no placeholder, which is
// what every fixture that is not exercising secret resolution uses.
func literalRepository(reference string) Repository {
	resolved, err := NewSecrets(nil).Resolve(reference)
	if err != nil {
		panic(err)
	}
	return resolved
}

func newTestSource(t *testing.T, repo *testRepo) (*Runner, *Source) {
	t.Helper()
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	source, err := OpenSource(context.Background(), runner, t.TempDir(), literalRepository(repo.dir))
	if err != nil {
		t.Fatalf("OpenSource: %v", err)
	}
	return runner, source
}

// TestResolveProvesRefKindFromTheRemote proves ref kind is remote-resolved
// evidence, never inferred from the configured string.
func TestResolveProvesRefKindFromTheRemote(t *testing.T) {
	repo := newTestRepo(t, "trunk")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	commit := repo.commit("add alpha")
	repo.git("tag", "v1.0.0")
	repo.git("branch", "feature")

	_, source := newTestSource(t, repo)
	ctx := context.Background()

	tests := []struct {
		name     string
		ref      string
		wantRef  string
		wantKind string
	}{
		{name: "omitted resolves the default branch", ref: "", wantRef: "trunk", wantKind: skilllock.RefKindBranch},
		{name: "branch", ref: "feature", wantRef: "feature", wantKind: skilllock.RefKindBranch},
		{name: "tag", ref: "v1.0.0", wantRef: "v1.0.0", wantKind: skilllock.RefKindTag},
		{name: "commit id", ref: commit, wantRef: commit, wantKind: skilllock.RefKindCommit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolution, err := source.Resolve(ctx, tt.ref)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tt.ref, err)
			}
			if resolution.Ref != tt.wantRef || resolution.Kind != tt.wantKind {
				t.Fatalf("resolution = %+v, want ref %q kind %q", resolution, tt.wantRef, tt.wantKind)
			}
			if resolution.Commit != commit {
				t.Fatalf("commit = %s, want %s", resolution.Commit, commit)
			}
		})
	}

	if _, err := source.Resolve(ctx, "absent"); err == nil {
		t.Fatal("expected a missing ref to fail")
	}

	repo.git("tag", "ambiguous")
	repo.git("branch", "ambiguous")
	if _, err := source.Resolve(ctx, "ambiguous"); err == nil {
		t.Fatal("expected an ambiguous branch/tag name to fail")
	} else if !strings.Contains(err.Error(), "both a branch and a tag") {
		t.Fatalf("ambiguity error = %v", err)
	}
}

// TestReadTreeReturnsExactCanonicalContent proves a source tree carries the
// same canonical shape as a local skill tree.
func TestReadTreeReturnsExactCanonicalContent(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	repo.write("skills/alpha/.hidden", "hidden\n", 0o644)
	repo.write("skills/alpha/scripts/run.sh", "#!/bin/sh\n", 0o755)
	repo.write("skills/alpha/.DS_Store", "junk", 0o644)
	commit := repo.commit("add alpha")

	_, source := newTestSource(t, repo)
	tree, err := source.ReadTree(context.Background(), commit, "skills/alpha")
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if got := strings.Join(tree.Paths(), ","); got != ".hidden,SKILL.md,scripts/run.sh" {
		t.Fatalf("paths = %q", got)
	}
	script, _ := tree.File("scripts/run.sh")
	if !script.Executable {
		t.Fatal("executable bit lost")
	}

	local := t.TempDir()
	if err := skilltree.Materialize(tree, local); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	roundTrip, err := skilltree.Read(skilltree.OSFS{}, local, skilltree.PolicyStrict)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if roundTrip.Hash() != tree.Hash() {
		t.Fatal("upstream and local hashes disagree for identical content")
	}
}

// TestLocalExecutableClassificationMatchesGit proves only owner execute maps to
// Git mode 100755. Group- or other-execute bits alone remain 100644, so local
// hashing and a committed source tree classify the same bytes identically.
func TestLocalExecutableClassificationMatchesGit(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	modes := map[string]os.FileMode{
		"owner.sh": 0o744,
		"group.sh": 0o654,
		"other.sh": 0o645,
	}
	for name, mode := range modes {
		repo.write("skills/alpha/"+name, name+"\n", 0o644)
		if err := os.Chmod(filepath.Join(repo.dir, "skills", "alpha", name), mode); err != nil {
			t.Fatalf("chmod %s: %v", name, err)
		}
	}
	commit := repo.commit("record executable modes")

	_, source := newTestSource(t, repo)
	upstream, err := source.ReadTree(context.Background(), commit, "skills/alpha")
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	local, err := skilltree.Read(skilltree.OSFS{}, filepath.Join(repo.dir, "skills", "alpha"), skilltree.PolicyStrict)
	if err != nil {
		t.Fatalf("local Read: %v", err)
	}
	if upstream.Hash() != local.Hash() {
		t.Fatalf("local hash %s does not match Git tree hash %s", local.Hash(), upstream.Hash())
	}
	for name, want := range map[string]bool{"owner.sh": true, "group.sh": false, "other.sh": false} {
		file, ok := local.File(name)
		if !ok || file.Executable != want {
			t.Fatalf("%s executable = %v, want %v", name, file.Executable, want)
		}
	}
}

// TestReadTreeRejectsUnsafeEntries proves a symlink recorded in Git is refused
// without being followed.
func TestReadTreeRejectsUnsafeEntries(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	repo.write("skills/alpha/target.md", "secret\n", 0o644)
	if err := os.Symlink("target.md", filepath.Join(repo.dir, "skills", "alpha", "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	commit := repo.commit("add link")

	_, source := newTestSource(t, repo)
	_, err := source.ReadTree(context.Background(), commit, "skills/alpha")
	if err == nil {
		t.Fatal("expected a symlink entry to be rejected")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("error = %v", err)
	}
}

// TestReadTreeRejectsGitlinks proves a committed submodule inside a selected
// skill root fails loudly. It is the realistic case of the same rejection the
// symlink test covers: git records it as a commit object, and following or
// silently dropping it would import a skill the source does not describe.
func TestReadTreeRejectsGitlinks(t *testing.T) {
	inner := newTestRepo(t, "main")
	innerCommit := inner.commit("inner")

	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	repo.commit("add alpha")
	repo.git("update-index", "--add", "--cacheinfo", "160000,"+innerCommit+",skills/alpha/vendor")
	tree := repo.git("write-tree")
	commit := repo.git("commit-tree", tree, "-p", repo.git("rev-parse", "HEAD"), "-m", "add gitlink")
	repo.git("update-ref", "refs/heads/main", commit)

	_, source := newTestSource(t, repo)
	_, err := source.ReadTree(context.Background(), commit, "skills/alpha")
	if err == nil {
		t.Fatal("expected a gitlink entry to be rejected")
	}
	if !strings.Contains(err.Error(), "gitlink") {
		t.Fatalf("error = %v", err)
	}
}

// TestPublishPreservesDestinationOnlyIgnoredFiles proves push contributes skill
// content without removing files it does not manage. Canonical trees exclude
// .DS_Store and Thumbs.db, so replacing the skill path wholesale would publish
// a deletion of any the destination itself committed.
func TestPublishPreservesDestinationOnlyIgnoredFiles(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	repo.write("skills/alpha/.DS_Store", "destination noise\n", 0o644)
	repo.write("skills/alpha/nested/Thumbs.db", "more noise\n", 0o644)
	head := repo.commit("add alpha with ignored artifacts")

	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	ctx := context.Background()
	destination, err := OpenDestination(ctx, runner, t.TempDir(), literalRepository(repo.dir))
	if err != nil {
		t.Fatalf("OpenDestination: %v", err)
	}
	if err := destination.FetchCommit(ctx, literalRepository(repo.dir), head); err != nil {
		t.Fatalf("FetchCommit: %v", err)
	}
	tree, err := skilltree.NewTree([]skilltree.File{
		{Path: "SKILL.md", Data: []byte("---\nname: alpha\ndescription: d\n---\nUpdated\n")},
	})
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	if _, err := destination.Publish(ctx, head, "main", []Update{{Path: "skills/alpha", Tree: tree}}, "Update skill alpha"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if got := repo.git("show", "main:skills/alpha/SKILL.md"); !strings.Contains(got, "Updated") {
		t.Fatalf("skill content was not published: %q", got)
	}
	for path, want := range map[string]string{
		"skills/alpha/.DS_Store":        "destination noise",
		"skills/alpha/nested/Thumbs.db": "more noise",
	} {
		got := repo.git("show", "main:"+path)
		if !strings.Contains(got, want) {
			t.Fatalf("%s was removed by publication: %q", path, got)
		}
	}
}

// TestPublishRejectsAnUnsafeUpdatePath proves the path a publication clears is
// validated before any removal, so a caller that bypassed lock parsing cannot
// direct os.RemoveAll outside the skill root.
func TestPublishRejectsAnUnsafeUpdatePath(t *testing.T) {
	repo := newTestRepo(t, "main")
	head := repo.commit("seed publish")

	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	ctx := context.Background()
	destination, err := OpenDestination(ctx, runner, t.TempDir(), literalRepository(repo.dir))
	if err != nil {
		t.Fatalf("OpenDestination: %v", err)
	}
	if err := destination.FetchCommit(ctx, literalRepository(repo.dir), head); err != nil {
		t.Fatalf("FetchCommit: %v", err)
	}
	tree, err := skilltree.NewTree([]skilltree.File{
		{Path: "SKILL.md", Data: []byte("---\nname: alpha\ndescription: d\n---\nBody\n")},
	})
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	_, err = destination.Publish(ctx, head, "main", []Update{{Path: "../escape", Tree: tree}}, "escape")
	if err == nil {
		t.Fatal("expected an unsafe destination path to be rejected")
	}
	if !strings.Contains(err.Error(), "unsafe") {
		t.Fatalf("error = %v", err)
	}
}

// TestListDirectoriesAndPathExistsSupportSelectorResolution proves wildcard
// expansion and exact-selector checks read the repository without a checkout.
func TestListDirectoriesAndPathExistsSupportSelectorResolution(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	repo.write("skills/beta/README.md", "not a skill\n", 0o644)
	commit := repo.commit("add candidates")

	_, source := newTestSource(t, repo)
	ctx := context.Background()

	dirs, err := source.ListDirectories(ctx, commit)
	if err != nil {
		t.Fatalf("ListDirectories: %v", err)
	}
	if got := strings.Join(dirs, ","); got != "skills,skills/alpha,skills/beta" {
		t.Fatalf("directories = %q", got)
	}

	exists, isDir, err := source.PathExists(ctx, commit, "skills/alpha")
	if err != nil || !exists || !isDir {
		t.Fatalf("PathExists(dir) = (%v, %v, %v)", exists, isDir, err)
	}
	exists, isDir, err = source.PathExists(ctx, commit, "skills/alpha/SKILL.md")
	if err != nil || !exists || isDir {
		t.Fatalf("PathExists(file) = (%v, %v, %v)", exists, isDir, err)
	}
	exists, _, err = source.PathExists(ctx, commit, "skills/absent")
	if err != nil || exists {
		t.Fatalf("PathExists(absent) = (%v, %v)", exists, err)
	}
}

// TestFetchRejectsAnUnknownCommit proves a locked commit that cannot be fetched
// fails loudly instead of resolving to something else.
func TestFetchRejectsAnUnknownCommit(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.commit("only commit")

	_, source := newTestSource(t, repo)
	if err := source.Fetch(context.Background(), "0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("expected an unknown commit to fail")
	}
	if source.Repository() != repo.dir {
		t.Fatalf("Repository() = %q", source.Repository())
	}
}

// TestMergeTextMergesCompatibleChangesAndReportsConflicts proves the production
// text merger routes both outcomes correctly.
func TestMergeTextMergesCompatibleChangesAndReportsConflicts(t *testing.T) {
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	ctx := context.Background()

	base := []byte("one\ntwo\nthree\nfour\nfive\n")
	local := []byte("local one\ntwo\nthree\nfour\nfive\n")
	remote := []byte("one\ntwo\nthree\nfour\nremote five\n")
	merged, conflicted, err := runner.MergeText(ctx, base, local, remote)
	if err != nil {
		t.Fatalf("MergeText: %v", err)
	}
	if conflicted {
		t.Fatalf("compatible changes reported a conflict: %s", merged)
	}
	if string(merged) != "local one\ntwo\nthree\nfour\nremote five\n" {
		t.Fatalf("merged = %q", merged)
	}

	_, conflicted, err = runner.MergeText(ctx, base, []byte("a\ntwo\nthree\nfour\nfive\n"), []byte("b\ntwo\nthree\nfour\nfive\n"))
	if err != nil {
		t.Fatalf("MergeText: %v", err)
	}
	if !conflicted {
		t.Fatal("incompatible changes to one line did not conflict")
	}

	merger := runner.TextMerger(ctx)
	if _, _, err := merger(base, local, remote); err != nil {
		t.Fatalf("TextMerger: %v", err)
	}
}

// TestDestinationPublishesWithoutForce proves a grouped contribution is
// committed onto the destination head and pushed as a fast-forward.
func TestDestinationPublishesWithoutForce(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	head := repo.commit("add alpha")

	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	ctx := context.Background()
	destination, err := OpenDestination(ctx, runner, t.TempDir(), literalRepository(repo.dir))
	if err != nil {
		t.Fatalf("OpenDestination: %v", err)
	}
	if destination.Repository() != repo.dir {
		t.Fatalf("Repository() = %q", destination.Repository())
	}
	branch, err := destination.DefaultBranch(ctx)
	if err != nil || branch != "main" {
		t.Fatalf("DefaultBranch = (%q, %v)", branch, err)
	}
	got, exists, err := destination.Head(ctx, "main")
	if err != nil || !exists || got != head {
		t.Fatalf("Head = (%q, %v, %v), want %s", got, exists, err, head)
	}
	if _, exists, err = destination.Head(ctx, "absent"); err != nil || exists {
		t.Fatalf("Head(absent) = (%v, %v)", exists, err)
	}
	if err := destination.FetchCommit(ctx, literalRepository(repo.dir), head); err != nil {
		t.Fatalf("FetchCommit: %v", err)
	}

	tree, err := skilltree.NewTree([]skilltree.File{
		{Path: "SKILL.md", Data: []byte("---\nname: alpha\ndescription: d\n---\nUpdated\n")},
	})
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	commit, err := destination.Publish(ctx, head, "main", []Update{{Path: "skills/alpha", Tree: tree}}, "Update skill alpha")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if commit == head {
		t.Fatal("Publish did not create a commit")
	}
	if got := repo.git("show", "main:skills/alpha/SKILL.md"); !strings.Contains(got, "Updated") {
		t.Fatalf("destination content = %q", got)
	}
	if _, err := destination.Publish(ctx, head, "main", nil, "no updates"); err == nil {
		t.Fatal("expected publishing zero updates to fail")
	}
}

// TestIsCommitIDAcceptsOnlyFullObjectIDs proves an abbreviated id is never
// treated as an unambiguous commit selection.
func TestIsCommitIDAcceptsOnlyFullObjectIDs(t *testing.T) {
	t.Parallel()
	// Both widths git uses are accepted, matching the widths skilllock records:
	// a SHA-256 repository's 64-character id must resolve as a commit rather
	// than being looked up as a branch or tag name that does not exist.
	for _, value := range []string{
		"0123456789abcdef0123456789abcdef01234567",
		strings.Repeat("ab", 32),
	} {
		if !IsCommitID(value) {
			t.Fatalf("%d-character object id was rejected", len(value))
		}
	}
	for _, value := range []string{"0123456", "main", "v1.0.0", strings.Repeat("g", 40), strings.Repeat("a", 41), strings.Repeat("a", 63)} {
		if IsCommitID(value) {
			t.Fatalf("%q was treated as a commit id", value)
		}
	}
}

// TestResolveNormalizesAnUppercaseCommitID proves a configured object id is
// recorded in the lowercase form git prints. skilllock only accepts lowercase
// hex, so an uppercase id recorded verbatim would make the lock unwritable and
// would never compare equal to a later resolution of the same commit.
func TestResolveNormalizesAnUppercaseCommitID(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	commit := repo.commit("add alpha")

	_, source := newTestSource(t, repo)
	resolution, err := source.Resolve(context.Background(), strings.ToUpper(commit))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolution.Commit != commit || resolution.Ref != commit {
		t.Fatalf("resolution = %+v, want lowercase commit %s", resolution, commit)
	}
	entry := skilllock.Entry{
		Name: "alpha", Repository: repo.dir, Selector: "skills/alpha", SelectedPath: "skills/alpha",
		ConfiguredRef: strings.ToUpper(commit), ResolvedRef: resolution.Ref, RefKind: resolution.Kind,
		Tracking: skilllock.TrackingPinned, Commit: resolution.Commit,
		TreeHash: "sha256:" + strings.Repeat("0", 64),
	}
	lock := skilllock.New()
	lock.Upsert(entry)
	if _, err := lock.Marshal(); err != nil {
		t.Fatalf("the resolved commit is not recordable in the lock: %v", err)
	}
}

// TestNonInteractiveEnvDropsInheritedRepositorySelection proves an ambient Git
// repository selection cannot redirect an isolated import at the consuming
// project's own repository, which is the isolation this package promises.
func TestNonInteractiveEnvDropsInheritedRepositorySelection(t *testing.T) {
	t.Parallel()
	env := nonInteractiveEnv([]string{
		"PATH=/usr/bin",
		"GIT_DIR=/elsewhere/.git",
		"GIT_WORK_TREE=/elsewhere",
		"GIT_INDEX_FILE=/elsewhere/.git/index",
		"GIT_COMMON_DIR=/elsewhere/.git",
		"GIT_OBJECT_DIRECTORY=/elsewhere/.git/objects",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES=/other/objects",
		"GIT_NAMESPACE=ns",
	})
	joined := strings.Join(env, "\n")
	for _, leaked := range []string{"GIT_DIR=", "GIT_WORK_TREE=", "GIT_INDEX_FILE=", "GIT_COMMON_DIR=", "GIT_OBJECT_DIRECTORY=", "GIT_ALTERNATE_OBJECT_DIRECTORIES=", "GIT_NAMESPACE="} {
		if strings.Contains(joined, leaked) {
			t.Fatalf("%s survived into the isolated environment: %v", leaked, env)
		}
	}
	if !strings.Contains(joined, "PATH=/usr/bin") {
		t.Fatal("unrelated environment entries were dropped")
	}
}

// TestRunnerIgnoresAnInheritedGitDir proves the filtering above holds for a
// real invocation: a runner built while GIT_DIR names another repository still
// reports the repository its working directory selects.
func TestRunnerIgnoresAnInheritedGitDir(t *testing.T) {
	elsewhere := newTestRepo(t, "main")
	elsewhere.write("marker.txt", "elsewhere\n", 0o644)
	elsewhereCommit := elsewhere.commit("elsewhere commit")

	here := newTestRepo(t, "main")
	hereCommit := here.commit("here commit")

	t.Setenv("GIT_DIR", filepath.Join(elsewhere.dir, ".git"))
	t.Setenv("GIT_WORK_TREE", elsewhere.dir)
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	output, err := runner.run(context.Background(), here.dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	got := strings.TrimSpace(string(output))
	if got == elsewhereCommit {
		t.Fatal("the inherited GIT_DIR redirected the command at another repository")
	}
	if got != hereCommit {
		t.Fatalf("HEAD = %s, want %s", got, hereCommit)
	}
}

// TestCommandErrorSurfacesGitDiagnostics proves a failed invocation reports the
// captured stderr so the user sees an actionable message.
func TestCommandErrorSurfacesGitDiagnostics(t *testing.T) {
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	_, err = runner.run(context.Background(), t.TempDir(), "rev-parse", "HEAD")
	if err == nil {
		t.Fatal("expected a failure outside a repository")
	}
	if !strings.Contains(err.Error(), "git rev-parse HEAD failed") {
		t.Fatalf("error = %v", err)
	}
}

// TestNonInteractiveEnvDisablesPrompting proves Agent Layer never blocks on a
// credential prompt and never reads or stores a credential itself.
func TestNonInteractiveEnvDisablesPrompting(t *testing.T) {
	t.Parallel()
	env := nonInteractiveEnv([]string{"PATH=/usr/bin", "GIT_TERMINAL_PROMPT=1", "GIT_ASKPASS=/bin/askpass"})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "GIT_TERMINAL_PROMPT=1") || strings.Contains(joined, "GIT_ASKPASS=/bin/askpass") {
		t.Fatalf("interactive settings survived: %v", env)
	}
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=", "GIT_OPTIONAL_LOCKS=0"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("environment is missing %q: %v", want, env)
		}
	}
	if !strings.Contains(joined, "PATH=/usr/bin") {
		t.Fatal("unrelated environment entries were dropped")
	}
}

// TestOpenSourceRequiresARunner proves the adapter refuses to operate without a
// resolved git executable rather than silently doing nothing.
func TestOpenSourceRequiresARunner(t *testing.T) {
	t.Parallel()
	if _, err := OpenSource(context.Background(), nil, t.TempDir(), literalRepository("repo")); err == nil {
		t.Fatal("expected a missing runner to fail")
	}
	if _, err := OpenDestination(context.Background(), nil, t.TempDir(), literalRepository("repo")); err == nil {
		t.Fatal("expected a missing runner to fail")
	}
}

// TestDestinationReadTreeSeesFetchedContent proves push reconciliation can read
// the destination head's current content for a skill path.
func TestDestinationReadTreeSeesFetchedContent(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nDestination\n", 0o644)
	head := repo.commit("add alpha")

	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	ctx := context.Background()
	destination, err := OpenDestination(ctx, runner, t.TempDir(), literalRepository(repo.dir))
	if err != nil {
		t.Fatalf("OpenDestination: %v", err)
	}
	if err := destination.FetchCommit(ctx, literalRepository(repo.dir), head); err != nil {
		t.Fatalf("FetchCommit: %v", err)
	}
	tree, err := destination.ReadTree(ctx, head, "skills/alpha")
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	manifest, ok := tree.File("SKILL.md")
	if !ok || !strings.Contains(string(manifest.Data), "Destination") {
		t.Fatalf("destination tree = %v", tree.Paths())
	}
	// A second fetch of an already-present commit is a no-op rather than an error.
	if err := destination.FetchCommit(ctx, literalRepository(repo.dir), head); err != nil {
		t.Fatalf("repeat FetchCommit: %v", err)
	}
	if err := destination.FetchCommit(ctx, literalRepository(repo.dir), "0000000000000000000000000000000000000000"); err == nil {
		t.Fatal("expected fetching an unknown commit to fail")
	}
}

// TestAnnotateIdentityFailureGuidesTheUser proves Agent Layer never invents a
// commit author and instead reports how to configure one.
func TestAnnotateIdentityFailureGuidesTheUser(t *testing.T) {
	t.Parallel()
	identity := &CommandError{Args: []string{"commit"}, Stderr: "Please tell me who you are", Err: errors.New("exit status 128")}
	annotated := annotateIdentityFailure(identity)
	if !strings.Contains(annotated.Error(), "git config user.name") {
		t.Fatalf("annotated error = %v", annotated)
	}
	if !errors.Is(annotated, identity.Err) {
		t.Fatal("annotation dropped the underlying error")
	}

	other := &CommandError{Args: []string{"push"}, Stderr: "permission denied", Err: errors.New("exit status 128")}
	if got := annotateIdentityFailure(other); got != error(other) {
		t.Fatalf("unrelated failure was annotated: %v", got)
	}
	if !errors.Is(other, other.Err) {
		t.Fatal("CommandError does not unwrap to its cause")
	}
	empty := &CommandError{Args: []string{"fetch"}, Err: errors.New("boom")}
	if !strings.Contains(empty.Error(), "boom") {
		t.Fatalf("a failure with no stderr lost its cause: %v", empty)
	}
}

// TestReadTreeRejectsMalformedRecords proves a record git could not have
// produced fails loudly instead of being silently skipped.
func TestReadTreeRejectsMalformedRecords(t *testing.T) {
	t.Parallel()
	for _, record := range []string{"no tab present", "100644 blob\tmissing-object"} {
		if _, _, _, _, err := parseTreeRecord(record); err == nil { //nolint:dogsled // only the error is under test.
			t.Fatalf("record %q was accepted", record)
		}
	}
	mode, objectType, object, name, err := parseTreeRecord("100755 blob abcdef\tscripts/run.sh")
	if err != nil || mode != "100755" || objectType != "blob" || object != "abcdef" || name != "scripts/run.sh" {
		t.Fatalf("parse = (%q, %q, %q, %q, %v)", mode, objectType, object, name, err)
	}
}

// TestSourceFetchesAllRefsOnlyOnce proves repeated reads reuse one mirror
// instead of refetching the repository per skill.
func TestSourceFetchesAllRefsOnlyOnce(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	repo.write("skills/beta/SKILL.md", "---\nname: beta\ndescription: d\n---\nBody\n", 0o644)
	commit := repo.commit("add skills")

	_, source := newTestSource(t, repo)
	ctx := context.Background()
	if err := source.Fetch(ctx, commit); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !source.fetched {
		t.Fatal("the first fetch did not mirror the repository")
	}
	// A repeat fetch is a no-op and every path stays readable.
	if err := source.Fetch(ctx, commit); err != nil {
		t.Fatalf("repeat Fetch: %v", err)
	}
	for _, path := range []string{"skills/alpha", "skills/beta"} {
		if _, err := source.ReadTree(ctx, commit, path); err != nil {
			t.Fatalf("ReadTree(%s): %v", path, err)
		}
	}
}

// TestPublishReportsAFailedPush proves a rejected push is surfaced rather than
// silently retried with force.
func TestPublishReportsAFailedPush(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	head := repo.commit("add alpha")

	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	ctx := context.Background()
	destination, err := OpenDestination(ctx, runner, t.TempDir(), literalRepository(repo.dir))
	if err != nil {
		t.Fatalf("OpenDestination: %v", err)
	}
	if err := destination.FetchCommit(ctx, literalRepository(repo.dir), head); err != nil {
		t.Fatalf("FetchCommit: %v", err)
	}
	// The destination refuses non-fast-forward updates to its checked-out
	// branch once denyCurrentBranch is restored to its default.
	repo.git("config", "receive.denyCurrentBranch", "refuse")

	tree, err := skilltree.NewTree([]skilltree.File{
		{Path: "SKILL.md", Data: []byte("---\nname: alpha\ndescription: d\n---\nUpdated\n")},
	})
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	if _, err := destination.Publish(ctx, head, "main", []Update{{Path: "skills/alpha", Tree: tree}}, "Update skill alpha"); err == nil {
		t.Fatal("expected a refused push to fail")
	}
	if repo.git("rev-parse", "main") != head {
		t.Fatal("a refused push still moved the destination branch")
	}
}

// TestSourceAndDestinationSurfaceUnreachableRepositories proves every remote
// entry point reports an unreachable repository instead of degrading silently.
func TestSourceAndDestinationSurfaceUnreachableRepositories(t *testing.T) {
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	ctx := context.Background()
	missing := literalRepository(filepath.Join(t.TempDir(), "absent-repository"))

	source, err := OpenSource(ctx, runner, t.TempDir(), missing)
	if err != nil {
		t.Fatalf("OpenSource: %v", err)
	}
	if _, err := source.DefaultBranch(ctx); err == nil {
		t.Fatal("DefaultBranch succeeded for an unreachable repository")
	}
	if _, err := source.Resolve(ctx, ""); err == nil {
		t.Fatal("Resolve succeeded for an unreachable repository")
	}
	if _, err := source.Resolve(ctx, "main"); err == nil {
		t.Fatal("Resolve succeeded for an unreachable repository")
	}
	if _, err := source.ListDirectories(ctx, "0123456789abcdef0123456789abcdef01234567"); err == nil {
		t.Fatal("ListDirectories succeeded for an unreachable repository")
	}
	if _, _, err := source.PathExists(ctx, "0123456789abcdef0123456789abcdef01234567", "skills"); err == nil {
		t.Fatal("PathExists succeeded for an unreachable repository")
	}
	if _, err := source.ReadTree(ctx, "0123456789abcdef0123456789abcdef01234567", "skills"); err == nil {
		t.Fatal("ReadTree succeeded for an unreachable repository")
	}

	destination, err := OpenDestination(ctx, runner, t.TempDir(), missing)
	if err != nil {
		t.Fatalf("OpenDestination: %v", err)
	}
	if _, err := destination.DefaultBranch(ctx); err == nil {
		t.Fatal("DefaultBranch succeeded for an unreachable destination")
	}
	if _, _, err := destination.Head(ctx, "main"); err == nil {
		t.Fatal("Head succeeded for an unreachable destination")
	}
	if err := destination.FetchCommit(ctx, missing, "0123456789abcdef0123456789abcdef01234567"); err == nil {
		t.Fatal("FetchCommit succeeded for an unreachable destination")
	}
}

// TestDefaultBranchFailsWhenHeadIsNotSymbolic proves a repository whose HEAD
// cannot be resolved to a branch produces actionable guidance rather than an
// invented default.
func TestDefaultBranchFailsWhenHeadIsNotSymbolic(t *testing.T) {
	repo := newTestRepo(t, "main")
	head := repo.commit("only commit")
	repo.git("checkout", "--quiet", "--detach", head)
	// Removing every branch leaves HEAD without a symbolic target to report.
	repo.git("branch", "-D", "main")

	_, source := newTestSource(t, repo)
	if _, err := source.DefaultBranch(context.Background()); err == nil {
		t.Fatal("expected an unresolvable default branch to fail")
	} else if !strings.Contains(err.Error(), "specify an explicit ref") {
		t.Fatalf("error %q does not guide the user", err)
	}
}

// TestPublishRefusesAnUnknownBase proves a base commit the destination does not
// have fails before anything is written.
func TestPublishRefusesAnUnknownBase(t *testing.T) {
	repo := newTestRepo(t, "main")
	repo.write("skills/alpha/SKILL.md", "---\nname: alpha\ndescription: d\n---\nBody\n", 0o644)
	head := repo.commit("add alpha")

	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	ctx := context.Background()
	destination, err := OpenDestination(ctx, runner, t.TempDir(), literalRepository(repo.dir))
	if err != nil {
		t.Fatalf("OpenDestination: %v", err)
	}
	tree, err := skilltree.NewTree([]skilltree.File{{Path: "SKILL.md", Data: []byte("x")}})
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	if _, err := destination.Publish(ctx, "0000000000000000000000000000000000000000", "main",
		[]Update{{Path: "skills/alpha", Tree: tree}}, "Update skill alpha"); err == nil {
		t.Fatal("expected an unknown base to fail")
	}
	if repo.git("rev-parse", "main") != head {
		t.Fatal("a failed publish moved the destination branch")
	}
}
