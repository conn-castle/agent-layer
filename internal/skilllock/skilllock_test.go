package skilllock

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validEntry(name string) Entry {
	return Entry{
		Name:          name,
		Repository:    "https://example.test/skills.git",
		Selector:      "skills/" + name,
		SelectedPath:  "skills/" + name,
		ConfiguredRef: "",
		ResolvedRef:   "main",
		RefKind:       RefKindBranch,
		Tracking:      TrackingTracked,
		Commit:        "0123456789abcdef0123456789abcdef01234567",
		TreeHash:      "sha256:" + strings.Repeat("ab", 32),
	}
}

// lockDocument renders a lock file containing exactly these entries, so a
// rejection case can differ from trustworthy state by one field.
func lockDocument(t *testing.T, entries ...Entry) string {
	t.Helper()
	data, err := json.Marshal(File{Version: Version, Skills: entries})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return string(data)
}

// mutated returns a valid entry with one field replaced.
func mutated(name string, apply func(*Entry)) Entry {
	entry := validEntry(name)
	apply(&entry)
	return entry
}

// TestSaveProducesDeterministicSortedOutput proves the serialized lock is
// stable regardless of insertion order, so committing it never produces
// spurious diffs.
func TestSaveProducesDeterministicSortedOutput(t *testing.T) {
	t.Parallel()
	first := New()
	first.Upsert(validEntry("zeta"))
	first.Upsert(validEntry("alpha"))

	second := New()
	second.Upsert(validEntry("alpha"))
	second.Upsert(validEntry("zeta"))

	firstData, err := first.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	secondData, err := second.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(firstData) != string(secondData) {
		t.Fatal("serialized lock depends on insertion order")
	}
	if strings.Index(string(firstData), `"alpha"`) > strings.Index(string(firstData), `"zeta"`) {
		t.Fatal("entries are not sorted by name")
	}
}

// TestSaveAndLoadRoundTrip proves saved state reloads identically.
func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "skills.lock.json")
	file := New()
	file.Upsert(validEntry("alpha"))
	if err := file.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, ok := loaded.Entry("alpha")
	if !ok || entry.Commit != validEntry("alpha").Commit {
		t.Fatalf("round-tripped entry = %+v", entry)
	}
}

