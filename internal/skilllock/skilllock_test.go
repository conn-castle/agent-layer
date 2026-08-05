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
		// Sorting alone does not put an ancestor next to its descendant: every
		// byte below '/' sorts ahead of it, so "skills-old" lands between
		// "skills" and "skills/alpha" and a neighbour-only check would accept
		// two overlapping editable owners.
		{
			name: "nested selected paths separated by a sibling",
			data: lockDocument(t,
				mutated("skills", func(e *Entry) { e.Selector, e.SelectedPath = "skills", "skills" }),
				mutated("skills-old", func(e *Entry) { e.Selector, e.SelectedPath = "skills-old", "skills-old" }),
				validEntry("alpha")),
			want: "overlap",
		},
		// A repository URL carrying a credential would be persisted here and
		// echoed back in status output and Git errors. A hand-edited lock is
		// exactly how one could arrive without passing configuration
		// validation, so the same rule is enforced on the way in.
		{
			name: "repository embeds a password",
			data: lockDocument(t, mutated("alpha", func(e *Entry) {
				e.Repository = "https://user:pa55phrase@example.test/skills.git"
			})),
			want: "literal password",
		},
		{
			name: "repository embeds a query secret",
			data: lockDocument(t, mutated("alpha", func(e *Entry) {
				e.Repository = "https://example.test/skills.git?access_token=ghp_literalvalue"
			})),
			want: `"access_token" query parameter`,
		},
		{
			name: "repository hides literal userinfo behind a placeholder scheme",
			data: lockDocument(t, mutated("alpha", func(e *Entry) {
				e.Repository = "${AL_SCHEME}://ghp_literalvalue@example.test/skills.git"
			})),
			want: "literal credentials",
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

// TestValidateRepositoryRejectsOnlyEmbeddedCredentials proves the credential
// rule is precise: Agent Layer never reads or stores a secret, so a repository
// URL that carries one is refused before it reaches config.toml, this lockfile,
// status output, or a Git command error. The ordinary SSH identity forms carry
// a username rather than a secret and stay accepted.
func TestValidateRepositoryRejectsOnlyEmbeddedCredentials(t *testing.T) {
	t.Parallel()
	accepted := []string{
		"https://example.test/skills.git",
		"git@github.com:org/skills.git",
		"ssh://git@example.test/org/skills.git",
		"git://git@example.test/org/skills.git",
		"/local/path/to/skills.git",
		"file:///local/skills.git",
		// A placeholder is not a secret: it is the text that stays canonical in
		// configuration and in this lockfile, and the value it names is resolved
		// only at the Git access boundary.
		"https://${AL_SKILLS_TOKEN}@example.test/skills.git",
		"https://oauth2:${AL_SKILLS_TOKEN}@example.test/skills.git",
		"https://${AL_SKILLS_USER}:${AL_SKILLS_TOKEN}@example.test/skills.git",
		"https://${AL_SKILLS_HOST}/skills.git",
		"${AL_SKILLS_REPOSITORY}",
		// A query value that is itself a reference carries no secret text.
		"https://example.test/skills.git?access_token=${AL_SKILLS_TOKEN}",
		// An ordinary query parameter is not a credential at all.
		"https://example.test/skills.git?depth=1",
	}
	for _, repository := range accepted {
		t.Run("accepted "+repository, func(t *testing.T) {
			t.Parallel()
			if err := ValidateRepository(repository); err != nil {
				t.Fatalf("ValidateRepository(%q) = %v, want accepted", repository, err)
			}
		})
	}

	// Each case names the literal credential separately, so the no-echo
	// assertion checks the actual value rather than tripping on the prose.
	// #nosec G101 -- invented credentials in a fixture whose whole point is proving such URLs are refused.
	rejected := []struct {
		name       string
		repository string
		want       string
		secret     string
	}{
		{name: "password", repository: "https://user:pa55phrase@example.test/skills.git", want: "literal password", secret: "pa55phrase"},
		{name: "http password", repository: "http://user:pa55phrase@example.test/skills.git", want: "literal password", secret: "pa55phrase"},
		{name: "ssh password", repository: "ssh://git:pa55phrase@example.test/skills.git", want: "literal password", secret: "pa55phrase"},
		{name: "bare https token", repository: "https://ghp_literalvalue@example.test/s.git", want: "literal credentials", secret: "ghp_literalvalue"},
		{name: "uppercase scheme", repository: "HTTPS://ghp_literalvalue@example.test/s.git", want: "literal credentials", secret: "ghp_literalvalue"},
		// A userinfo that only partly uses a placeholder still carries literal
		// secret text, so it is refused rather than partly protected.
		{name: "partly literal password", repository: "https://user:${AL_TOKEN}pa55phrase@example.test/s.git", want: "literal password", secret: "pa55phrase"},
		{name: "partly literal userinfo", repository: "https://${AL_TOKEN}ghp_literalvalue@example.test/s.git", want: "literal credentials", secret: "ghp_literalvalue"},
		// The last "@" ends the userinfo, so a literal password containing "@"
		// is not mistaken for a host separator.
		{name: "password containing at", repository: "https://user:pa55@phrase@example.test/s.git", want: "literal password", secret: "pa55@phrase"},
		// Only the AL_ namespace can ever resolve, so anything else is reported
		// now rather than as a missing value at the first Git access.
		{name: "foreign namespace", repository: "https://${SKILLS_TOKEN}@example.test/s.git", want: "outside the AL_ namespace"},
		// A credential rides in the query string as readily as in userinfo, and
		// the key vocabulary is the one the MCP policy warning already uses.
		{name: "query access token", repository: "https://example.test/s.git?access_token=ghp_literalvalue", want: `"access_token" query parameter`, secret: "ghp_literalvalue"},
		{name: "query api key beside an ordinary parameter", repository: "https://example.test/s.git?depth=1&api_key=ghp_literalvalue", want: `"api_key" query parameter`, secret: "ghp_literalvalue"},
		// The query check does not depend on a parseable scheme or host.
		{name: "query behind a placeholder repository", repository: "${AL_SKILLS_REPOSITORY}?token=ghp_literalvalue", want: `"token" query parameter`, secret: "ghp_literalvalue"},
		// A percent-encoded key must not slip past the vocabulary.
		{name: "encoded query key", repository: "https://example.test/s.git?access%5Ftoken=ghp_literalvalue", want: `"access_token" query parameter`, secret: "ghp_literalvalue"},
		// A scheme built from a placeholder could resolve to https, so literal
		// userinfo behind one is refused rather than assumed to be an identity.
		{name: "placeholder scheme", repository: "${AL_SCHEME}://ghp_literalvalue@example.test/s.git", want: "literal credentials", secret: "ghp_literalvalue"},
		// An unrecognized transport is treated the same way: only ssh and git
		// are known to carry an account name rather than a credential.
		{name: "unknown scheme", repository: "ftps://ghp_literalvalue@example.test/s.git", want: "literal credentials", secret: "ghp_literalvalue"},
	}
	for _, tc := range rejected {
		t.Run("rejected "+tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRepository(tc.repository)
			if err == nil {
				t.Fatalf("ValidateRepository(%q) accepted an embedded credential", tc.repository)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateRepository(%q) = %v, want a message naming %q", tc.repository, err, tc.want)
			}
			// Repeating the credential in the error would reproduce the exposure
			// the rule exists to prevent. Naming the query key is fine: the key
			// is what makes the message actionable, and it is not the secret.
			if tc.secret != "" && strings.Contains(err.Error(), tc.secret) {
				t.Fatalf("ValidateRepository(%q) echoed the credential back: %v", tc.repository, err)
			}
		})
	}
}

