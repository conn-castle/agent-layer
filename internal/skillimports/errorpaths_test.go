package skillimports

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestNewBuildsAServiceWithTheRealGitRunner(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	projected := 0
	service := New("/tmp/example", func(string) error { projected++; return nil }, &out)
	if service.Root != "/tmp/example" {
		t.Fatalf("root = %q", service.Root)
	}
	if _, ok := service.Runner.(ExecGitRunner); !ok {
		t.Fatalf("runner = %T, want the real git runner", service.Runner)
	}
	if err := service.Project(service.Root); err != nil || projected != 1 {
		t.Fatalf("projector was not wired: err=%v calls=%d", err, projected)
	}
}

func TestOperationsFailLoudlyOnAnUnreadableConfig(t *testing.T) {
	hermeticGitEnv(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	service := &Service{Root: root, Runner: ExecGitRunner{}, Out: &bytes.Buffer{}}

	// Every operation reads the same snapshot, so none of them may quietly treat a
	// missing configuration as "no imports configured".
	if err := service.Pull(ctx(t)); err == nil {
		t.Fatal("pull with no config must fail")
	}
	if err := service.Push(ctx(t)); err == nil {
		t.Fatal("push with no config must fail")
	}
	if err := service.Add(ctx(t), AddOptions{Repository: "r", Selectors: []string{"skills/a"}}); err == nil {
		t.Fatal("add with no config must fail")
	}
	if err := service.Remove(ctx(t), "r", "skills/a"); err == nil {
		t.Fatal("remove with no config must fail")
	}
	if _, err := service.Status(); err == nil {
		t.Fatal("status with no config must fail")
	}
}

func TestAddRejectsAnEmptySelectorList(t *testing.T) {
	hermeticGitEnv(t)
	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), AddOptions{Repository: "r"})
	})
	message := requireError(t, out, err)
	requireContains(t, message, "at least one selector is required")
}

func TestAddRejectsAnUnusableSelectorBeforeContactingTheRemote(t *testing.T) {
	hermeticGitEnv(t)
	p := newProject(t)
	// A refusing runner proves selector validation happens before any git call,
	// so a typo never costs a network round trip or a credential prompt.
	service := &Service{Root: p.root, Runner: refusingRunner{t: t}, Out: &bytes.Buffer{}}
	err := service.Add(ctx(t), AddOptions{Repository: "r", Selectors: []string{"/absolute"}})
	if err == nil {
		t.Fatal("an absolute selector must be rejected")
	}
	requireContains(t, err.Error(), "must be repository-relative")
}

