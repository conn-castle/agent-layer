package skilltree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/skillvalidator"
)

func writeFile(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil { // #nosec G306 -- mode is the behavior under test.
		t.Fatalf("write %s: %v", path, err)
	}
}

func manifest(name string) string {
	return "---\nname: " + name + "\ndescription: A test skill.\n---\nBody\n"
}

// TestReadIncludesEveryRegularResource proves the canonical content definition:
// hidden files count, nested directories are flattened to slash-normalized
// relative paths, the executable bit is preserved, and only the three named
// artifacts are ignored.
func TestReadIncludesEveryRegularResource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), manifest("alpha"), 0o644)
	writeFile(t, filepath.Join(dir, ".hidden"), "hidden", 0o644)
	writeFile(t, filepath.Join(dir, "scripts", "run.sh"), "#!/bin/sh\n", 0o755)
	writeFile(t, filepath.Join(dir, ".git", "config"), "[core]\n", 0o644)
	writeFile(t, filepath.Join(dir, ".DS_Store"), "junk", 0o644)
	writeFile(t, filepath.Join(dir, "assets", "Thumbs.db"), "junk", 0o644)

	tree, err := Read(OSFS{}, dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	got := strings.Join(tree.Paths(), ",")
	want := ".hidden,SKILL.md,scripts/run.sh"
	if got != want {
		t.Fatalf("paths = %q, want %q", got, want)
	}
	script, ok := tree.File("scripts/run.sh")
	if !ok || !script.Executable {
		t.Fatalf("executable bit lost: %+v ok=%v", script, ok)
	}
}

// TestReadRejectsUnsafeNodes proves every tree refuses a symlink without
// dereferencing or silently skipping it.
func TestReadRejectsUnsafeNodes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), manifest("alpha"), 0o644)
	writeFile(t, filepath.Join(dir, "target.txt"), "secret", 0o644)
	if err := os.Symlink(filepath.Join(dir, "target.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, err := Read(OSFS{}, dir); err == nil {
		t.Fatal("expected the tree reader to reject a symlink")
	} else if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("error %q does not name the node type", err)
	}
}

// TestHashDistinguishesContentPathAndMode proves the canonical hash reacts to
// each dimension it claims to cover and is stable for identical content.
func TestHashDistinguishesContentPathAndMode(t *testing.T) {
	t.Parallel()
	base := mustTree(t, []File{{Path: "a.md", Data: []byte("one")}, {Path: "b.md", Data: []byte("two")}})
	same := mustTree(t, []File{{Path: "b.md", Data: []byte("two")}, {Path: "a.md", Data: []byte("one")}})
	if base.Hash() != same.Hash() {
		t.Fatal("hash depends on input order")
	}

	variants := map[string]Tree{
		"content": mustTree(t, []File{{Path: "a.md", Data: []byte("ONE")}, {Path: "b.md", Data: []byte("two")}}),
		"path":    mustTree(t, []File{{Path: "a2.md", Data: []byte("one")}, {Path: "b.md", Data: []byte("two")}}),
		"mode":    mustTree(t, []File{{Path: "a.md", Data: []byte("one"), Executable: true}, {Path: "b.md", Data: []byte("two")}}),
	}
	for name, variant := range variants {
		if variant.Hash() == base.Hash() {
			t.Fatalf("hash ignores %s changes", name)
		}
	}
}

// TestHashResistsPathContentAmbiguity proves the length-prefixed encoding
// prevents two different trees from colliding by concatenation.
func TestHashResistsPathContentAmbiguity(t *testing.T) {
	t.Parallel()
	left := mustTree(t, []File{{Path: "ab", Data: []byte("c")}})
	right := mustTree(t, []File{{Path: "a", Data: []byte("bc")}})
	if left.Hash() == right.Hash() {
		t.Fatal("path and content boundaries are ambiguous in the hash encoding")
	}
}

func mustTree(t *testing.T, files []File) Tree {
	t.Helper()
	tree, err := NewTree(files)
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	return tree
}

