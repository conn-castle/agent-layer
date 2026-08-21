package skillimport

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/gitenv"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

// TestPushSkipsBlocksWithoutAWritePolicy proves `none` is the default and
// performs no upstream write.
func TestPushSkipsBlocksWithoutAWritePolicy(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	before := source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "Local body"))

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(report.Skills) != 0 || report.Failed() {
		t.Fatalf("a none write policy produced results: %s", report.Render("push"))
	}
	if source.Head("main") != before {
		t.Fatal("a none write policy pushed upstream")
	}
}

// TestPushDirectPolicyCommitsToTheDestinationDefaultBranch proves a direct push
// publishes the local result and advances the lock when the destination is the
// exact tracked source repository and ref.
func TestPushDirectPolicyCommitsToTheDestinationDefaultBranch(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	locked, _ := proj.Lock().Entry("alpha")

	proj.WriteImportedFile("alpha", "notes.md", "local note\n")
	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v\n%s", err, report.Render("push"))
	}
	result := requireOutcome(t, report, "alpha", OutcomePushed)
	if !strings.Contains(result.Detail, "lock advanced") {
		t.Fatalf("a push to the tracked source ref did not advance the lock: %q", result.Detail)
	}
	if got := source.FileAt("main", "skills/alpha/notes.md"); got != "local note" {
		t.Fatalf("destination content = %q", got)
	}
	advanced, _ := proj.Lock().Entry("alpha")
	if advanced.Commit == locked.Commit {
		t.Fatal("lock did not advance after a successful push to the tracked ref")
	}

	// A second push with no further local change reports unchanged and creates
	// no commit.
	head := source.Head("main")
	report, err = proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("second Push: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeUnchanged)
	if source.Head("main") != head {
		t.Fatal("an unchanged push created a commit")
	}
}

// TestPushBranchUsesThePriorPublicationForFollowUpEdits proves an agent can
// address review feedback by pushing another change to the same PR branch.
// Using the original source tree again would make these edits to the same line
// look like competing local and destination changes.
func TestPushBranchUsesThePriorPublicationForFollowUpEdits(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Original body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}

	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "First review body"))
	first, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("first Push: %v\n%s", err, first.Render("push"))
	}
	requireOutcome(t, first, "alpha", OutcomePushed)
	firstHead := source.Head("skill-updates")
	locked, _ := proj.Lock().Entry("alpha")
	if locked.Publication == nil || locked.Publication.Commit != firstHead {
		t.Fatalf("publication = %+v, want checkpoint at %s", locked.Publication, firstHead)
	}

	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "Second review body"))
	second, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("second Push: %v\n%s", err, second.Render("push"))
	}
	requireOutcome(t, second, "alpha", OutcomePushed)
	if got := source.FileAt("skill-updates", "skills/alpha/SKILL.md"); !strings.Contains(got, "Second review body") {
		t.Fatalf("destination content = %q, want the follow-up edit", got)
	}
	if source.Head("skill-updates") == firstHead {
		t.Fatal("the follow-up push did not advance the contribution branch")
	}
}

func TestPushRecreatesADeletedMergedBranchFromItsPublication(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Original body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`tracking = "pinned"`, `write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "First body"))
	first, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("first Push: %v\n%s", err, first.Render("push"))
	}
	requireOutcome(t, first, "alpha", OutcomePushed)
	firstPublication := source.Head("skill-updates")

	source.run("merge", "--quiet", "--no-ff", "--no-edit", "skill-updates")
	source.run("branch", "--delete", "skill-updates")
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "Second body"))

	second, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("second Push: %v\n%s", err, second.Render("push"))
	}
	requireOutcome(t, second, "alpha", OutcomePushed)
	if got := source.FileAt("skill-updates", "skills/alpha/SKILL.md"); !strings.Contains(got, "Second body") {
		t.Fatalf("recreated destination content = %q", got)
	}
	entry, _ := proj.Lock().Entry("alpha")
	if entry.Publication == nil || entry.Publication.Commit == firstPublication {
		t.Fatalf("publication was not advanced after branch recreation: %+v", entry.Publication)
	}
}

// TestPushPreservesADestinationDeletionWhenRecreatingACheckpointedBranch proves
// that restoring a publication checkpoint is not enough to skip destination
// reconciliation. After the contribution branch is deleted and the skill is
// then removed from the default branch, an unchanged local tree must preserve
// that deletion instead of republishing the checkpointed skill onto a new
// branch.
func TestPushPreservesADestinationDeletionWhenRecreatingACheckpointedBranch(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Original body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`tracking = "pinned"`, `write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "First body"))
	first, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("first Push: %v\n%s", err, first.Render("push"))
	}
	requireOutcome(t, first, "alpha", OutcomePushed)

	source.run("merge", "--quiet", "--no-ff", "--no-edit", "skill-updates")
	source.run("branch", "--delete", "skill-updates")
	source.RemovePath("skills/alpha")
	defaultHead := source.Commit("remove alpha from default")

	second, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("second Push: %v\n%s", err, second.Render("push"))
	}
	alpha := requireOutcome(t, second, "alpha", OutcomeUnchanged)
	if !strings.Contains(alpha.Detail, "removal is preserved") {
		t.Fatalf("alpha detail = %q, want the preserved destination removal", alpha.Detail)
	}
	if source.HasPath("skill-updates", "skills/alpha/SKILL.md") {
		t.Fatal("recreating the branch resurrected a destination-side whole-skill deletion")
	}
	if source.Head("main") != defaultHead || source.HasPath("main", "skills/alpha/SKILL.md") {
		t.Fatal("preserving the deletion changed the default branch")
	}
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "First body") {
		t.Fatal("preserving a destination deletion removed the managed local skill")
	}
}

