package skillimports

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tree(t *testing.T, files ...File) *Tree {
	t.Helper()
	built, err := NewTree(files)
	if err != nil {
		t.Fatalf("build tree: %v", err)
	}
	return built
}

func file(path string, content string) File {
	return File{Path: path, Data: []byte(content)}
}

func TestTreeHashDistinguishesContentPathAndExecutableBit(t *testing.T) {
	t.Parallel()
	base := tree(t, file("a.md", "one"), file("b.md", "two"))

	cases := map[string]*Tree{
		"same content in a different order": tree(t, file("b.md", "two"), file("a.md", "one")),
	}
	if got := cases["same content in a different order"].Hash(); got != base.Hash() {
		t.Fatalf("hash must not depend on input order")
	}

	different := map[string]*Tree{
		"changed content":     tree(t, file("a.md", "ONE"), file("b.md", "two")),
		"renamed path":        tree(t, file("a2.md", "one"), file("b.md", "two")),
		"removed file":        tree(t, file("a.md", "one")),
		"added file":          tree(t, file("a.md", "one"), file("b.md", "two"), file("c.md", "")),
		"executable bit only": tree(t, File{Path: "a.md", Data: []byte("one"), Executable: true}, file("b.md", "two")),
	}
	for name, candidate := range different {
		if candidate.Hash() == base.Hash() {
			t.Fatalf("%s must change the tree hash", name)
		}
	}
}

func TestTreeHashIsNotConfusedByPathAndContentBoundaries(t *testing.T) {
	t.Parallel()
	// Two different file sets whose concatenated bytes are identical must not
	// collide, or a crafted skill could impersonate a clean tree.
	left := tree(t, file("ab", "cd"))
	right := tree(t, file("a", "bcd"))
	if left.Hash() == right.Hash() {
		t.Fatalf("path and content boundaries must be unambiguous in the hash")
	}
}

func TestReadLocalTreeExcludesOnlyTheDocumentedNames(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(relative string, content string, mode os.FileMode) {
		target := filepath.Join(dir, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(target, []byte(content), mode); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	write(SkillManifestName, "---\nname: x\ndescription: y\n---\n", 0o644)
	write(".hidden", "kept", 0o644)
	write("nested/.also-hidden", "kept", 0o644)
	write("scripts/run.sh", "#!/bin/sh\n", 0o755)
	write(".DS_Store", "noise", 0o644)
	write("nested/Thumbs.db", "noise", 0o644)
	write(".git/config", "noise", 0o644)

	built, err := ReadLocalTree(dir)
	if err != nil {
		t.Fatalf("read tree: %v", err)
	}
	got := strings.Join(built.Paths(), ",")
	want := ".hidden,SKILL.md,nested/.also-hidden,scripts/run.sh"
	if got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	script, ok := built.Lookup("scripts/run.sh")
	if !ok || !script.Executable {
		t.Fatalf("the executable bit must be part of the canonical tree")
	}
}

func TestReadLocalTreeRejectsIrregularNodes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SkillManifestName), []byte("---\nname: x\n---\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(dir, "link")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if _, err := ReadLocalTree(dir); err == nil {
		t.Fatal("a symlink inside a managed skill must fail rather than be followed or skipped")
	}
}

func TestWriteTreeRoundTripsThroughDisk(t *testing.T) {
	t.Parallel()
	original := tree(t,
		file(SkillManifestName, "---\nname: x\ndescription: y\n---\n"),
		File{Path: "scripts/run.sh", Data: []byte("#!/bin/sh\n"), Executable: true},
		file("nested/deep/.keep", ""),
	)
	dir := filepath.Join(t.TempDir(), "skill")
	if err := WriteTree(original, dir); err != nil {
		t.Fatalf("write tree: %v", err)
	}
	roundTripped, err := ReadLocalTree(dir)
	if err != nil {
		t.Fatalf("read tree: %v", err)
	}
	if roundTripped.Hash() != original.Hash() {
		t.Fatalf("materializing and re-reading a tree must preserve its canonical hash")
	}
}

func TestWriteTreeResetsAStaleExecutableBit(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "skill")
	executable := tree(t, File{Path: "run.sh", Data: []byte("#!/bin/sh\n"), Executable: true})
	if err := WriteTree(executable, dir); err != nil {
		t.Fatalf("write executable tree: %v", err)
	}
	// Upstream drops the executable bit; republishing over the same path must
	// apply that change instead of inheriting the old mode.
	plain := tree(t, file("run.sh", "#!/bin/sh\n"))
	if err := WriteTree(plain, dir); err != nil {
		t.Fatalf("write plain tree: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "run.sh"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != RegularFileMode {
		t.Fatalf("mode = %v, want %v", info.Mode().Perm(), RegularFileMode)
	}
}

func TestIsBinaryUsesTheNamedInspectionWindow(t *testing.T) {
	t.Parallel()
	if IsBinary([]byte("plain text\nwith lines\n")) {
		t.Fatal("text must not be classified as binary")
	}
	if !IsBinary([]byte("head\x00tail")) {
		t.Fatal("a NUL byte in the window makes the file binary")
	}
	// A NUL past the window is deliberately not inspected, matching the
	// documented bound rather than scanning arbitrarily large files.
	late := append(repeatByte(binaryInspectionWindow, 'a'), 0)
	if IsBinary(late) {
		t.Fatal("a NUL beyond the inspection window must not be inspected")
	}
}

func repeatByte(count int, value byte) []byte {
	out := make([]byte, count)
	for i := range out {
		out[i] = value
	}
	return out
}

// stubMerger records whether the text merger was consulted and returns a fixed
// outcome, so tree-level merge rules are tested without invoking git.
type stubMerger struct {
	merged     string
	conflicted bool
	calls      int
}

