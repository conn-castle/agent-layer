package skillimports

import (
	"os"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestPullAdvancesTrackedBranch(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Original body")
	first := repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	repo.writeSkill("skills/alpha", "alpha", "Upstream revision")
	second := repo.commit("revise alpha")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	requireNoError(t, out, err)

	body, _ := p.readSkillFile("alpha", SkillManifestName)
	requireContains(t, body, "Upstream revision")
	if got := p.entry("alpha").SourceCommit; got != second {
		t.Fatalf("locked commit = %q, want the advanced commit %q (was %q)", got, second, first)
	}
}

func TestPullDoesNotAdvancePinnedImport(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Original body")
	first := repo.commit("add alpha")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Tracking = config.SkillTrackingPinned
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	repo.writeSkill("skills/alpha", "alpha", "Upstream revision")
	repo.commit("revise alpha")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	requireNoError(t, out, err)

	if got := p.entry("alpha").SourceCommit; got != first {
		t.Fatalf("a pinned import moved to %q; it must stay at %q", got, first)
	}
	body, _ := p.readSkillFile("alpha", SkillManifestName)
	requireContains(t, body, "Original body")
}

func TestPullMergesUpstreamChangeWithLocalEdit(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Shared body")
	repo.writeFile("skills/alpha/references/upstream.md", "upstream v1\n", 0o644)
	repo.writeFile("skills/alpha/references/local.md", "placeholder\n", 0o644)
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	// The user edits one file locally; upstream changes a different one.
	p.writeSkillFile("alpha", "references/local.md", "local notes\n")
	repo.writeFile("skills/alpha/references/upstream.md", "upstream v2\n", 0o644)
	second := repo.commit("revise upstream reference")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	requireNoError(t, out, err)

	upstream, _ := p.readSkillFile("alpha", "references/upstream.md")
	if upstream != "upstream v2\n" {
		t.Fatalf("upstream change was not applied: %q", upstream)
	}
	local, _ := p.readSkillFile("alpha", "references/local.md")
	if local != "local notes\n" {
		t.Fatalf("local edit was lost: %q", local)
	}

	entry := p.entry("alpha")
	if entry.SourceCommit != second {
		t.Fatalf("lock did not advance after a clean merge: %q", entry.SourceCommit)
	}
	// The lock records the upstream tree, not the merged working tree, so the
	// next pull still has the correct merge base.
	localTree, readErr := ReadLocalTree(p.skillDir("alpha"))
	if readErr != nil {
		t.Fatalf("read local tree: %v", readErr)
	}
	if entry.UpstreamTreeHash == localTree.Hash() {
		t.Fatalf("lock recorded the merged working tree instead of the upstream tree")
	}
}

func TestPullReportsConflictAndPublishesNothingForThatSkill(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeFile("skills/alpha/references/notes.md", "line one\n", 0o644)
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)
	first := p.entry("alpha").SourceCommit

	p.writeSkillFile("alpha", "references/notes.md", "local rewrite\n")
	repo.writeFile("skills/alpha/references/notes.md", "upstream rewrite\n", 0o644)
	repo.commit("rewrite notes")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "conflict")
	requireContains(t, message, "references/notes.md")

	// Conflict markers must never reach the managed tree, and the lock must not
	// advance past content that was never published.
	notes, _ := p.readSkillFile("alpha", "references/notes.md")
	if notes != "local rewrite\n" {
		t.Fatalf("local content was overwritten during a conflict: %q", notes)
	}
	requireNotContains(t, notes, "<<<<<<<")
	if got := p.entry("alpha").SourceCommit; got != first {
		t.Fatalf("lock advanced past a conflicted merge: %q", got)
	}
}

func TestPullTreatsBinaryDualChangeAsConflict(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeFile("skills/alpha/assets/logo.bin", "base\x00payload", 0o644)
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	p.writeSkillFile("alpha", "assets/logo.bin", "local\x00payload")
	repo.writeFile("skills/alpha/assets/logo.bin", "upstream\x00payload", 0o644)
	repo.commit("change binary asset")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	message := requireError(t, out, err)
	// A binary file has no line-wise merge, so guessing a result would corrupt it.
	requireContains(t, message, "binary file changed in both")
}

func TestPullRetiresSkillThatDisappearedUpstream(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.commit("add skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/*")) })
	requireNoError(t, out, err)
	if !p.hasEntry("beta") {
		t.Fatalf("expected beta to be imported\n%s", out)
	}

	repo.removeAll("skills/beta")
	repo.commit("remove beta upstream")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	requireNoError(t, out, err)

	if p.hasEntry("beta") {
		t.Fatalf("a wildcard member that disappeared upstream must be retired\n%s", out)
	}
	if _, statErr := os.Stat(p.skillDir("beta")); !os.IsNotExist(statErr) {
		t.Fatalf("an unmodified retired skill must be deleted")
	}
	if !p.hasEntry("alpha") {
		t.Fatalf("the surviving wildcard member must stay managed")
	}
}