// TestValidateRepositoryRestrictsLiteralTransports proves unsupported URL and
// remote-helper schemes fail during configuration validation while every
// supported local and remote spelling remains available.
func TestValidateRepositoryRestrictsLiteralTransports(t *testing.T) {
	t.Parallel()
	for _, repository := range []string{
		"https://example.test/skills.git",
		"ssh://git@example.test/org/skills.git",
		"git://example.test/skills.git",
		"file:///tmp/skills.git",
		"git@example.test:org/skills.git",
		"../skills.git",
	} {
		if err := ValidateRepository(repository); err != nil {
			t.Fatalf("ValidateRepository(%q) = %v", repository, err)
		}
	}
	for _, repository := range []string{
		"http://example.test/skills.git",
		"ftp://example.test/skills.git",
		"ext::sh -c true",
		"ssh::git@example.test/org/skills.git",
		"https::example.test/skills.git",
		"unknown://example.test/skills.git",
	} {
		err := ValidateRepository(repository)
		if err == nil || !strings.Contains(err.Error(), "not supported") {
			t.Fatalf("ValidateRepository(%q) = %v, want unsupported transport", repository, err)
		}
	}
}

// TestParseAcceptsPlaceholderRepositories proves a lockfile written from a
// placeholder-backed import round-trips. The lock records configured text, so
// refusing a placeholder here would make the machine-written file unreadable by
// the next operation.
func TestParseAcceptsPlaceholderRepositories(t *testing.T) {
	t.Parallel()
	for _, repository := range []string{
		"${AL_SKILLS_REPOSITORY}",
		"https://${AL_SKILLS_TOKEN}@example.test/skills.git",
		"https://oauth2:${AL_SKILLS_TOKEN}@example.test/skills.git",
		"https://example.test/skills.git?access_token=${AL_SKILLS_TOKEN}",
	} {
		t.Run(repository, func(t *testing.T) {
			t.Parallel()
			data := lockDocument(t, mutated("alpha", func(e *Entry) { e.Repository = repository }))
			file, err := Parse([]byte(data), "skills.lock.json")
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			entry, ok := file.Entry("alpha")
			if !ok || entry.Repository != repository {
				t.Fatalf("round-tripped repository = %q, want the configured text", entry.Repository)
			}
		})
	}
}
