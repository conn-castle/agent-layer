package skillimports

import (
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

// writeEnabledOptions builds add options that push directly to the source
// repository's default branch.
func directWriteOptions(repo *sourceRepo, selectors ...string) AddOptions {
	options := addOptions(repo, selectors...)
	options.Write = config.SkillWriteDirect
	return options
}

func TestPushSkipsImportsWithNoWritePolicy(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	p.writeSkillFile("alpha", "notes.md", "local only\n")
	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	requireNoError(t, out, err)
	requireContains(t, out, "no imports are configured to write upstream")

	if _, ok := repo.fileAt("HEAD", "skills/alpha/notes.md"); ok {
		t.Fatalf("write = none must not write upstream")
	}
}

func TestPushRefusesStaleWritePolicyFromLock(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")
	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Write = config.SkillWriteBranch
	options.PushBranch = "contrib"
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	updated := strings.Replace(p.configText(), `write = "branch"`, `write = "none"`, 1)
	updated = strings.Replace(updated, "push_branch = \"contrib\"\n", "", 1)
	p.writeConfig(updated)
	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	if err == nil {
		t.Fatalf("push used stale write-enabled lock policy\n%s", out)
	}
	requireContains(t, out, "configuration no longer matches")
}

func TestPushDirectWritesToDestinationDefaultBranch(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), directWriteOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	p.writeSkillFile("alpha", "references/notes.md", "contributed upstream\n")

	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	requireNoError(t, out, err)
	requireContains(t, out, "pushed")

	content, ok := repo.fileAt("main", "skills/alpha/references/notes.md")
	if !ok {
		t.Fatalf("the local change did not reach the destination\n%s", out)
	}
	if content != "contributed upstream\n" {
		t.Fatalf("destination content = %q", content)
	}

	// A push that updated the exact tracked source repository and ref advances
	// the source lock, so the next pull has the right merge base.
	entry := p.entry("alpha")
	head := strings.TrimSpace(runGit(t, repo.path(), "rev-parse", "main"))
	if entry.SourceCommit != head {
		t.Fatalf("source lock = %q, want the pushed commit %q", entry.SourceCommit, head)
	}
	localTree, readErr := ReadLocalTree(p.skillDir("alpha"))
	if readErr != nil {
		t.Fatalf("read local tree: %v", readErr)
	}
	if entry.UpstreamTreeHash != localTree.Hash() {
		t.Fatalf("after pushing, the local tree is the upstream tree; hashes must match")
	}
}

func TestPushReportsUnchangedWhenDestinationAlreadyMatches(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), directWriteOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	before := strings.TrimSpace(runGit(t, repo.path(), "rev-parse", "main"))
	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	requireNoError(t, out, err)
	requireContains(t, out, "unchanged")

	after := strings.TrimSpace(runGit(t, repo.path(), "rev-parse", "main"))
	if before != after {
		t.Fatalf("an unchanged push created a commit: %q -> %q", before, after)
	}
}

func TestPushBranchModeCreatesConfiguredBranch(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Write = config.SkillWriteBranch
	options.PushBranch = "skill-updates"
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	p.writeSkillFile("alpha", "references/notes.md", "proposed change\n")
	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	requireNoError(t, out, err)

	content, ok := repo.fileAt("skill-updates", "skills/alpha/references/notes.md")
	if !ok {
		t.Fatalf("the configured branch was not created with the change\n%s", out)
	}
	if content != "proposed change\n" {
		t.Fatalf("branch content = %q", content)
	}
	// The default branch is untouched: branch mode never writes to the primary.
	if _, ok := repo.fileAt("main", "skills/alpha/references/notes.md"); ok {
		t.Fatalf("branch mode wrote to the default branch")
	}
	// The push went to a different ref than the tracked source ref, so the source
	// lock must not advance.
	if got := p.entry("alpha").SourceCommit; got == strings.TrimSpace(runGit(t, repo.path(), "rev-parse", "skill-updates")) {
		t.Fatalf("the source lock advanced onto a branch it does not track")
	}
}

func TestPushBranchModeRejectsThePrimaryBranch(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Write = config.SkillWriteBranch
	options.PushBranch = "main"
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	p.writeSkillFile("alpha", "notes.md", "change\n")
	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	message := requireError(t, out, err)
	// `branch` exists so a contribution lands somewhere reviewable; silently
	// writing the primary branch would defeat that.
	requireContains(t, message, "requires a non-primary branch")
}

