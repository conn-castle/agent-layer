package organizescratch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/gitenv"
)

// runOrganize executes a run against opts and returns everything it wrote.
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
	data, err := os.ReadFile(filepath.Join(root, reviewDocName)) // #nosec G304 -- test-owned temporary root.
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
	command := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...) // #nosec G204 -- fixed git arguments below a test-owned root.
	// Strip inherited discovery variables for the same reason the command does:
	// under a git hook or pre-commit, GIT_DIR wins over -C and these fixtures
	// would operate on this repository instead of their own temporary one.
	command.Env = append(gitenv.WithoutDiscovery(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// newRepoWithScratch builds a git repository with one commit and returns the
// repository root plus a gitignored scratch directory inside it.
func newRepoWithScratch(t *testing.T) (repo, scratch string) {
	t.Helper()
	repo = t.TempDir()
	git(t, repo, "init", "--initial-branch=main")
	writeFileAt(t, filepath.Join(repo, "main.go"), "package main\n")
	writeFileAt(t, filepath.Join(repo, ".gitignore"), "scratch/\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "initial")
	return repo, mkdirAt(t, filepath.Join(repo, "scratch"))
}

func TestRunRejectsUnusableOptions(t *testing.T) {
	// Every one of these would otherwise be resolved by guessing, and this tool
	// moves real files: guessing is not acceptable.
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
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestRunReportsWhenThereIsNothingToDo(t *testing.T) {
	root := t.TempDir()
	mkdirAt(t, filepath.Join(root, "reports"))
	mkdirAt(t, filepath.Join(root, "artifacts"))
	mkdirAt(t, filepath.Join(root, "review"))
	writeFileAt(t, filepath.Join(root, reviewDocName), "old")
	mkdirAt(t, filepath.Join(root, "venv"))

	stdout, _, err := runOrganize(t, Options{Root: root, Apply: true, Keep: []string{"venv", ""}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout, "nothing to organize") {
		t.Fatalf("stdout = %q, want a nothing-to-organize report", stdout)
	}
	// The kept entry must still be where the tool that manages it expects it.
	requireFile(t, filepath.Join(root, "venv"))
	if readReviewDoc(t, root) != "old" {
		t.Fatal("an empty run must not rewrite the existing review list")
	}
}

func TestRunDryRunMovesNothing(t *testing.T) {
	// The dry run is the safety rail: it must describe the plan and leave every
	// entry exactly where it was.
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "notes.md"), "x")
	writeFileAt(t, filepath.Join(root, "run.log"), "x")

	stdout, _, err := runOrganize(t, Options{Root: root})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout, "2 entries — 2 to move, 0 left in place") {
		t.Fatalf("stdout = %q, want the entry summary", stdout)
	}
	if !strings.Contains(stdout, "DRY RUN — nothing moved. Re-run with --apply.") {
		t.Fatalf("stdout = %q, want the dry-run notice", stdout)
	}
	// A dry run still writes the review list, so it must say where it landed.
	if !strings.Contains(stdout, "review list: "+filepath.Join(root, reviewDocName)) {
		t.Fatalf("stdout = %q, want the review list path", stdout)
	}
	requireFile(t, filepath.Join(root, "notes.md"))
	requireFile(t, filepath.Join(root, "run.log"))
	requireNoFile(t, filepath.Join(root, "reports"))
	requireNoFile(t, filepath.Join(root, "artifacts"))
	if doc := readReviewDoc(t, root); !strings.Contains(doc, "DRY RUN — nothing was moved yet.") {
		t.Fatalf("review doc = %q, want it to state that nothing moved", doc)
	}
}

func TestRunApplyMovesEntriesToTheirDestinations(t *testing.T) {
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "ship-pr-19.md"), "x")
	writeFileAt(t, filepath.Join(root, "build.log"), "x")
	writeFileAt(t, filepath.Join(root, "logo.svg"), "<svg/>")

	stdout, stderr, err := runOrganize(t, Options{Root: root, Apply: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout, "moved 3/3") {
		t.Fatalf("stdout = %q, want moved 3/3", stdout)
	}
	// The temp root sits outside any repository, so the only warning expected is
	// the one announcing that git-backed detection is off.
	if strings.Count(stderr, "WARNING") != 1 || !strings.Contains(stderr, "not a git repository") {
		t.Fatalf("stderr = %q, want only the not-a-git-repository warning", stderr)
	}
	requireFile(t, filepath.Join(root, reportsAdhocPrefix, "pr", "ship-pr-19.md"))
	requireFile(t, filepath.Join(root, destArtifactsLogs, "build.log"))
	requireFile(t, filepath.Join(root, destReviewUniqueAssets, "logo.svg"))
	requireNoFile(t, filepath.Join(root, "build.log"))

	doc := readReviewDoc(t, root)
	if !strings.Contains(doc, "Entries have been moved.") {
		t.Fatalf("review doc = %q, want it to state entries moved", doc)
	}
	// Only review/ entries need a human; routed-by-rule folders must not appear.
	if !strings.Contains(doc, "`"+destReviewUniqueAssets+"` (1)") {
		t.Fatalf("review doc = %q, want the unique-assets section", doc)
	}
	if strings.Contains(doc, destArtifactsLogs) {
		t.Fatalf("review doc = %q, want no unambiguously routed folders", doc)
	}
	if !strings.Contains(doc, reviewGuides[destReviewUniqueAssets]) {
		t.Fatalf("review doc = %q, want the guidance for that folder", doc)
	}
}

func TestRunNeverOverwritesAnOccupiedDestination(t *testing.T) {
	// Refusing to overwrite is the tool's central promise: a same-named file from
	// an earlier run must survive untouched.
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "build.log"), "new")
	writeFileAt(t, filepath.Join(root, destArtifactsLogs, "build.log"), "earlier")

	stdout, stderr, err := runOrganize(t, Options{Root: root, Apply: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout, "moved 0/1") {
		t.Fatalf("stdout = %q, want moved 0/1", stdout)
	}
	if !strings.Contains(stderr, "COLLISION, left in place: build.log") {
		t.Fatalf("stderr = %q, want a reported collision", stderr)
	}
	existing, err := os.ReadFile(filepath.Join(root, destArtifactsLogs, "build.log")) // #nosec G304 -- test-owned temporary root.
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(existing) != "earlier" {
		t.Fatalf("destination content = %q, want the earlier file preserved", existing)
	}
	requireFile(t, filepath.Join(root, "build.log"))
}

