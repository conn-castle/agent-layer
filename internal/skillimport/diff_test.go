package skillimport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffComparesLiveLocalAndUpstreamTrees(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "base\n", 0o644)
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	proj.WriteImportedFile("alpha", "notes.md", "local\n")
	source.WriteFile("skills/alpha/notes.md", "upstream\n", 0o644)
	source.Commit("advance")

	diff, err := proj.Service().Diff(context.Background(), "alpha", "local", "upstream")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	defaulted, err := proj.Service().Diff(context.Background(), "alpha", "", "")
	if err != nil {
		t.Fatalf("Diff defaults: %v", err)
	}
	if string(defaulted) != string(diff) {
		t.Fatalf("default diff differs from local/upstream diff:\n%s", defaulted)
	}
	text := string(diff)
	for _, fragment := range []string{
		"diff --git a/local/notes.md b/upstream/notes.md",
		"-local",
		"+upstream",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("diff %q does not contain %q", text, fragment)
		}
	}

	baseDiff, err := proj.Service().Diff(context.Background(), "alpha", "base", "local")
	if err != nil {
		t.Fatalf("Diff base local: %v", err)
	}
	if !strings.Contains(string(baseDiff), "a/base/notes.md") || !strings.Contains(string(baseDiff), "b/local/notes.md") {
		t.Fatalf("base/local prefixes missing: %q", baseDiff)
	}

	identical, err := proj.Service().Diff(context.Background(), "alpha", "local", "local")
	if err != nil {
		t.Fatalf("Diff identical: %v", err)
	}
	if len(identical) != 0 {
		t.Fatalf("identical sides produced output %q", identical)
	}
}

func TestDiffRejectsUnknownSidesAndMissingDestination(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	if _, err := proj.Service().Diff(context.Background(), "alpha", "ours", "upstream"); err == nil || !strings.Contains(err.Error(), "unsupported diff side") {
		t.Fatalf("unknown side error = %v", err)
	}
	if _, err := proj.Service().Diff(context.Background(), "alpha", "local", "destination"); err == nil || !strings.Contains(err.Error(), "no writable destination") {
		t.Fatalf("read-only destination error = %v", err)
	}

	proj.ReplaceInConfig(`selectors = ["skills/alpha"]`, `selectors = ["skills/alpha"]
write_policy = "branch"
push_branch = "missing-branch"`)
	if _, err := proj.Service().Diff(context.Background(), "alpha", "local", "destination"); err == nil || !strings.Contains(err.Error(), "has no branch") {
		t.Fatalf("missing destination branch error = %v", err)
	}
}

func TestDiffRejectsBranchPolicyTargetingDefaultBranch(t *testing.T) {
	source := newGitRepo(t, "trunk")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "trunk"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	_, err := proj.Service().Diff(context.Background(), "alpha", "local", "destination")
	if err == nil || !strings.Contains(err.Error(), "default branch") {
		t.Fatalf("default-branch destination error = %v", err)
	}
}

func TestDiffTreatsAbsentDestinationPathAsEmptyTree(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "present\n", 0o644)
	source.Commit("add alpha")
	source.Checkout("skill-updates", true)
	source.RemovePath("skills/alpha")
	source.Commit("remove skill on destination branch")
	source.Checkout("main", false)

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	diff, err := proj.Service().Diff(context.Background(), "alpha", "local", "destination")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	text := string(diff)
	if !strings.Contains(text, "a/local/notes.md") || !strings.Contains(text, "b/destination/notes.md") {
		t.Fatalf("empty destination diff missing prefixes: %q", text)
	}
	if !strings.Contains(text, "-present") {
		t.Fatalf("empty destination did not show the local addition as a deletion: %q", text)
	}
}

func TestDiffReadsAnExistingWritableDestination(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.WriteFile("skills/alpha/notes.md", "base\n", 0o644)
	source.Commit("add alpha")
	source.Checkout("skill-updates", true)
	source.WriteFile("skills/alpha/notes.md", "destination\n", 0o644)
	source.Commit("destination change")
	source.Checkout("main", false)

	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"},
		`write_policy = "branch"`, `push_branch = "skill-updates"`))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}

	diff, err := proj.Service().Diff(context.Background(), "alpha", "base", "destination")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	text := string(diff)
	for _, fragment := range []string{"a/base/notes.md", "b/destination/notes.md", "-base", "+destination"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("destination diff %q does not contain %q", text, fragment)
		}
	}
}

func TestDiffReportsUnavailableLocalAndUpstreamSides(t *testing.T) {
	source := newGitRepo(t, "main")
	source.WriteSkill("skills/alpha", "alpha", "Alpha body")
	source.Commit("add alpha")
	proj := newProject(t)
	proj.AppendConfig(importBlock(source.URL(), []string{"skills/alpha"}))
	if _, err := proj.Service().Pull(context.Background()); err != nil {
		t.Fatalf("initial pull: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(proj.paths.ImportedSkillsDir, "alpha")); err != nil {
		t.Fatalf("remove local skill: %v", err)
	}
	if _, err := proj.Service().Diff(context.Background(), "alpha", "local", "base"); err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("missing local diff error = %v", err)
	}
	writeProjectFile(t, proj.paths.ConfigPath, baseConfigTOML)
	if _, err := proj.Service().Diff(context.Background(), "alpha", "base", "upstream"); err == nil || !strings.Contains(err.Error(), "not in the current configuration") {
		t.Fatalf("unconfigured upstream diff error = %v", err)
	}
}