func TestPushToForkDoesNotAdvanceSourceLock(t *testing.T) {
	hermeticGitEnv(t)
	upstream := newSourceRepo(t, "main")
	upstream.writeSkill("skills/alpha", "alpha", "Alpha")
	upstream.commit("add alpha")

	fork := newSourceRepo(t, "main")
	runGit(t, fork.path(), "fetch", upstream.path(), "main")
	runGit(t, fork.path(), "reset", "--hard", "FETCH_HEAD")

	p := newProject(t)
	options := addOptions(upstream, "skills/alpha")
	options.Write = config.SkillWriteDirect
	options.PushRepository = fork.path()
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)
	lockedBefore := p.entry("alpha").SourceCommit

	p.writeSkillFile("alpha", "references/notes.md", "fork contribution\n")
	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	requireNoError(t, out, err)

	if _, ok := fork.fileAt("main", "skills/alpha/references/notes.md"); !ok {
		t.Fatalf("the change did not reach the fork\n%s", out)
	}
	if _, ok := upstream.fileAt("main", "skills/alpha/references/notes.md"); ok {
		t.Fatalf("a fork push must not write the source repository")
	}
	if got := p.entry("alpha").SourceCommit; got != lockedBefore {
		t.Fatalf("a fork push advanced the source lock: %q", got)
	}
}

func TestPushRefusesTrackedImportWhoseSourceMoved(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), directWriteOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	p.writeSkillFile("alpha", "references/notes.md", "local change\n")
	repo.writeFile("README.md", "upstream moved on\n", 0o644)
	repo.commit("unrelated upstream commit")

	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	message := requireError(t, out, err)
	// Pushing from a stale base would silently derive the change from the wrong
	// starting point.
	requireContains(t, message, "run 'al skills pull' first")
	if _, ok := repo.fileAt("main", "skills/alpha/references/notes.md"); ok {
		t.Fatalf("a refused push must write nothing")
	}
}

func TestPushRefusesMissingSkillRatherThanDeletingUpstream(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), directWriteOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	if err := removeAllForTest(p.skillDir("alpha")); err != nil {
		t.Fatalf("remove skill: %v", err)
	}

	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "never propagates a whole-skill deletion upstream")
	if _, ok := repo.fileAt("main", "skills/alpha/"+SkillManifestName); !ok {
		t.Fatalf("the upstream skill was deleted")
	}
}

func TestPushPropagatesFileDeletionInsideAValidSkill(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeFile("skills/alpha/references/old.md", "obsolete\n", 0o644)
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), directWriteOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	if err := removeAllForTest(p.skillDir("alpha") + "/references"); err != nil {
		t.Fatalf("remove reference: %v", err)
	}

	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	requireNoError(t, out, err)

	// A file-level deletion inside a still-valid skill is an ordinary local change.
	if _, ok := repo.fileAt("main", "skills/alpha/references/old.md"); ok {
		t.Fatalf("the deleted reference was not propagated\n%s", out)
	}
	if _, ok := repo.fileAt("main", "skills/alpha/"+SkillManifestName); !ok {
		t.Fatalf("the skill itself must survive")
	}
}