func TestPullPreservesModifiedSkillThatLeftTheDesiredSet(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.commit("add skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/*")) })
	requireNoError(t, out, err)

	p.writeSkillFile("beta", "notes.md", "work in progress\n")
	repo.removeAll("skills/beta")
	repo.commit("remove beta upstream")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	message := requireError(t, out, err)
	// Deleting unsaved local work is not Agent Layer's decision to make.
	requireContains(t, message, "has local changes")
	requireContains(t, message, "adopt it")
	if _, ok := p.readSkillFile("beta", "notes.md"); !ok {
		t.Fatalf("local work was deleted during retirement")
	}
	if !p.hasEntry("beta") {
		t.Fatalf("the lock entry must be preserved alongside the preserved directory")
	}
}

func TestPullRestoresMissingDesiredSkill(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	if err := os.RemoveAll(p.skillDir("alpha")); err != nil {
		t.Fatalf("remove skill directory: %v", err)
	}

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	requireNoError(t, out, err)
	requireContains(t, out, "restored")

	if _, ok := p.readSkillFile("alpha", SkillManifestName); !ok {
		t.Fatalf("a desired skill whose directory vanished must be restored")
	}
	// A missing local directory is not authorization to delete the upstream skill.
	if _, ok := repo.fileAt("HEAD", "skills/alpha/"+SkillManifestName); !ok {
		t.Fatalf("the upstream skill must be untouched")
	}
}

func TestPullDoesNotRestoreIntoAUserManagedCollision(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	// The user adopts the skill: it moves into the user-managed root, leaving the
	// managed directory absent while the selector still matches.
	if err := os.Rename(p.skillDir("alpha"), p.userSkillPath("alpha")); err != nil {
		t.Fatalf("adopt skill: %v", err)
	}

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "narrow or remove the selector")
	if _, statErr := os.Stat(p.skillDir("alpha")); !os.IsNotExist(statErr) {
		t.Fatalf("restoring must not recreate the collision")
	}
}

func TestPullPrunesAdoptedSkillOnceTheSelectorIsRemoved(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.commit("add skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha", "skills/beta"))
	})
	requireNoError(t, out, err)

	if err := os.Rename(p.skillDir("alpha"), p.userSkillPath("alpha")); err != nil {
		t.Fatalf("adopt skill: %v", err)
	}

	out, err = p.run(func(s *Service) error { return s.Remove(ctx(t), repo.path(), "skills/alpha") })
	requireNoError(t, out, err)

	if p.hasEntry("alpha") {
		t.Fatalf("the lock entry for an adopted skill must be pruned\n%s", out)
	}
	if _, statErr := os.Stat(p.userSkillPath("alpha")); statErr != nil {
		t.Fatalf("the adopted user-managed skill must survive: %v", statErr)
	}
	if !p.hasEntry("beta") {
		t.Fatalf("the other import must stay managed")
	}
}

func TestPullTreatsDefaultBranchRenameAsRetarget(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha on main")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)
	if got := p.entry("alpha").ResolvedRefName; got != "main" {
		t.Fatalf("resolved ref = %q, want main", got)
	}

	// Upstream renames its default branch and moves on.
	runGit(t, repo.path(), "branch", "-m", "main", "trunk")
	runGit(t, repo.path(), "symbolic-ref", "HEAD", "refs/heads/trunk")
	repo.writeSkill("skills/alpha", "alpha", "Alpha on trunk")
	renamed := repo.commit("revise on trunk")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	requireNoError(t, out, err)

	entry := p.entry("alpha")
	if entry.ResolvedRefName != "trunk" {
		t.Fatalf("the lock must record the new default branch name, got %q", entry.ResolvedRefName)
	}
	if entry.SourceCommit != renamed {
		t.Fatalf("a renamed default branch is a retarget, not a mismatch: %q", entry.SourceCommit)
	}
	body, _ := p.readSkillFile("alpha", SkillManifestName)
	requireContains(t, body, "Alpha on trunk")
}