func TestPushReportsADeleteModifyConflictWhenRecreatingACheckpointedBranch(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Original body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`tracking = "pinned"`, `write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "First body"))
	first, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("first Push: %v\n%s", err, first.Render("push"))
	}
	requireOutcome(t, first, "alpha", OutcomePushed)

	source.run("merge", "--quiet", "--no-ff", "--no-edit", "skill-updates")
	source.run("branch", "--delete", "skill-updates")
	source.RemovePath("skills/alpha")
	defaultHead := source.Commit("remove alpha from default")

	proj.WriteImportedFile("alpha", "notes.md", "local change\n")

	second, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("second Push: %v", err)
	}
	failed := requireOutcome(t, second, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "delete/modify") {
		t.Fatalf("failure %q does not report the delete/modify conflict", failed.Err)
	}
	if source.Head("main") != defaultHead {
		t.Fatal("a conflicted push still wrote to the destination")
	}
	if source.HasPath("skill-updates", "skills/alpha/SKILL.md") {
		t.Fatal("a conflicted push re-added the deleted skill")
	}
}

// TestPushBranchPublishesAReversionToTheSourceVersion proves returning a file
// to its original source content is still a change relative to what Agent
// Layer last published. Without publication state, three-way merge treats the
// local side as unchanged and silently retains the old destination edit.
func TestPushBranchPublishesAReversionToTheSourceVersion(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Original body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	original := proj.ImportedFile("alpha", "SKILL.md")

	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "Temporary body"))
	first, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("first Push: %v\n%s", err, first.Render("push"))
	}
	requireOutcome(t, first, "alpha", OutcomePushed)
	firstHead := source.Head("skill-updates")

	proj.WriteImportedFile("alpha", "SKILL.md", original)
	second, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("reversion Push: %v\n%s", err, second.Render("push"))
	}
	requireOutcome(t, second, "alpha", OutcomePushed)
	if got := source.FileAt("skill-updates", "skills/alpha/SKILL.md"); got != strings.TrimSuffix(original, "\n") {
		t.Fatalf("destination content = %q, want original %q", got, strings.TrimSuffix(original, "\n"))
	}
	if source.Head("skill-updates") == firstHead {
		t.Fatal("publishing the reversion did not advance the contribution branch")
	}
}

func TestPushReportsAReversionWithoutAPublicationCheckpoint(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Original body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	original := proj.ImportedFile("alpha", "SKILL.md")
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "Temporary body"))
	first, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("first Push: %v", err)
	}
	requireOutcome(t, first, "alpha", OutcomePushed)
	branchHead := source.Head("skill-updates")

	lock := proj.Lock()
	entry, _ := lock.Entry("alpha")
	entry.Publication = nil
	lock.Upsert(entry)
	if err := lock.Save(proj.paths.SkillsLockPath); err != nil {
		t.Fatalf("save checkpoint-less lock: %v", err)
	}
	proj.WriteImportedFile("alpha", "SKILL.md", original)

	second, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("second Push: %v", err)
	}
	failed := requireOutcome(t, second, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "no publication checkpoint") {
		t.Fatalf("failure = %q", failed.Err)
	}
	if source.Head("skill-updates") != branchHead {
		t.Fatal("ambiguous checkpoint-less reversion changed the destination")
	}
}

// TestPushBranchStillPreservesExternalChangesAfterAPriorPublication proves the
// checkpoint does not turn repeated push into destination overwrite. Changes
// made directly on the PR branch remain the remote side of the three-way merge.
func TestPushBranchStillPreservesExternalChangesAfterAPriorPublication(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "one\ntwo\nthree\nfour\nfive\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "published one\ntwo\nthree\nfour\nfive\n")
	first, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("first Push: %v\n%s", err, first.Render("push"))
	}
	requireOutcome(t, first, "alpha", OutcomePushed)

	source.Checkout("skill-updates", false)
	source.WriteFile("skills/alpha/notes.md", "published one\ntwo\nthree\nfour\nexternal five\n", 0o644)
	source.Commit("external PR branch edit")
	source.Checkout("main", false)
	proj.WriteImportedFile("alpha", "notes.md", "published one\nlocal two\nthree\nfour\nfive\n")

	second, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("second Push: %v\n%s", err, second.Render("push"))
	}
	requireOutcome(t, second, "alpha", OutcomePushed)
	got := source.FileAt("skill-updates", "skills/alpha/notes.md")
	if got != "published one\nlocal two\nthree\nfour\nexternal five" {
		t.Fatalf("destination content = %q; external branch change was not preserved", got)
	}
	if local := proj.ImportedFile("alpha", "notes.md"); local != got+"\n" {
		t.Fatalf("local content = %q, want merged destination content %q", local, got+"\n")
	}
	secondHead := source.Head("skill-updates")
	third, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("third Push: %v\n%s", err, third.Render("push"))
	}
	requireOutcome(t, third, "alpha", OutcomeUnchanged)
	if source.Head("skill-updates") != secondHead {
		t.Fatal("an unchanged follow-up push reverted the synchronized external edit")
	}
}

func TestPushDoesNotCheckpointAMissingUnchangedBranch(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeUnchanged)
	entry, _ := proj.Lock().Entry("alpha")
	if entry.Publication != nil {
		t.Fatalf("missing branch produced publication checkpoint %+v", entry.Publication)
	}
	if output := source.run("branch", "--list", "skill-updates"); output != "" {
		t.Fatalf("unchanged push unexpectedly created branch: %s", output)
	}
}

func TestGroupedPushAdvancesUnchangedSiblingPublication(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add grouped skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha", "skills/beta"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v\n%s", err, report.Render("push"))
	}
	requireOutcome(t, report, "alpha", OutcomePushed)
	requireOutcome(t, report, "beta", OutcomeUnchanged)
	head := source.Head("skill-updates")
	beta, _ := proj.Lock().Entry("beta")
	if beta.Publication == nil || beta.Publication.Commit != head {
		t.Fatalf("unchanged sibling publication = %+v, want checkpoint at %s", beta.Publication, head)
	}
}

func TestPushFallsBackSafelyWhenPublicationWasRebasedAway(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Original body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "First body"))
	first, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("first Push: %v", err)
	}
	requireOutcome(t, first, "alpha", OutcomePushed)
	oldPublication := source.Head("skill-updates")

	source.run("tag", "keep-old-publication", oldPublication)
	source.run("branch", "--force", "skill-updates", "main")
	proj.WriteImportedFile("alpha", "SKILL.md", skillManifest("alpha", "Second body"))

	second, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("second Push: %v\n%s", err, second.Render("push"))
	}
	requireOutcome(t, second, "alpha", OutcomePushed)
	entry, _ := proj.Lock().Entry("alpha")
	if entry.Publication == nil || entry.Publication.Commit == oldPublication {
		t.Fatalf("publication = %+v, want replacement for unreachable %s", entry.Publication, oldPublication)
	}
	if got := source.FileAt("skill-updates", "skills/alpha/SKILL.md"); !strings.Contains(got, "Second body") {
		t.Fatalf("destination content = %q, want fallback push result", got)
	}
}

// TestPullIgnoresButPushRejectsDestinationArtifacts proves source ingestion may
// omit platform noise without authorizing a later push to silently preserve or
// delete that same committed destination state.
func TestPullIgnoresButPushRejectsDestinationArtifacts(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/.DS_Store", "noise\n", 0o644)
	head := source.Commit("add alpha with destination noise")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `write_policy = "direct"`))
	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeImported)
	if _, err := os.Stat(filepath.Join(proj.paths.ImportedSkillsDir, "alpha", ".DS_Store")); !os.IsNotExist(err) {
		t.Fatalf("pull materialized an ignored source artifact: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local note\n")

	report, err = proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	for _, want := range []string{"skills/alpha/.DS_Store", "remove", "destination repository"} {
		if !strings.Contains(failed.Err.Error(), want) {
			t.Fatalf("failure %q does not contain %q", failed.Err, want)
		}
	}
	if source.Head("main") != head {
		t.Fatal("artifact rejection still modified the destination")
	}
}

// TestPushBranchPolicyCreatesTheConfiguredBranchAndGroupsSkills proves an
// explicitly configured branch is created from the destination default branch
// and every skill sharing a destination is committed together.
func TestPushBranchPolicyCreatesTheConfiguredBranchAndGroupsSkills(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
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
		t.Fatalf("skills sharing a destination were not grouped into one commit: %q vs %q", alpha.Detail, beta.Detail)
	}
	if strings.Contains(alpha.Detail, "lock advanced") {
		t.Fatalf("a push to a non-tracked branch advanced the lock: %q", alpha.Detail)
	}
	if got := source.FileAt("skill-updates", "skills/alpha/notes.md"); got != "alpha note" {
		t.Fatalf("alpha not published: %q", got)
	}
	if got := source.FileAt("skill-updates", "skills/beta/notes.md"); got != "beta note" {
		t.Fatalf("beta not published: %q", got)
	}
	if source.Head("main") == source.Head("skill-updates") {
		t.Fatal("the configured branch was not advanced past its base")
	}
}

// TestPushForkDestinationDoesNotAdvanceTheLock proves a contribution routed to
// a different repository never advances the source lock.
func TestPushForkDestinationDoesNotAdvanceTheLock(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	fork := newGitRepo(t, "main")
	fork.run("remote", "add", "upstream", source.URL())
	fork.run("fetch", "--quiet", "upstream")
	fork.run("reset", "--quiet", "--hard", "upstream/main")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "direct"`, `push_repository = "`+fork.URL()+`"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	locked, _ := proj.Lock().Entry("alpha")

	proj.WriteImportedFile("alpha", "notes.md", "fork note\n")
	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v\n%s", err, report.Render("push"))
	}
	result := requireOutcome(t, report, "alpha", OutcomePushed)
	if strings.Contains(result.Detail, "lock advanced") {
		t.Fatalf("a fork push advanced the source lock: %q", result.Detail)
	}
	after, _ := proj.Lock().Entry("alpha")
	if after.Commit != locked.Commit {
		t.Fatalf("source lock moved from %s to %s after a fork push", locked.Commit, after.Commit)
	}
	if got := fork.FileAt("main", "skills/alpha/notes.md"); got != "fork note" {
		t.Fatalf("fork content = %q", got)
	}
}

// TestPushRefusesWhenTheTrackedSourceAdvanced proves a stale merge base is
// refused with instructions instead of producing a wrong upstream result.
func TestPushRefusesWhenTheTrackedSourceAdvanced(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local note\n")

	source.WriteFile("skills/alpha/upstream.md", "upstream note\n", 0o644)
	head := source.Commit("advance upstream")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "al skills pull") {
		t.Fatalf("advance failure %q does not direct the user to pull", failed.Err)
	}
	if source.Head("main") != head {
		t.Fatal("a refused push still wrote upstream")
	}
}

// TestPushChecksSourceAdvancementForEveryEntry proves freshness is proved per
// skill. A partial pull advances only the skills it succeeded on, so a block
// can hold entries at different commits; checking one entry would let a stale
// skill through on the strength of an advanced sibling.
func TestPushChecksSourceAdvancementForEveryEntry(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/zulu", "zulu", "Zulu body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}

	// Upstream advances. The alphabetically first skill is recorded at the new
	// commit, as a partial pull would leave it; the later one stays behind.
	source.WriteSkill("skills/alpha", "alpha", "Alpha advanced")
	advanced := source.Commit("advance upstream")
	lock := proj.Lock()
	alpha, _ := lock.Entry("alpha")
	stale, _ := lock.Entry("zulu")
	alpha.Commit = advanced
	alpha.TreeHash = importedTreeHash(t, proj, "alpha")
	lock.Upsert(alpha)
	if err := lock.Save(proj.paths.SkillsLockPath); err != nil {
		t.Fatalf("save lock: %v", err)
	}
	proj.WriteImportedFile("zulu", "notes.md", "local note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	failed := requireOutcome(t, report, "zulu", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "al skills pull") {
		t.Fatalf("stale entry failure %q does not direct the user to pull", failed.Err)
	}
	if source.HasPath("main", "skills/zulu/notes.md") {
		t.Fatal("a stale per-skill merge base was pushed")
	}
	if after, _ := proj.Lock().Entry("zulu"); after.Commit != stale.Commit {
		t.Fatal("a refused push advanced the stale lock entry")
	}
}

// TestPushRefusesToWriteToADestinationDefaultBranchUnderBranchPolicy proves the
// non-primary requirement is enforced against the destination's actual default
// branch, not just the conventional names static validation can recognize.
func TestPushRefusesToWriteToADestinationDefaultBranchUnderBranchPolicy(t *testing.T) {
	source := newGitRepo(t, "develop")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")
	head := source.Head("develop")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "develop"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), `write_policy = "branch" never writes`) {
		t.Fatalf("failure %q does not name the primary-branch policy", failed.Err)
	}
	if source.Head("develop") != head {
		t.Fatal("branch policy wrote to the destination's primary branch")
	}
}

// TestPushCannotCreateABranchWithoutADestinationDefault proves Agent Layer does
// not invent a base or select one candidate's locked source when the
// destination has no default branch.
func TestPushCannotCreateABranchWithoutADestinationDefault(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	destination := newGitRepo(t, "main")
	destination.run("checkout", "--quiet", "--detach", destination.Head("main"))
	destination.run("branch", "-D", "main")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`, `push_repository = "`+destination.URL()+`"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "could not determine the default branch") {
		t.Fatalf("failure %q does not explain the missing destination base", failed.Err)
	}
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/skill-updates")
	cmd.Dir = destination.dir
	cmd.Env = gitenv.WithoutDiscovery()
	if cmd.Run() == nil {
		t.Fatal("a failed push still created the configured branch")
	}
}

// TestPushRejectsOverlappingDestinationPathsInOneGroup proves the overlap rule
// is enforced against the runtime group. Configuration edited after import can
// route two repositories at nested destination paths, and push never pulls, so
// nothing else would catch it before one update overwrote the other.
func TestPushRejectsOverlappingDestinationPathsInOneGroup(t *testing.T) {
	outer := newGitRepo(t, "main")
	outer.WriteSkill("skills", "skills", "Outer body")
	outer.Commit("add outer")

	inner := newGitRepo(t, "main")
	inner.WriteSkill("skills/alpha", "alpha", "Inner body")
	inner.Commit("add inner")

	destination := newGitRepo(t, "main")

	proj := newProject(t)
	// The two sources import cleanly while they target different destination
	// branches, so nothing rejects them up front.
	proj.AppendConfig(importBlock(outer.URL(), []string{"skills"},
		`write_policy = "branch"`, `push_branch = "outer-updates"`, `push_repository = "`+destination.URL()+`"`))
	proj.AppendConfig(importBlock(inner.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`, `push_repository = "`+destination.URL()+`"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("skills", "notes.md", "outer note\n")
	proj.WriteImportedFile("alpha", "notes.md", "inner note\n")

	// Editing configuration after the import routes both into one destination
	// group. Push never pulls, so only the runtime group check can catch it.
	proj.ReplaceInConfig(`push_branch = "outer-updates"`, `push_branch = "skill-updates"`)

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	for _, name := range []string{"skills", "alpha"} {
		failed := requireOutcome(t, report, name, OutcomeFailed)
		if !strings.Contains(failed.Err.Error(), "overlap") {
			t.Fatalf("%s failure %q does not name the overlap", name, failed.Err)
		}
	}
	if destination.HasPath("skill-updates", "skills/alpha/SKILL.md") {
		t.Fatal("overlapping destination paths were published")
	}
}

// TestDestinationOverlapChecksAllAncestorPrefixes proves a sorting sibling
// cannot hide an ancestor/descendant pair from the publication preflight.
func TestDestinationOverlapChecksAllAncestorPrefixes(t *testing.T) {
	t.Parallel()
	group := &pushGroup{Branch: "updates"}
	for _, selectedPath := range []string{"skills", "skills-old", "skills/alpha"} {
		group.Candidates = append(group.Candidates, pushCandidate{Entry: skilllock.Entry{SelectedPath: selectedPath}})
	}
	err := rejectOverlappingDestinationPaths(group)
	if err == nil {
		t.Fatal("a non-adjacent ancestor/descendant pair was accepted")
	}
	if !strings.Contains(err.Error(), "skills and skills/alpha overlap") {
		t.Fatalf("error %q does not identify the hidden overlap", err)
	}
}

// TestPushReconcilesCompatibleDestinationChanges proves a destination change
// Agent Layer did not make is preserved rather than overwritten.
func TestPushReconcilesCompatibleDestinationChanges(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "one\ntwo\nthree\nfour\nfive\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}

	// The destination branch already carries an independent change.
	source.Checkout("skill-updates", true)
	source.WriteFile("skills/alpha/notes.md", "one\ntwo\nthree\nfour\ndestination five\n", 0o644)
	source.Commit("destination change")
	source.Checkout("main", false)

	proj.WriteImportedFile("alpha", "notes.md", "local one\ntwo\nthree\nfour\nfive\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v\n%s", err, report.Render("push"))
	}
	requireOutcome(t, report, "alpha", OutcomePushed)
	got := source.FileAt("skill-updates", "skills/alpha/notes.md")
	if got != "local one\ntwo\nthree\nfour\ndestination five" {
		t.Fatalf("destination content = %q; a compatible destination change must be preserved", got)
	}
}

// TestPushReportsDestinationConflicts proves an incompatible destination change
// is reported instead of being force-resolved.
func TestPushReportsDestinationConflicts(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}

	source.Checkout("skill-updates", true)
	source.WriteFile("skills/alpha/notes.md", "destination change\n", 0o644)
	destinationHead := source.Commit("destination change")
	source.Checkout("main", false)

	proj.WriteImportedFile("alpha", "notes.md", "local change\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "notes.md") {
		t.Fatalf("conflict error %q does not name the path", failed.Err)
	}
	if source.Head("skill-updates") != destinationHead {
		t.Fatal("a conflicted push still wrote to the destination")
	}
}

// TestPushNeverPropagatesWholeSkillDeletion proves a missing imported directory
// fails that skill instead of deleting the skill upstream.
func TestPushNeverPropagatesWholeSkillDeletion(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	head := source.Head("main")
	if err := os.RemoveAll(filepath.Join(proj.paths.ImportedSkillsDir, "alpha")); err != nil {
		t.Fatalf("remove imported directory: %v", err)
	}

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "requires an existing, valid imported skill") {
		t.Fatalf("error = %q", failed.Err)
	}
	if source.Head("main") != head || !source.HasPath("main", "skills/alpha/SKILL.md") {
		t.Fatal("a missing local directory was propagated as an upstream deletion")
	}
}

// TestPushPublishesFileLevelDeletionsWithinAValidSkill proves file-level
// deletions inside an existing valid skill remain ordinary local changes.
func TestPushPublishesFileLevelDeletionsWithinAValidSkill(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "remove me\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if err := os.Remove(filepath.Join(proj.paths.ImportedSkillsDir, "alpha", "notes.md")); err != nil {
		t.Fatalf("remove resource: %v", err)
	}

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v\n%s", err, report.Render("push"))
	}
	requireOutcome(t, report, "alpha", OutcomePushed)
	if source.HasPath("main", "skills/alpha/notes.md") {
		t.Fatal("a file-level deletion was not published")
	}
	if !source.HasPath("main", "skills/alpha/SKILL.md") {
		t.Fatal("the skill itself was deleted upstream")
	}
}

// TestPushRefusesAPinnedLockAdvance proves a pinned import never advances its
// lock even when the push succeeds.
func TestPushRefusesAPinnedLockAdvance(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")
	source.Tag("v1.0.0")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`ref = "v1.0.0"`, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	locked, _ := proj.Lock().Entry("alpha")

	proj.WriteImportedFile("alpha", "notes.md", "local note\n")
	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v\n%s", err, report.Render("push"))
	}
	result := requireOutcome(t, report, "alpha", OutcomePushed)
	if strings.Contains(result.Detail, "lock advanced") {
		t.Fatalf("a pinned import advanced its lock: %q", result.Detail)
	}
	after, _ := proj.Lock().Entry("alpha")
	if after.Commit != locked.Commit {
		t.Fatalf("pinned lock moved from %s to %s", locked.Commit, after.Commit)
	}
}

// TestPushFailsWhenTheDestinationDefaultBranchCannotBeResolved proves an
// unreachable `direct` destination blocks that source block without affecting
// other work.
func TestPushFailsWhenTheDestinationDefaultBranchCannotBeResolved(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	missing := filepath.Join(t.TempDir(), "absent-destination")
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "direct"`, `push_repository = "`+missing+`"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(report.Sources) != 1 {
		t.Fatalf("expected one source failure: %s", report.Render("push"))
	}
	// The block-level failure blocks alpha, so alpha must appear in the report
	// rather than being silently dropped from it.
	requireOutcome(t, report, "alpha", OutcomeFailed)
}

// importedTreeHash returns the canonical hash of an imported skill's current
// content, so a test can record lock state that matches what is on disk.
func importedTreeHash(t *testing.T, proj *project, name string) string {
	t.Helper()
	tree, err := skilltree.Read(skilltree.OSFS{}, filepath.Join(proj.paths.ImportedSkillsDir, name))
	if err != nil {
		t.Fatalf("read imported %s: %v", name, err)
	}
	return tree.Hash()
}

// TestPushFailsASkillWithAnUnreadableMergeBase proves push refuses to derive a
// delta when the locked source tree cannot be read.
func TestPushFailsASkillWithAnUnreadableMergeBase(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`ref = "main"`, `tracking = "pinned"`, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}

	// Point the lock at a commit the source repository does not contain.
	lock := proj.Lock()
	entry, _ := lock.Entry("alpha")
	entry.Commit = "0000000000000000000000000000000000000000"
	lock.Upsert(entry)
	if err := lock.Save(proj.paths.SkillsLockPath); err != nil {
		t.Fatalf("save lock: %v", err)
	}

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "no merge base exists") {
		t.Fatalf("error = %q", failed.Err)
	}
}

// TestPushSkipsBlocksWithNoRecordedSkills proves a write-enabled block that has
// never been pulled produces no upstream activity.
func TestPushSkipsBlocksWithNoRecordedSkills(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	head := source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `write_policy = "direct"`))

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if report.Failed() || len(report.Skills) != 0 {
		t.Fatalf("an unpulled block produced results: %s", report.Render("push"))
	}
	if source.Head("main") != head {
		t.Fatal("an unpulled block wrote upstream")
	}
}

// TestPushRefusesToPublishAnInvalidMergedTree proves every result is validated
// as a skill before it is committed. A destination that dropped the manifest
// merges cleanly with an unrelated local change, so without the check push
// would publish a directory that is no longer a skill at all.
func TestPushRefusesToPublishAnInvalidMergedTree(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}

	// The destination branch dropped the manifest but kept the rest.
	source.Checkout("skill-updates", true)
	source.RemovePath("skills/alpha/SKILL.md")
	destinationHead := source.Commit("drop the manifest downstream")
	source.Checkout("main", false)

	proj.WriteImportedFile("alpha", "notes.md", "local change\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "would not be a valid skill") {
		t.Fatalf("failure %q does not report the invalid result", failed.Err)
	}
	if source.Head("skill-updates") != destinationHead {
		t.Fatal("an invalid merged tree was published")
	}
}

// TestPushRefusesALocallyInvalidImportedSkill proves a push requires an
// existing, valid imported skill: a locally broken manifest fails that skill
// rather than publishing content the source repository would reject.
func TestPushRefusesALocallyInvalidImportedSkill(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")
	head := source.Head("main")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "SKILL.md", "no frontmatter at all\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if failed.Err == nil {
		t.Fatal("the failure carries no diagnostic")
	}
	if source.Head("main") != head {
		t.Fatal("a locally invalid skill was published upstream")
	}
}

// TestPushFailsEverySkillInARejectedGroup proves the grouped-write failure
// scope: one commit and push carries the whole destination group, so a rejected
// push must fail every skill in it rather than reporting some as pushed.
func TestPushFailsEverySkillInARejectedGroup(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")
	head := source.Head("main")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	// Only alpha changes. Beta is still a surviving member of the same atomic
	// destination group, so the rejected publication must fail beta too rather
	// than reporting it unchanged.
	proj.WriteImportedFile("alpha", "notes.md", "alpha note\n")

	// The destination refuses updates to its checked-out branch, so the push
	// itself is rejected after the group's commit was built.
	source.run("config", "receive.denyCurrentBranch", "refuse")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	alpha := requireOutcome(t, report, "alpha", OutcomeFailed)
	beta := requireOutcome(t, report, "beta", OutcomeFailed)
	if alpha.Err == nil || beta.Err == nil {
		t.Fatal("a rejected group push produced results with no diagnostic")
	}
	if source.Head("main") != head {
		t.Fatal("a rejected push still advanced the destination")
	}
	for _, name := range []string{"alpha", "beta"} {
		entry, _ := proj.Lock().Entry(name)
		if entry.TreeHash == "" {
			t.Fatalf("%s lost its lock entry after a failed push", name)
		}
	}
}

// TestPushPreservesADestinationDeletionOnAnExistingBranch proves an absent path
// on a branch that already exists is a destination-side whole-skill deletion,
// not a skill the destination never had. With local content still equal to the
// locked base, the one-sided deletion applies: the skill reports unchanged and
// no commit is created. Re-adding it here would silently revert the
// destination's removal.
func TestPushPreservesADestinationDeletionOnAnExistingBranch(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/beta", "beta", "Beta body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}

	// The destination branch exists and has removed alpha entirely.
	source.Checkout("skill-updates", true)
	source.RemovePath("skills/alpha")
	destinationHead := source.Commit("remove alpha downstream")
	source.Checkout("main", false)

	// Only beta carries a local change, so alpha still equals its locked base.
	proj.WriteImportedFile("beta", "notes.md", "beta note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v\n%s", err, report.Render("push"))
	}
	alpha := requireOutcome(t, report, "alpha", OutcomeUnchanged)
	if !strings.Contains(alpha.Detail, "removal is preserved") {
		t.Fatalf("alpha detail = %q, want the preserved destination removal", alpha.Detail)
	}
	requireOutcome(t, report, "beta", OutcomePushed)

	if source.HasPath("skill-updates", "skills/alpha/SKILL.md") {
		t.Fatal("a destination-side whole-skill deletion was reverted by push")
	}
	if got := source.FileAt("skill-updates", "skills/beta/notes.md"); got != "beta note" {
		t.Fatalf("beta was not published alongside the preserved deletion: %q", got)
	}
	if source.Head("skill-updates") == destinationHead {
		t.Fatal("the fixture did not actually publish beta")
	}
	if !strings.Contains(proj.ImportedFile("alpha", "SKILL.md"), "Alpha body") {
		t.Fatal("preserving a destination deletion removed the managed local skill")
	}
	secondHead := source.Head("skill-updates")
	second, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("second Push: %v\n%s", err, second.Render("push"))
	}
	requireOutcome(t, second, "alpha", OutcomeUnchanged)
	if source.Head("skill-updates") != secondHead || source.HasPath("skill-updates", "skills/alpha/SKILL.md") {
		t.Fatal("a follow-up push reverted the preserved destination deletion")
	}
}

// TestPushReportsADeleteModifyConflictAgainstADestinationDeletion proves the
// other half of that contract: when the destination removed the skill and the
// local tree diverged from the locked base, the two changes cannot be
// reconciled, so the skill fails as a delete/modify conflict instead of being
// re-added or silently dropped.
func TestPushReportsADeleteModifyConflictAgainstADestinationDeletion(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}

	source.Checkout("skill-updates", true)
	source.RemovePath("skills/alpha")
	destinationHead := source.Commit("remove alpha downstream")
	source.Checkout("main", false)

	proj.WriteImportedFile("alpha", "notes.md", "local change\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "delete/modify") {
		t.Fatalf("failure %q does not report the delete/modify conflict", failed.Err)
	}
	if !strings.Contains(failed.Err.Error(), "notes.md") {
		t.Fatalf("failure %q does not name the conflicted path", failed.Err)
	}
	if source.Head("skill-updates") != destinationHead {
		t.Fatal("a conflicted push still wrote to the destination")
	}
	if source.HasPath("skill-updates", "skills/alpha/SKILL.md") {
		t.Fatal("a conflicted push re-added the deleted skill")
	}
}

// TestPushAdvancesUnchangedSiblingsOnATrackedSourceRef proves a grouped direct
// push leaves every skill on the ref it just moved at the same commit.
//
// Publishing to the exact tracked source ref advances that ref for all of the
// block's skills, not only the ones the commit changed. Leaving an unchanged
// sibling at the previous commit makes the very next push reject it as a stale
// source and demand a pull that has nothing to do.
func TestPushAdvancesUnchangedSiblingsOnATrackedSourceRef(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/zulu", "zulu", "Zulu body")
	before := source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"}, `write_policy = "direct"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	// Only one of the two grouped skills has a local change to contribute.
	proj.WriteImportedFile("alpha", "notes.md", "local note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomePushed)
	unchanged := requireOutcome(t, report, "zulu", OutcomeUnchanged)
	if !strings.Contains(unchanged.Detail, "lock advanced") {
		t.Fatalf("unchanged sibling detail = %q, want it to report the lock advance", unchanged.Detail)
	}

	pushed := source.Head("main")
	if pushed == before {
		t.Fatal("the push created no commit")
	}
	lock := proj.Lock()
	for _, name := range []string{"alpha", "zulu"} {
		entry, ok := lock.Entry(name)
		if !ok {
			t.Fatalf("%s left the lock", name)
		}
		if entry.Commit != pushed {
			t.Fatalf("%s is locked to %s, want the pushed commit %s", name, entry.Commit, pushed)
		}
	}

	// The proof that matters: pushing again is a clean no-op rather than a
	// stale-source rejection demanding a pull.
	second, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("second Push: %v", err)
	}
	if second.Failed() {
		t.Fatalf("the follow-up push failed:\n%s", second.Render("al skills push"))
	}
	requireOutcome(t, second, "zulu", OutcomeUnchanged)
}

