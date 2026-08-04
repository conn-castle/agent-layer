package skillimports

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestPushAddsASkillThatDoesNotExistAtTheDestinationYet(t *testing.T) {
	hermeticGitEnv(t)
	upstream := newSourceRepo(t, "main")
	upstream.writeSkill("skills/alpha", "alpha", "Alpha")
	upstream.commit("add alpha")

	// The fork's branch predates the skill. That destination-side deletion must
	// remain part of the three-way merge rather than inventing an empty base.
	fork := newSourceRepo(t, "main")
	fork.writeFile("README.md", "fork\n", 0o644)
	fork.commit("fork base")

	p := newProject(t)
	options := addOptions(upstream, "skills/alpha")
	options.Write = config.SkillWriteDirect
	options.PushRepository = fork.path()
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	if err == nil {
		t.Fatalf("expected the destination-side whole-skill deletion to be refused\n%s", out)
	}

	if _, ok := fork.fileAt("main", "skills/alpha/"+SkillManifestName); ok {
		t.Fatalf("a missing destination skill was resurrected:\n%s", out)
	}
	// Unrelated destination content survives.
	if _, ok := fork.fileAt("main", "README.md"); !ok {
		t.Fatal("unrelated destination content was removed")
	}
}

func TestResolveRefFailures(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Ref = "no-such-ref"
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	message := requireError(t, out, err)
	// Naming all three possibilities tells the user which kind of ref they meant.
	requireContains(t, message, "has no branch or tag named")
	requireContains(t, message, "not a full commit id")
}

func TestResolveRefRejectsAnAmbiguousBranchAndTagName(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")
	repo.tag("release")
	runGit(t, repo.path(), "branch", "release")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Ref = "release"
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	message := requireError(t, out, err)
	// Picking one silently would import different content than the user expects.
	requireContains(t, message, "both a branch and a tag")
	// The remedy the error names has to be the remedy that works.
	requireContains(t, message, "refs/heads/release")
	requireContains(t, message, "refs/tags/release")
}

func TestResolveRefImportsTheQualifiedRefNamedByTheAmbiguityError(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha at the tag")
	repo.commit("add alpha")
	repo.tag("release")
	// The branch of the same name moves past the tag, so the two names resolve to
	// different content and picking the wrong one is visible.
	runGit(t, repo.path(), "branch", "release")
	repo.checkout("release")
	repo.writeSkill("skills/alpha", "alpha", "Alpha on the branch")
	branchCommit := repo.commit("revise alpha on the branch")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Ref = "refs/heads/release"
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	entry := p.entry("alpha")
	if entry.SourceCommit != branchCommit {
		t.Fatalf("locked commit = %q, want the branch tip %q", entry.SourceCommit, branchCommit)
	}
	// The namespace decides the ref kind, and a branch tracks while a tag pins.
	if entry.ResolvedRefType != config.SkillRefBranch || entry.Tracking != config.SkillTrackingTracked {
		t.Fatalf("qualified branch recorded as type %q tracking %q", entry.ResolvedRefType, entry.Tracking)
	}
	// The short name is the ref identity however the ref was spelled.
	if entry.ResolvedRefName != "release" {
		t.Fatalf("resolved ref name = %q, want release", entry.ResolvedRefName)
	}
	body, _ := p.readSkillFile("alpha", SkillManifestName)
	requireContains(t, body, "Alpha on the branch")
}

