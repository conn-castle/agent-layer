package skillimport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilllock"
)

// importBlock renders one configuration block for a test fixture.
func importBlock(repository string, selectors []string, extra ...string) string {
	quoted := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		quoted = append(quoted, fmt.Sprintf("%q", selector))
	}
	block := fmt.Sprintf("\n[[skills.imports]]\nrepository = %q\nselectors = [%s]\n",
		repository, strings.Join(quoted, ", "))
	for _, line := range extra {
		block += line + "\n"
	}
	return block
}

// TestPullImportsResolvesAndProjects proves the primary path end to end against
// a real local repository: exact and wildcard selectors resolve, the complete
// tree (including hidden and executable resources) lands in the imported tier,
// lock state records the resolved source, and ordinary projection runs.
func TestPullImportsResolvesAndProjects(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/scripts/run.sh", "#!/bin/sh\necho alpha\n", 0o755)
	source.WriteFile("skills/alpha/.hidden", "hidden\n", 0o644)
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.WriteFile("skills/notaskill/README.md", "not a skill\n", 0o644)
	commit := source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v\n%s", err, report.Render("pull"))
	}
	if report.Failed() {
		t.Fatalf("pull failed: %s", report.Render("pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeImported)
	requireOutcome(t, report, "beta", OutcomeImported)
	if len(report.Skills) != 2 {
		t.Fatalf("a wildcard imported an ordinary directory: %s", report.Render("pull"))
	}

	if got := proj.ImportedFile("alpha", ".hidden"); got != "hidden\n" {
		t.Fatalf("hidden resource = %q", got)
	}
	info, err := os.Stat(filepath.Join(proj.paths.ImportedSkillsDir, "alpha", "scripts", "run.sh"))
	if err != nil {
		t.Fatalf("stat imported script: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("imported script lost its executable bit: %v", info.Mode())
	}

	lock := proj.Lock()
	if lock == nil {
		t.Fatal("pull did not write lock state")
	}
	entry, ok := lock.Entry("alpha")
	if !ok {
		t.Fatalf("lock has no alpha entry: %+v", lock.Skills)
	}
	if entry.Commit != commit || entry.ResolvedRef != "main" || entry.RefKind != skilllock.RefKindBranch {
		t.Fatalf("lock entry = %+v, want commit %s on branch main", entry, commit)
	}
	if entry.Tracking != "tracked" {
		t.Fatalf("an omitted tracking mode on a branch resolved to %q, want tracked", entry.Tracking)
	}
	if entry.ConfiguredRef != "" {
		t.Fatalf("configured ref = %q, want the omitted value preserved", entry.ConfiguredRef)
	}

	if _, ok := proj.ProjectedFile("alpha", "SKILL.md"); !ok {
		t.Fatal("pull did not project the imported skill")
	}
	if _, ok := proj.ProjectedFile("alpha", "scripts/run.sh"); !ok {
		t.Fatal("pull did not project the imported skill's resources")
	}
}

// TestPullMergesUpstreamAndLocalChanges proves a tracked advance merges the
// locked base, local edits, and new upstream content without discarding local
// work, and advances the lock even though local modifications remain.
func TestPullMergesUpstreamAndLocalChanges(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "line one\nline two\nline three\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	// Local edit to the last line only.
	proj.WriteImportedFile("alpha", "notes.md", "line one\nline two\nlocal three\n")
	// Upstream edit to the first line only.
	source.WriteFile("skills/alpha/notes.md", "upstream one\nline two\nline three\n", 0o644)
	source.WriteFile("skills/alpha/added.md", "new upstream file\n", 0o644)
	advanced := source.Commit("advance alpha")

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("second pull: %v\n%s", err, report.Render("pull"))
	}
	result := requireOutcome(t, report, "alpha", OutcomeUpdated)
	if !strings.Contains(result.Detail, "local modifications retained") {
		t.Fatalf("update detail = %q, want a note that local modifications were retained", result.Detail)
	}

	merged := proj.ImportedFile("alpha", "notes.md")
	if merged != "upstream one\nline two\nlocal three\n" {
		t.Fatalf("merged content = %q; both one-sided changes must apply", merged)
	}
	if got := proj.ImportedFile("alpha", "added.md"); got != "new upstream file\n" {
		t.Fatalf("new upstream file not applied: %q", got)
	}

	entry, _ := proj.Lock().Entry("alpha")
	if entry.Commit != advanced {
		t.Fatalf("lock commit = %s, want the advanced upstream commit %s", entry.Commit, advanced)
	}
}

// TestPullConflictPreservesLocalContentAndLock proves an incompatible change
// fails only that skill, leaves local content untouched, and does not advance
// its lock.
func TestPullConflictPreservesLocalContentAndLock(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	lockedAlpha, _ := proj.Lock().Entry("alpha")

	proj.WriteImportedFile("alpha", "notes.md", "local change\n")
	source.WriteFile("skills/alpha/notes.md", "upstream change\n", 0o644)
	source.WriteSkill("skills/beta", "beta", "Beta body updated")
	source.Commit("diverge")

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull returned a fatal error for a per-skill conflict: %v", err)
	}
	if !report.Partial() {
		t.Fatalf("expected partial success, got: %s", report.Render("pull"))
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "notes.md") {
		t.Fatalf("conflict error %q does not name the conflicted path", failed.Err)
	}
	requireOutcome(t, report, "beta", OutcomeUpdated)

	if got := proj.ImportedFile("alpha", "notes.md"); got != "local change\n" {
		t.Fatalf("conflicted skill's local content was overwritten: %q", got)
	}
	entry, _ := proj.Lock().Entry("alpha")
	if entry.Commit != lockedAlpha.Commit {
		t.Fatalf("conflicted skill's lock advanced from %s to %s", lockedAlpha.Commit, entry.Commit)
	}
	advancedBeta, _ := proj.Lock().Entry("beta")
	if advancedBeta.Commit == entry.Commit {
		t.Fatal("partial success did not leave independent per-skill commits")
	}

	// A later pull discovers a new wildcard member at the current target while
	// alpha remains independently blocked. No block-level base is manufactured
	// from alpha's older commit or beta's newer one.
	source.WriteSkill("skills/beta", "beta", "Beta body updated again")
	source.WriteSkill("skills/gamma", "gamma", "Gamma body")
	current := source.Commit("advance beta and add gamma")
	report, err = proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("second conflicted pull: %v\n%s", err, report.Render("pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeFailed)
	requireOutcome(t, report, "beta", OutcomeUpdated)
	requireOutcome(t, report, "gamma", OutcomeImported)
	for _, name := range []string{"beta", "gamma"} {
		locked, _ := proj.Lock().Entry(name)
		if locked.Commit != current {
			t.Fatalf("%s locked at %s, want current target %s", name, locked.Commit, current)
		}
	}
	stillBlocked, _ := proj.Lock().Entry("alpha")
	if stillBlocked.Commit != lockedAlpha.Commit {
		t.Fatalf("alpha moved during another member's convergence: %s", stillBlocked.Commit)
	}

	// The user resolves alpha by restoring its locked base. Its next pull then
	// converges independently, while the already-current siblings stay put.
	proj.WriteImportedFile("alpha", "notes.md", "shared\n")
	report, err = proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("convergence pull: %v\n%s", err, report.Render("pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeUpdated)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		locked, _ := proj.Lock().Entry(name)
		if locked.Commit != current {
			t.Fatalf("%s did not converge independently at %s: %+v", name, current, locked)
		}
	}
}

// TestPullRetiresCleanAndPreservesModifiedDisappearances proves the one
// retirement rule: a clean skill that leaves the desired set is deleted, and a
// modified one is preserved and reported as a failure with instructions.
func TestPullRetiresCleanAndPreservesModifiedDisappearances(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	proj.WriteImportedFile("beta", "local.md", "work in progress\n")

	source.RemovePath("skills/alpha")
	source.RemovePath("skills/beta")
	source.Commit("remove skills upstream")

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeRetired)
	preserved := requireOutcome(t, report, "beta", OutcomeFailed)
	if !strings.Contains(preserved.Err.Error(), "adopt it as user-managed") {
		t.Fatalf("retirement failure %q lacks adoption instructions", preserved.Err)
	}

	if proj.ImportedExists("alpha") {
		t.Fatal("a clean retired skill was not deleted")
	}
	if !proj.ImportedExists("beta") {
		t.Fatal("a modified retired skill was deleted instead of preserved")
	}
	if _, ok := proj.Lock().Entry("beta"); !ok {
		t.Fatal("a preserved skill lost its lock entry")
	}
}

// TestPullRestoresMissingImportedDirectory proves a desired skill whose
// directory was deleted is rebuilt from the selected source and reported.
func TestPullRestoresMissingImportedDirectory(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(proj.paths.ImportedSkillsDir, "alpha")); err != nil {
		t.Fatalf("remove imported directory: %v", err)
	}

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeRestored)
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha body") {
		t.Fatal("restored skill does not carry its source content")
	}
	if source.HasPath("HEAD", "skills/alpha/SKILL.md") == false {
		t.Fatal("restoration must never delete the upstream skill")
	}
}