// TestPushDoesNotAdvanceUnchangedSiblingsOnAnotherBranch proves the advance
// above is scoped to the exact tracked source ref. A `branch` policy publishes
// somewhere the lock does not track, so no lock may move.
func TestPushDoesNotAdvanceUnchangedSiblingsOnAnotherBranch(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteSkill("skills/zulu", "zulu", "Zulu body")
	source.Commit("add skills")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/*"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("pull: %v", err)
	}
	// The baseline has to exist, or the comparison below would pass on two
	// empty commits and prove nothing.
	locked, ok := proj.Lock().Entry("zulu")
	if !ok {
		t.Fatal("the pull recorded no lock entry for zulu")
	}
	proj.WriteImportedFile("alpha", "notes.md", "local note\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	unchanged := requireOutcome(t, report, "zulu", OutcomeUnchanged)
	if strings.Contains(unchanged.Detail, "lock advanced") {
		t.Fatalf("a push to a non-tracked branch advanced a lock: %q", unchanged.Detail)
	}
	after, ok := proj.Lock().Entry("zulu")
	if !ok {
		t.Fatal("the push removed zulu from the lock")
	}
	if after.Commit != locked.Commit {
		t.Fatalf("lock commit = %s, want the untouched %s", after.Commit, locked.Commit)
	}
}