func TestResolveRefQualifiedTagPinsAndUnknownNamespaceFails(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha at the tag")
	tagCommit := repo.commit("add alpha")
	repo.tag("release")
	runGit(t, repo.path(), "branch", "release")
	repo.checkout("release")
	repo.writeSkill("skills/alpha", "alpha", "Alpha on the branch")
	repo.commit("revise alpha on the branch")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Ref = "refs/tags/release"
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	entry := p.entry("alpha")
	if entry.SourceCommit != tagCommit {
		t.Fatalf("locked commit = %q, want the tagged commit %q", entry.SourceCommit, tagCommit)
	}
	if entry.ResolvedRefType != config.SkillRefTag || entry.Tracking != config.SkillTrackingPinned {
		t.Fatalf("qualified tag recorded as type %q tracking %q", entry.ResolvedRefType, entry.Tracking)
	}

	// A namespace Agent Layer does not import must say so instead of reporting a
	// missing branch or tag the user never named.
	other := newProject(t)
	options = addOptions(repo, "skills/alpha")
	options.Ref = "refs/remotes/origin/release"
	out, err = other.run(func(s *Service) error { return s.Add(ctx(t), options) })
	message := requireError(t, out, err)
	requireContains(t, message, "ref namespace Agent Layer does not import")
}

func TestAddRejectsAnAbbreviatedCommitRef(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	head := repo.commit("add alpha")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Ref = head[:8]
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	message := requireError(t, out, err)
	// Expanding an abbreviation would silently record a ref the user never wrote.
	requireContains(t, message, "not a full commit id")
}

func TestPullFetchesAMergeBaseThatIsNoLongerAReachableTip(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeFile("skills/alpha/references/notes.md", "base\n", 0o644)
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	// The user edits locally; upstream advances twice so the locked commit is
	// buried in history rather than being the tip.
	p.writeSkillFile("alpha", "references/local.md", "local\n")
	repo.writeFile("skills/alpha/references/notes.md", "second\n", 0o644)
	repo.commit("second")
	repo.writeFile("skills/alpha/references/notes.md", "third\n", 0o644)
	third := repo.commit("third")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	requireNoError(t, out, err)

	notes, _ := p.readSkillFile("alpha", "references/notes.md")
	if notes != "third\n" {
		t.Fatalf("upstream change was not applied: %q", notes)
	}
	if local, _ := p.readSkillFile("alpha", "references/local.md"); local != "local\n" {
		t.Fatalf("local edit was lost: %q", local)
	}
	if got := p.entry("alpha").SourceCommit; got != third {
		t.Fatalf("lock = %q, want %q", got, third)
	}
}

func TestPushRejectsOverlappingDestinationPaths(t *testing.T) {
	hermeticGitEnv(t)
	// Two source repositories contribute to one destination at nested paths, so
	// the group would have two owners for the same files.
	first := newSourceRepo(t, "main")
	first.writeSkill("skills/alpha", "alpha", "Alpha")
	first.commit("add alpha")

	second := newSourceRepo(t, "main")
	second.writeSkill("skills/alpha/inner", "inner", "Inner")
	second.commit("add inner")

	destination := newSourceRepo(t, "main")
	destination.writeFile("README.md", "dest\n", 0o644)
	destination.commit("base")

	p := newProject(t)
	firstOptions := addOptions(first, "skills/alpha")
	firstOptions.Write = config.SkillWriteDirect
	firstOptions.PushRepository = destination.path()
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), firstOptions) })
	requireNoError(t, out, err)

	secondOptions := addOptions(second, "skills/alpha/inner")
	secondOptions.Write = config.SkillWriteDirect
	secondOptions.PushRepository = destination.path()
	out, err = p.run(func(s *Service) error { return s.Add(ctx(t), secondOptions) })
	requireNoError(t, out, err)

	p.writeSkillFile("alpha", "notes.md", "a\n")
	p.writeSkillFile("inner", "notes.md", "b\n")

	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "skill import failed")
	requireContains(t, out, "overlap")
	if _, ok := destination.fileAt("main", "skills/alpha/notes.md"); ok {
		t.Fatal("an overlapping group must be rejected before any remote write")
	}
}

func TestStatusReportsAnInvalidImportedTree(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	// The user deletes the manifest but leaves the directory: the skill can no
	// longer be projected, so status must not call it merely "modified".
	if err := os.Remove(filepath.Join(p.skillDir("alpha"), SkillManifestName)); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}

	view, err := p.service(nil).Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := view.Totals().Invalid; got != 1 {
		t.Fatalf("invalid = %d, want 1", got)
	}
	if !view.Failed() {
		t.Fatal("an unprojectable skill must make status fail")
	}
	requireContains(t, StatusError(view).Error(), "invalid")
}