func TestPullReportsUnreachableSourceWithoutTouchingOtherSources(t *testing.T) {
	hermeticGitEnv(t)
	good := newSourceRepo(t, "main")
	good.writeSkill("skills/alpha", "alpha", "Alpha")
	good.commit("add alpha")

	broken := newSourceRepo(t, "main")
	broken.writeSkill("skills/beta", "beta", "Beta")
	broken.commit("add beta")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(good, "skills/alpha")) })
	requireNoError(t, out, err)
	out, err = p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(broken, "skills/beta")) })
	requireNoError(t, out, err)

	// The second source becomes unreachable, standing in for an authentication or
	// network failure.
	if err := os.RemoveAll(broken.path()); err != nil {
		t.Fatalf("remove source: %v", err)
	}
	good.writeSkill("skills/alpha", "alpha", "Alpha revised")
	revised := good.commit("revise alpha")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "some skill imports failed")
	requireContains(t, out, "failed")

	// The reachable source still advanced, and the unreachable one kept both its
	// local tree and its lock entry.
	if got := p.entry("alpha").SourceCommit; got != revised {
		t.Fatalf("a healthy source was blocked by an unrelated failure: %q", got)
	}
	body, _ := p.readSkillFile("alpha", SkillManifestName)
	requireContains(t, body, "Alpha revised")
	if !p.hasEntry("beta") {
		t.Fatalf("a source failure must not retire that source's skills")
	}
	if _, ok := p.readSkillFile("beta", SkillManifestName); !ok {
		t.Fatalf("a source failure must preserve local content")
	}
	// Successful results are still projected after a partial failure.
	if p.projected == 0 {
		t.Fatalf("partial success must still be projected")
	}
}

func TestPullFailsSkillThatBecameInvalidUpstreamWithoutRetiringIt(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)
	first := p.entry("alpha").SourceCommit

	repo.writeFile("skills/alpha/"+SkillManifestName,
		"---\nname: alpha\ndescription: \n---\n\nBody\n", 0o644)
	repo.commit("break alpha upstream")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "skill import failed")
	requireContains(t, out, "description")

	// Invalidity upstream is not removal: the local directory and lock entry stay.
	if got := p.entry("alpha").SourceCommit; got != first {
		t.Fatalf("lock advanced onto an invalid upstream skill: %q", got)
	}
	if _, ok := p.readSkillFile("alpha", SkillManifestName); !ok {
		t.Fatalf("local content must be preserved when upstream becomes invalid")
	}
}

func TestPullIsANoOpWhenNothingChanged(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	before, statErr := os.Stat(p.skillDir("alpha"))
	if statErr != nil {
		t.Fatalf("stat skill: %v", statErr)
	}

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	requireNoError(t, out, err)
	requireContains(t, out, "unchanged")

	after, statErr := os.Stat(p.skillDir("alpha"))
	if statErr != nil {
		t.Fatalf("stat skill: %v", statErr)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("an unchanged skill must not be republished")
	}
}

func TestPullKeepsLocalModificationsWhenUpstreamDidNotMove(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	p.writeSkillFile("alpha", "notes.md", "my notes\n")

	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	requireNoError(t, out, err)
	requireContains(t, out, "locally modified")

	notes, ok := p.readSkillFile("alpha", "notes.md")
	if !ok || notes != "my notes\n" {
		t.Fatalf("local edits were lost by a no-op pull: %q", notes)
	}
}

func TestPullWithNoConfiguredImportsSucceeds(t *testing.T) {
	hermeticGitEnv(t)
	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	requireNoError(t, out, err)
	requireContains(t, out, "no skill imports are configured")
	if p.projected != 0 {
		t.Fatalf("nothing to do must not trigger a projection")
	}
}

func TestPullReportsProjectionFailureWithoutDiscardingSourceState(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	repo.writeSkill("skills/alpha", "alpha", "Alpha revised")
	revised := repo.commit("revise alpha")

	p.projectFn = func(string) error { return errProjectionForTest }
	out, err = p.run(func(s *Service) error { return s.Pull(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "projecting it into the clients failed")

	// The imported skill and its lock are valid; only the projection failed, so
	// discarding them would throw away completed work.
	if got := p.entry("alpha").SourceCommit; got != revised {
		t.Fatalf("valid source state was discarded because projection failed: %q", got)
	}
	body, _ := p.readSkillFile("alpha", SkillManifestName)
	requireContains(t, body, "Alpha revised")
	requireContains(t, out, "rerun 'al sync'")
}

func TestPullRejectsChangedSelectorsWithoutAdvancingPinnedBlock(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	first := repo.commit("add skills")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Tracking = config.SkillTrackingPinned
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	repo.writeSkill("skills/beta", "beta", "Beta revised")
	repo.commit("revise beta")

	options = addOptions(repo, "skills/beta")
	options.Tracking = config.SkillTrackingPinned
	out, err = p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	// A newly desired skill in a pinned block comes from the block's locked
	// commit, so adding a selector cannot smuggle in an upstream advance.
	if got := p.entry("beta").SourceCommit; got != first {
		t.Fatalf("beta imported at %q; want the block's locked commit %q", got, first)
	}
	body, _ := p.readSkillFile("beta", SkillManifestName)
	requireNotContains(t, body, "Beta revised")
}

var errProjectionForTest = &projectionTestError{}

type projectionTestError struct{}

func (*projectionTestError) Error() string { return "client projection failed in this test" }

// userSkillPath returns where a user-managed skill of this name would live.
func (p *project) userSkillPath(name string) string {
	return strings.TrimSuffix(p.root, "/") + "/.agent-layer/skills/" + name
}
