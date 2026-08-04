package skillimports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestAddImportsExactSelectorAndProjects(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha body")
	repo.writeFile("skills/alpha/scripts/run.sh", "#!/bin/sh\necho alpha\n", 0o755)
	repo.writeFile("skills/alpha/.config", "hidden but part of the skill\n", 0o644)
	head := repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha"))
	})
	requireNoError(t, out, err)

	// The whole tree is imported, not just SKILL.md: an agent that follows a
	// skill's instructions to run a script must find the script.
	if _, ok := p.readSkillFile("alpha", "scripts/run.sh"); !ok {
		t.Fatalf("expected scripts/run.sh to be imported\n%s", out)
	}
	if _, ok := p.readSkillFile("alpha", ".config"); !ok {
		t.Fatalf("expected the dotfile to be imported\n%s", out)
	}
	info, statErr := os.Stat(filepath.Join(p.skillDir("alpha"), "scripts", "run.sh"))
	if statErr != nil {
		t.Fatalf("stat imported script: %v", statErr)
	}
	if info.Mode().Perm() != ExecutableFileMode {
		t.Fatalf("imported script mode = %v, want %v", info.Mode().Perm(), ExecutableFileMode)
	}

	entry := p.entry("alpha")
	if entry.SourceCommit != head {
		t.Fatalf("locked commit = %q, want %q", entry.SourceCommit, head)
	}
	if entry.Tracking != config.SkillTrackingTracked {
		t.Fatalf("tracking = %q, want %q (a branch tracks by default)", entry.Tracking, config.SkillTrackingTracked)
	}
	if entry.ResolvedRefName != "main" || !entry.RefOmitted {
		t.Fatalf("expected the omitted ref to resolve to the actual default branch, got %+v", entry)
	}
	if entry.Write != config.SkillWriteNone {
		t.Fatalf("write = %q, want the documented default %q", entry.Write, config.SkillWriteNone)
	}

	// Configuration is the desired state, so the selector must be recorded there.
	requireContains(t, p.configText(), `selectors = ["skills/alpha"]`)

	// A successful add reaches the clients without a separate sync.
	if p.projected != 1 {
		t.Fatalf("projection ran %d times, want 1", p.projected)
	}
}

func TestAddWildcardSkipsNonSkillDirectoriesAndAppliesExclusions(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.writeSkill("skills/internal", "internal", "Internal")
	repo.writeFile("skills/docs/README.md", "not a skill\n", 0o644)
	repo.commit("add skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/*", "!skills/internal"))
	})
	requireNoError(t, out, err)

	if !p.hasEntry("alpha") || !p.hasEntry("beta") {
		t.Fatalf("expected alpha and beta to be imported\n%s", out)
	}
	// An excluded path is outside the desired set: it is never imported and never
	// validated as an import.
	if p.hasEntry("internal") {
		t.Fatalf("excluded skill was imported\n%s", out)
	}
	// A wildcard walks ordinary directories rather than failing on them.
	if p.hasEntry("docs") {
		t.Fatalf("a directory without SKILL.md was imported\n%s", out)
	}
}

func TestAddExclusionWinsOverAlreadyConfiguredExactSelector(t *testing.T) {
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
	if !p.hasEntry("beta") {
		t.Fatalf("expected beta to be imported first\n%s", out)
	}

	// The exclusion is added after the exact selector it cancels; exclusions win
	// regardless of order, and the excluded skill is retired by the one
	// retirement rule.
	out, err = p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "!skills/beta"))
	})
	requireNoError(t, out, err)

	if !p.hasEntry("alpha") {
		t.Fatalf("alpha must stay managed\n%s", out)
	}
	if p.hasEntry("beta") {
		t.Fatalf("an exclusion must cancel an exact selector in the same block\n%s", out)
	}
	if _, statErr := os.Stat(p.skillDir("beta")); !os.IsNotExist(statErr) {
		t.Fatalf("an unmodified retired skill must be deleted")
	}
}

func TestAddRejectsSelectorCancelledByItsOwnExclusion(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.commit("add skills")

	p := newProject(t)
	before := p.configText()
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/beta", "!skills/beta"))
	})
	message := requireError(t, out, err)
	// Silently importing nothing would look like success; naming the contradiction
	// tells the user which of the two selectors to drop.
	requireContains(t, message, "cancelled by exclusion")
	if p.configText() != before {
		t.Fatalf("a contradictory add must change no configuration")
	}
}