func TestRenderSelectorsArraySplitsLongLists(t *testing.T) {
	t.Parallel()
	short := renderSelectorsArray("", []string{"skills/a", "skills/b"}, "")
	if len(short) != 1 {
		t.Fatalf("a short list must stay on one line, got %v", short)
	}

	long := make([]string, 0, 8)
	for i := 0; i < 8; i++ {
		long = append(long, "skills/a-fairly-long-selector-name-"+strings.Repeat("x", 5)+string(rune('a'+i)))
	}
	lines := renderSelectorsArray("  ", long, "# note")
	if len(lines) != len(long)+2 {
		t.Fatalf("a long list must be split one element per line, got %d lines", len(lines))
	}
	if !strings.HasPrefix(lines[0], "  selectors = [") {
		t.Fatalf("indentation was lost: %q", lines[0])
	}
	// A trailing comment on the assignment is carried to the closing bracket
	// rather than dropped.
	if !strings.HasSuffix(lines[len(lines)-1], "# note") {
		t.Fatalf("the inline comment was dropped: %q", lines[len(lines)-1])
	}
}

func TestEditSelectorsRejectsANonArrayValue(t *testing.T) {
	t.Parallel()
	document := "[[skills.imports]]\nrepository = \"r\"\nselectors = \"skills/a\"\n"
	if _, err := AddSelectorToBlock(document, 0, "skills/b"); err == nil {
		t.Fatal("a selectors value that is not an array must fail rather than be rewritten blindly")
	}
	missing := "[[skills.imports]]\nrepository = \"r\"\n"
	if _, err := AddSelectorToBlock(missing, 0, "skills/b"); err == nil {
		t.Fatal("a block with no selectors key must fail")
	}
	if _, err := AddSelectorToBlock(document, 5, "skills/b"); err == nil {
		t.Fatal("an out-of-range block index must fail")
	}
	if _, err := RemoveImportBlock(document, 5); err == nil {
		t.Fatal("removing an out-of-range block must fail")
	}
}

func TestAddSelectorToBlockRejectsADuplicate(t *testing.T) {
	t.Parallel()
	document := "[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\n"
	if _, err := AddSelectorToBlock(document, 0, "skills/a"); err == nil {
		t.Fatal("adding a selector twice must fail rather than duplicate it")
	}
}

func TestGitErrorRendersWithAndWithoutStderr(t *testing.T) {
	t.Parallel()
	cause := errors.New("exit status 1")
	quiet := &GitError{Args: []string{"fetch"}, Err: cause}
	if got := quiet.Error(); got != "git fetch: exit status 1" {
		t.Fatalf("error = %q", got)
	}
	loud := &GitError{Args: []string{"fetch"}, Stderr: "  fatal: nope  ", Err: cause}
	requireContains(t, loud.Error(), "fatal: nope")
	if !errors.Is(loud, cause) {
		t.Fatal("the underlying exec error must stay unwrappable")
	}
}

func TestTreeHandlesNilReceiver(t *testing.T) {
	t.Parallel()
	var empty *Tree
	if empty.Len() != 0 || empty.Paths() != nil || empty.Files() != nil {
		t.Fatal("a nil tree must behave as an empty one")
	}
	if _, ok := empty.Lookup("anything"); ok {
		t.Fatal("a nil tree contains nothing")
	}
	if empty.HasManifest() {
		t.Fatal("a nil tree has no manifest")
	}
}

func TestNewTreeRejectsDuplicatePaths(t *testing.T) {
	t.Parallel()
	if _, err := NewTree([]File{file("a.md", "one"), file("a.md", "two")}); err == nil {
		t.Fatal("one path cannot have two contents")
	}
}