func TestOperationsFailWhenTheSkillsRootIsNotADirectory(t *testing.T) {
	hermeticGitEnv(t)
	p := newProject(t)
	skillsDir := filepath.Join(p.root, ".agent-layer", "skills")
	if err := os.RemoveAll(skillsDir); err != nil {
		t.Fatalf("remove skills dir: %v", err)
	}
	if err := os.WriteFile(skillsDir, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	service := &Service{Root: p.root, Runner: refusingRunner{t: t}}
	if _, err := service.Status(); err == nil {
		t.Fatal("a skills root that is not a directory must fail rather than be read as empty")
	}
}

func TestStatusFailsWhenTheImportedRootIsNotADirectory(t *testing.T) {
	hermeticGitEnv(t)
	p := newProject(t)
	if err := os.WriteFile(ImportedSkillsRoot(p.root), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	service := &Service{Root: p.root, Runner: refusingRunner{t: t}}
	if _, err := service.Status(); err == nil {
		t.Fatal("a managed root that is not a directory must fail")
	}
}

func TestWriteStatusRendersRecoveryAndProblems(t *testing.T) {
	t.Parallel()
	view := Status{
		Recovered: true,
		Problems:  []string{"an example problem"},
		Skills: []SkillStatus{{
			Repository: "https://example.invalid/s.git", SourcePath: "skills/alpha", SkillName: "alpha",
			State: StatusClean, Tracking: config.SkillTrackingTracked, Write: config.SkillWriteNone,
			Ref: "main", Commit: "abcdef123456",
		}},
	}
	var out bytes.Buffer
	WriteStatus(&out, view, true)
	text := out.String()
	// The user must learn that an interrupted publish was resolved, because it
	// may explain why local content changed since their last command.
	requireContains(t, text, "recovered an interrupted skill import publish")
	requireContains(t, text, "an example problem")
	requireContains(t, text, "alpha")
	if !view.Failed() {
		t.Fatal("a recorded problem must make status fail")
	}
}

func TestTransactionStagingFailsWhenTheStagingRootCannotBeCreated(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// A file where the staging directory belongs makes every staging operation
	// impossible; the transaction must refuse rather than publish partially.
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(StagingRoot(root), []byte("blocked"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := NewTransaction(root); err == nil {
		t.Fatal("a shared staging path occupied by an unexpected file must fail rather than deleting it")
	}
	if err := os.Remove(StagingRoot(root)); err != nil {
		t.Fatalf("remove blocker: %v", err)
	}

	transaction, err := NewTransaction(root)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}
	stagedParent := filepath.Join(transaction.stagingDir, "staged")
	if err := os.RemoveAll(stagedParent); err != nil {
		t.Fatalf("remove staged dir: %v", err)
	}
	if err := os.WriteFile(stagedParent, []byte("blocked"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	if err := transaction.StageSkill("alpha", tree(t, file(SkillManifestName, "x"))); err == nil {
		t.Fatal("staging into an unusable directory must fail")
	}
}

func TestTransactionRejectsStagingOneSkillTwice(t *testing.T) {
	t.Parallel()
	fixture := newTransactionFixture(t)
	transaction, err := NewTransaction(fixture.root)
	if err != nil {
		t.Fatalf("new transaction: %v", err)
	}
	if err := transaction.StageSkill("alpha", tree(t, file(SkillManifestName, "one"))); err != nil {
		t.Fatalf("stage: %v", err)
	}
	// Two conflicting intentions for one directory would make the publish order
	// decide the outcome.
	if err := transaction.StageSkillRemoval("alpha"); err == nil {
		t.Fatal("staging the same skill twice must fail")
	}
}

func TestRecoverTransactionRejectsAnUnknownPhaseAndVersion(t *testing.T) {
	t.Parallel()
	for name, document := range map[string]string{
		"unknown phase":   `{"version":1,"phase":"halfway","staging_dir":"x","publishes":[]}`,
		"unknown version": `{"version":99,"phase":"pending","staging_dir":"x","publishes":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o750); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(JournalPath(root), []byte(document), 0o600); err != nil {
				t.Fatalf("write journal: %v", err)
			}
			if _, err := RecoverTransaction(root); err == nil {
				t.Fatalf("%s must fail rather than be guessed at", name)
			}
		})
	}
}

func TestNormalizeTreePathRejectsUnusableNames(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "..", "../escape", "/absolute", "a\\b", "./relative", "a//b"} {
		if _, err := normalizeTreePath(raw); err == nil {
			t.Fatalf("path %q must be rejected", raw)
		}
	}
	got, err := normalizeTreePath("scripts/run.sh")
	if err != nil || got != "scripts/run.sh" {
		t.Fatalf("normalizeTreePath = (%q, %v)", got, err)
	}
}

func TestParseLsTreeRecordRejectsMalformedOutput(t *testing.T) {
	t.Parallel()
	if _, _, _, _, err := parseLsTreeRecord("no tab separator"); err == nil {
		t.Fatal("a record without a tab must be rejected rather than half-parsed")
	}
	if _, _, _, _, err := parseLsTreeRecord("100644 blob\tname"); err == nil {
		t.Fatal("a record with missing metadata fields must be rejected")
	}
	mode, objectType, object, name, err := parseLsTreeRecord("100755 blob abc123\tscripts/run.sh")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if mode != "100755" || objectType != "blob" || object != "abc123" || name != "scripts/run.sh" {
		t.Fatalf("parsed = %q %q %q %q", mode, objectType, object, name)
	}
}

func TestHasIgnoredSegmentMatchesAtAnyDepth(t *testing.T) {
	t.Parallel()
	if !hasIgnoredSegment("nested/.git/config") {
		t.Fatal("git internals must be excluded at any depth")
	}
	if hasIgnoredSegment("nested/.gitignore") {
		t.Fatal("a dotfile that is not on the ignore list is skill content")
	}
}

func TestIsPathAncestorTreatsIdenticalPathsAsOverlapping(t *testing.T) {
	t.Parallel()
	if !isPathAncestor("skills/a", "skills/a") {
		t.Fatal("a path always contains itself")
	}
	if isPathAncestor("skills/a", "skills/ab") {
		t.Fatal("a shared prefix is not containment")
	}
}

func TestPlanHasEntryMatchesOnTheFullKey(t *testing.T) {
	t.Parallel()
	entries := []config.SkillImportLockEntry{{Repository: "r", SourcePath: "skills/a"}}
	if !planHasEntry(entries, config.SkillImportEntryKey{Repository: "r", SourcePath: "skills/a"}) {
		t.Fatal("the entry must be found by its full key")
	}
	if planHasEntry(entries, config.SkillImportEntryKey{Repository: "other", SourcePath: "skills/a"}) {
		t.Fatal("a different repository must not match")
	}
}

func TestTrimTrailingBlankLinesLeavesContentAlone(t *testing.T) {
	t.Parallel()
	got := trimTrailingBlankLines([]string{"a", "", "b", "", "  "})
	if strings.Join(got, "|") != "a||b" {
		t.Fatalf("trimmed = %v", got)
	}
	if len(trimTrailingBlankLines([]string{"", " "})) != 0 {
		t.Fatal("an all-blank document must trim to nothing")
	}
}

func TestResolveDestinationBranchRejectsANonWritingPolicy(t *testing.T) {
	t.Parallel()
	service := &Service{}
	_, err := service.resolveDestinationBranch(t.Context(), nil,
		config.SkillImportLockEntry{Write: config.SkillWriteNone}, map[string]string{})
	if err == nil {
		t.Fatal("a non-writing policy must not resolve a destination branch")
	}
	_, err = service.resolveDestinationBranch(t.Context(), nil,
		config.SkillImportLockEntry{Write: config.SkillWriteBranch}, map[string]string{})
	if err == nil {
		t.Fatal("branch mode without a push branch must fail")
	}
}

func TestFetchCommitRequiresACommitID(t *testing.T) {
	t.Parallel()
	if err := fetchCommit(t.Context(), nil, "https://example.invalid/s.git", ""); err == nil {
		t.Fatal("fetching without a commit id must fail rather than fetch something arbitrary")
	}
}

func TestPushFailsWhenTheDestinationCannotBeReached(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	destination := newSourceRepo(t, "main")
	destination.writeFile("README.md", "dest\n", 0o644)
	destination.commit("base")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Write = config.SkillWriteBranch
	options.PushBranch = "contrib"
	options.PushRepository = destination.path()
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	p.writeSkillFile("alpha", "notes.md", "change\n")
	if err := os.RemoveAll(destination.path()); err != nil {
		t.Fatalf("remove destination: %v", err)
	}

	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "skill import failed")
	// The source lock must not advance when the destination was never written.
	if p.entry("alpha").UpstreamTreeHash == "" {
		t.Fatal("the lock entry was damaged by a failed push")
	}
}