func TestAddExactSelectorMissingUpstreamFails(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/missing"))
	})
	message := requireError(t, out, err)
	requireContains(t, message, "does not exist in the source repository")
	// Nothing was written, so a mistyped selector leaves the project untouched.
	requireNotContains(t, p.configText(), "skills/missing")
	if _, statErr := os.Stat(ImportedSkillsRoot(p.root)); !os.IsNotExist(statErr) {
		t.Fatalf("a failed add must not create the managed root")
	}
}

func TestAddWildcardMatchingNothingFailsAndChangesNoState(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	before := p.configText()
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "plugins/*"))
	})
	message := requireError(t, out, err)
	requireContains(t, message, "resolved to no valid skills")
	if p.configText() != before {
		t.Fatalf("a zero-result wildcard add must leave configuration unchanged")
	}
	if p.projected != 0 {
		t.Fatalf("a failed add must not project")
	}
}

func TestAddRejectsInvalidUpstreamSkill(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	// The frontmatter name does not match the directory, so the skill cannot be
	// projected into a directory that matches its own declared name.
	repo.writeFile("skills/alpha/"+SkillManifestName,
		"---\nname: something-else\ndescription: Mismatched.\n---\n\nBody\n", 0o644)
	repo.commit("add mismatched skill")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha"))
	})
	message := requireError(t, out, err)
	requireContains(t, message, "must match the selected directory")
}

func TestAddRejectsUnsupportedFrontMatterField(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeFile("skills/alpha/"+SkillManifestName,
		"---\nname: alpha\ndescription: Alpha.\nmodel: gpt-5\n---\n\nBody\n", 0o644)
	repo.commit("add skill with unsupported field")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha"))
	})
	message := requireError(t, out, err)
	// Rejecting the field is the point: accepting it would silently drop
	// behavior the skill author asked for.
	requireContains(t, message, `"model"`)
	requireContains(t, message, "cannot project")
}

func TestAddRejectsSymlinkInsideSkill(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	if err := os.Symlink("/etc/passwd", filepath.Join(repo.path(), "skills", "alpha", "leak")); err != nil {
		t.Skipf("symlinks are unavailable in this environment: %v", err)
	}
	repo.commit("add skill with symlink")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha"))
	})
	message := requireError(t, out, err)
	requireContains(t, message, "symlink")
	// The link must never be followed: the target's content must not appear.
	requireNotContains(t, message, "root:")
}

func TestAddSecondSelectorExtendsMatchingBlockWithoutAdvancingIt(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	first := repo.commit("add skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha"))
	})
	requireNoError(t, out, err)

	repo.writeSkill("skills/alpha", "alpha", "Alpha, revised upstream")
	second := repo.commit("revise alpha")
	if first == second {
		t.Fatal("expected a second commit")
	}

	out, err = p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/beta"))
	})
	requireNoError(t, out, err)

	// Adding a selector resolves the new skill at the block's locked commit; it
	// must not smuggle in an unrelated upstream advance for the existing skill.
	if got := p.entry("alpha").SourceCommit; got != first {
		t.Fatalf("alpha advanced to %q during add; want the locked commit %q", got, first)
	}
	if got := p.entry("beta").SourceCommit; got != first {
		t.Fatalf("beta imported at %q; want the block's locked commit %q", got, first)
	}
	body, _ := p.readSkillFile("alpha", SkillManifestName)
	requireContains(t, body, "Alpha body"[:5])
	requireNotContains(t, body, "revised upstream")

	// One block, two selectors: matching policy extends rather than duplicates.
	if strings.Count(p.configText(), "[[skills.imports]]") != 1 {
		t.Fatalf("expected one import block, got:\n%s", p.configText())
	}
}

func TestAddWithDifferentPolicyCreatesSecondBlock(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.commit("add skills")
	repo.tag("v1")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha"))
	})
	requireNoError(t, out, err)

	options := addOptions(repo, "skills/beta")
	options.Ref = "v1"
	out, err = p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	if strings.Count(p.configText(), "[[skills.imports]]") != 2 {
		t.Fatalf("a different ref needs its own block, got:\n%s", p.configText())
	}
	if got := p.entry("beta").Tracking; got != config.SkillTrackingPinned {
		t.Fatalf("a tag import must pin, got %q", got)
	}
	if got := p.entry("beta").ResolvedRefType; got != config.SkillRefTag {
		t.Fatalf("resolved ref type = %q, want %q", got, config.SkillRefTag)
	}
}