func TestSanitizeLabelProducesUsableDirectoryNames(t *testing.T) {
	t.Parallel()
	if got := sanitizeLabel("push-feature/branch name"); got != "push-feature-branch-name" {
		t.Fatalf("sanitized = %q", got)
	}
	if got := sanitizeLabel("///"); got != "workspace" {
		t.Fatalf("a label with nothing usable must fall back to a fixed name, got %q", got)
	}
}

func TestShortCommitLeavesShortValuesAlone(t *testing.T) {
	t.Parallel()
	if got := shortCommit("abc"); got != "abc" {
		t.Fatalf("short = %q", got)
	}
	if got := shortCommit(strings.Repeat("a", 40)); len(got) != 12 {
		t.Fatalf("long commit = %q", got)
	}
}

func TestFormatConflictsListsEveryPath(t *testing.T) {
	t.Parallel()
	text := FormatConflicts([]MergeConflict{
		{Path: "a.md", Reason: "reason one"},
		{Path: "b.md", Reason: "reason two"},
	})
	requireContains(t, text, "a.md: reason one")
	requireContains(t, text, "b.md: reason two")
	if strings.Count(text, "\n") != 1 {
		t.Fatalf("conflicts must be one per line: %q", text)
	}
}

func TestStatusRecoversBeforeReportingAndSurfacesTheRecovery(t *testing.T) {
	hermeticGitEnv(t)
	p := newProject(t)
	view, err := p.service(nil).Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if view.Recovered {
		t.Fatal("a project with no interrupted publish must not report a recovery")
	}
	if view.Failed() {
		t.Fatal("a project with no imports is an ordinary successful state")
	}
}

func TestPushSkipsWriteNoneWhileStillWritingOtherBlocks(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.commit("add skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)
	out, err = p.run(func(s *Service) error { return s.Add(ctx(t), directWriteOptions(repo, "skills/beta")) })
	requireNoError(t, out, err)

	p.writeSkillFile("alpha", "notes.md", "not pushed\n")
	p.writeSkillFile("beta", "notes.md", "pushed\n")

	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	requireNoError(t, out, err)
	requireContains(t, out, "skipped")

	if _, ok := repo.fileAt("main", "skills/alpha/notes.md"); ok {
		t.Fatal("a write = none block must not be pushed")
	}
	if _, ok := repo.fileAt("main", "skills/beta/notes.md"); !ok {
		t.Fatalf("the write-enabled block was not pushed:\n%s", out)
	}
}

func TestPushRefusesAnImportedDirectoryThatIsNotADirectory(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), directWriteOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	if err := os.RemoveAll(p.skillDir("alpha")); err != nil {
		t.Fatalf("remove skill: %v", err)
	}
	if err := os.WriteFile(p.skillDir("alpha"), []byte("not a skill"), 0o600); err != nil {
		t.Fatalf("write file in place of the skill: %v", err)
	}

	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "skill import failed")
	requireContains(t, out, "not a directory")
}

func TestApplyPlanIsANoOpWhenNothingChanged(t *testing.T) {
	t.Parallel()
	fixture := newTransactionFixture(t)
	state := &projectState{
		configPath: fixture.configPath,
		configText: "original config\n",
		lockPath:   fixture.lockPath,
		lock:       &config.SkillImportLock{Version: config.SkillImportLockVersion},
	}
	service := &Service{Root: fixture.root}
	plan := newReconcilePlan()

	if err := service.applyPlan(state, plan, "original config\n"); err != nil {
		t.Fatalf("apply empty plan: %v", err)
	}
	// An empty transaction must leave no journal and no staging directory, so the
	// next command has nothing to recover.
	if _, err := os.Stat(JournalPath(fixture.root)); !os.IsNotExist(err) {
		t.Fatal("an empty plan must not write a journal")
	}
	if got := fixture.read(t, fixture.configPath); got != "original config\n" {
		t.Fatalf("config = %q", got)
	}
}