// TestNewTreeRejectsDuplicatePaths proves a tree cannot hold two entries for
// one path, which would make hashing and materialization ambiguous.
func TestNewTreeRejectsDuplicatePaths(t *testing.T) {
	t.Parallel()
	if _, err := NewTree([]File{{Path: "a.md"}, {Path: "a.md"}}); err == nil {
		t.Fatal("expected duplicate paths to be rejected")
	}
}

// TestTreeOwnsItsBytes proves callers cannot mutate a canonical snapshot
// through either the constructor input or an accessor result.
func TestTreeOwnsItsBytes(t *testing.T) {
	t.Parallel()
	data := []byte("original")
	tree := mustTree(t, []File{{Path: "notes.md", Data: data}})
	data[0] = 'X'

	files := tree.Files()
	files[0].Data[1] = 'X'
	file, ok := tree.File("notes.md")
	if !ok {
		t.Fatal("tree lost notes.md")
	}
	file.Data[2] = 'X'

	unchanged, _ := tree.File("notes.md")
	if got := string(unchanged.Data); got != "original" {
		t.Fatalf("tree bytes changed through an external slice: %q", got)
	}
}

// TestValidateSkillEnforcesImportRules proves import acceptance requires a
// canonical manifest, required metadata, a safe name, a name matching the
// selected directory, and valid required frontmatter.
func TestValidateSkillEnforcesImportRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		files      []File
		sourcePath string
		wantErr    string
	}{
		{
			name:       "valid",
			files:      []File{{Path: "SKILL.md", Data: []byte(manifest("alpha"))}},
			sourcePath: "skills/alpha",
		},
		{
			name:       "missing manifest",
			files:      []File{{Path: "README.md", Data: []byte("hi")}},
			sourcePath: "skills/alpha",
			wantErr:    "has no SKILL.md",
		},
		{
			name:       "lowercase manifest",
			files:      []File{{Path: "skill.md", Data: []byte(manifest("alpha"))}},
			sourcePath: "skills/alpha",
			wantErr:    "canonical SKILL.md",
		},
		{
			name:       "name does not match directory",
			files:      []File{{Path: "SKILL.md", Data: []byte(manifest("beta"))}},
			sourcePath: "skills/alpha",
			wantErr:    "must match canonical source name",
		},
		{
			name:       "unknown frontmatter field passes through",
			files:      []File{{Path: "SKILL.md", Data: []byte("---\nname: alpha\ndescription: d\nmodel: opus\n---\nBody\n")}},
			sourcePath: "skills/alpha",
		},
		{
			name:       "missing description",
			files:      []File{{Path: "SKILL.md", Data: []byte("---\nname: alpha\n---\nBody\n")}},
			sourcePath: "skills/alpha",
			wantErr:    "description",
		},
		{
			name:       "whitespace-bearing required keys are not required keys",
			files:      []File{{Path: "SKILL.md", Data: []byte("---\n\" name \": alpha\n\" description \": d\n---\nBody\n")}},
			sourcePath: "skills/alpha",
			wantErr:    "missing required frontmatter field \"name\"",
		},
		{
			name:       "unsafe name",
			files:      []File{{Path: "SKILL.md", Data: []byte("---\nname: Alpha_One\ndescription: d\n---\nBody\n")}},
			sourcePath: "skills/Alpha_One",
			wantErr:    "lowercase letters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tree := mustTree(t, tt.files)
			info, err := ValidateSkill(tree, tt.sourcePath)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateSkill: %v", err)
				}
				if info.Name != "alpha" {
					t.Fatalf("name = %q, want alpha", info.Name)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestValidateSkillAcceptsLongSkills proves the size recommendation is not
// treated as a blocking import failure, since it describes style rather than
// projectability.
func TestValidateSkillAcceptsLongSkills(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("line\n", skillvalidator.MaxRecommendedSkillLines+10)
	tree := mustTree(t, []File{{Path: "SKILL.md", Data: []byte("---\nname: alpha\ndescription: d\n---\n" + body)}})
	if _, err := ValidateSkill(tree, "skills/alpha"); err != nil {
		t.Fatalf("a long skill must import: %v", err)
	}
}

// TestMaterializeWritesExactContent proves materialization preserves bytes and
// the executable bit and creates intermediate directories.
func TestMaterializeWritesExactContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tree := mustTree(t, []File{
		{Path: "SKILL.md", Data: []byte(manifest("alpha"))},
		{Path: "scripts/run.sh", Data: []byte("#!/bin/sh\n"), Executable: true},
	})
	if err := Materialize(tree, dir); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	roundTrip, err := Read(OSFS{}, dir)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if roundTrip.Hash() != tree.Hash() {
		t.Fatal("materialized tree does not round-trip to the same canonical hash")
	}
}

// TestValidateRelativePathRejectsEscapes proves a tree path can never escape
// its skill root.
func TestValidateRelativePathRejectsEscapes(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"", "/abs", "../up", "a/../b", "a\\b", "a/./b"} {
		if err := ValidateRelativePath(path); err == nil {
			t.Fatalf("path %q was accepted", path)
		}
	}
	if err := ValidateRelativePath("a/b/c.md"); err != nil {
		t.Fatalf("normalized path rejected: %v", err)
	}
}

