package skillimport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/skilllock"
)

func TestPullConflictWorkspaceCoversBinaryAndDeleteModify(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/data.bin", string([]byte{0, 1, 2}), 0o644)
	source.WriteFile("skills/alpha/gone.md", "keep\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	proj.WriteImportedFile("alpha", "data.bin", string([]byte{0, 1, 9}))
	if err := os.Remove(filepath.Join(proj.paths.ImportedSkillsDir, "alpha", "gone.md")); err != nil {
		t.Fatalf("delete local gone.md: %v", err)
	}
	source.WriteFile("skills/alpha/data.bin", string([]byte{0, 3, 4}), 0o644)
	source.WriteFile("skills/alpha/gone.md", "changed\n", 0o644)
	source.Commit("conflict")

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("conflicted pull: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "al skills resolve alpha") {
		t.Fatalf("conflict error is not actionable: %v", failed.Err)
	}
	workspace, err := conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	unmerged := runGit(t, workspace, "ls-files", "-u")
	for _, path := range []string{"data.bin", "gone.md"} {
		if !strings.Contains(unmerged, path) {
			t.Fatalf("unmerged entries %q do not include %s", unmerged, path)
		}
	}
	if _, err := proj.Service().Resolve(context.Background(), "alpha"); err == nil || !strings.Contains(err.Error(), "unmerged") {
		t.Fatalf("Resolve before finishing merge = %v", err)
	}
}

func TestResolveIgnoresUntrackedFilesAndRejectsStaleWorkspaces(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local\n")
	source.WriteFile("skills/alpha/notes.md", "upstream\n", 0o644)
	source.Commit("conflict")
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("conflicted pull: %v", err)
	}
	workspace, err := conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	writeProjectFile(t, filepath.Join(workspace, "notes.md"), "resolved\n")
	writeProjectFile(t, filepath.Join(workspace, "notes.md.orig"), "junk\n")
	runGit(t, workspace, "add", "--", "notes.md")

	report, err := proj.Service().Resolve(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeResolved)
	if _, err := os.Stat(filepath.Join(proj.paths.ImportedSkillsDir, "alpha", "notes.md.orig")); !os.IsNotExist(err) {
		t.Fatal("untracked mergetool file was imported")
	}

	proj.WriteImportedFile("alpha", "notes.md", "local-again\n")
	source.WriteFile("skills/alpha/notes.md", "upstream-again\n", 0o644)
	source.Commit("conflict again")
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("second conflicted pull: %v", err)
	}
	workspace, err = conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("second workspace: %v", err)
	}
	writeProjectFile(t, filepath.Join(workspace, "notes.md"), "second\n")
	runGit(t, workspace, "add", "--", "notes.md")

	proj.WriteImportedFile("alpha", "notes.md", "upstream-again\n")
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("alignment pull: %v", err)
	}
	if _, err := proj.Service().Resolve(context.Background(), "alpha"); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale workspace error = %v", err)
	}
}

func TestResolveRejectsPullWorkspaceAfterConfigRefChange(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local\n")
	source.WriteFile("skills/alpha/notes.md", "upstream\n", 0o644)
	source.Commit("conflict")
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("conflicted pull: %v", err)
	}
	workspace, err := conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	writeProjectFile(t, filepath.Join(workspace, "notes.md"), "resolved\n")
	runGit(t, workspace, "add", "--", "notes.md")

	proj.ReplaceInConfig(`selectors = ["skills/alpha"]`, "selectors = [\"skills/alpha\"]\nref = \"other\"")
	if _, err := proj.Service().Resolve(context.Background(), "alpha"); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("config-changed workspace error = %v", err)
	}
	status, err := proj.Service().Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Entries) != 1 || status.Entries[0].Condition == ConditionConflicted {
		t.Fatalf("status after config change = %+v", status.Entries)
	}
}