// TestLoadDistinguishesMissingFromMalformed proves callers can treat "no
// imports yet" as normal while a corrupt lock fails loudly rather than being
// silently replaced by an empty merge base.
func TestLoadDistinguishesMissingFromMalformed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.json")
	if _, err := Load(missing); !errors.Is(err, ErrMissing) {
		t.Fatalf("missing file error = %v, want ErrMissing", err)
	}

	malformed := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(malformed, []byte("{"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load(malformed); !errors.Is(err, ErrMalformed) {
		t.Fatalf("malformed file error = %v, want ErrMalformed", err)
	}
}

// TestParseRejectsUntrustworthyState proves every way a lockfile can fail to
// establish a trustworthy merge base is reported rather than tolerated.
func TestParseRejectsUntrustworthyState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "unknown schema version", data: `{"version":99,"skills":[]}`, want: "unsupported schema version"},
		{name: "unknown field", data: `{"version":1,"skills":[],"extra":true}`, want: "unknown field"},
		{name: "trailing content", data: `{"version":1,"skills":[]}{}`, want: "trailing content"},
		{
			name: "missing commit",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.Commit = "" })),
			want: "commit is required",
		},
		{
			name: "invalid ref kind",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.RefKind = "guess" })),
			want: "ref_kind",
		},
		{
			name: "duplicate skill name",
			data: lockDocument(t, validEntry("alpha"),
				mutated("alpha", func(e *Entry) { e.Selector, e.SelectedPath = "other/alpha", "other/alpha" })),
			want: "already recorded",
		},
		// A path that escapes the destination checkout would let a corrupt lock
		// direct RemoveAll and materialization outside the isolated working
		// repository during push.
		{
			name: "escaping selected path",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.SelectedPath = "../../etc/alpha" })),
			want: "selected_path",
		},
		{
			name: "selected path that is not the skill",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.SelectedPath = "skills/other" })),
			want: "does not end in the skill name",
		},
		{
			name: "unresolved selected path pattern",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.SelectedPath = "skills/*" })),
			want: "not a resolved skill root",
		},
		{
			name: "skill name that is not a directory name",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.Name, e.Selector, e.SelectedPath = "../escape", "skills/x", "skills/x" })),
			want: "not a directory name",
		},
		{
			name: "exclusion selector",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.Selector = "!skills/alpha" })),
			want: "exclusion",
		},
		// Any tracking value other than "tracked" exempts an entry from the
		// source-advance check, so an unrecognized one could suppress stale-push
		// protection entirely.
		{
			name: "unknown tracking mode",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.Tracking = "frozen" })),
			want: "tracking",
		},
		{
			name: "tracked tag",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.RefKind, e.ResolvedRef = RefKindTag, "v1.0.0" })),
			want: "requires ref_kind",
		},
		{
			name: "commit that is not an object id",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.Commit = "HEAD~1" })),
			want: "git object id",
		},
		{
			name: "tree hash without the canonical algorithm",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.TreeHash = strings.Repeat("ab", 32) })),
			want: "tree_hash",
		},
		{
			name: "commit ref kind whose resolved ref is not the commit",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.RefKind, e.Tracking = RefKindCommit, TrackingPinned })),
			want: "resolved_ref",
		},
		{
			name: "unnormalized repository",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.Repository = "https://example.test/skills.git/" })),
			want: "not normalized",
		},
		// A dot-prefixed directory is skipped by imported-skill enumeration, so
		// an entry claiming one would always read as a missing skill.
		{
			name: "hidden skill name",
			data: lockDocument(t, mutated("alpha", func(e *Entry) { e.Name, e.Selector, e.SelectedPath = ".hidden", "skills/.hidden", "skills/.hidden" })),
			want: "must not start with a dot",
		},
		// Overlapping owners cannot both be reconciled against one source.
		{
			name: "nested selected paths in one repository",
			data: lockDocument(t,
				mutated("skills", func(e *Entry) { e.Selector, e.SelectedPath = "skills", "skills" }),
				validEntry("alpha")),
			want: "overlap",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(tt.data), "skills.lock.json")
			if err == nil {
				t.Fatalf("expected an error containing %q", tt.want)
			}
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("error %v does not wrap ErrMalformed", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

// TestCloneIsolatesPendingState proves an operation can build its next lock
// without mutating the snapshot it was planned against.
func TestCloneIsolatesPendingState(t *testing.T) {
	t.Parallel()
	original := New()
	original.Upsert(validEntry("alpha"))

	clone := original.Clone()
	clone.Upsert(validEntry("beta"))
	if !clone.Remove("alpha") {
		t.Fatal("Remove reported no entry for a present skill")
	}

	if names := strings.Join(original.Names(), ","); names != "alpha" {
		t.Fatalf("original mutated by clone: %s", names)
	}
	if names := strings.Join(clone.Names(), ","); names != "beta" {
		t.Fatalf("clone = %s, want beta", names)
	}
	if clone.Remove("absent") {
		t.Fatal("Remove reported success for an absent skill")
	}
}

// TestUpsertReplacesAnExistingEntry proves advancing a skill's recorded state
// rewrites its entry rather than adding a second one for the same name.
func TestUpsertReplacesAnExistingEntry(t *testing.T) {
	t.Parallel()
	file := New()
	file.Upsert(validEntry("alpha"))
	advanced := validEntry("alpha")
	advanced.Commit = "89abcdef0123456789abcdef0123456789abcdef"
	file.Upsert(advanced)

	if len(file.Skills) != 1 {
		t.Fatalf("skills = %+v, want one entry", file.Skills)
	}
	entry, _ := file.Entry("alpha")
	if entry.Commit != advanced.Commit {
		t.Fatalf("commit = %s, want the advanced value", entry.Commit)
	}
	if _, ok := file.Entry("absent"); ok {
		t.Fatal("Entry reported a skill the lock does not carry")
	}
}

// TestSaveReportsAWriteFailure proves an unwritable lock path fails loudly
// instead of leaving the caller believing state was recorded.
func TestSaveReportsAWriteFailure(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "readonly")
	if err := os.MkdirAll(dir, 0o500); err != nil { // #nosec G301 -- an unwritable directory is the condition under test.
		t.Fatalf("mkdir: %v", err)
	}
	file := New()
	file.Upsert(validEntry("alpha"))
	if err := file.Save(filepath.Join(dir, "skills.lock.json")); err == nil {
		t.Fatal("expected an unwritable lock path to fail")
	}
}

// TestMarshalRefusesToPersistUntrustworthyState proves the write path enforces
// the same invariants the read path does, so a producer bug cannot record a
// lock that the next Load would reject as malformed — which would strand the
// project with no usable merge base.
func TestMarshalRefusesToPersistUntrustworthyState(t *testing.T) {
	t.Parallel()
	file := New()
	file.Upsert(mutated("alpha", func(e *Entry) { e.Commit = "HEAD" }))

	if _, err := file.Marshal(); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Marshal error = %v, want ErrMalformed", err)
	}
	if err := file.Save(filepath.Join(t.TempDir(), "skills.lock.json")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Save error = %v, want ErrMalformed", err)
	}
}