func TestAddExclusionOnlyRequiresExistingBlock(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "!skills/alpha"))
	})
	message := requireError(t, out, err)
	requireContains(t, message, "exclusion-only add must extend an existing import block")
	requireNotContains(t, p.configText(), "skills.imports")
}

func TestAddRejectsDuplicateRepositoryAndSelectorPair(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha"))
	})
	requireNoError(t, out, err)

	out, err = p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha"))
	})
	message := requireError(t, out, err)
	// Uniqueness is what lets `al skills remove` name exactly one selector.
	requireContains(t, message, "already configured")
}

func TestAddBlockedByUserManagedSkillOfTheSameName(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")

	p := newProject(t)
	p.writeUserSkill("alpha")

	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha"))
	})
	message := requireError(t, out, err)
	requireContains(t, message, "user-managed skill already owns the name")
	if _, statErr := os.Stat(p.skillDir("alpha")); !os.IsNotExist(statErr) {
		t.Fatalf("the blocked import must not have been materialized")
	}
}

func TestAddRejectsTwoSourcesResolvingToOneSkillName(t *testing.T) {
	hermeticGitEnv(t)
	first := newSourceRepo(t, "main")
	first.writeSkill("skills/alpha", "alpha", "First alpha")
	first.commit("add alpha")

	second := newSourceRepo(t, "main")
	second.writeSkill("vendor/alpha", "alpha", "Second alpha")
	second.commit("add alpha")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(first, "skills/alpha"))
	})
	requireNoError(t, out, err)

	out, err = p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(second, "vendor/alpha"))
	})
	message := requireError(t, out, err)
	// One local directory cannot have two owners, whatever the sync history.
	requireContains(t, message, "collides")
	requireContains(t, message, "one local directory cannot have two owners")
	// The first import survives untouched.
	body, ok := p.readSkillFile("alpha", SkillManifestName)
	if !ok || !strings.Contains(body, "First alpha") {
		t.Fatalf("the existing import was disturbed by a failed add: %q", body)
	}
}

func TestAddRejectsAncestorAndDescendantSelectors(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/alpha/nested", "nested", "Nested")
	repo.commit("add nested skills")

	p := newProject(t)
	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha", "skills/alpha/nested"))
	})
	message := requireError(t, out, err)
	requireContains(t, message, "overlap")
	requireContains(t, message, "two editable owners")
}

func TestAddPinnedCommitRef(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "First")
	first := repo.commit("first")
	repo.writeSkill("skills/alpha", "alpha", "Second")
	repo.commit("second")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Ref = first
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	requireNoError(t, out, err)

	entry := p.entry("alpha")
	if entry.SourceCommit != first {
		t.Fatalf("locked commit = %q, want the pinned commit %q", entry.SourceCommit, first)
	}
	if entry.Tracking != config.SkillTrackingPinned {
		t.Fatalf("a commit ref must pin, got %q", entry.Tracking)
	}
	body, _ := p.readSkillFile("alpha", SkillManifestName)
	requireContains(t, body, "First")
}

func TestAddRejectsTrackingATag(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.commit("add alpha")
	repo.tag("v1")

	p := newProject(t)
	options := addOptions(repo, "skills/alpha")
	options.Ref = "v1"
	options.Tracking = config.SkillTrackingTracked
	out, err := p.run(func(s *Service) error { return s.Add(ctx(t), options) })
	message := requireError(t, out, err)
	requireContains(t, message, "requires a branch")
}

func TestAddPreservesUnrelatedConfigComments(t *testing.T) {
	hermeticGitEnv(t)
	repo := newSourceRepo(t, "main")
	repo.writeSkill("skills/alpha", "alpha", "Alpha")
	repo.writeSkill("skills/beta", "beta", "Beta")
	repo.commit("add skills")

	p := newProject(t)
	p.writeConfig("# A comment the user wrote and expects to keep.\n" + minimalConfig)

	out, err := p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/alpha"))
	})
	requireNoError(t, out, err)
	out, err = p.run(func(s *Service) error {
		return s.Add(ctx(t), addOptions(repo, "skills/beta"))
	})
	requireNoError(t, out, err)

	text := p.configText()
	requireContains(t, text, "# A comment the user wrote and expects to keep.")
	requireContains(t, text, `mode = "all"`)
	requireContains(t, text, "skills/beta")
}