func TestStatusReportsMatchingConflictWorkspace(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local\n")
	source.WriteFile("skills/alpha/notes.md", "upstream\n", 0o644)
	source.Commit("conflict")
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("conflicted pull: %v", err)
	}

	status, err := proj.Service().Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Entries) != 1 || status.Entries[0].Condition != ConditionConflicted {
		t.Fatalf("entries = %+v, want one conflicted skill", status.Entries)
	}
	if status.Entries[0].Workspace == "" {
		t.Fatal("conflicted status omitted the workspace path")
	}
	expanded := status.Render(true)
	if !strings.Contains(expanded, "alpha\tconflicted") || !strings.Contains(expanded, status.Entries[0].Workspace) {
		t.Fatalf("--all output = %q", expanded)
	}
	summary := status.Render(false)
	if !strings.Contains(summary, "1 conflicted") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestRetryPreservesAnActiveConflictWorkspace(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local\n")
	source.WriteFile("skills/alpha/notes.md", "upstream\n", 0o644)
	source.Commit("conflict")
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("conflicted pull: %v", err)
	}
	workspace, err := conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	writeProjectFile(t, filepath.Join(workspace, "notes.md"), "work in progress\n")
	runGit(t, workspace, "add", "--", "notes.md")
	lock := proj.Lock()
	lock.Skills[0].Publication = &skilllock.Publication{
		Repository: source.URL(),
		Branch:     "skill-updates",
		Commit:     lock.Skills[0].Commit,
		TreeHash:   lock.Skills[0].TreeHash,
	}
	if err := lock.Save(proj.paths.SkillsLockPath); err != nil {
		t.Fatalf("record publication while resolving pull: %v", err)
	}
	status, err := proj.Service().Status()
	if err != nil {
		t.Fatalf("Status after publication change: %v", err)
	}
	if len(status.Entries) != 1 || status.Entries[0].Condition != ConditionConflicted {
		t.Fatalf("publication-only change invalidated pull workspace: %+v", status.Entries)
	}

	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("retry pull: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "active pull conflict workspace") {
		t.Fatalf("retry error = %v", failed.Err)
	}
	data, err := os.ReadFile(filepath.Join(workspace, "notes.md")) // #nosec G304 -- workspace is rooted in t.TempDir().
	if err != nil {
		t.Fatalf("read staged resolution: %v", err)
	}
	if got := string(data); got != "work in progress\n" {
		t.Fatalf("retry replaced staged resolution with %q", got)
	}
	if _, err := proj.Service().Resolve(context.Background(), "alpha"); err != nil {
		t.Fatalf("Resolve after publication change: %v", err)
	}
	resolved, _ := proj.Lock().Entry("alpha")
	if resolved.Publication == nil || resolved.Publication.Branch != "skill-updates" {
		t.Fatalf("pull resolve lost current publication: %+v", resolved.Publication)
	}
}

func TestStatusFailsForCorruptConflictMetadata(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	if _, err := proj.Service().Resolve(context.Background(), "alpha"); err == nil || !strings.Contains(err.Error(), "no conflict workspace") {
		t.Fatalf("Resolve without workspace = %v", err)
	}
	workspace, err := conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".git"), 0o750); err != nil {
		t.Fatalf("mkdir corrupt workspace: %v", err)
	}
	writeProjectFile(t, filepath.Join(workspace, ".git", conflictMetaFile), "{")
	if _, err := proj.Service().Status(); err == nil || !strings.Contains(err.Error(), filepath.Join(".agent-layer", "tmp", "skill-conflicts", "alpha")) || !strings.Contains(err.Error(), "move or remove it") {
		t.Fatalf("Status corrupt workspace error = %v", err)
	}
}

func TestResolveRejectsUnknownSkillAndWorkspaceKind(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")
	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	if _, err := proj.Service().Resolve(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), "no lock entry") {
		t.Fatalf("unknown skill resolve error = %v", err)
	}
	workspace, err := conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, ".git"), 0o750); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	writeProjectFile(t, filepath.Join(workspace, ".git", conflictMetaFile), `{"kind":"unknown"}`)
	if _, err := proj.Service().Resolve(context.Background(), "alpha"); err == nil || !strings.Contains(err.Error(), "unknown kind") {
		t.Fatalf("unknown workspace kind error = %v", err)
	}
}

