package gitrepo

import (
	"context"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/skilltree"
)

func testSkillTree(t *testing.T, files []skilltree.File) skilltree.Tree {
	t.Helper()
	tree, err := skilltree.NewTree(files)
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	return tree
}

func TestDiffTreesWritesPrefixedUnifiedDiff(t *testing.T) {
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	from := testSkillTree(t, []skilltree.File{
		{Path: "SKILL.md", Data: []byte("one\n")},
		{Path: "notes.md", Data: []byte("local\n")},
	})
	to := testSkillTree(t, []skilltree.File{
		{Path: "SKILL.md", Data: []byte("one\n")},
		{Path: "notes.md", Data: []byte("upstream\n")},
	})

	diff, err := runner.DiffTrees(context.Background(), "local", from, "upstream", to)
	if err != nil {
		t.Fatalf("DiffTrees: %v", err)
	}
	text := string(diff)
	for _, fragment := range []string{
		"diff --git a/local/notes.md b/upstream/notes.md",
		"--- a/local/notes.md",
		"+++ b/upstream/notes.md",
		"-local",
		"+upstream",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("diff %q does not contain %q", text, fragment)
		}
	}
	if strings.Contains(text, "SKILL.md") {
		t.Fatalf("unchanged file appeared in the diff: %s", text)
	}
}

func TestDiffTreesSilentWhenIdentical(t *testing.T) {
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	tree := testSkillTree(t, []skilltree.File{{Path: "SKILL.md", Data: []byte("one\n")}})
	diff, err := runner.DiffTrees(context.Background(), "base", tree, "local", tree)
	if err != nil {
		t.Fatalf("DiffTrees: %v", err)
	}
	if len(diff) != 0 {
		t.Fatalf("identical trees produced output %q", diff)
	}
}

func TestDiffTreesRejectsUnsafeLabels(t *testing.T) {
	runner, err := NewRunner(nil)
	if err != nil {
		t.Skipf("git runner unavailable: %v", err)
	}
	tree := testSkillTree(t, nil)
	if _, err := runner.DiffTrees(context.Background(), "../ours", tree, "local", tree); err == nil {
		t.Fatal("expected an unsafe label to fail")
	}
}
