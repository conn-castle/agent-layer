package skillimport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/gitrepo"
)

// TestStatusReportsLocalStateWithoutNetworkAccess proves status classifies each
// imported skill from local files only. The service is given a runner
// constructor that fails, so any attempt to contact a remote would fail the
// test rather than pass silently.
func TestStatusReportsLocalStateWithoutNetworkAccess(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.WriteSkill("skills/gone", "gone", "Gone body")
	source.WriteSkill("skills/broken", "broken", "Broken body")
	source.WriteSkill("skills/skipped", "skipped", "Skipped body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*", "!skills/skipped"},
		`write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}

	proj.WriteImportedFile("beta", "local.md", "modified\n")
	if err := os.RemoveAll(filepath.Join(proj.paths.ImportedSkillsDir, "gone")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	proj.WriteImportedFile("broken", "SKILL.md", "not a skill\n")

	service := proj.Service()
	service.newRunner = func(map[string]string) (*gitrepo.Runner, error) {
		return nil, errors.New("status must not contact a remote")
	}
	status, err := service.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}

	conditions := map[string]Condition{}
	for _, entry := range status.Entries {
		conditions[entry.Name] = entry.Condition
	}
	want := map[string]Condition{
		"alpha":  ConditionClean,
		"beta":   ConditionModified,
		"gone":   ConditionMissing,
		"broken": ConditionInvalid,
	}
	for name, condition := range want {
		if conditions[name] != condition {
			t.Fatalf("%s condition = %q, want %q", name, conditions[name], condition)
		}
	}
	if _, imported := conditions["skipped"]; imported {
		t.Fatal("an excluded skill was imported")
	}

	summary := status.Render(false)
	for _, fragment := range []string{"4 total", "1 clean", "1 modified", "1 missing", "1 invalid", "4 tracked", "4 write-enabled", "configured exclusions: 1"} {
		if !strings.Contains(summary, fragment) {
			t.Fatalf("summary %q does not contain %q", summary, fragment)
		}
	}
	if strings.Contains(summary, "exclusion\t") {
		t.Fatalf("the default summary expanded per-entry detail:\n%s", summary)
	}

	expanded := status.Render(true)
	for _, fragment := range []string{"alpha\tclean", "beta\tmodified", "exclusion\t"} {
		if !strings.Contains(expanded, fragment) {
			t.Fatalf("--all output %q does not contain %q", expanded, fragment)
		}
	}
}

// TestStatusReportsMissingRefEvidence proves status never guesses a ref kind
// offline: a configured block with no recorded state is reported instead.
func TestStatusReportsMissingRefEvidence(t *testing.T) {
	proj := newProject(t)
	proj.AppendConfig(importBlock("https://example.test/skills.git", []string{"skills/alpha"}))

	service := proj.Service()
	service.newRunner = func(map[string]string) (*gitrepo.Runner, error) {
		return nil, errors.New("status must not contact a remote")
	}
	status, err := service.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.MissingRefEvidence) != 1 {
		t.Fatalf("missing ref evidence = %v", status.MissingRefEvidence)
	}
	if !strings.Contains(status.Render(false), "configured exclusions: 0") {
		t.Fatalf("summary = %q", status.Render(false))
	}
	if !strings.Contains(status.Render(true), "al skills pull") {
		t.Fatalf("expanded output does not direct the user to pull:\n%s", status.Render(true))
	}
}

// TestStatusMapsAManuallyReplacedSelectorToItsUniqueBlock proves offline status
// keeps reporting the current policy when one unambiguous wildcard replaces an
// entry's recorded exact selector.
func TestStatusMapsAManuallyReplacedSelectorToItsUniqueBlock(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.ReplaceInConfig(`selectors = ["skills/alpha"]`, `selectors = ["skills/*"]`)

	status, err := proj.Service().Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Entries) != 1 || !status.Entries[0].WriteEnabled || status.Entries[0].WritePolicy != config.SkillWritePolicyDirect {
		t.Fatalf("status lost the current block policy: %+v", status.Entries)
	}
	if len(status.MissingRefEvidence) != 0 {
		t.Fatalf("status treated the remapped entry as missing evidence: %v", status.MissingRefEvidence)
	}
}

// TestStatusDetectsUserManagedCollision proves an ownership collision is
// surfaced by status rather than only failing at projection time.
func TestStatusDetectsUserManagedCollision(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteUserSkill("alpha")

	status, err := proj.Service().Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Entries) != 1 || status.Entries[0].Condition != ConditionCollided {
		t.Fatalf("entries = %+v, want one collided entry", status.Entries)
	}
}

// TestConcurrentImportOperationsSerialize proves import operations and status
// share the project lock: concurrent callers observe one consistent snapshot
// rather than interleaving reads and mutations.
func TestConcurrentImportOperationsSerialize(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))

	var wait sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 2; i++ {
		wait.Add(2)
		go func() {
			defer wait.Done()
			if _, err := proj.Service().Pull(context.Background()); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wait.Done()
			if _, err := proj.Service().Status(); err != nil {
				errs <- err
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent operation failed: %v", err)
	}

	if _, ok := proj.Lock().Entry("alpha"); !ok {
		t.Fatal("concurrent operations lost lock state")
	}
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha body") {
		t.Fatal("concurrent operations produced an inconsistent imported tree")
	}
}

// TestReportDistinguishesPartialFromTotalFailure proves the reporting contract
// callers exit on.
func TestReportDistinguishesPartialFromTotalFailure(t *testing.T) {
	t.Parallel()
	success := &Report{Skills: []SkillResult{{Name: "a", Outcome: OutcomeImported}}}
	if success.Failed() || success.Partial() {
		t.Fatal("a fully successful report reported failure")
	}
	if !strings.Contains(success.Render("al skills pull"), "succeeded: 1 skills") {
		t.Fatalf("render = %q", success.Render("al skills pull"))
	}

	partial := &Report{Skills: []SkillResult{
		{Name: "a", Outcome: OutcomeImported},
		{Name: "b", Outcome: OutcomeFailed, Err: errors.New("boom")},
	}}
	if !partial.Failed() || !partial.Partial() {
		t.Fatal("a mixed report is not reported as partial")
	}
	if !strings.Contains(partial.Render("al skills pull"), "partially succeeded: 1 of 2") {
		t.Fatalf("render = %q", partial.Render("al skills pull"))
	}

	total := &Report{Skills: []SkillResult{{Name: "a", Outcome: OutcomeFailed, Err: errors.New("boom")}}}
	if total.Partial() || !total.Failed() {
		t.Fatal("a total failure was reported as partial")
	}

	sourceOnly := &Report{}
	sourceOnly.AddSourceFailure("repo", "", errors.New("unreachable"))
	if !sourceOnly.Failed() {
		t.Fatal("a source failure did not fail the report")
	}
	if !strings.Contains(sourceOnly.Render("al skills pull"), "(default branch)") {
		t.Fatalf("render = %q", sourceOnly.Render("al skills pull"))
	}

	projection := &Report{Skills: []SkillResult{{Name: "a", Outcome: OutcomeImported}}, ProjectionErr: errors.New("write failed")}
	if !projection.Failed() {
		t.Fatal("a projection failure did not fail the report")
	}
	if !strings.Contains(projection.Render("al skills pull"), "after source state was committed") {
		t.Fatalf("render = %q", projection.Render("al skills pull"))
	}
}

// TestReportKeepsOneResultPerSkill proves a skill touched by several stages of
// one operation renders exactly one final line, while two skills that merely
// share a name keep their own lines.
func TestReportKeepsOneResultPerSkill(t *testing.T) {
	t.Parallel()
	report := &Report{}
	report.Add(SkillResult{Name: "alpha", Repository: "https://one.test/s.git", SelectedPath: "skills/alpha", Outcome: OutcomeImported})
	report.Add(SkillResult{Name: "alpha", Repository: "https://one.test/s.git", SelectedPath: "skills/alpha", Outcome: OutcomeUpdated})
	report.Add(SkillResult{Name: "beta", Repository: "https://one.test/s.git", SelectedPath: "skills/beta", Outcome: OutcomeImported})
	if len(report.Skills) != 2 {
		t.Fatalf("skills = %+v, want one entry per skill", report.Skills)
	}
	if got := resultFor(t, report, "https://one.test/s.git", "skills/alpha"); got.Outcome != OutcomeUpdated {
		t.Fatalf("later stage outcome = %q, want the earlier result replaced", got.Outcome)
	}

	// A second block whose distinct path resolves to the same name is a
	// different unit of work. Collapsing the two would let its rejection
	// silently overwrite the first block's success.
	report.Add(SkillResult{Name: "alpha", Repository: "https://two.test/s.git", SelectedPath: "vendor/alpha", Outcome: OutcomeFailed})
	if len(report.Skills) != 3 {
		t.Fatalf("skills = %+v, want a separate line per repository and path", report.Skills)
	}
	if got := resultFor(t, report, "https://one.test/s.git", "skills/alpha"); got.Outcome != OutcomeUpdated {
		t.Fatalf("a same-named failure overwrote another block's result: %+v", got)
	}
}

// TestPushReportsDestinationFailureForEveryGroupedSkill proves a destination
// that cannot be opened fails every skill routed to it, not just the first.
func TestPushReportsDestinationFailureForEveryGroupedSkill(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")

	proj := newProject(t)
	missing := filepath.Join(t.TempDir(), "absent-destination")
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`,
		`push_repository = "`+missing+`"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "alpha note\n")
	proj.WriteImportedFile("beta", "notes.md", "beta note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeFailed)
	requireOutcome(t, report, "beta", OutcomeFailed)
}

// TestStatusPreservesLocalStateWhenTheImportedTierIsUnreadable proves an
// unreadable imported skill fails only itself and is reported as invalid.
func TestStatusPreservesLocalStateWhenTheImportedTierIsUnreadable(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(proj.paths.ImportedSkillsDir, "alpha", "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	status, err := proj.Service().Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Entries) != 1 || status.Entries[0].Condition != ConditionInvalid {
		t.Fatalf("entries = %+v, want one invalid entry", status.Entries)
	}

	// The same skill fails only itself on pull, and its content survives.
	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha body") {
		t.Fatal("an invalid import lost its local content")
	}
}

// TestLocalSkillValidReportsReadability proves the observed-state helper
// distinguishes a present, readable skill from an absent or broken one.
func TestLocalSkillValidReportsReadability(t *testing.T) {
	t.Parallel()
	if (localSkill{}).Valid() {
		t.Fatal("an absent skill reported as valid")
	}
	if (localSkill{Present: true, Err: errors.New("broken")}).Valid() {
		t.Fatal("an unreadable skill reported as valid")
	}
	if !(localSkill{Present: true}).Valid() {
		t.Fatal("a present readable skill reported as invalid")
	}
}

// TestUserManagedNamesRejectNFKCCollisions proves filesystem enumeration never
// lets a later compatibility-equivalent directory silently replace the first
// collision path in the ownership map.
func TestUserManagedNamesRejectNFKCCollisions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := filepath.Join(dir, "1")
	second := filepath.Join(dir, "①")
	if err := os.Mkdir(first, 0o750); err != nil {
		t.Fatalf("mkdir first: %v", err)
	}
	if err := os.Mkdir(second, 0o750); err != nil {
		t.Fatalf("mkdir second: %v", err)
	}
	_, err := readUserSkillNames(dir)
	if err == nil {
		t.Fatal("NFKC-equivalent skill directories were accepted")
	}
	if !strings.Contains(err.Error(), first) || !strings.Contains(err.Error(), second) {
		t.Fatalf("error %q does not identify both colliding paths", err)
	}
}

// TestPullFailsASkillWhoseRecordedBaseNoLongerMatches proves a lock entry whose
// hash disagrees with its recorded commit is refused rather than used as a
// merge base, and that local content survives.
func TestPullFailsASkillWhoseRecordedBaseNoLongerMatches(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	// Corrupt the recorded upstream hash and advance upstream so a merge is
	// attempted.
	lock := proj.Lock()
	entry, _ := lock.Entry("alpha")
	entry.TreeHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	lock.Upsert(entry)
	if err := lock.Save(proj.paths.SkillsLockPath); err != nil {
		t.Fatalf("save lock: %v", err)
	}
	source.WriteSkill("skills/alpha", "alpha", "Alpha advanced")
	source.Commit("advance")

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "does not match commit") {
		t.Fatalf("error = %q", failed.Err)
	}
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha body") {
		t.Fatal("local content was replaced despite an untrustworthy merge base")
	}
}

// TestPullRetiresSkillsWhoseBlockWasDeletedFromConfiguration proves removing an
// import block by hand still applies the one retirement rule.
func TestPullRetiresSkillsWhoseBlockWasDeletedFromConfiguration(t *testing.T) {
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

	writeProjectFile(t, proj.paths.ConfigPath, baseConfigTOML)

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeRetired)
	requireOutcome(t, report, "beta", OutcomeFailed)
	if proj.ImportedExists("alpha") {
		t.Fatal("a clean skill with no configured block was not retired")
	}
	if !proj.ImportedExists("beta") {
		t.Fatal("a modified skill with no configured block was deleted")
	}
}

// TestPullReconcilesWildcardMembershipWhileAdvancing proves a tracked wildcard
// recomputes membership at the new commit: a new valid root is imported and a
// disappeared one retires, in the same operation that advances the rest.
func TestPullReconcilesWildcardMembershipWhileAdvancing(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/gone", "gone", "Gone body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	source.RemovePath("skills/gone")
	source.WriteSkill("skills/added", "added", "Added body")
	source.WriteSkill("skills/alpha", "alpha", "Alpha advanced")
	source.Commit("evolve the wildcard membership")

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("Pull: %v\n%s", err, report.Render("pull"))
	}
	requireOutcome(t, report, "added", OutcomeImported)
	requireOutcome(t, report, "gone", OutcomeRetired)
	requireOutcome(t, report, "alpha", OutcomeUpdated)
	if proj.ImportedExists("gone") || !proj.ImportedExists("added") {
		t.Fatal("wildcard membership was not recomputed at the new commit")
	}
}

// TestReportSortsSourcesAndSkillsDeterministically proves identical state
// always renders in the same order.
func TestReportSortsSourcesAndSkillsDeterministically(t *testing.T) {
	t.Parallel()
	report := &Report{}
	report.AddSourceFailure("zeta", "main", errors.New("z"))
	report.AddSourceFailure("alpha", "v2", errors.New("a2"))
	report.AddSourceFailure("alpha", "v1", errors.New("a1"))
	report.Add(SkillResult{Name: "zulu", Repository: "https://one.test/s.git", SelectedPath: "skills/zulu", Outcome: OutcomeImported})
	report.Add(SkillResult{Name: "alpha", Repository: "https://two.test/s.git", SelectedPath: "vendor/alpha", Outcome: OutcomeFailed})
	report.Add(SkillResult{Name: "alpha", Repository: "https://one.test/s.git", SelectedPath: "skills/alpha", Outcome: OutcomeImported})
	report.Sort()

	if report.Sources[0].Repository != "alpha" || report.Sources[0].Ref != "v1" {
		t.Fatalf("sources = %+v", report.Sources)
	}
	if report.Sources[1].Ref != "v2" || report.Sources[2].Repository != "zeta" {
		t.Fatalf("sources = %+v", report.Sources)
	}
	// Two skills share the name "alpha", so the name alone is not a total
	// order; the repository and selected path break the tie.
	if report.Skills[0].Name != "alpha" || report.Skills[0].Repository != "https://one.test/s.git" {
		t.Fatalf("skills = %+v", report.Skills)
	}
	if report.Skills[1].Name != "alpha" || report.Skills[1].Repository != "https://two.test/s.git" {
		t.Fatalf("skills = %+v", report.Skills)
	}
	if report.Skills[2].Name != "zulu" {
		t.Fatalf("skills = %+v", report.Skills)
	}
}

// TestAddReportsAProjectionFailureWithoutDiscardingSourceState proves a
// projection failure after a successful source-state commit is reported but
// never rolls that valid state back.
func TestAddReportsAProjectionFailureWithoutDiscardingSourceState(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	// A regular file where the projection root must be a directory makes
	// projection fail after the import has already been committed.
	writeProjectFile(t, filepath.Join(proj.root, ".claude"), "not a directory")

	report, err := proj.Service().Add(context.Background(), AddOptions{
		Repository: source.URL(),
		Selectors:  []string{"skills/alpha"},
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if report.ProjectionErr == nil {
		t.Fatalf("expected a projection failure: %s", report.Render("add"))
	}
	if !report.Failed() {
		t.Fatal("a projection failure did not fail the operation")
	}
	requireOutcome(t, report, "alpha", OutcomeImported)

	// The committed source state survives.
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha body") {
		t.Fatal("a projection failure discarded the imported skill")
	}
	if _, ok := proj.Lock().Entry("alpha"); !ok {
		t.Fatal("a projection failure discarded lock state")
	}
	if !strings.Contains(proj.ConfigContent(), "skills/alpha") {
		t.Fatal("a projection failure discarded the configuration change")
	}
}

// TestStatusReportsSkillsWhoseBlockIsNoLongerConfigured proves recorded state
// still renders when its configuration block was removed by hand, so the user
// can see what `al skills pull` is about to retire.
func TestStatusReportsSkillsWhoseBlockIsNoLongerConfigured(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	writeProjectFile(t, proj.paths.ConfigPath, baseConfigTOML)

	status, err := proj.Service().Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Entries) != 1 {
		t.Fatalf("entries = %+v, want the orphaned recorded skill", status.Entries)
	}
	entry := status.Entries[0]
	if entry.Condition != ConditionClean {
		t.Fatalf("condition = %q, want clean", entry.Condition)
	}
	if entry.WriteEnabled || entry.WritePolicy != "none" {
		t.Fatalf("an unconfigured skill reported a write policy: %+v", entry)
	}
	if !strings.Contains(status.Render(false), "0 write-enabled") {
		t.Fatalf("summary = %q", status.Render(false))
	}
}

// TestStatusReportsASkillWhoseSelectorLeftConfiguration proves status stays a
// complete local view. A hand-edited `config.toml` can drop a selector without
// running `al skills remove`, and the imported directory and its lock entry
// still exist; status must list that skill and report it as not write-enabled
// rather than omitting it.
func TestStatusReportsASkillWhoseSelectorLeftConfiguration(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}

	// The block is edited away by hand; the import stays on disk and in the lock.
	writeProjectFile(t, proj.paths.ConfigPath, baseConfigTOML)

	status, err := proj.Service().Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Entries) != 1 || status.Entries[0].Name != "alpha" {
		t.Fatalf("entries = %+v, want the still-recorded alpha import", status.Entries)
	}
	entry := status.Entries[0]
	if entry.WriteEnabled || entry.WritePolicy != config.SkillWritePolicyNone {
		t.Fatalf("an unconfigured import reported write policy %q (enabled=%v)", entry.WritePolicy, entry.WriteEnabled)
	}
	if entry.Condition != ConditionClean {
		t.Fatalf("condition = %q, want clean", entry.Condition)
	}
}