func TestResolveAppliesAConflictedPushWithoutMovingSource(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	locked, _ := proj.Lock().Entry("alpha")

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
	if !strings.Contains(failed.Err.Error(), "al skills resolve alpha") {
		t.Fatalf("conflict error is not actionable: %v", failed.Err)
	}
	if source.Head("skill-updates") != destinationHead {
		t.Fatal("a conflicted push still wrote to the destination")
	}

	workspace, err := conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	if runGit(t, workspace, "rev-parse", "--abbrev-ref", "HEAD") != "local" {
		t.Fatal("push workspace did not check out local")
	}
	if runGit(t, workspace, "rev-parse", "--verify", "destination") == "" {
		t.Fatal("push workspace is missing the destination branch")
	}
	writeProjectFile(t, filepath.Join(workspace, "notes.md"), "resolved\n")
	runGit(t, workspace, "add", "--", "notes.md")

	report, err = proj.Service().Resolve(context.Background(), "alpha")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	resolved := requireOutcome(t, report, "alpha", OutcomeResolved)
	if !strings.Contains(resolved.Detail, "al skills push") {
		t.Fatalf("push resolve did not ask for a rerun: %q", resolved.Detail)
	}
	if got := proj.ImportedFile("alpha", "notes.md"); got != "resolved\n" {
		t.Fatalf("resolved content = %q", got)
	}
	entry, _ := proj.Lock().Entry("alpha")
	if entry.Commit != locked.Commit {
		t.Fatalf("push resolve moved the source lock from %s to %s", locked.Commit, entry.Commit)
	}
	if entry.Publication == nil || entry.Publication.Commit != destinationHead || entry.Publication.TreeHash == "" {
		t.Fatalf("push resolve did not checkpoint the destination: %+v", entry.Publication)
	}

	report, err = proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("rerun Push: %v\n%s", err, report.Render("push"))
	}
	requireOutcome(t, report, "alpha", OutcomePushed)
	if got := source.FileAt("skill-updates", "skills/alpha/notes.md"); got != "resolved" {
		t.Fatalf("destination content = %q", got)
	}
}