func TestReconcilePlanChangeDetection(t *testing.T) {
	t.Parallel()
	entry := config.SkillImportLockEntry{Repository: "r", SourcePath: "skills/a", SkillName: "a"}
	plan := newReconcilePlan()
	plan.entries = []config.SkillImportLockEntry{entry}

	if plan.changed([]config.SkillImportLockEntry{entry}) {
		t.Fatal("an identical entry set is not a change")
	}
	if !plan.changed(nil) {
		t.Fatal("adding an entry is a change")
	}

	other := entry
	other.SourceCommit = "moved"
	if !plan.changed([]config.SkillImportLockEntry{other}) {
		t.Fatal("an advanced commit is a change")
	}

	withTree := newReconcilePlan()
	withTree.staged["a"] = tree(t, file(SkillManifestName, "x"))
	if !withTree.changed(nil) {
		t.Fatal("a staged tree is a change even when the lock is unchanged")
	}
}

func TestPublishIsSerializedAgainstProjectionAndRefusesAConcurrentEdit(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	var out bytes.Buffer
	held := 0
	service := &Service{
		Root:    p.root,
		Runner:  ExecGitRunner{},
		Project: func(string) error { return nil },
		Out:     &out,
		WithProjectLock: func(root string, fn func() error) error {
			held++
			// A concurrent command edits the configuration while this one was
			// resolving its remote. Publishing the stale plan would silently discard
			// that edit.
			if held == 2 {
				p.writeConfig(minimalConfig + "\n# edited by another command\n")
			}
			return fn()
		},
	}
	err := service.Add(ctx(t), addOptions(repo, "skills/alpha"))
	if err == nil {
		t.Fatalf("a concurrent configuration change must abort the publish\n%s", out.String())
	}
	if held == 0 {
		t.Fatal("the publish must run under the project lock so projection cannot read a half-published state")
	}
	requireContains(t, err.Error(), "changed while this command was running")
	// Nothing was published, so the concurrent edit survives intact.
	requireContains(t, p.configText(), "# edited by another command")
	requireNotContains(t, p.configText(), "skills.imports")
}

func TestPublishRefusesConcurrentImportedTreeEdit(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")
	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)
	repo.writeSkill("skills/alpha", "alpha", "Alpha upstream")
	repo.commit("update alpha")

	locks := 0
	service := &Service{Root: p.root, Runner: ExecGitRunner{}, Project: func(string) error { return nil }, Out: &bytes.Buffer{}, WithProjectLock: func(_ string, fn func() error) error {
		locks++
		if locks == 2 {
			p.writeSkillFile("alpha", "notes.md", "concurrent edit\n")
		}
		return fn()
	}}
	if err := service.Pull(ctx(t)); err == nil || !strings.Contains(err.Error(), "local skill sources changed") {
		t.Fatalf("concurrent imported-tree edit did not abort publish: %v", err)
	}
	if got, ok := p.readSkillFile("alpha", "notes.md"); !ok || got != "concurrent edit\n" {
		t.Fatalf("concurrent edit was not preserved: %q", got)
	}
}

func TestPublishHoldsTheProjectLockForEveryMutatingOperation(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	held := 0
	newService := func() *Service {
		return &Service{
			Root:            p.root,
			Runner:          ExecGitRunner{},
			Project:         func(string) error { return nil },
			Out:             &bytes.Buffer{},
			WithProjectLock: func(_ string, fn func() error) error { held++; return fn() },
		}
	}

	if err := newService().Add(ctx(t), addOptions(repo, "skills/alpha")); err != nil {
		t.Fatalf("add: %v", err)
	}
	if held != 2 {
		t.Fatalf("add held the lock %d times, want 2 (snapshot and publish)", held)
	}

	repo.writeSkill("skills/alpha", "alpha", "Alpha revised")
	repo.commit("revise")
	if err := newService().Pull(ctx(t)); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if held != 4 {
		t.Fatalf("pull held the lock %d times in total, want 4", held)
	}

	if err := newService().Remove(ctx(t), repo.path(), "skills/alpha"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if held != 6 {
		t.Fatalf("remove held the lock %d times in total, want 6", held)
	}
}
