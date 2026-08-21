package gitrepo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/gitenv"
	"github.com/conn-castle/agent-layer/internal/skilltree"
)

func TestCreateConflictWorkspaceMaterializesGitMerge(t *testing.T) {
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "workspace")
	base := testSkillTree(t, []skilltree.File{
		{Path: "SKILL.md", Data: []byte("---\nname: alpha\ndescription: d\n---\nBody\n")},
		{Path: "notes.md", Data: []byte("shared\n")},
	})
	local := testSkillTree(t, []skilltree.File{
		{Path: "SKILL.md", Data: []byte("---\nname: alpha\ndescription: d\n---\nBody\n")},
		{Path: "notes.md", Data: []byte("local\n")},
	})
	upstream := testSkillTree(t, []skilltree.File{
		{Path: "SKILL.md", Data: []byte("---\nname: alpha\ndescription: d\n---\nBody\n")},
		{Path: "notes.md", Data: []byte("upstream\n")},
	})
	if err := runner.CreateConflictWorkspace(context.Background(), dir, ConflictWorkspaceSpec{
		Base: base, Local: local, Theirs: upstream, TheirsBranch: ConflictBranchUpstream,
	}); err != nil {
		t.Fatalf("CreateConflictWorkspace: %v", err)
	}

	head := gitOutput(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if head != ConflictBranchLocal {
		t.Fatalf("HEAD = %q, want local", head)
	}
	for _, branch := range []string{ConflictBranchBase, ConflictBranchLocal, ConflictBranchUpstream} {
		if gitOutput(t, dir, "rev-parse", "--verify", branch) == "" {
			t.Fatalf("missing branch %s", branch)
		}
	}
	marked, err := os.ReadFile(filepath.Join(dir, "notes.md")) // #nosec G304 -- test-owned temp path.
	if err != nil || !strings.Contains(string(marked), "<<<<<<<") || !strings.Contains(string(marked), "|||||||") {
		t.Fatalf("conflicted file = %q, err %v", marked, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "info", "attributes")); err != nil {
		t.Fatalf("missing attributes: %v", err)
	}
	if err := runner.ConflictIndexReady(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "unmerged") {
		t.Fatalf("ConflictIndexReady = %v", err)
	}
	if _, err := runner.ReadConflictIndex(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "unmerged") {
		t.Fatalf("ReadConflictIndex before resolution = %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("resolved\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md.orig"), []byte("junk\n"), 0o600); err != nil {
		t.Fatalf("write untracked: %v", err)
	}
	if err := runner.ConflictIndexReady(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "unmerged") {
		t.Fatalf("edited unmerged index = %v", err)
	}
	gitOutput(t, dir, "add", "--", "notes.md")
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("unstaged\n"), 0o600); err != nil {
		t.Fatalf("write unstaged resolution: %v", err)
	}
	if err := runner.ConflictIndexReady(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "unstaged") {
		t.Fatalf("ConflictIndexReady with unstaged edit = %v", err)
	}
	gitOutput(t, dir, "checkout", "--", "notes.md")
	tree, err := runner.ReadConflictIndex(context.Background(), dir)
	if err != nil {
		t.Fatalf("ReadConflictIndex: %v", err)
	}
	file, ok := tree.File("notes.md")
	if !ok || string(file.Data) != "resolved\n" {
		t.Fatalf("resolved notes = %v, %q", ok, file.Data)
	}
	if _, ok := tree.File("notes.md.orig"); ok {
		t.Fatal("untracked mergetool file entered the staged tree")
	}
}

func TestCreateConflictWorkspaceRejectsUnknownOtherBranch(t *testing.T) {
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	err = runner.CreateConflictWorkspace(context.Background(), filepath.Join(t.TempDir(), "workspace"), ConflictWorkspaceSpec{
		TheirsBranch: "other",
	})
	if err == nil || !strings.Contains(err.Error(), "theirs branch") {
		t.Fatalf("CreateConflictWorkspace unknown branch = %v", err)
	}
}

func TestReadConflictIndexRejectsAStagedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix semantics")
	}
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "workspace")
	manifest := testSkillTree(t, []skilltree.File{
		{Path: "SKILL.md", Data: []byte("---\nname: alpha\ndescription: d\n---\nBody\n")},
		{Path: "tool.sh", Data: []byte("#!/bin/sh\n"), Executable: true},
	})
	if err := runner.CreateConflictWorkspace(context.Background(), dir, ConflictWorkspaceSpec{
		Base: manifest, Local: manifest, Theirs: manifest, TheirsBranch: ConflictBranchUpstream,
	}); err != nil {
		t.Fatalf("CreateConflictWorkspace: %v", err)
	}
	if err := os.Symlink("SKILL.md", filepath.Join(dir, "link.md")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	gitOutput(t, dir, "add", "--", "link.md")
	if _, err := runner.ReadConflictIndex(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("ReadConflictIndex symlink = %v", err)
	}
	gitOutput(t, dir, "rm", "--cached", "--", "link.md")
	if err := os.Remove(filepath.Join(dir, "link.md")); err != nil {
		t.Fatalf("remove symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("ignored\n"), 0o600); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}
	gitOutput(t, dir, "add", "--", ".DS_Store")
	tree, err := runner.ReadConflictIndex(context.Background(), dir)
	if err != nil {
		t.Fatalf("ReadConflictIndex regular tree: %v", err)
	}
	tool, ok := tree.File("tool.sh")
	if !ok || !tool.Executable {
		t.Fatalf("executable file = %+v, present %v", tool, ok)
	}
	if _, ok := tree.File(".DS_Store"); ok {
		t.Fatal("ignored artifact entered resolved tree")
	}
	head := gitOutput(t, dir, "rev-parse", "HEAD")
	gitOutput(t, dir, "update-index", "--add", "--cacheinfo", "160000", head, "nested")
	gitOutput(t, dir, "update-index", "--skip-worktree", "nested")
	if _, err := runner.ReadConflictIndex(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "gitlink") {
		t.Fatalf("ReadConflictIndex gitlink = %v", err)
	}
}

func TestGitTreeHelpersSurfaceRepositoryFailures(t *testing.T) {
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tree := testSkillTree(t, nil)
	if _, err := runner.DiffTrees(canceled, "local", tree, "upstream", tree); err == nil {
		t.Fatal("DiffTrees with canceled context succeeded")
	}
	dir := t.TempDir()
	if _, err := runner.writeSkillTree(context.Background(), dir, tree); err == nil {
		t.Fatal("writeSkillTree outside a repository succeeded")
	}
	if _, err := runner.readSkillTreeObject(context.Background(), dir, "missing"); err == nil {
		t.Fatal("readSkillTreeObject outside a repository succeeded")
	}
	if err := runner.CreateConflictWorkspace(canceled, filepath.Join(t.TempDir(), "workspace"), ConflictWorkspaceSpec{
		TheirsBranch: ConflictBranchUpstream,
	}); err == nil {
		t.Fatal("CreateConflictWorkspace with canceled context succeeded")
	}
	if err := runner.ConflictIndexReady(context.Background(), dir); err == nil {
		t.Fatal("ConflictIndexReady outside a repository succeeded")
	}
	if _, err := runner.ReadConflictIndex(context.Background(), dir); err == nil {
		t.Fatal("ReadConflictIndex outside a repository succeeded")
	}
}

func TestCreateConflictWorkspaceIgnoresInTreeMergeDriver(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "driver-ran")
	driver := filepath.Join(t.TempDir(), "failing-merge.sh")
	script := "#!/bin/sh\necho ran >\"$AL_MERGE_DRIVER_MARKER\"\nexit 1\n"
	if err := os.WriteFile(driver, []byte(script), 0o700); err != nil { // #nosec G306 -- executable test stub requires owner execute permission.
		t.Fatalf("write driver: %v", err)
	}
	t.Setenv("AL_MERGE_DRIVER_MARKER", marker)
	t.Setenv("GIT_CONFIG_COUNT", "2")
	t.Setenv("GIT_CONFIG_KEY_0", "merge.failing.driver")
	t.Setenv("GIT_CONFIG_VALUE_0", driver+" %O %A %B")
	t.Setenv("GIT_CONFIG_KEY_1", "merge.failing.required")
	t.Setenv("GIT_CONFIG_VALUE_1", "true")

	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	attrs := skilltree.File{Path: ".gitattributes", Data: []byte("notes.md merge=failing\n")}
	manifest := skilltree.File{Path: "SKILL.md", Data: []byte("---\nname: alpha\ndescription: d\n---\nBody\n")}
	dir := filepath.Join(t.TempDir(), "workspace")
	base := testSkillTree(t, []skilltree.File{manifest, attrs, {Path: "notes.md", Data: []byte("shared\n")}})
	local := testSkillTree(t, []skilltree.File{manifest, attrs, {Path: "notes.md", Data: []byte("local\n")}})
	upstream := testSkillTree(t, []skilltree.File{manifest, attrs, {Path: "notes.md", Data: []byte("upstream\n")}})
	if err := runner.CreateConflictWorkspace(context.Background(), dir, ConflictWorkspaceSpec{
		Base: base, Local: local, Theirs: upstream, TheirsBranch: ConflictBranchUpstream,
	}); err != nil {
		t.Fatalf("CreateConflictWorkspace: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("in-tree merge driver ran during the workspace merge")
	}
	got := gitOutput(t, dir, "check-attr", "merge", "--", "notes.md")
	if strings.Contains(got, "merge: failing") {
		t.Fatalf("custom merge driver still selected: %q", got)
	}
	marked, err := os.ReadFile(filepath.Join(dir, "notes.md")) // #nosec G304 -- test-owned temp path.
	if err != nil || !strings.Contains(string(marked), "<<<<<<<") {
		t.Fatalf("expected a default text merge, got %q, err %v", marked, err)
	}
}

func TestCreateConflictWorkspaceCoversDeleteModify(t *testing.T) {
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "workspace")
	base := testSkillTree(t, []skilltree.File{
		{Path: "SKILL.md", Data: []byte("base\n")},
		{Path: "gone.md", Data: []byte("x\n")},
	})
	local := testSkillTree(t, []skilltree.File{
		{Path: "SKILL.md", Data: []byte("base\n")},
	})
	theirs := testSkillTree(t, []skilltree.File{
		{Path: "SKILL.md", Data: []byte("base\n")},
		{Path: "gone.md", Data: []byte("changed\n")},
	})
	if err := runner.CreateConflictWorkspace(context.Background(), dir, ConflictWorkspaceSpec{
		Base: base, Local: local, Theirs: theirs, TheirsBranch: ConflictBranchDestination,
	}); err != nil {
		t.Fatalf("CreateConflictWorkspace: %v", err)
	}
	if err := runner.ConflictIndexReady(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "unmerged") {
		t.Fatalf("delete/modify workspace ready = %v", err)
	}
	unmerged := gitOutput(t, dir, "ls-files", "-u")
	if !strings.Contains(unmerged, "gone.md") {
		t.Fatalf("unmerged entries = %q", unmerged)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- test-controlled arguments.
	cmd.Dir = dir
	cmd.Env = gitenv.WithoutDiscovery()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