func TestResolvedPushConflictCanCreateAMissingContributionBranch(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	destination := newGitRepo(t, "main")
	destination.WriteSkill("skills/alpha", "alpha", "Alpha body")
	destination.WriteFile("skills/alpha/notes.md", "destination change\n", 0o644)
	destination.Commit("destination change")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`,
		`push_repository = "`+destination.URL()+`"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local change\n")

	report, err := proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	requireOutcome(t, report, "alpha", OutcomeFailed)
	workspace, err := conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	writeProjectFile(t, filepath.Join(workspace, "notes.md"), "resolved\n")
	runGit(t, workspace, "add", "--", "notes.md")

	if _, err := proj.Service().Resolve(context.Background(), "alpha"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	report, err = proj.Service().Push(context.Background())
	if err != nil {
		t.Fatalf("rerun Push: %v\n%s", err, report.Render("push"))
	}
	requireOutcome(t, report, "alpha", OutcomePushed)
	if got := destination.FileAt("skill-updates", "skills/alpha/notes.md"); got != "resolved" {
		t.Fatalf("destination content = %q", got)
	}
}

func TestResetInvalidatesAConflictedPushWorkspace(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	source.Checkout("skill-updates", true)
	source.WriteFile("skills/alpha/notes.md", "destination change\n", 0o644)
	source.Commit("destination change")
	source.Checkout("main", false)
	proj.WriteImportedFile("alpha", "notes.md", "local change\n")

	if _, err := proj.Service().Push(context.Background()); err != nil {
		t.Fatalf("conflicted Push: %v", err)
	}
	if _, err := proj.Service().Reset(context.Background(), "alpha"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if got := proj.ImportedFile("alpha", "notes.md"); got != "shared\n" {
		t.Fatalf("reset content = %q", got)
	}
	if _, err := proj.Service().Resolve(context.Background(), "alpha"); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("Resolve after reset error = %v", err)
	}
	status, err := proj.Service().Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(status.Entries) != 1 || status.Entries[0].Condition == ConditionConflicted {
		t.Fatalf("status after reset = %+v", status.Entries)
	}
}

func TestWriteConflictWorkspacePreservesAStaleWorkspace(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local\n")
	source.WriteFile("skills/alpha/notes.md", "upstream\n", 0o644)
	source.Commit("conflict")
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("conflicted pull: %v", err)
	}
	workspace, err := conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	writeProjectFile(t, filepath.Join(workspace, "notes.md"), "in-progress\n")
	marker := filepath.Join(workspace, "keep-me.txt")
	writeProjectFile(t, marker, "staged work\n")

	proj.WriteImportedFile("alpha", "notes.md", "local-changed\n")
	report, err := proj.Service().Pull(context.Background())
	if err != nil {
		t.Fatalf("retry pull: %v", err)
	}
	failed := requireOutcome(t, report, "alpha", OutcomeFailed)
	if !strings.Contains(failed.Err.Error(), "stale") || !strings.Contains(failed.Err.Error(), "move or remove") {
		t.Fatalf("retry pull error = %v", failed.Err)
	}
	if got := proj.ImportedFile("alpha", "notes.md"); got != "local-changed\n" {
		t.Fatalf("local skill changed while the stale workspace was protected: %q", got)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("stale workspace was replaced: %v", err)
	}
	kept, err := os.ReadFile(filepath.Join(workspace, "notes.md")) // #nosec G304 -- test-owned workspace path.
	if err != nil {
		t.Fatalf("read in-progress resolution: %v", err)
	}
	if string(kept) != "in-progress\n" {
		t.Fatalf("in-progress resolution was overwritten: %q", kept)
	}
}

func TestResolveRejectsPushWorkspaceAfterWritePolicyChange(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	source.Checkout("skill-updates", true)
	source.WriteFile("skills/alpha/notes.md", "destination change\n", 0o644)
	source.Commit("destination change")
	source.Checkout("main", false)
	proj.WriteImportedFile("alpha", "notes.md", "local change\n")
	if _, err := proj.Service().Push(context.Background()); err != nil {
		t.Fatalf("conflicted Push: %v", err)
	}
	workspace, err := conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	writeProjectFile(t, filepath.Join(workspace, "notes.md"), "resolved\n")
	runGit(t, workspace, "add", "--", "notes.md")

	proj.ReplaceInConfig("write_policy = \"branch\"\npush_branch = \"skill-updates\"\n", "write_policy = \"direct\"\n")
	if _, err := proj.Service().Resolve(context.Background(), "alpha"); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("write-policy-changed workspace error = %v", err)
	}
}

func TestResolveRejectsPullWorkspaceAfterClearingPinnedTracking(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "shared\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`tracking = "pinned"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local\n")
	source.Checkout("other", true)
	source.WriteFile("skills/alpha/notes.md", "upstream\n", 0o644)
	source.Commit("retarget conflict")
	source.Checkout("main", false)
	proj.ReplaceInConfig(`selectors = ["skills/alpha"]`, "selectors = [\"skills/alpha\"]\nref = \"other\"")
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("conflicted pull: %v", err)
	}
	workspace, err := conflictWorkspace(proj.root, "alpha")
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	writeProjectFile(t, filepath.Join(workspace, "notes.md"), "resolved\n")
	runGit(t, workspace, "add", "--", "notes.md")

	proj.ReplaceInConfig("\ntracking = \"pinned\"\n", "\n")
	if _, err := proj.Service().Resolve(context.Background(), "alpha"); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("cleared-tracking workspace error = %v", err)
	}
}
