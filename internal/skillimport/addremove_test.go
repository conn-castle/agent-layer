package skillimport

import (
	"context"
	"strings"
	"testing"
)

// TestAddCreatesBlockPerformsInitialPullAndProjects proves `al skills add`
// writes exactly one configuration block, imports the selected skills, records
// lock state, and projects — all in one atomic step.
func TestAddCreatesBlockPerformsInitialPullAndProjects(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	report, err := proj.Service().Add(context.Background(), AddOptions{
		Repository: source.URL(),
		Selectors:  []string{"skills/alpha"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeImported)

	content := proj.ConfigContent()
	if strings.Count(content, "[[skills.imports]]") != 1 {
		t.Fatalf("expected exactly one import block:\n%s", content)
	}
	if !strings.Contains(content, "skills/alpha") {
		t.Fatalf("selector not recorded:\n%s", content)
	}
	if _, ok := proj.Lock().Entry("alpha"); !ok {
		t.Fatal("add did not record lock state")
	}
	if _, ok := proj.ProjectedFile("alpha", "SKILL.md"); !ok {
		t.Fatal("add did not project the imported skill")
	}
}

// TestAddExtendsAMatchingBlockAndRejectsDuplicates proves a second add with the
// same policy extends the existing block rather than creating a second one, and
// that a repeated selector is refused so `al skills remove` stays unambiguous.
func TestAddExtendsAMatchingBlockAndRejectsDuplicates(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")

	proj := newProject(t)
	service := proj.Service()
	if _, err := service.Add(context.Background(), AddOptions{Repository: source.URL(), Selectors: []string{"skills/alpha"}}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	report, err := service.Add(context.Background(), AddOptions{Repository: source.URL(), Selectors: []string{"skills/beta"}})
	if err != nil {
		t.Fatalf("second add: %v", err)
	}
	requireOutcome(t, report, "beta", OutcomeImported)
	requireOutcome(t, report, "alpha", OutcomeUnchanged)

	content := proj.ConfigContent()
	if strings.Count(content, "[[skills.imports]]") != 1 {
		t.Fatalf("a matching policy created a second block:\n%s", content)
	}

	if _, err := service.Add(context.Background(), AddOptions{Repository: source.URL(), Selectors: []string{"skills/beta"}}); err == nil {
		t.Fatal("expected a duplicate selector to be refused")
	}
}

// TestAddAndRemoveRejectMalformedPatternsBeforeMutation proves both CLI-backed
// selector edit paths fail before changing configuration, content, or lock
// evidence when a wildcard cannot be parsed.
func TestAddAndRemoveRejectMalformedPatternsBeforeMutation(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	service := proj.Service()
	if _, err := service.Add(context.Background(), AddOptions{Repository: source.URL(), Selectors: []string{"skills/alpha"}}); err != nil {
		t.Fatalf("initial add: %v", err)
	}
	configBefore := proj.ConfigContent()
	lockBefore := proj.LockContent()
	contentBefore := proj.ImportedFile("alpha", "SKILL.md")

	if _, err := service.Add(context.Background(), AddOptions{Repository: source.URL(), Selectors: []string{"skills/[a"}}); err == nil || !strings.Contains(err.Error(), "invalid wildcard pattern") {
		t.Fatalf("malformed add error = %v", err)
	}
	if _, err := service.Remove(context.Background(), source.URL(), "!skills/[a"); err == nil || !strings.Contains(err.Error(), "invalid wildcard pattern") {
		t.Fatalf("malformed remove error = %v", err)
	}
	if proj.ConfigContent() != configBefore || proj.LockContent() != lockBefore || proj.ImportedFile("alpha", "SKILL.md") != contentBefore {
		t.Fatal("malformed selector changed configuration, lock evidence, or local content")
	}
}

// TestAddWithDifferentPolicyCreatesASeparateBlock proves selectors that need a
// different policy live in their own block.
func TestAddWithDifferentPolicyCreatesASeparateBlock(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")
	source.Tag("v1.0.0")

	proj := newProject(t)
	service := proj.Service()
	if _, err := service.Add(context.Background(), AddOptions{Repository: source.URL(), Selectors: []string{"skills/alpha"}}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if _, err := service.Add(context.Background(), AddOptions{
		Repository: source.URL(),
		Selectors:  []string{"skills/beta"},
		Ref:        "v1.0.0",
	}); err != nil {
		t.Fatalf("second add: %v", err)
	}
	if got := strings.Count(proj.ConfigContent(), "[[skills.imports]]"); got != 2 {
		t.Fatalf("blocks = %d, want 2:\n%s", got, proj.ConfigContent())
	}
	beta, ok := proj.Lock().Entry("beta")
	if !ok || beta.Tracking != "pinned" || beta.ConfiguredRef != "v1.0.0" {
		t.Fatalf("beta lock = %+v, want a pinned v1.0.0 import", beta)
	}
}

// TestAddWildcardResolvingToNothingChangesNoState proves an actionable failure
// leaves configuration, lock state, and the imported tier untouched.
func TestAddWildcardResolvingToNothingChangesNoState(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteFile("docs/README.md", "no skills here\n", 0o644)
	source.Commit("no skills")

	proj := newProject(t)
	before := proj.ConfigContent()

	if _, err := proj.Service().Add(context.Background(), AddOptions{
		Repository: source.URL(),
		Selectors:  []string{"docs/*"},
	}); err == nil {
		t.Fatal("expected a wildcard resolving to zero valid skills to fail")
	}
	if proj.ConfigContent() != before {
		t.Fatal("a failed add modified configuration")
	}
	if proj.Lock() != nil {
		t.Fatal("a failed add wrote lock state")
	}
}

// TestAddExactSelectorForAnInvalidSkillFails proves an exact selector that does
// not resolve to a valid skill is an actionable error rather than a silent skip.
func TestAddExactSelectorForAnInvalidSkillFails(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteFile("skills/broken/README.md", "no manifest\n", 0o644)
	source.WriteFile("skills/invalid/SKILL.md", "---\nname: other\ndescription: d\n---\nBody\n", 0o644)
	source.Commit("add candidates")

	proj := newProject(t)
	for _, selector := range []string{"skills/broken", "skills/invalid", "skills/absent"} {
		if _, err := proj.Service().Add(context.Background(), AddOptions{
			Repository: source.URL(),
			Selectors:  []string{selector},
		}); err == nil {
			t.Fatalf("selector %q was accepted", selector)
		}
	}
}

// TestAddExclusionOnlyRequiresAnExistingBlock proves an exclusion never imports
// a skill by itself.
func TestAddExclusionOnlyRequiresAnExistingBlock(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")

	proj := newProject(t)
	service := proj.Service()
	if _, err := service.Add(context.Background(), AddOptions{
		Repository: source.URL(),
		Selectors:  []string{"!skills/beta"},
	}); err == nil {
		t.Fatal("an exclusion-only addition created a block")
	}

	if _, err := service.Add(context.Background(), AddOptions{Repository: source.URL(), Selectors: []string{"skills/*"}}); err != nil {
		t.Fatalf("wildcard add: %v", err)
	}
	report, err := service.Add(context.Background(), AddOptions{Repository: source.URL(), Selectors: []string{"!skills/beta"}})
	if err != nil {
		t.Fatalf("exclusion add: %v\n%s", err, report.Render("add"))
	}
	requireOutcome(t, report, "beta", OutcomeRetired)
	if proj.ImportedExists("beta") {
		t.Fatal("an excluded clean skill was not retired")
	}
	if !proj.ImportedExists("alpha") {
		t.Fatal("an exclusion retired an unrelated skill")
	}
}

// TestRemoveSelectorRetiresAndKeepsOthers proves removing one selector removes
// only the skills that leave the desired set and drops the block when no
// positive selector remains.
func TestRemoveSelectorRetiresAndKeepsOthers(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")

	proj := newProject(t)
	service := proj.Service()
	if _, err := service.Add(context.Background(), AddOptions{
		Repository: source.URL(),
		Selectors:  []string{"skills/alpha", "skills/beta"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	report, err := service.Remove(context.Background(), source.URL(), "skills/beta")
	if err != nil {
		t.Fatalf("remove: %v\n%s", err, report.Render("remove"))
	}
	requireOutcome(t, report, "beta", OutcomeRetired)
	if proj.ImportedExists("beta") {
		t.Fatal("removed selector's skill was not retired")
	}
	if !proj.ImportedExists("alpha") {
		t.Fatal("removing one selector retired an unrelated skill")
	}
	if _, ok := proj.ProjectedFile("beta", "SKILL.md"); ok {
		t.Fatal("removal did not reproject without the retired skill")
	}

	report, err = service.Remove(context.Background(), source.URL(), "skills/alpha")
	if err != nil {
		t.Fatalf("second remove: %v\n%s", err, report.Render("remove"))
	}
	if strings.Contains(proj.ConfigContent(), "[[skills.imports]]") {
		t.Fatalf("a block with no positive selector survived:\n%s", proj.ConfigContent())
	}
	if len(proj.Lock().Skills) != 0 {
		t.Fatalf("lock still records retired skills: %+v", proj.Lock().Skills)
	}
}

// TestRemoveExclusionRevealsSkillAtCurrentTarget proves newly revealed
// membership is imported immediately from the operation's resolved target.
func TestRemoveExclusionRevealsSkillAtCurrentTarget(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta at lock time")
	source.Commit("add skills")

	proj := newProject(t)
	service := proj.Service()
	if _, err := service.Add(context.Background(), AddOptions{
		Repository: source.URL(),
		Selectors:  []string{"skills/*", "!skills/beta"},
	}); err != nil {
		t.Fatalf("add: %v", err)
	}

	// Upstream moves on; removing the exclusion resolves the new member at the
	// current target without moving the already imported sibling as a side effect.
	source.WriteSkill("skills/beta", "beta", "Beta after lock")
	advanced := source.Commit("advance")

	report, err := service.Remove(context.Background(), source.URL(), "!skills/beta")
	if err != nil {
		t.Fatalf("remove exclusion: %v\n%s", err, report.Render("remove"))
	}
	requireOutcome(t, report, "beta", OutcomeImported)
	if !strings.Contains(proj.ImportedFile("beta", "SKILL.md"), "Beta after lock") {
		t.Fatalf("revealed skill was not imported from the current target: %q", proj.ImportedFile("beta", "SKILL.md"))
	}
	entry, _ := proj.Lock().Entry("beta")
	if entry.Commit != advanced {
		t.Fatalf("revealed skill locked at %s, want current target %s", entry.Commit, advanced)
	}
}

// TestRemoveModifiedSkillLeavesPriorStateUnchanged proves a retirement failure
// during remove aborts the whole operation rather than half-applying it.
func TestRemoveModifiedSkillLeavesPriorStateUnchanged(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	service := proj.Service()
	if _, err := service.Add(context.Background(), AddOptions{Repository: source.URL(), Selectors: []string{"skills/alpha"}}); err != nil {
		t.Fatalf("add: %v", err)
	}
	proj.WriteImportedFile("alpha", "local.md", "work in progress\n")
	before := proj.ConfigContent()

	if _, err := service.Remove(context.Background(), source.URL(), "skills/alpha"); err == nil {
		t.Fatal("expected removing a modified skill to fail")
	}
	if proj.ConfigContent() != before {
		t.Fatal("a failed remove modified configuration")
	}
	if !proj.ImportedExists("alpha") {
		t.Fatal("a failed remove deleted local work")
	}
	if _, ok := proj.Lock().Entry("alpha"); !ok {
		t.Fatal("a failed remove pruned lock state")
	}
}

// TestRemoveUnknownSelectorFails proves the command identifies exactly one
// configured selector or refuses.
func TestRemoveUnknownSelectorFails(t *testing.T) {
	proj := newProject(t)
	if _, err := proj.Service().Remove(context.Background(), "https://example.test/absent.git", "skills/alpha"); err == nil {
		t.Fatal("expected an unknown selector to fail")
	}
}

// TestAddRejectsOverlappingSelectedPaths proves ancestor and descendant skill
// roots from one repository are refused because they would create overlapping
// editable owners.
func TestAddRejectsOverlappingSelectedPaths(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/outer", "outer", "Outer body")
	source.WriteSkill("skills/outer/inner", "inner", "Inner body")
	source.Commit("add nested skills")

	proj := newProject(t)
	_, err := proj.Service().Add(context.Background(), AddOptions{
		Repository: source.URL(),
		Selectors:  []string{"skills/outer", "skills/outer/inner"},
	})
	if err == nil {
		t.Fatal("expected overlapping selected paths to be rejected")
	}
	if !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("error %q does not explain the overlap", err)
	}
}

// TestAddRejectsDistinctPathsResolvingToOneName proves a normalized-name
// collision is a deterministic configuration error.
func TestAddRejectsDistinctPathsResolvingToOneName(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("one/alpha", "alpha", "First")
	source.WriteSkill("two/alpha", "alpha", "Second")
	source.Commit("add duplicate names")

	proj := newProject(t)
	_, err := proj.Service().Add(context.Background(), AddOptions{
		Repository: source.URL(),
		Selectors:  []string{"one/alpha", "two/alpha"},
	})
	if err == nil {
		t.Fatal("expected a name collision to be rejected")
	}
	if !strings.Contains(err.Error(), "resolve to skill name") {
		t.Fatalf("error %q does not explain the name collision", err)
	}
}

// TestAddRejectsANameAlreadyOwnedByAnotherBlock proves identity rules are
// enforced across the complete managed set, not just within the block being
// changed.
func TestAddRejectsANameAlreadyOwnedByAnotherBlock(t *testing.T) {
	first := newGitRepo(t, "main")
	first.WriteSkill("skills/alpha", "alpha", "First alpha")
	first.Commit("add alpha")

	second := newGitRepo(t, "main")
	second.WriteSkill("shared/alpha", "alpha", "Second alpha")
	second.Commit("add alpha")

	proj := newProject(t)
	service := proj.Service()
	if _, err := service.Add(context.Background(), AddOptions{Repository: first.URL(), Selectors: []string{"skills/alpha"}}); err != nil {
		t.Fatalf("first add: %v", err)
	}

	_, err := service.Add(context.Background(), AddOptions{Repository: second.URL(), Selectors: []string{"shared/alpha"}})
	if err == nil {
		t.Fatal("expected a cross-block name collision to be rejected")
	}
	if !strings.Contains(err.Error(), "resolve to skill name") {
		t.Fatalf("error %q does not explain the collision", err)
	}
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "First alpha") {
		t.Fatal("the rejected add replaced the existing import")
	}
	if strings.Count(proj.ConfigContent(), "[[skills.imports]]") != 1 {
		t.Fatalf("the rejected add wrote a block:\n%s", proj.ConfigContent())
	}
}

// TestPushGroupsTwoSourcesIntoOneDestinationCommit proves skills from different
// source blocks that share a destination are published together and that
// neither one's lock advances, since the destination matches no single tracked
// source ref exactly.
func TestPushGroupsTwoSourcesIntoOneDestinationCommit(t *testing.T) {
	first := newGitRepo(t, "main")
	first.WriteSkill("skills/alpha", "alpha", "Alpha body")
	first.WriteFile("skills/alpha/reference.md", "alpha reference\n", 0o644)
	first.Commit("add alpha")

	second := newGitRepo(t, "main")
	second.WriteSkill("skills/beta", "beta", "Beta body")
	second.WriteFile("skills/beta/reference.md", "beta reference\n", 0o644)
	second.Commit("add beta")

	destination := newGitRepo(t, "main")
	destination.WriteFile("destination-only.md", "destination base\n", 0o644)
	destination.Commit("add destination-only base content")

	proj := newProject(t)
	proj.AppendConfig(importBlock(first.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`, `push_repository = "`+destination.URL()+`"`))
	proj.AppendConfig(importBlock(second.URL(), []string{"skills/beta"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`, `push_repository = "`+destination.URL()+`"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "alpha note\n")
	proj.WriteImportedFile("beta", "notes.md", "beta note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v\n%s", err, report.Render("push"))
	}
	alpha := requireOutcome(t, report, "alpha", OutcomePushed)
	beta := requireOutcome(t, report, "beta", OutcomePushed)
	if alpha.Detail != beta.Detail {
		t.Fatalf("skills sharing a destination were not grouped: %q vs %q", alpha.Detail, beta.Detail)
	}
	for _, result := range []SkillResult{alpha, beta} {
		if strings.Contains(result.Detail, "lock advanced") {
			t.Fatalf("%s advanced its lock for a shared foreign destination: %q", result.Name, result.Detail)
		}
	}
	// The published trees must be complete skills. A destination branch created
	// from the destination default does not carry either source's skill, so an
	// unguarded merge would read every unchanged file as a destination deletion
	// and commit a skill with the added note but no manifest or resources.
	for _, published := range []struct {
		path      string
		body      string
		reference string
	}{
		{"skills/alpha", "Alpha body", "alpha reference\n"},
		{"skills/beta", "Beta body", "beta reference\n"},
	} {
		manifest := destination.FileAt("skill-updates", published.path+"/SKILL.md")
		if !strings.Contains(manifest, published.body) {
			t.Fatalf("%s was published without its manifest content: %q", published.path, manifest)
		}
		if got := destination.FileAt("skill-updates", published.path+"/reference.md"); got != strings.TrimSpace(published.reference) {
			t.Fatalf("%s lost an unchanged resource: %q", published.path, got)
		}
	}
	if got := destination.FileAt("skill-updates", "skills/alpha/notes.md"); got != "alpha note" {
		t.Fatalf("alpha not published: %q", got)
	}
	if got := destination.FileAt("skill-updates", "skills/beta/notes.md"); got != "beta note" {
		t.Fatalf("beta not published: %q", got)
	}
	if got := destination.FileAt("skill-updates", "destination-only.md"); got != "destination base" {
		t.Fatalf("the new branch did not start from the destination default: %q", got)
	}
}