// TestPullAdoptionPrunesLockAndSkipsRestoration proves the documented adoption
// flow: after a skill is moved into the user-managed tier and its selector no
// longer matches, the stale lock entry is pruned and the imported collision is
// never recreated.
func TestPullAdoptionPrunesLockAndSkipsRestoration(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	// Adoption step one: move the skill into the user-managed tier.
	proj.WriteUserSkill("alpha")
	if err := os.RemoveAll(filepath.Join(proj.paths.ImportedSkillsDir, "alpha")); err != nil {
		t.Fatalf("remove imported directory: %v", err)
	}

	// While the selector still matches, the missing directory must not be
	// restored on top of the user-managed skill; the incomplete adoption is
	// reported instead.
	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("pull during incomplete adoption: %v", err)
	}
	incomplete := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(incomplete.Err.Error(), "narrow the selector") {
		t.Fatalf("incomplete adoption error %q does not offer the adoption resolution", incomplete.Err)
	}
	if proj.ImportedExists("alpha") {
		t.Fatal("restoration recreated the imported collision")
	}
	// The stale lock entry is pruned as soon as the directory is absent and the
	// same-name user-managed skill exists.
	if _, ok := proj.Lock().Entry("alpha"); ok {
		t.Fatal("adoption did not prune the stale lock entry")
	}

	// Adoption step two: narrow the import so it no longer matches.
	writeProjectFile(t, proj.paths.ConfigPath, baseConfigTOML)

	report, err = proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("pull after adoption: %v\n%s", err, report.Render("pull"))
	}
	if report.Failed() {
		t.Fatalf("completed adoption still reports failure: %s", report.Render("pull"))
	}
	if proj.ImportedExists("alpha") {
		t.Fatal("adoption recreated the imported collision")
	}
	if content, ok := proj.ProjectedFile("alpha", "SKILL.md"); !ok || !strings.Contains(content, "User body") {
		t.Fatalf("the adopted user-managed skill does not project: ok=%v content=%q", ok, content)
	}
}