func TestPushGroupsOneCommitPerDestination(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.commit("add skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), directWriteOptions(repo, "skills/*")) })
	requireNoError(t, out, err)

	before := strings.TrimSpace(runGit(t, repo.path(), "rev-list", "--count", "main"))
	p.writeSkillFile("alpha", "references/a.md", "a\n")
	p.writeSkillFile("beta", "references/b.md", "b\n")

	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	requireNoError(t, out, err)

	after := strings.TrimSpace(runGit(t, repo.path(), "rev-list", "--count", "main"))
	if before == after {
		t.Fatalf("nothing was pushed\n%s", out)
	}
	// Both skills share one destination, so they share one commit.
	if countDelta(t, before, after) != 1 {
		t.Fatalf("expected exactly one commit for the destination group, got %s -> %s", before, after)
	}
	if _, ok := repo.fileAt("main", "skills/alpha/references/a.md"); !ok {
		t.Fatalf("alpha was not included in the group commit")
	}
	if _, ok := repo.fileAt("main", "skills/beta/references/b.md"); !ok {
		t.Fatalf("beta was not included in the group commit")
	}
}

func TestPushPreservesUnrelatedDestinationChanges(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeFile("skills/alpha/references/upstream.md", "v1\n", 0o644)
	repo.commit("add alpha")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Write = config.SkillWriteBranch
	options.PushBranch = "contributions"
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	// The destination branch exists and already carries a change to a different
	// file inside the same skill.
	repo.branch("contributions")
	repo.writeFile("skills/alpha/references/upstream.md", "v2 from the destination\n", 0o644)
	repo.commit("destination-side change")
	repo.checkout("main")

	p.writeSkillFile("alpha", "references/local.md", "local addition\n")
	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	requireNoError(t, out, err)

	upstreamFile, ok := repo.fileAt("contributions", "skills/alpha/references/upstream.md")
	if !ok || upstreamFile != "v2 from the destination\n" {
		t.Fatalf("a compatible destination change was clobbered: %q", upstreamFile)
	}
	localFile, ok := repo.fileAt("contributions", "skills/alpha/references/local.md")
	if !ok || localFile != "local addition\n" {
		t.Fatalf("the local change did not reach the destination: %q", localFile)
	}
}

func TestPushReportsDestinationConflict(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeFile("skills/alpha/references/notes.md", "base\n", 0o644)
	repo.commit("add alpha")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Write = config.SkillWriteBranch
	options.PushBranch = "contributions"
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	repo.branch("contributions")
	repo.writeFile("skills/alpha/references/notes.md", "destination rewrite\n", 0o644)
	destinationHead := repo.commit("destination-side rewrite")
	repo.checkout("main")

	p.writeSkillFile("alpha", "references/notes.md", "local rewrite\n")
	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "conflict")

	if got := strings.TrimSpace(runGit(t, repo.path(), "rev-parse", "contributions")); got != destinationHead {
		t.Fatalf("a conflicted push moved the destination branch: %q", got)
	}
}

func TestPushBuildsOnDestinationHeadAndNeverForces(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Write = config.SkillWriteBranch
	options.PushBranch = "contributions"
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	// The destination branch carries history unrelated to the skill.
	runGit(t, repo.path(), "checkout", "--quiet", "--orphan", "contributions")
	runGit(t, repo.path(), "rm", "-rq", "--cached", ".")
	repo.writeFile("UNRELATED.md", "destination-only history\n", 0o644)
	orphanHead := repo.commit("unrelated orphan history")
	repo.checkout("main")

	p.writeSkillFile("alpha", "notes.md", "local change\n")

	recorder := &recordingRunner{inner: ExecGitRunner{}}
	var report strings.Builder
	service := &Service{Root: p.root, Runner: recorder, Project: func(string) error { return nil }, Out: &report}
	if err := service.Push(ctx(t)); err != nil {
		t.Fatalf("push failed: %v\n%s", err, report.String())
	}

	// Force flags would let a push discard destination history that Agent Layer
	// never inspected.
	for _, args := range recorder.calls {
		for _, arg := range args {
			if arg == "--force" || arg == "-f" || arg == "--force-with-lease" || strings.HasPrefix(arg, "+refs/heads/") && args[0] == "push" {
				t.Fatalf("push used a forcing argument: git %s", strings.Join(args, " "))
			}
		}
	}

	// The destination's own history survives because the commit was built on top
	// of its head rather than replacing it.
	if _, ok := repo.fileAt("contributions", "UNRELATED.md"); !ok {
		t.Fatalf("destination-only history was discarded")
	}
	if _, ok := repo.fileAt("contributions", "skills/alpha/notes.md"); !ok {
		t.Fatalf("the local change did not reach the destination")
	}
	parent := strings.TrimSpace(runGit(t, repo.path(), "rev-parse", "contributions^"))
	if parent != orphanHead {
		t.Fatalf("the pushed commit's parent = %q, want the destination head %q", parent, orphanHead)
	}
}

func TestPushValidatesMergedResultBeforeWriting(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), directWriteOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	// A local edit that breaks the skill contract must not be published upstream.
	p.writeSkillFile("alpha", SkillManifestName, "---\nname: alpha\ndescription: \n---\n\nBody\n")

	out, err = p.run(func(s *Service) error { return s.Push(ctx(t)) })
	message := requireError(t, out, err)
	requireContains(t, message, "description")

	content, _ := repo.fileAt("main", "skills/alpha/"+SkillManifestName)
	requireContains(t, content, "The alpha skill.")
}

func countDelta(t *testing.T, before string, after string) int {
	t.Helper()
	return atoiForTest(t, after) - atoiForTest(t, before)
}