// TestTreeAccessorsDescribeContent proves the small query surface callers rely
// on to decide whether two versions differ and whether a directory is a skill.
func TestTreeAccessorsDescribeContent(t *testing.T) {
	t.Parallel()
	tree := mustTree(t, []File{
		{Path: SkillManifestName, Data: []byte(manifest("alpha"))},
		{Path: "notes.md", Data: []byte("notes")},
	})
	if tree.Len() != 2 || tree.IsEmpty() {
		t.Fatalf("Len = %d, IsEmpty = %v", tree.Len(), tree.IsEmpty())
	}
	if !tree.Equal(mustTree(t, tree.Files())) {
		t.Fatal("identical trees compared unequal")
	}
	if tree.Equal(mustTree(t, []File{{Path: "notes.md", Data: []byte("other")}})) {
		t.Fatal("different trees compared equal")
	}
	if !HasManifest(tree) {
		t.Fatal("a tree with SKILL.md was not recognized as a skill")
	}
	if HasManifest(mustTree(t, []File{{Path: "README.md", Data: []byte("x")}})) {
		t.Fatal("an ordinary directory was recognized as a skill")
	}
	if _, ok := tree.File("absent.md"); ok {
		t.Fatal("File reported a path the tree does not carry")
	}
	if NormalizeName("  Alpha  ") == "" {
		t.Fatal("NormalizeName produced an empty name for non-empty input")
	}
	if NormalizeName("ﬁle") != "file" {
		t.Fatalf("NormalizeName did not apply compatibility normalization: %q", NormalizeName("ﬁle"))
	}
}

// TestReadRejectsNonRegularNodesByType proves the strict policy names the node
// type it refused, which is what makes the failure actionable.
func TestReadRejectsNonRegularNodesByType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), manifest("alpha"), 0o600)
	fifo := filepath.Join(dir, "pipe")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("named pipes unavailable: %v", err)
	}

	_, err := Read(OSFS{}, dir)
	if err == nil {
		t.Fatal("expected a named pipe to be rejected")
	}
	if !strings.Contains(err.Error(), "named pipe") {
		t.Fatalf("error %q does not name the node type", err)
	}
}

// TestMaterializeRefusesAnEscapingPath proves a tree that somehow carries an
// unsafe path cannot write outside its destination.
func TestMaterializeRefusesAnEscapingPath(t *testing.T) {
	t.Parallel()
	tree := Tree{files: []File{{Path: "../escape.md", Data: []byte("x")}}}
	if err := Materialize(tree, t.TempDir()); err == nil {
		t.Fatal("expected an escaping path to be refused")
	}
}

// TestConflictRendersAStableDescription proves conflict reporting names the
// path and the reason.
func TestConflictRendersAStableDescription(t *testing.T) {
	t.Parallel()
	got := Conflict{Path: "notes.md", Kind: ConflictDeleteModify}.Error()
	if got != "notes.md (delete/modify)" {
		t.Fatalf("conflict description = %q", got)
	}
}
