package skillimport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResetDiscardsLocalEditsAndAcceptsCurrentUpstream proves reset is the
// explicit destructive escape hatch for a conflicted skill and changes no
// selector membership around it.
func TestResetDiscardsLocalEditsAndAcceptsCurrentUpstream(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha original")
	source.WriteFile("skills/alpha/notes.md", "base\n", 0o644)
	source.WriteSkill("skills/beta", "beta", "Beta original")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local\n")
	source.WriteFile("skills/alpha/notes.md", "upstream\n", 0o644)
	source.WriteSkill("skills/beta", "beta", "Beta newly selectable")
	advanced := source.Commit("advance and change unselected beta")

	report, err := proj.Service().Reset(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("Reset: %v\n%s", err, report.Render("reset"))
	}
	requireOutcome(t, report, "alpha", OutcomeReset)
	if got := proj.ImportedFile("alpha", "notes.md"); got != "upstream\n" {
		t.Fatalf("reset content = %q, want current upstream", got)
	}
	entry, ok := proj.Lock().Entry("alpha")
	if !ok || entry.Commit != advanced {
		t.Fatalf("reset lock = %+v, want commit %s", entry, advanced)
	}
	if proj.ImportedExists("beta") {
		t.Fatal("reset reconciled unrelated selector membership")
	}
}

// TestResetRepinsAPinnedBranch proves reset deliberately moves a pin once,
// while ordinary pull leaves the replacement pinned afterward.
func TestResetRepinsAPinnedBranch(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha one")
	first := source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `ref = "main"`, `tracking = "pinned"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	source.WriteSkill("skills/alpha", "alpha", "Alpha two")
	second := source.Commit("advance alpha")
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pinned pull: %v", err)
	}
	if entry, _ := proj.Lock().Entry("alpha"); entry.Commit != first {
		t.Fatalf("ordinary pull moved pin to %s", entry.Commit)
	}

	if _, err := proj.Service().Reset(context.Background(), "alpha"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if entry, _ := proj.Lock().Entry("alpha"); entry.Commit != second || entry.Tracking != "pinned" {
		t.Fatalf("reset did not repin at current branch: %+v", entry)
	}

	source.WriteSkill("skills/alpha", "alpha", "Alpha three")
	source.Commit("advance again")
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull after reset: %v", err)
	}
	if entry, _ := proj.Lock().Entry("alpha"); entry.Commit != second {
		t.Fatalf("ordinary pull moved the reset pin to %s", entry.Commit)
	}
}

// TestResetUsesTheCurrentSelectorAfterManualReplacement proves a lock entry
// remains addressable when configuration replaces its recorded exact selector
// with one unambiguous wildcard that still selects the same path.
func TestResetUsesTheCurrentSelectorAfterManualReplacement(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha one")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	proj.ReplaceInConfig(`selectors = ["skills/alpha"]`, `selectors = ["skills/*"]`)
	proj.WriteImportedFile("alpha", "local.md", "discard me\n")
	source.WriteSkill("skills/alpha", "alpha", "Alpha two")
	advanced := source.Commit("advance alpha")

	if _, err := proj.Service().Reset(context.Background(), "alpha"); err != nil {
		t.Fatalf("Reset after selector replacement: %v", err)
	}
	if proj.ImportedFile("alpha", "SKILL.md") != skillManifest("alpha", "Alpha two") {
		t.Fatal("reset did not install current upstream content")
	}
	if _, err := os.Stat(filepath.Join(proj.paths.ImportedSkillsDir, "alpha", "local.md")); !os.IsNotExist(err) {
		t.Fatal("reset preserved a discarded local-only file")
	}
	entry, _ := proj.Lock().Entry("alpha")
	if entry.Commit != advanced || entry.Selector != "skills/*" {
		t.Fatalf("reset lock = %+v, want current commit and selector", entry)
	}
}

// TestResetReportsAmbiguousSelectorOwnership proves reset does not mislabel a
// path selected by several current blocks as unconfigured or choose one policy
// arbitrarily.
func TestResetReportsAmbiguousSelectorOwnership(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	before, _ := proj.Lock().Entry("alpha")
	proj.WriteImportedFile("alpha", "local.md", "keep me\n")
	writeProjectFile(t, proj.paths.ConfigPath,
		baseConfigTOML+
			importBlock(source.URL(), []string{"skills/*"})+
			importBlock(source.URL(), []string{"skills/a*"}, `write_policy = "direct"`))

	_, err := proj.Service().Reset(context.Background(), "alpha")
	if err == nil || !strings.Contains(err.Error(), "selected by multiple configured blocks") {
		t.Fatalf("ambiguous reset error = %v", err)
	}
	after, _ := proj.Lock().Entry("alpha")
	if after != before || !strings.Contains(proj.ImportedFile("alpha", "local.md"), "keep me") {
		t.Fatal("ambiguous reset changed local content or lock evidence")
	}
}

// TestResetFailurePreservesLocalContentAndLock proves source validation is a
// preflight: a missing upstream path cannot discard the user's local tree.
func TestResetFailurePreservesLocalContentAndLock(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha original")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	before, _ := proj.Lock().Entry("alpha")
	proj.WriteImportedFile("alpha", "local.md", "keep me\n")
	source.RemovePath("skills/alpha")
	source.Commit("remove alpha")

	report, err := proj.Service().Reset(context.Background(), "alpha")
	if err == nil {
		t.Fatalf("missing upstream path reset successfully:\n%s", report.Render("reset"))
	}
	if !strings.Contains(proj.ImportedFile("alpha", "local.md"), "keep me") {
		t.Fatal("failed reset discarded local content")
	}
	after, _ := proj.Lock().Entry("alpha")
	if after != before {
		t.Fatalf("failed reset changed lock: before=%+v after=%+v", before, after)
	}
}