// TestPullBlocksImportOfAUserManagedName proves an existing user-managed skill
// blocks an import of the same name instead of shadowing either source.
func TestPullBlocksImportOfAUserManagedName(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.WriteUserSkill("alpha")
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	blocked := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(blocked.Err.Error(), "already owns the name") {
		t.Fatalf("collision error = %q", blocked.Err)
	}
	if proj.ImportedExists("alpha") {
		t.Fatal("a blocked import created an imported directory")
	}
}

// TestPullPinnedRefStaysStationaryAndRetargets proves a pinned tag does not
// move when upstream advances, and that changing the configured ref reconciles
// local edits onto the newly selected version.
func TestPullPinnedRefStaysStationaryAndRetargets(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha v1")
	source.WriteFile("skills/alpha/notes.md", "one\ntwo\nthree\nfour\nfive\n", 0o644)
	v1 := source.Commit("v1")
	source.Tag("v1.0.0")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `ref = "v1.0.0"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	entry, _ := proj.Lock().Entry("alpha")
	if entry.RefKind != skilllock.RefKindTag || entry.Tracking != "pinned" {
		t.Fatalf("a tag resolved to %+v, want a pinned tag", entry)
	}

	source.WriteFile("skills/alpha/notes.md", "one\ntwo\nthree\nfour\nupstream five\n", 0o644)
	source.Commit("v2")
	source.Tag("v2.0.0")

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("pinned pull: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeUnchanged)
	pinned, _ := proj.Lock().Entry("alpha")
	if pinned.Commit != v1 {
		t.Fatalf("pinned lock moved from %s to %s", v1, pinned.Commit)
	}

	// Retarget: a local edit is reconciled onto the newly selected version.
	proj.WriteImportedFile("alpha", "notes.md", "local one\ntwo\nthree\nfour\nfive\n")
	retargeted := strings.Replace(proj.ConfigContent(), `ref = "v1.0.0"`, `ref = "v2.0.0"`, 1)
	writeProjectFile(t, proj.paths.ConfigPath, retargeted)

	report, err = proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("retarget pull: %v\n%s", err, report.Render("pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeUpdated)
	if got := proj.ImportedFile("alpha", "notes.md"); got != "local one\ntwo\nthree\nfour\nupstream five\n" {
		t.Fatalf("retarget did not reconcile local edits onto the new version: %q", got)
	}
	after, _ := proj.Lock().Entry("alpha")
	if after.ConfiguredRef != "v2.0.0" || after.ResolvedRef != "v2.0.0" {
		t.Fatalf("lock identity after retarget = %+v", after)
	}
}

// TestPinnedWildcardImportsNewMembershipWithoutMovingExistingPins proves the
// block is only shorthand: a newly discovered member pins at today's target
// while an existing member keeps its own older pin.
func TestPinnedWildcardImportsNewMembershipWithoutMovingExistingPins(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha one")
	first := source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}, `ref = "main"`, `tracking = "pinned"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	source.WriteSkill("skills/alpha", "alpha", "Alpha two")
	source.WriteSkill("skills/beta", "beta", "Beta one")
	second := source.Commit("advance alpha and add beta")
	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("second pull: %v\n%s", err, report.Render("pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeUnchanged)
	requireOutcome(t, report, "beta", OutcomeImported)
	alpha, _ := proj.Lock().Entry("alpha")
	beta, _ := proj.Lock().Entry("beta")
	if alpha.Commit != first || beta.Commit != second {
		t.Fatalf("independent pins = alpha %s beta %s, want %s and %s", alpha.Commit, beta.Commit, first, second)
	}
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha one") {
		t.Fatal("discovering beta moved alpha's pinned content")
	}

	proj.ReplaceInConfig(`selectors = ["skills/*"]`, `selectors = ["skills/*", "!skills/alpha"]`)
	report, err = proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("pull after excluding an older pin: %v\n%s", err, report.Render("pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeRetired)
	if proj.ImportedExists("alpha") {
		t.Fatal("an excluded pinned member remained imported")
	}
	if remaining, _ := proj.Lock().Entry("beta"); remaining.Commit != second {
		t.Fatalf("retiring alpha moved beta's pin: %+v", remaining)
	}
}

// TestPullRejectsExplicitTrackedNonBranch proves a tracked tag is an actionable
// error rather than a silent downgrade to pinning.
func TestPullRejectsExplicitTrackedNonBranch(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")
	source.Tag("v1.0.0")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `ref = "v1.0.0"`, `tracking = "tracked"`))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(report.Sources) != 1 {
		t.Fatalf("expected one source failure: %s", report.Render("pull"))
	}
	if !strings.Contains(report.Sources[0].Err.Error(), "requires a branch") {
		t.Fatalf("source failure = %v", report.Sources[0].Err)
	}
}

// TestPullContinuesAfterOneSourceFails proves a source-level failure blocks
// only its own block while other sources still import and project.
func TestPullContinuesAfterOneSourceFails(t *testing.T) {
	good := newGitRepo(t, "main")
	good.WriteSkill("skills/alpha", "alpha", "Alpha body")
	good.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(good.URL(), []string{"skills/alpha"}))
	proj.AppendConfig(importBlock(filepath.Join(t.TempDir(), "missing-repo"), []string{"skills/beta"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(report.Sources) != 1 {
		t.Fatalf("expected exactly one source failure: %s", report.Render("pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeImported)
	// Every skill the failed source blocked must appear in the report; the
	// source line alone leaves the blocked skill unaccounted for.
	requireOutcome(t, report, "beta", OutcomeFailed)
	if !report.Partial() {
		t.Fatalf("expected partial success: %s", report.Render("pull"))
	}
	if !proj.ImportedExists("alpha") {
		t.Fatal("a healthy source did not import while another failed")
	}
}

// TestPullReportsEveryAlreadyImportedSkillBlockedByASourceFailure proves the
// recorded members of a failed block are reported as failed rather than
// silently omitted or, worse, mistaken for skills that left the desired set.
func TestPullReportsEveryAlreadyImportedSkillBlockedByASourceFailure(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	// The configured source becomes unreachable after the skills were imported.
	if err := os.Rename(source.dir, source.dir+"-moved"); err != nil {
		t.Fatalf("move the source repository: %v", err)
	}

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(report.Sources) != 1 {
		t.Fatalf("expected one source failure: %s", report.Render("pull"))
	}
	for _, name := range []string{"alpha", "beta"} {
		requireOutcome(t, report, name, OutcomeFailed)
		if !proj.ImportedExists(name) {
			t.Fatalf("a source failure deleted %s", name)
		}
	}
}

// TestPullFailsOnMalformedLockWithoutTouchingContent proves a lockfile that
// cannot establish a merge base fails loudly and preserves local content.
func TestPullFailsOnMalformedLockWithoutTouchingContent(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	writeProjectFile(t, proj.paths.SkillsLockPath, `{"version":1,"skills":[{"name":"alpha"}]}`)

	if _, err := proj.Service().Pull(context.Background()); err == nil {
		t.Fatal("expected a malformed lock to fail the operation")
	}
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha body") {
		t.Fatal("a malformed lock destroyed local content")
	}
}

// TestPullRejectsOrphanImportedDirectories proves a directory Agent Layer does
// not own blocks operations with actionable instructions.
func TestPullRejectsOrphanImportedDirectories(t *testing.T) {
	proj := newProject(t)
	dir := filepath.Join(proj.paths.ImportedSkillsDir, "stray")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeProjectFile(t, filepath.Join(dir, "SKILL.md"), skillManifest("stray", "Body"))

	_, err := proj.Service().Pull(context.Background())
	if err == nil {
		t.Fatal("expected an orphan imported directory to fail the operation")
	}
	if !strings.Contains(err.Error(), "adopt it as user-managed") {
		t.Fatalf("orphan error %q lacks adoption instructions", err)
	}
}

// TestPullRejectsUnsafeUpstreamNodes proves an imported skill containing a
// symlink is refused rather than dereferenced.
func TestPullRejectsUnsafeUpstreamNodes(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/target.md", "secret\n", 0o644)
	if err := os.Symlink("target.md", filepath.Join(source.dir, "skills", "alpha", "link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	source.Commit("add alpha with a link")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "symbolic link") {
		t.Fatalf("expected a symlink rejection: %s", report.Render("pull"))
	}
	if proj.ImportedExists("alpha") {
		t.Fatal("an unsafe skill was imported")
	}
}

// TestPullDefaultBranchRenameIsARetarget proves an omitted ref re-resolves the
// default branch on every pull and treats a rename as a retarget.
func TestPullDefaultBranchRenameIsARetarget(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	source.run("branch", "--move", "main", "trunk")
	source.WriteSkill("skills/alpha", "alpha", "Alpha renamed")
	renamed := source.Commit("advance on trunk")

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v\n%s", err, report.Render("pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeUpdated)
	entry, _ := proj.Lock().Entry("alpha")
	if entry.ResolvedRef != "trunk" || entry.Commit != renamed {
		t.Fatalf("lock after default-branch rename = %+v, want trunk @ %s", entry, renamed)
	}
}

// TestPullDeduplicatesOnePathMatchedBySeveralSelectors proves a source path
// matched by more than one positive selector becomes exactly one lock entry,
// recorded against the first matching selector in configuration order.
func TestPullDeduplicatesOnePathMatchedBySeveralSelectors(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha", "skills/*"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v\n%s", err, report.Render("pull"))
	}
	if len(report.Skills) != 1 {
		t.Fatalf("skills = %+v, want one deduplicated entry", report.Skills)
	}
	entry, ok := proj.Lock().Entry("alpha")
	if !ok {
		t.Fatal("the deduplicated skill has no lock entry")
	}
	if entry.Selector != "skills/alpha" {
		t.Fatalf("recorded selector = %q, want the first matching selector", entry.Selector)
	}
	if len(proj.Lock().Skills) != 1 {
		t.Fatalf("lock = %+v, want one entry", proj.Lock().Skills)
	}
}

// TestPullExclusionsApplyBeforeValidation proves an excluded path is outside the
// desired set and is never validated as an import, so an invalid directory the
// user excluded does not fail the block.
func TestPullExclusionsApplyBeforeValidation(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/broken/SKILL.md", "---\nname: wrong-name\ndescription: d\n---\nBody\n", 0o644)
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*", "!skills/broken"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v\n%s", err, report.Render("pull"))
	}
	if report.Failed() {
		t.Fatalf("an excluded invalid directory failed the block: %s", report.Render("pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeImported)
	if proj.ImportedExists("broken") {
		t.Fatal("an excluded directory was imported")
	}
}

// TestPullFailsAWildcardMatchWithAnInvalidManifest proves a matched directory
// that does carry a SKILL.md must be valid; it is an actionable error, not a
// silent skip. The failure is scoped to that skill: a validation failure blocks
// only its own skill, so the block's unaffected skills still import.
func TestPullFailsAWildcardMatchWithAnInvalidManifest(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/broken/SKILL.md", "---\nname: wrong-name\ndescription: d\n---\nBody\n", 0o644)
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(report.Sources) != 0 {
		t.Fatalf("one invalid skill was promoted to a source failure: %s", report.Render("pull"))
	}
	failed := requireOutcome(t, report, "broken", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "must match canonical source name") {
		t.Fatalf("failure = %v", failed.Err)
	}
	requireOutcome(t, report, "alpha", OutcomeImported)
	if !proj.ImportedExists("alpha") {
		t.Fatal("an unrelated valid skill was blocked by another skill's invalid manifest")
	}
	if proj.ImportedExists("broken") {
		t.Fatal("an invalid skill was imported")
	}
	if _, ok := proj.Lock().Entry("broken"); ok {
		t.Fatal("an invalid skill was recorded in the lock")
	}
	if !report.Partial() {
		t.Fatalf("expected partial success: %s", report.Render("pull"))
	}
}

// TestPullPreservesAPreviouslyImportedSkillThatBecomesInvalidUpstream proves
// upstream invalidity is never treated as upstream removal: the local
// directory and its lock entry survive and only that skill fails.
func TestPullPreservesAPreviouslyImportedSkillThatBecomesInvalidUpstream(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	locked, ok := proj.Lock().Entry("beta")
	if !ok {
		t.Fatal("beta was not imported")
	}

	source.WriteFile("skills/beta/SKILL.md", "---\nname: renamed\ndescription: d\n---\nBody\n", 0o644)
	source.WriteSkill("skills/alpha", "alpha", "Alpha advanced")
	source.Commit("break beta upstream")

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v\n%s", err, report.Render("pull"))
	}
	requireOutcome(t, report, "beta", OutcomeFailed)
	requireOutcome(t, report, "alpha", OutcomeUpdated)
	if !strings.Contains(proj.ImportedFile("beta", "SKILL.md"), "Beta body") {
		t.Fatal("an upstream validation failure destroyed local content")
	}
	after, ok := proj.Lock().Entry("beta")
	if !ok || after.Commit != locked.Commit {
		t.Fatal("an upstream validation failure was treated as upstream removal")
	}
}

// TestPullImportsNewSelectorMembershipAtCurrentTarget proves a manually added
// selector creates its own lock directly at the operation's resolved target.
func TestPullImportsNewSelectorMembershipAtCurrentTarget(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha at lock time")
	source.WriteSkill("skills/beta", "beta", "Beta at lock time")
	locked := source.Commit("initial")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	// Upstream advances, and the user adds a selector by hand for a skill that
	// already existed at the locked commit.
	source.WriteSkill("skills/alpha", "alpha", "Alpha advanced")
	source.WriteSkill("skills/beta", "beta", "Beta advanced")
	advanced := source.Commit("advance both skills")
	writeProjectFile(t, proj.paths.ConfigPath,
		baseConfigTOML+importBlock(source.URL(), []string{"skills/alpha", "skills/beta"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v\n%s", err, report.Render("pull"))
	}
	requireOutcome(t, report, "alpha", OutcomeUpdated)
	requireOutcome(t, report, "beta", OutcomeImported)

	if got := proj.ImportedFile("beta", "SKILL.md"); !strings.Contains(got, "Beta advanced") {
		t.Fatalf("beta content = %q, want the advanced upstream body", got)
	}
	for _, name := range []string{"alpha", "beta"} {
		entry, ok := proj.Lock().Entry(name)
		if !ok {
			t.Fatalf("%s has no lock entry", name)
		}
		if entry.Commit != advanced {
			t.Fatalf("%s locked at %s, want the advanced commit %s", name, entry.Commit, advanced)
		}
	}
	if locked == advanced {
		t.Fatal("the fixture did not actually advance upstream")
	}
}

// TestPullRejectsAMergeResultThatIsNoLongerAValidSkill proves a merge whose
// output would not validate fails that skill instead of publishing an
// unusable tree.
func TestPullRejectsAMergeResultThatIsNoLongerAValidSkill(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteFile("skills/alpha/SKILL.md", skillManifest("alpha", "Alpha body"), 0o644)
	source.WriteFile("skills/alpha/notes.md", "one\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	// The local side deletes the manifest while upstream changes an unrelated
	// file, so the one-sided deletion merges cleanly into an invalid skill.
	if err := os.Remove(filepath.Join(proj.paths.ImportedSkillsDir, "alpha", "SKILL.md")); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	source.WriteFile("skills/alpha/notes.md", "two\n", 0o644)
	source.Commit("advance")

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if failed.Err == nil {
		t.Fatal("the failure carries no diagnostic")
	}
	entry, _ := proj.Lock().Entry("alpha")
	if entry.TreeHash == "" {
		t.Fatal("the failing skill lost its lock entry")
	}
}

// TestPullPreservesALocallyInvalidImportedSkill proves a locally broken import
// fails only itself: its content and lock entry survive so the user can repair
// or adopt it, unaffected skills still advance, and projection reports the
// broken tree instead of publishing a lossy copy of it.
func TestPullPreservesALocallyInvalidImportedSkill(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	locked, _ := proj.Lock().Entry("beta")

	proj.WriteImportedFile("beta", "SKILL.md", "no frontmatter at all\n")
	source.WriteSkill("skills/alpha", "alpha", "Alpha advanced")
	source.Commit("advance alpha")

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	requireOutcome(t, report, "beta", OutcomeFailed)
	requireOutcome(t, report, "alpha", OutcomeUpdated)
	if got := proj.ImportedFile("beta", "SKILL.md"); got != "no frontmatter at all\n" {
		t.Fatalf("local content = %q, want the untouched local edit", got)
	}
	if after, _ := proj.Lock().Entry("beta"); after.Commit != locked.Commit {
		t.Fatal("a locally invalid skill lost its recorded merge base")
	}
	if report.ProjectionErr == nil {
		t.Fatalf("projection accepted a locally invalid imported skill: %s", report.Render("pull"))
	}
}

// TestPullRecordsATrackingPolicyChange proves a configuration change that only
// moves the tracking mode is written through to the lock.
//
// Nothing else would ever apply it: content and commit are unchanged, so the
// pull takes its "already at the target state" path. A lock left saying
// "tracked" keeps status reporting the superseded policy, keeps push running
// tracked-source freshness checks, and lets a direct push advance a lock the
// user has pinned.
func TestPullRecordsATrackingPolicyChange(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `ref = "main"`, `tracking = "tracked"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("first pull: %v", err)
	}
	entry, ok := proj.Lock().Entry("alpha")
	if !ok || entry.Tracking != config.SkillTrackingTracked {
		t.Fatalf("first pull recorded tracking %q, want tracked", entry.Tracking)
	}

	proj.ReplaceInConfig(`tracking = "tracked"`, `tracking = "pinned"`)
	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("second pull: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeUnchanged)

	entry, ok = proj.Lock().Entry("alpha")
	if !ok {
		t.Fatal("the lock entry disappeared")
	}
	if entry.Tracking != config.SkillTrackingPinned {
		t.Fatalf("lock tracking = %q, want pinned after the configuration change", entry.Tracking)
	}
}

// TestPullValidatesAcrossImportBlocks proves one block cannot stage an import
// that collides with another block's skill.
//
// Configuration validation cannot see this: two distinct selectors resolving to
// the same skill name is remote-dependent, so it is only knowable once both
// sources are resolved. Without the cross-block check the first block imports
// and the second fails on a local-directory conflict, and the later failed
// result also replaces the earlier successful one in the report.
func TestPullValidatesAcrossImportBlocks(t *testing.T) {
	first := newGitRepo(t, "main")
	first.WriteSkill("skills/alpha", "alpha", "First alpha")
	first.Commit("add first alpha")

	second := newGitRepo(t, "main")
	second.WriteSkill("vendor/alpha", "alpha", "Second alpha")
	second.Commit("add second alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(first.URL(), []string{"skills/alpha"}))
	proj.AppendConfig(importBlock(second.URL(), []string{"vendor/alpha"}))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if !report.Failed() {
		t.Fatalf("a cross-block name collision was accepted:\n%s", report.Render("al skills pull"))
	}

	// The first block's import is intact and still reported as its own result:
	// the second block's rejection names the same skill, so a name-keyed report
	// would have overwritten the success with the failure.
	imported := resultFor(t, report, first.URL(), "skills/alpha")
	if imported.Outcome != OutcomeImported {
		t.Fatalf("first block outcome = %q (%v), want imported", imported.Outcome, imported.Err)
	}
	rejected := resultFor(t, report, second.URL(), "vendor/alpha")
	if rejected.Outcome != OutcomeFailed {
		t.Fatalf("second block outcome = %q, want failed", rejected.Outcome)
	}
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "First alpha") {
		t.Fatal("the first block's import was not preserved")
	}
	entry, ok := proj.Lock().Entry("alpha")
	if !ok || entry.Repository != first.URL() {
		t.Fatalf("lock entry = %+v, want the first block's import", entry)
	}
	// The second block is blocked as a whole, naming the identity collision.
	if len(report.Sources) != 1 || !strings.Contains(report.Sources[0].Err.Error(), "resolve to skill name") {
		t.Fatalf("source failures = %+v, want one naming the name collision", report.Sources)
	}
}

// TestPullPreservesAnEntryWhoseReplacementSelectorOwnershipIsAmbiguous proves
// cross-block validation fails without retiring the prior independent entry
// merely because no synthetic block identity can be assigned to it.
func TestPullPreservesAnEntryWhoseReplacementSelectorOwnershipIsAmbiguous(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	before, _ := proj.Lock().Entry("alpha")
	writeProjectFile(t, proj.paths.ConfigPath,
		baseConfigTOML+
			importBlock(source.URL(), []string{"skills/*"})+
			importBlock(source.URL(), []string{"skills/a*"}, `write_policy = "direct"`))

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if !report.Failed() {
		t.Fatalf("ambiguous selector ownership was accepted:\n%s", report.Render("pull"))
	}
	after, ok := proj.Lock().Entry("alpha")
	if !ok || after != before || !proj.ImportedExists("alpha") {
		t.Fatalf("ambiguous ownership retired prior state: before=%+v after=%+v exists=%v", before, after, proj.ImportedExists("alpha"))
	}
}

// TestPullRetiresAnUnconfiguredSkillDespiteASameNamedFailure proves retirement
// is decided per repository and selected path rather than per skill name.
//
// Two blocks can resolve different paths to the same name. Skipping retirement
// whenever the *name* already appears in the report let one block's failure
// keep another block's unconfigured entry in the lock indefinitely.
func TestPullRetiresAnUnconfiguredSkillDespiteASameNamedFailure(t *testing.T) {
	retiring := newGitRepo(t, "main")
	retiring.WriteSkill("skills/alpha", "alpha", "Retiring alpha")
	retiring.Commit("add retiring alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(retiring.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("first pull: %v", err)
	}
	if _, ok := proj.Lock().Entry("alpha"); !ok {
		t.Fatal("the first pull recorded no entry")
	}

	// The selector leaves configuration, and an unreachable block that resolves
	// the same name takes its place.
	unreachable := filepath.Join(t.TempDir(), "missing-repository.git")
	proj.ReplaceInConfig(
		"repository = \""+retiring.URL()+"\"",
		"repository = \""+unreachable+"\"")
	proj.ReplaceInConfig(`selectors = ["skills/alpha"]`, `selectors = ["vendor/alpha"]`)

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("second pull: %v", err)
	}
	if _, ok := proj.Lock().Entry("alpha"); ok {
		t.Fatalf("the unconfigured entry was not retired:\n%s", report.Render("al skills pull"))
	}
	if proj.ImportedExists("alpha") {
		t.Fatal("the retired skill directory was not removed")
	}
}

// TestRetireUnconfiguredIgnoresRepositorySpelling proves retirement compares
// normalized keys on both sides.
//
// The configured set is built from `config.toml`, the candidates come from lock
// entries, and the two files are written by different code paths. Keying either
// side on raw text would let a trailing slash or stray whitespace make a
// still-configured skill look unconfigured — and retirement deletes the local
// directory, so a false positive is destructive.
//
// This is exercised at the function rather than through a pull, because
// skilllock.Parse rejects a non-canonical repository before a loaded lock could
// reach here. The rule is asserted directly so retirement stays correct on its
// own terms rather than depending on that separate validator.
func TestRetireUnconfiguredIgnoresRepositorySpelling(t *testing.T) {
	t.Parallel()
	const canonical = "https://example.test/skills.git"

	tests := []struct {
		name            string
		entryRepository string
		wantRetired     bool
	}{
		{name: "canonical spelling", entryRepository: canonical},
		{name: "trailing slash", entryRepository: canonical + "/"},
		{name: "repeated trailing slashes", entryRepository: canonical + "//"},
		{name: "surrounding whitespace", entryRepository: "  " + canonical + "  "},
		// A genuinely different repository is still retired, so the test cannot
		// pass by never retiring anything.
		{name: "different repository", entryRepository: "https://other.test/skills.git", wantRetired: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{}
			cfg.Skills.Imports = []config.SkillImport{{
				Repository: canonical,
				Selectors:  []string{"skills/alpha"},
			}}
			st := &state{
				paths: config.DefaultPaths(t.TempDir()),
				cfg:   cfg,
				lock:  skilllock.New(),
				local: map[string]localSkill{},
			}
			txn := newTransaction(pathSetFor(st), st.lock)
			txn.SetLockEntry(skilllock.Entry{
				Name:         "alpha",
				Repository:   tt.entryRepository,
				Selector:     "skills/alpha",
				SelectedPath: "skills/alpha",
				ResolvedRef:  "main",
				RefKind:      skilllock.RefKindBranch,
				Tracking:     skilllock.TrackingTracked,
				Commit:       strings.Repeat("a", 40),
				TreeHash:     "sha256:" + strings.Repeat("b", 64),
			})

			report := &Report{}
			retireUnconfigured(st, txn, report)

			_, stillLocked := txn.lock.Entry("alpha")
			if tt.wantRetired {
				if stillLocked {
					t.Fatalf("an unconfigured entry survived retirement:\n%s", report.Render("al skills pull"))
				}
				return
			}
			if !stillLocked {
				t.Fatalf("a still-configured skill was retired because of its repository spelling %q:\n%s",
					tt.entryRepository, report.Render("al skills pull"))
			}
			if len(report.Skills) != 0 {
				t.Fatalf("a still-configured skill produced a retirement result: %+v", report.Skills)
			}
		})
	}
}
