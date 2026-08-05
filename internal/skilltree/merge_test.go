package skilltree

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// errTextMergerUnavailable stands in for a text merger that could not run.
var errTextMergerUnavailable = errors.New("text merger unavailable")

// lineMerger is a deterministic stand-in for the production text merger. It
// merges when at most one side changed a line region, which is enough to prove
// Merge routes compatible text changes to a merger and conflicts otherwise.
func lineMerger(base, local, remote []byte) ([]byte, bool, error) {
	baseLines := strings.Split(string(base), "\n")
	localLines := strings.Split(string(local), "\n")
	remoteLines := strings.Split(string(remote), "\n")
	if len(baseLines) != len(localLines) || len(baseLines) != len(remoteLines) {
		return nil, true, nil
	}
	merged := make([]string, len(baseLines))
	for i := range baseLines {
		switch {
		case localLines[i] == baseLines[i]:
			merged[i] = remoteLines[i]
		case remoteLines[i] == baseLines[i]:
			merged[i] = localLines[i]
		case localLines[i] == remoteLines[i]:
			merged[i] = localLines[i]
		default:
			return nil, true, nil
		}
	}
	return []byte(strings.Join(merged, "\n")), false, nil
}

func file(path string, content string) File { return File{Path: path, Data: []byte(content)} }

// TestMergeAppliesOneSidedAndCoalescedChanges proves the whole merge matrix in
// one place: one-sided changes apply, identical changes coalesce, and unchanged
// paths survive.
func TestMergeAppliesOneSidedAndCoalescedChanges(t *testing.T) {
	t.Parallel()
	base := mustTree(t, []File{
		file("keep.md", "same"),
		file("local-only.md", "base"),
		file("remote-only.md", "base"),
		file("both-same.md", "base"),
		file("local-deleted.md", "base"),
		file("remote-deleted.md", "base"),
	})
	local := mustTree(t, []File{
		file("keep.md", "same"),
		file("local-only.md", "local"),
		file("remote-only.md", "base"),
		file("both-same.md", "agreed"),
		file("remote-deleted.md", "base"),
		file("local-added.md", "new"),
	})
	remote := mustTree(t, []File{
		file("keep.md", "same"),
		file("local-only.md", "base"),
		file("remote-only.md", "remote"),
		file("both-same.md", "agreed"),
		file("local-deleted.md", "base"),
		file("remote-added.md", "new"),
	})

	merged, conflicts, err := Merge(base, local, remote, lineMerger)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %v", conflicts)
	}

	want := map[string]string{
		"keep.md":         "same",
		"local-only.md":   "local",
		"remote-only.md":  "remote",
		"both-same.md":    "agreed",
		"local-added.md":  "new",
		"remote-added.md": "new",
	}
	for path, content := range want {
		got, ok := merged.File(path)
		if !ok {
			t.Fatalf("merged tree is missing %s", path)
		}
		if !bytes.Equal(got.Data, []byte(content)) {
			t.Fatalf("%s = %q, want %q", path, got.Data, content)
		}
	}
	for _, deleted := range []string{"local-deleted.md", "remote-deleted.md"} {
		if _, ok := merged.File(deleted); ok {
			t.Fatalf("one-sided deletion of %s was not applied", deleted)
		}
	}
}