func (m *stubMerger) MergeText(_ context.Context, _ []byte, _ []byte, _ []byte, _ MergeLabels) ([]byte, bool, error) {
	m.calls++
	if m.conflicted {
		return nil, true, nil
	}
	return []byte(m.merged), false, nil
}

func TestMergeTreesAppliesOneSidedChanges(t *testing.T) {
	t.Parallel()
	base := tree(t, file("keep.md", "base"), file("upstream.md", "v1"), file("local.md", "v1"))
	local := tree(t, file("keep.md", "base"), file("upstream.md", "v1"), file("local.md", "local edit"), file("new-local.md", "added"))
	other := tree(t, file("keep.md", "base"), file("upstream.md", "v2"), file("local.md", "v1"), file("new-upstream.md", "added"))

	merger := &stubMerger{}
	merged, conflicts, err := MergeTrees(context.Background(), base, local, other, MergeLabels{}, merger)
	if err != nil || len(conflicts) > 0 {
		t.Fatalf("unexpected conflicts %v err %v", conflicts, err)
	}
	if merger.calls != 0 {
		t.Fatalf("one-sided changes must not need a text merge, got %d calls", merger.calls)
	}
	expect := map[string]string{
		"keep.md":         "base",
		"upstream.md":     "v2",
		"local.md":        "local edit",
		"new-local.md":    "added",
		"new-upstream.md": "added",
	}
	for path, want := range expect {
		got, ok := merged.Lookup(path)
		if !ok {
			t.Fatalf("%s missing from the merge result", path)
		}
		if string(got.Data) != want {
			t.Fatalf("%s = %q, want %q", path, got.Data, want)
		}
	}
}

func TestMergeTreesTreatsRenameAsDeletePlusAdd(t *testing.T) {
	t.Parallel()
	base := tree(t, file("old.md", "content"))
	local := tree(t, file("new.md", "content"))
	other := tree(t, file("old.md", "content"))

	merged, conflicts, err := MergeTrees(context.Background(), base, local, other, MergeLabels{}, &stubMerger{})
	if err != nil || len(conflicts) > 0 {
		t.Fatalf("unexpected conflicts %v err %v", conflicts, err)
	}
	if _, ok := merged.Lookup("old.md"); ok {
		t.Fatal("the local deletion half of the rename was not applied")
	}
	if _, ok := merged.Lookup("new.md"); !ok {
		t.Fatal("the local addition half of the rename was not applied")
	}
}

func TestMergeTreesReportsDeleteModifyConflict(t *testing.T) {
	t.Parallel()
	base := tree(t, file("notes.md", "base"))
	local := tree(t)
	other := tree(t, file("notes.md", "upstream edit"))

	_, conflicts, err := MergeTrees(context.Background(), base, local, other,
		MergeLabels{Local: "local", Other: "upstream"}, &stubMerger{})
	if err != nil {
		t.Fatalf("merge error: %v", err)
	}
	if len(conflicts) != 1 || conflicts[0].Path != "notes.md" {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	requireContains(t, conflicts[0].Reason, "deleted in local and modified in upstream")
}

func TestMergeTreesAcceptsIdenticalChangesOnBothSides(t *testing.T) {
	t.Parallel()
	base := tree(t, file("notes.md", "base"))
	same := tree(t, file("notes.md", "identical new content"))

	merger := &stubMerger{conflicted: true}
	merged, conflicts, err := MergeTrees(context.Background(), base, same, same, MergeLabels{}, merger)
	if err != nil || len(conflicts) > 0 {
		t.Fatalf("identical changes must apply cleanly: %v %v", conflicts, err)
	}
	if merger.calls != 0 {
		t.Fatal("identical changes must not reach the text merger")
	}
	got, _ := merged.Lookup("notes.md")
	if string(got.Data) != "identical new content" {
		t.Fatalf("content = %q", got.Data)
	}
}

func TestMergeTreesConflictsOnDivergentExecutableBit(t *testing.T) {
	t.Parallel()
	base := tree(t, file("run.sh", "#!/bin/sh\n"))
	local := tree(t, File{Path: "run.sh", Data: []byte("#!/bin/sh\nlocal\n"), Executable: true})
	other := tree(t, file("run.sh", "#!/bin/sh\nupstream\n"))

	_, conflicts, err := MergeTrees(context.Background(), base, local, other,
		MergeLabels{Local: "local", Other: "upstream"}, &stubMerger{})
	if err != nil {
		t.Fatalf("merge error: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	// Merging the text but silently picking one mode would change whether the
	// script is runnable without telling anyone.
	requireContains(t, conflicts[0].Reason, "executable bit")
}

func TestGitTextMergerMergesAndDetectsConflicts(t *testing.T) {
	hermeticGitEnv(t)
	merger := GitTextMerger{Runner: ExecGitRunner{}, TempDir: t.TempDir()}
	labels := MergeLabels{Base: "base", Local: "local", Other: "upstream"}

	base := []byte("one\ntwo\nthree\n")
	local := []byte("ONE\ntwo\nthree\n")
	other := []byte("one\ntwo\nTHREE\n")
	merged, conflicted, err := merger.MergeText(t.Context(), base, local, other, labels)
	if err != nil || conflicted {
		t.Fatalf("non-overlapping edits must merge: conflicted=%v err=%v", conflicted, err)
	}
	if string(merged) != "ONE\ntwo\nTHREE\n" {
		t.Fatalf("merged = %q", merged)
	}

	_, conflicted, err = merger.MergeText(t.Context(), base, []byte("A\ntwo\nthree\n"), []byte("B\ntwo\nthree\n"), labels)
	if err != nil {
		t.Fatalf("merge error: %v", err)
	}
	if !conflicted {
		t.Fatal("overlapping edits to the same line must conflict")
	}
}