func TestRunGroupsRecurringReportPrefixes(t *testing.T) {
	// A prefix earns its own folder only when it recurs AND produced markdown, so
	// a repeated data prefix cannot masquerade as a skill's report family.
	root := t.TempDir()
	for i := 0; i < DefaultMinGroup; i++ {
		writeFileAt(t, filepath.Join(root, fmt.Sprintf("audit-tests.run%d.md", i)), "x")
		writeFileAt(t, filepath.Join(root, fmt.Sprintf("telemetry.chunk%d.json", i)), "{}")
	}

	if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireFile(t, filepath.Join(root, reportsPrefix+"audit-tests", "audit-tests.run0.md"))
	requireFile(t, filepath.Join(root, destArtifactsData, "telemetry.chunk0.json"))
	requireNoFile(t, filepath.Join(root, reportsPrefix+"telemetry"))
}

func TestRunHonoursMinGroup(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 2; i++ {
		writeFileAt(t, filepath.Join(root, fmt.Sprintf("audit-tests.run%d.md", i)), "x")
	}

	if _, _, err := runOrganize(t, Options{Root: root, Apply: true, MinGroup: 2}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireFile(t, filepath.Join(root, reportsPrefix+"audit-tests", "audit-tests.run0.md"))
}

func TestRunAnnouncesDisabledChecksOutsideAGitRepository(t *testing.T) {
	// Copy and asset detection depend on git. Losing them silently would make the
	// output look authoritative when it is not.
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "notes.md"), "x")

	_, stderr, err := runOrganize(t, Options{Root: root})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr, "not a git repository") {
		t.Fatalf("stderr = %q, want a warning that git-backed checks are off", stderr)
	}
}

