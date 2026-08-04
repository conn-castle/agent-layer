package skillimports

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestRemoveSelectorRetiresOnlyTheSkillsItOwned(t *testing.T) {
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

	out, err = p.run(func(s *Service) error { return s.Remove(ctx(t), repo.path(), "skills/beta") })
	requireNoError(t, out, err)

	if p.hasEntry("beta") {
		t.Fatalf("the removed selector's skill must be retired\n%s", out)
	}
	if !p.hasEntry("alpha") {
		t.Fatalf("an unrelated skill must stay managed\n%s", out)
	}
	requireNotContains(t, p.configText(), "skills/beta")
	requireContains(t, p.configText(), "skills/alpha")
}

func TestRemoveKeepsSkillsStillMatchedByAnotherSelector(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	// Two selectors in the same block resolve to the same path; the path is
	// deduplicated into one lock entry.
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha", "skills/*"))
	})
	requireNoError(t, out, err)
	if len(p.lock().Entries) != 1 {
		t.Fatalf("one source path must produce one lock entry, got %d", len(p.lock().Entries))
	}

	out, err = p.run(func(s *Service) error { return s.Remove(ctx(t), repo.path(), "skills/alpha") })
	requireNoError(t, out, err)

	if !p.hasEntry("alpha") {
		t.Fatalf("a skill still matched by another selector must stay managed\n%s", out)
	}
	if _, ok := p.readSkillFile("alpha", SkillManifestName); !ok {
		t.Fatalf("the skill directory must survive")
	}
}

func TestRemoveLastPositiveSelectorRemovesTheBlock(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.commit("add skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/*", "!skills/beta"))
	})
	requireNoError(t, out, err)

	out, err = p.run(func(s *Service) error { return s.Remove(ctx(t), repo.path(), "skills/*") })
	requireNoError(t, out, err)

	// Leaving an exclusion-only block behind would be a block that can never
	// import anything.
	requireNotContains(t, p.configText(), "skills.imports")
	if len(p.lock().Entries) != 0 {
		t.Fatalf("expected an empty lock, got %d entries", len(p.lock().Entries))
	}
}

func TestRemoveExclusionRevealsSkillAtTheLockedCommit(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta at the locked commit")
	first := repo.commit("add skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/*", "!skills/beta"))
	})
	requireNoError(t, out, err)
	if p.hasEntry("beta") {
		t.Fatalf("beta must start excluded\n%s", out)
	}

	repo.writeSkill("skills/beta", "beta", "Beta revised after the lock")
	repo.commit("revise beta")

	out, err = p.run(func(s *Service) error { return s.Remove(ctx(t), repo.path(), "!skills/beta") })
	requireNoError(t, out, err)

	// Removing an exclusion resolves the revealed skill at the block's locked
	// commit, so it cannot smuggle in an upstream advance.
	if got := p.entry("beta").SourceCommit; got != first {
		t.Fatalf("revealed skill imported at %q, want the locked commit %q", got, first)
	}
	body, _ := p.readSkillFile("beta", SkillManifestName)
	requireContains(t, body, "Beta at the locked commit")
}

func TestRemoveUnknownSelectorFails(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)
	before := p.configText()

	out, err = p.run(func(s *Service) error { return s.Remove(ctx(t), repo.path(), "skills/nope") })
	message := requireError(t, out, err)
	requireContains(t, message, "no configured selector")
	if p.configText() != before {
		t.Fatalf("a failed remove must change no configuration")
	}
}

func TestRemovePreservesModifiedRetiredSkillAndLeavesConfigUnchanged(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)
	before := p.configText()

	p.writeSkillFile("alpha", "notes.md", "unsaved work\n")

	out, err = p.run(func(s *Service) error { return s.Remove(ctx(t), repo.path(), "skills/alpha") })
	message := requireError(t, out, err)
	requireContains(t, message, "has local changes")

	// Remove is all-or-nothing: prior configuration, content, and lock survive.
	if p.configText() != before {
		t.Fatalf("a failed remove must leave configuration unchanged")
	}
	if _, ok := p.readSkillFile("alpha", "notes.md"); !ok {
		t.Fatalf("local work was deleted")
	}
	if !p.hasEntry("alpha") {
		t.Fatalf("the lock entry must be preserved")
	}
}

func TestStatusIsNetworkFreeAndSummarizesLocalState(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.writeSkill("skills/gamma", "gamma", "Gamma")
	repo.commit("add skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/*", "!skills/gamma"))
	})
	requireNoError(t, out, err)

	p.writeSkillFile("beta", "notes.md", "local edit\n")

	// The source repository disappears entirely; status must still work, which is
	// only possible if it never contacts a remote.
	if err := os.RemoveAll(repo.path()); err != nil {
		t.Fatalf("remove source: %v", err)
	}

	service := &Service{Root: p.root, Runner: refusingRunner{t: t}}
	view, err := service.Status()
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	totals := view.Totals()
	if totals.Total != 2 || totals.Clean != 1 || totals.Modified != 1 {
		t.Fatalf("totals = %+v, want 2 total / 1 clean / 1 modified", totals)
	}
	if totals.Tracked != 2 || totals.Pinned != 0 || totals.WriteEnabled != 0 {
		t.Fatalf("policy totals = %+v", totals)
	}
	if totals.Exclusions != 1 {
		t.Fatalf("expected the configured exclusion to be reported, got %d", totals.Exclusions)
	}
	// Clean and modified are ordinary successful states.
	if view.Failed() {
		t.Fatalf("a clean-or-modified project must not fail status")
	}

	var buffer bytes.Buffer
	WriteStatus(&buffer, view, true)
	text := buffer.String()
	requireContains(t, text, "alpha")
	requireContains(t, text, "modified")
	requireContains(t, text, "exclusion")
	requireContains(t, text, "!skills/gamma")
}

func TestStatusFailsOnMissingSkillAndOrphanDirectory(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	if err := os.RemoveAll(p.skillDir("alpha")); err != nil {
		t.Fatalf("remove skill: %v", err)
	}
	// A directory nobody owns must be surfaced, not silently projected.
	orphan := filepath.Join(ImportedSkillsRoot(p.root), "stray")
	if err := os.MkdirAll(orphan, 0o750); err != nil {
		t.Fatalf("mkdir orphan: %v", err)
	}

	service := &Service{Root: p.root, Runner: refusingRunner{t: t}}
	view, err := service.Status()
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if !view.Failed() {
		t.Fatalf("a missing skill and an orphan directory must make status fail")
	}
	statusErr := StatusError(view)
	if statusErr == nil {
		t.Fatal("expected a status error")
	}
	requireContains(t, statusErr.Error(), "missing")
	requireContains(t, statusErr.Error(), "unmanaged directory")
}

func TestStatusReportsCollisionWithUserManagedSkill(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), addOptions(repo, "skills/alpha")) })
	requireNoError(t, out, err)

	p.writeUserSkill("alpha")

	service := &Service{Root: p.root, Runner: refusingRunner{t: t}}
	view, err := service.Status()
	if err != nil {
		t.Fatalf("status failed: %v", err)
	}
	if got := view.Totals().Collided; got != 1 {
		t.Fatalf("collided = %d, want 1", got)
	}
	if !view.Failed() {
		t.Fatalf("a collision must make status fail")
	}
}

func TestStatusFailsOnMalformedLock(t *testing.T) {
	hermeticGitEnv(t)
	p := newProject(t)
	lockPath := config.DefaultPaths(p.root).SkillImportLockPath
	if err := os.WriteFile(lockPath, []byte(`{"version": 99, "entries": []}`), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	service := &Service{Root: p.root, Runner: refusingRunner{t: t}}
	_, err := service.Status()
	if err == nil {
		t.Fatal("a lock Agent Layer cannot trust must fail rather than be reconstructed")
	}
	requireContains(t, err.Error(), "not supported")
}

// refusingRunner fails any git invocation. Injecting it proves a code path is
// genuinely network-free rather than merely appearing to succeed offline.
type refusingRunner struct{ t *testing.T }

func (r refusingRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.t.Fatalf("git must not run here: git %s", strings.Join(args, " "))
	return nil, nil
}
