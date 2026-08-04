package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/skilltree"
)

func TestLoadSkillImportLockFromDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, SkillImportLockFileName)

	// A project with no imports has never written a lock; that is the ordinary
	// empty state, not a failure.
	lock, err := LoadSkillImportLock(path)
	if err != nil {
		t.Fatalf("a missing lock must load as empty: %v", err)
	}
	if len(lock.Entries) != 0 || lock.Version != SkillImportLockVersion {
		t.Fatalf("empty lock = %+v", lock)
	}

	entry := SkillImportLockEntry{
		Repository: "https://example.invalid/s.git", SourcePath: "skills/alpha", RefOmitted: true,
		ResolvedRefName: "main", ResolvedRefType: SkillRefBranch, SourceCommit: strings.Repeat("a", 40),
		UpstreamTreeHash: skilltree.Hash(nil), Tracking: SkillTrackingTracked, Write: SkillWriteNone,
		PushRepository: "https://example.invalid/s.git", SkillName: "alpha",
	}
	data, err := MarshalSkillImportLock(&SkillImportLock{Version: SkillImportLockVersion, Entries: []SkillImportLockEntry{entry}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	loaded, err := LoadSkillImportLock(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	found, ok := FindSkillImportLockEntry(loaded, "https://example.invalid/s.git", "skills/alpha")
	if !ok {
		t.Fatal("the written entry must be findable by repository and source path")
	}
	if found.SkillName != "alpha" {
		t.Fatalf("entry = %+v", found)
	}
	// Lookup is keyed by the exact repository string: Agent Layer never guesses
	// that two spellings name the same remote.
	if _, ok := FindSkillImportLockEntry(loaded, "https://example.invalid/s", "skills/alpha"); ok {
		t.Fatal("a different repository spelling must not match")
	}
	if _, ok := FindSkillImportLockEntry(nil, "r", "p"); ok {
		t.Fatal("a nil lock has no entries")
	}
}

func TestLoadSkillImportLockSurfacesAnUnreadableFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A directory where the lock should be is not "no imports": it is a broken
	// state the user must see.
	path := filepath.Join(dir, SkillImportLockFileName)
	if err := os.Mkdir(path, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := LoadSkillImportLock(path); err == nil {
		t.Fatal("an unreadable lock must fail rather than be treated as empty")
	}
}

func TestMarshalSkillImportLockRejectsNil(t *testing.T) {
	t.Parallel()
	if _, err := MarshalSkillImportLock(nil); err == nil {
		t.Fatal("marshalling a nil lock must fail rather than write an empty file")
	}
}

func TestIsIgnoredSkillResourceNameCoversOnlyTheDocumentedNames(t *testing.T) {
	t.Parallel()
	for _, ignored := range []string{gitDirName, ".DS_Store", "Thumbs.db"} {
		if !IsIgnoredSkillResourceName(ignored) {
			t.Fatalf("%s must be excluded from every skill file set", ignored)
		}
	}
	// Everything else, including dotfiles, is skill content.
	for _, kept := range []string{".gitignore", ".config", "SKILL.md", "scripts", "thumbs.db"} {
		if IsIgnoredSkillResourceName(kept) {
			t.Fatalf("%s must be part of the skill file set", kept)
		}
	}
}

func TestParseSkillSelectorRejectsUnusableInput(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":                       "must not be empty",
		"   ":                    "must not be empty",
		"!":                      "no path after",
		strings.Repeat("a", 600): "exceeds",
		"skills/alpha\x00":       "NUL byte",
		"skills/../../outside":   "must stay inside the repository",
		"skills/alpha/../../../": "must stay inside the repository",
	}
	for selector, want := range cases {
		_, _, err := ParseSkillSelector(selector)
		if err == nil {
			t.Fatalf("selector %q must be rejected", selector)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("selector %q error = %v, want it to mention %q", selector, err, want)
		}
	}
}

func TestFindSkillImportSelectorReturnsFalseForUnusableInput(t *testing.T) {
	t.Parallel()
	cfg, err := parseSkillImportConfig(t, "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := FindSkillImportSelector(cfg.Skills, "r", ""); ok {
		t.Fatal("an unparsable selector must not match anything")
	}
	if _, ok := FindSkillImportSelector(cfg.Skills, "other", "skills/a"); ok {
		t.Fatal("a selector must only match within its own repository")
	}
}

func TestEffectivePushRepositoryPrefersTheConfiguredDestination(t *testing.T) {
	t.Parallel()
	imp := SkillImport{Repository: " https://example.invalid/upstream.git ", PushRepository: " https://example.invalid/fork.git "}
	// Whitespace is trimmed, but nothing else is rewritten, so a fork is a
	// deliberate exact string rather than a guess.
	if got := imp.EffectivePushRepository(); got != "https://example.invalid/fork.git" {
		t.Fatalf("push repository = %q", got)
	}
}