func TestRunLeavesRegisteredWorktreesInPlaceByDefault(t *testing.T) {
	// Relocating a registered worktree edits real git state, so it takes an
	// explicit opt-in — and the reason must say so.
	repo, scratch := newRepoWithScratch(t)
	git(t, repo, "worktree", "add", filepath.Join("scratch", "wt-feature"), "-b", "feature")

	stdout, _, err := runOrganize(t, Options{Root: scratch, Apply: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stdout, "LEFT IN PLACE  wt-feature") {
		t.Fatalf("stdout = %q, want the worktree left in place", stdout)
	}
	if !strings.Contains(stdout, "pass --move-worktrees to relocate") {
		t.Fatalf("stdout = %q, want the opt-in hint", stdout)
	}
	requireFile(t, filepath.Join(scratch, "wt-feature", ".git"))
	if doc := readReviewDoc(t, scratch); !strings.Contains(doc, "## Left in place") {
		t.Fatalf("review doc = %q, want a left-in-place section", doc)
	}
}

func TestRunMoveWorktreesRelocatesAndRepairsRegistration(t *testing.T) {
	repo, scratch := newRepoWithScratch(t)
	git(t, repo, "worktree", "add", filepath.Join("scratch", "wt-feature"), "-b", "feature")

	if _, stderr, err := runOrganize(t, Options{Root: scratch, Apply: true, MoveWorktrees: true}); err != nil {
		t.Fatalf("Run: %v (stderr %q)", err, stderr)
	}
	moved := filepath.Join(scratch, destReviewCheckouts, "wt-feature")
	requireFile(t, filepath.Join(moved, ".git"))

	// The registration must follow the checkout: an unrepaired move leaves git
	// pointing at a path that no longer exists.
	list := git(t, repo, "worktree", "list")
	if !strings.Contains(list, canonicalPath(moved)) {
		t.Fatalf("worktree list = %q, want the relocated path %q", list, canonicalPath(moved))
	}
	if strings.Contains(list, "prunable") {
		t.Fatalf("worktree list = %q, want no stale registration", list)
	}
	// The relocated checkout must still be a working checkout.
	git(t, moved, "status", "--porcelain")
}

func TestRunMoveWorktreesRepairsEachNestedCheckout(t *testing.T) {
	// Repairing only the moved parent leaves every nested worktree registered at
	// its old location, where git reports it as prunable.
	repo, scratch := newRepoWithScratch(t)
	mkdirAt(t, filepath.Join(scratch, "worktrees"))
	git(t, repo, "worktree", "add", filepath.Join("scratch", "worktrees", "alpha"), "-b", "alpha")
	git(t, repo, "worktree", "add", filepath.Join("scratch", "worktrees", "beta"), "-b", "beta")

	stdout, stderr, err := runOrganize(t, Options{Root: scratch, Apply: true, MoveWorktrees: true})
	if err != nil {
		t.Fatalf("Run: %v (stderr %q)", err, stderr)
	}
	if !strings.Contains(stdout, "moved 1/1") {
		t.Fatalf("stdout = %q, want the parent moved as one entry", stdout)
	}
	list := git(t, repo, "worktree", "list")
	for _, branch := range []string{"alpha", "beta"} {
		want := canonicalPath(filepath.Join(scratch, destReviewCheckouts, "worktrees", branch))
		if !strings.Contains(list, want) {
			t.Fatalf("worktree list = %q, want %s repaired to %q", list, branch, want)
		}
	}
	if strings.Contains(list, "prunable") {
		t.Fatalf("worktree list = %q, want no stale registrations", list)
	}
}

func TestRunTreatsATrackedFileCopyAsRegenerable(t *testing.T) {
	// End to end, this is what makes git-backed detection worth having: a copy of
	// the repo is reproducible and does not need a per-file decision.
	repo, scratch := newRepoWithScratch(t)
	copyDir := mkdirAt(t, filepath.Join(scratch, "repo-copy"))
	for i := 0; i < repoCopyMinFiles; i++ {
		name := fmt.Sprintf("tracked%d.go", i)
		writeFileAt(t, filepath.Join(repo, name), "package main\n")
		writeFileAt(t, filepath.Join(copyDir, name), "package main\n")
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "more sources")

	if _, _, err := runOrganize(t, Options{Root: scratch, Apply: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireFile(t, filepath.Join(scratch, destReviewRegenerable, "repo-copy"))
}

func TestPrintPlanOrdersDestinationsByVolume(t *testing.T) {
	// The summary exists to show where the bulk went, so the biggest destination
	// has to come first.
	movable := []placement{
		{entry: entry{name: "a.log"}, dest: destArtifactsLogs},
		{entry: entry{name: "b.json"}, dest: destArtifactsData},
		{entry: entry{name: "c.log"}, dest: destArtifactsLogs},
	}
	var out bytes.Buffer
	if err := printPlan(&out, 3, movable, nil); err != nil {
		t.Fatalf("printPlan: %v", err)
	}
	logsAt := strings.Index(out.String(), destArtifactsLogs)
	dataAt := strings.Index(out.String(), destArtifactsData)
	if logsAt < 0 || dataAt < 0 || logsAt > dataAt {
		t.Fatalf("output = %q, want %s listed before %s", out.String(), destArtifactsLogs, destArtifactsData)
	}
}

func TestWriteReviewDocStatesWhenNoJudgementIsNeeded(t *testing.T) {
	// Silence would read as "the section is missing"; an explicit statement tells
	// the reader they are done.
	root := t.TempDir()
	path := filepath.Join(root, reviewDocName)
	plan := []placement{{entry: entry{name: "a.log"}, dest: destArtifactsLogs, reason: "log extension"}}
	if err := writeReviewDoc(path, plan, nil, true); err != nil {
		t.Fatalf("writeReviewDoc: %v", err)
	}
	if doc := readReviewDoc(t, root); !strings.Contains(doc, "Nothing required a judgement call.") {
		t.Fatalf("review doc = %q, want an explicit all-clear", doc)
	}
}

func TestRunPropagatesWriteFailures(t *testing.T) {
	// A closed stdout must surface, not be swallowed into a silent success.
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "notes.md"), "x")
	if err := Run(t.Context(), Options{Root: root, MinGroup: DefaultMinGroup}, failingWriter{}, &bytes.Buffer{}); err == nil {
		t.Fatal("Run must report a stdout write failure")
	}
}

func TestGitOutputReportsFailures(t *testing.T) {
	if _, err := gitOutput(context.Background(), t.TempDir(), "rev-parse", "--show-toplevel"); err == nil {
		t.Fatal("gitOutput must return an error outside a repository")
	}
}

func TestDisplayPathPrefersTheRelativeForm(t *testing.T) {
	root := t.TempDir()
	if got := displayPath(root, filepath.Join(root, "a", "b")); got != filepath.Join("a", "b") {
		t.Fatalf("displayPath = %q, want a/b", got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestRunDoesNotGroupDotfilesUnderAnEmptyPrefix(t *testing.T) {
	// Every dotfile has an empty prefix, so counting them would invent a report
	// folder named after nothing and sweep unrelated files into it.
	root := t.TempDir()
	for i := 0; i < DefaultMinGroup; i++ {
		writeFileAt(t, filepath.Join(root, fmt.Sprintf(".note%d.md", i)), "x")
	}

	if _, _, err := runOrganize(t, Options{Root: root, Apply: true}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireNoFile(t, filepath.Join(root, "reports", ".note0.md"))
	requireFile(t, filepath.Join(root, reportsAdhocPrefix, adhocFallbackFolder, ".note0.md"))
}

func TestRunAnnouncesAnEmptyTrackedFileSet(t *testing.T) {
	// `git ls-files` returning nothing disables copy detection just as surely as
	// having no repository at all, and the reader has to be told.
	repo := t.TempDir()
	git(t, repo, "init", "--initial-branch=main")
	scratch := mkdirAt(t, filepath.Join(repo, "scratch"))
	writeFileAt(t, filepath.Join(scratch, "notes.md"), "x")

	_, stderr, err := runOrganize(t, Options{Root: scratch})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr, "no tracked files") {
		t.Fatalf("stderr = %q, want a warning that copy detection is disabled", stderr)
	}
}

func TestRunTreatsADanglingSymlinkAsACollision(t *testing.T) {
	// A rename over a symlink destroys the symlink. Deciding occupancy with Stat
	// instead of Lstat would call a dangling link "absent" and overwrite it, which
	// is precisely what this tool promises never to do.
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "build.log"), "new")
	link := filepath.Join(root, destArtifactsLogs, "build.log")
	mkdirAt(t, filepath.Dir(link))
	if err := os.Symlink(filepath.Join(root, "gone"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	_, stderr, err := runOrganize(t, Options{Root: root, Apply: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr, "COLLISION, left in place: build.log") {
		t.Fatalf("stderr = %q, want the dangling symlink reported as a collision", stderr)
	}
	target, err := os.Readlink(link)
	if err != nil || target != filepath.Join(root, "gone") {
		t.Fatalf("readlink = %q, %v; want the symlink intact", target, err)
	}
	requireFile(t, filepath.Join(root, "build.log"))
}

func TestRunDoesNotRepairAWorktreeThatCouldNotMove(t *testing.T) {
	// A worktree blocked by a collision is still at its original path with a
	// correct registration. Repairing it anyway would point git at whatever
	// occupies the destination and orphan the live checkout.
	repo, scratch := newRepoWithScratch(t)
	git(t, repo, "worktree", "add", filepath.Join("scratch", "wt-feature"), "-b", "feature")
	occupied := filepath.Join(scratch, destReviewCheckouts, "wt-feature")
	writeFileAt(t, filepath.Join(occupied, "stale-copy.txt"), "not the real checkout")

	_, stderr, err := runOrganize(t, Options{Root: scratch, Apply: true, MoveWorktrees: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(stderr, "COLLISION, left in place: wt-feature") {
		t.Fatalf("stderr = %q, want the collision reported", stderr)
	}
	if strings.Contains(stderr, "worktree repair failed") {
		t.Fatalf("stderr = %q, want no repair attempted for an entry that never moved", stderr)
	}
	list := git(t, repo, "worktree", "list")
	if !strings.Contains(list, canonicalPath(filepath.Join(scratch, "wt-feature"))) {
		t.Fatalf("worktree list = %q, want the registration still at the original path", list)
	}
	if strings.Contains(list, "prunable") {
		t.Fatalf("worktree list = %q, want no stale registration", list)
	}
}

func TestRunWritesTheReviewListEvenWhenStdoutBreaks(t *testing.T) {
	// `al organize-scratch --apply | head` closes stdout mid-run. Entries have
	// already moved by then, so the review list — the only record of where they
	// went and what still needs a decision — must survive the write failure.
	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "notes.md"), "x")
	writeFileAt(t, filepath.Join(root, "logo.svg"), "<svg/>")

	err := Run(t.Context(), Options{Root: root, Apply: true, MinGroup: DefaultMinGroup},
		pipeClosedWriter{marker: "moved "}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("Run must report the stdout failure rather than exit clean")
	}
	requireFile(t, filepath.Join(root, destReviewUniqueAssets, "logo.svg"))
	if doc := readReviewDoc(t, root); !strings.Contains(doc, "logo.svg") {
		t.Fatalf("review doc = %q, want the moved entry recorded", doc)
	}
}

// pipeClosedWriter fails on the write containing marker, the way a pipe whose
// reader has exited fails partway through a command's output.
type pipeClosedWriter struct{ marker string }

func (w pipeClosedWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.marker) {
		return 0, os.ErrClosed
	}
	return len(p), nil
}

func TestRunResolvesGitFromTheRootNotAnInheritedGitDir(t *testing.T) {
	// Git hooks, pre-commit, and `git rebase --exec` all export GIT_DIR and
	// GIT_WORK_TREE, and those take precedence over `git -C <dir>`. Honouring them
	// would resolve a different repository than the scratch root: the wrong
	// tracked-file set for copy detection, the wrong worktree registrations to
	// protect, and `git worktree repair` writes aimed at someone else's checkout.
	decoy, decoyScratch := newRepoWithScratch(t)
	git(t, decoy, "worktree", "add", filepath.Join("scratch", "decoy-wt"), "-b", "decoy")

	root := t.TempDir()
	writeFileAt(t, filepath.Join(root, "notes.md"), "x")
	t.Setenv("GIT_DIR", filepath.Join(decoy, ".git"))
	t.Setenv("GIT_WORK_TREE", decoy)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(decoy, ".git", "index"))

	_, stderr, err := runOrganize(t, Options{Root: root, Apply: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// root is outside any repository, so the decoy must not stand in for it.
	if !strings.Contains(stderr, "not a git repository") {
		t.Fatalf("stderr = %q, want the ambient repository ignored and git checks reported off", stderr)
	}
	requireFile(t, filepath.Join(root, reportsAdhocPrefix, adhocFallbackFolder, "notes.md"))
	// The decoy is untouched: nothing was organized in it and its worktree stands.
	requireFile(t, filepath.Join(decoyScratch, "decoy-wt", ".git"))
	requireNoFile(t, filepath.Join(decoyScratch, reviewDocName))
}