// TestMergeReportsIncompatibleChanges proves every documented conflict class is
// reported rather than resolved by preference.
func TestMergeReportsIncompatibleChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		base   []File
		local  []File
		remote []File
		want   ConflictKind
	}{
		{
			name:   "same path changed incompatibly",
			base:   []File{file("a.md", "one\n")},
			local:  []File{file("a.md", "local\n")},
			remote: []File{file("a.md", "remote\n")},
			want:   ConflictContent,
		},
		{
			name:   "local deletes what remote modified",
			base:   []File{file("a.md", "one")},
			local:  nil,
			remote: []File{file("a.md", "remote")},
			want:   ConflictDeleteModify,
		},
		{
			name:   "remote deletes what local modified",
			base:   []File{file("a.md", "one")},
			local:  []File{file("a.md", "local")},
			remote: nil,
			want:   ConflictDeleteModify,
		},
		{
			name:   "binary changed on both sides",
			base:   []File{{Path: "a.bin", Data: []byte{0, 1}}},
			local:  []File{{Path: "a.bin", Data: []byte{0, 2}}},
			remote: []File{{Path: "a.bin", Data: []byte{0, 3}}},
			want:   ConflictBinary,
		},
		{
			name:   "executable bit changed differently",
			base:   []File{file("run.sh", "x")},
			local:  []File{{Path: "run.sh", Data: []byte("local"), Executable: true}},
			remote: []File{{Path: "run.sh", Data: []byte("remote")}},
			want:   ConflictMode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			merged, conflicts, err := Merge(mustTree(t, tt.base), mustTree(t, tt.local), mustTree(t, tt.remote), lineMerger)
			if err != nil {
				t.Fatalf("Merge: %v", err)
			}
			if len(conflicts) != 1 {
				t.Fatalf("conflicts = %v, want exactly one", conflicts)
			}
			if conflicts[0].Kind != tt.want {
				t.Fatalf("conflict kind = %q, want %q", conflicts[0].Kind, tt.want)
			}
			if !merged.IsEmpty() {
				t.Fatal("a conflicted merge must not return partial content")
			}
		})
	}
}

// TestMergeRequiresATextMerger proves the merge refuses to guess when no text
// merger is supplied instead of silently preferring one side.
func TestMergeRequiresATextMerger(t *testing.T) {
	t.Parallel()
	if _, _, err := Merge(Tree{}, Tree{}, Tree{}, nil); err == nil {
		t.Fatal("expected a missing text merger to fail")
	}
}

// TestCompareReportsFileLevelDelta proves push derives an accurate delta from
// the locked base to current local content.
func TestCompareReportsFileLevelDelta(t *testing.T) {
	t.Parallel()
	base := mustTree(t, []File{file("keep.md", "same"), file("gone.md", "x"), file("edit.md", "one")})
	next := mustTree(t, []File{file("keep.md", "same"), file("edit.md", "two"), file("new.md", "y")})
	diff := Compare(base, next)

	if strings.Join(diff.Added, ",") != "new.md" {
		t.Fatalf("added = %v", diff.Added)
	}
	if strings.Join(diff.Modified, ",") != "edit.md" {
		t.Fatalf("modified = %v", diff.Modified)
	}
	if strings.Join(diff.Deleted, ",") != "gone.md" {
		t.Fatalf("deleted = %v", diff.Deleted)
	}
	if diff.IsEmpty() {
		t.Fatal("a changed tree reported an empty diff")
	}
	if !Compare(base, base).IsEmpty() {
		t.Fatal("identical trees reported a non-empty diff")
	}
	if strings.Join(diff.Changed(), ",") != "edit.md,gone.md,new.md" {
		t.Fatalf("changed = %v", diff.Changed())
	}
}

// TestMergeSurfacesTextMergerFailures proves a merger that cannot run at all is
// reported as an error rather than treated as a conflict or a clean merge.
func TestMergeSurfacesTextMergerFailures(t *testing.T) {
	t.Parallel()
	failing := func([]byte, []byte, []byte) ([]byte, bool, error) {
		return nil, false, errTextMergerUnavailable
	}
	base := mustTree(t, []File{file("a.md", "one\n")})
	local := mustTree(t, []File{file("a.md", "local\n")})
	remote := mustTree(t, []File{file("a.md", "remote\n")})

	if _, _, err := Merge(base, local, remote, failing); err == nil {
		t.Fatal("expected the merger failure to be returned")
	}
}

// TestMergeCoalescesIdenticalDeletions proves both sides deleting one path is
// not treated as a delete/modify conflict.
func TestMergeCoalescesIdenticalDeletions(t *testing.T) {
	t.Parallel()
	base := mustTree(t, []File{file("keep.md", "x"), file("gone.md", "y")})
	side := mustTree(t, []File{file("keep.md", "x")})

	merged, conflicts, err := Merge(base, side, side, lineMerger)
	if err != nil || len(conflicts) != 0 {
		t.Fatalf("Merge = (%v, %v)", conflicts, err)
	}
	if _, ok := merged.File("gone.md"); ok {
		t.Fatal("an agreed deletion was not applied")
	}
	if merged.Len() != 1 {
		t.Fatalf("merged tree = %v", merged.Paths())
	}
}
