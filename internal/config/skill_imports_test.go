package config

import (
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/skilltree"
)

const skillImportBaseConfig = `[approvals]
mode = "all"

[agents.antigravity]
enabled = false

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = false

[agents.codex]
enabled = false

[agents.vscode]
enabled = false

[agents.copilot_cli]
enabled = false
`

func parseSkillImportConfig(t *testing.T, extra string) (*Config, error) {
	t.Helper()
	return ParseConfig([]byte(skillImportBaseConfig+extra), "config.toml")
}

func TestSkillImportBlockDecodesAndAppliesDocumentedDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := parseSkillImportConfig(t, `
[[skills.imports]]
repository = "https://example.invalid/skills.git"
selectors = ["skills/alpha", "!skills/internal"]
`)
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if len(cfg.Skills.Imports) != 1 {
		t.Fatalf("imports = %d, want 1", len(cfg.Skills.Imports))
	}
	imp := cfg.Skills.Imports[0]
	// An omitted write policy resolves to the named constant rather than an
	// implicit behavior buried in the writer.
	if got := imp.EffectiveWrite(); got != DefaultSkillWrite {
		t.Fatalf("effective write = %q, want %q", got, DefaultSkillWrite)
	}
	// An omitted push repository means the source repository, so a fork is always
	// an explicit choice.
	if got := imp.EffectivePushRepository(); got != "https://example.invalid/skills.git" {
		t.Fatalf("effective push repository = %q", got)
	}
	if got := strings.Join(imp.PositiveSelectors(), ","); got != "skills/alpha" {
		t.Fatalf("positive selectors = %q", got)
	}
	if got := strings.Join(imp.ExclusionSelectors(), ","); got != "skills/internal" {
		t.Fatalf("exclusion selectors = %q", got)
	}
}

func TestSkillImportValidationRejections(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		document string
		want     string
	}{
		"missing repository": {
			document: "\n[[skills.imports]]\nselectors = [\"skills/alpha\"]\n",
			want:     "repository is required",
		},
		"no selectors": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = []\n",
			want:     "at least one selector",
		},
		"exclusion only": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"!skills/alpha\"]\n",
			want:     "at least one positive selector",
		},
		"absolute selector": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"/skills/alpha\"]\n",
			want:     "must be repository-relative",
		},
		"escaping selector": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"../outside\"]\n",
			want:     "must stay inside the repository",
		},
		"backslash selector": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills\\\\alpha\"]\n",
			want:     "must use forward slashes",
		},
		"invalid tracking": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\ntracking = \"sometimes\"\n",
			want:     "tracking must be",
		},
		"invalid write": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\nwrite = \"force\"\n",
			want:     "write must be",
		},
		"branch write without push branch": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\nwrite = \"branch\"\n",
			want:     "push_branch is required",
		},
		"push branch without branch write": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\npush_branch = \"contrib\"\n",
			want:     "only valid when write",
		},
		"repository with embedded credentials": { // #nosec G101 -- this is the rejected input the test exists to prove is rejected, not a real credential.
			document: "\n[[skills.imports]]\nrepository = \"https://user:secret@example.invalid/s.git\"\nselectors = [\"skills/a\"]\n",
			want:     "must not embed credentials",
		},
		"scp-style repository with a password": {
			document: "\n[[skills.imports]]\nrepository = \"git:secret@example.invalid:org/s.git\"\nselectors = [\"skills/a\"]\n",
			want:     "must not embed credentials",
		},
		"repository that looks like a flag": {
			document: "\n[[skills.imports]]\nrepository = \"--upload-pack=evil\"\nselectors = [\"skills/a\"]\n",
			want:     "must not start with '-'",
		},
		"ref that looks like a flag": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\nref = \"--exec=evil\"\n",
			want:     "must not start with '-'",
		},
		"invalid ref characters": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\nref = \"feature branch\"\n",
			want:     "git does not accept",
		},
		"duplicate selector in one block": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\", \"skills/a\"]\n",
			want:     "listed twice",
		},
		"duplicate repository and selector across blocks": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\n" +
				"\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\nref = \"v1\"\n",
			want: "must be unique so 'al skills remove' identifies one selector",
		},
		"two blocks with an identical policy": {
			document: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\n" +
				"\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/b\"]\n",
			want: "merge their selectors into one block",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseSkillImportConfig(t, testCase.document)
			if err == nil {
				t.Fatalf("%s must be rejected", name)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want it to mention %q", err, testCase.want)
			}
		})
	}
}

func TestSkillImportAcceptsScpStyleRemoteWithoutPassword(t *testing.T) {
	t.Parallel()
	// user@host:path is ordinary SSH syntax and carries no secret, so rejecting it
	// would block the most common private-repository setup.
	if _, err := parseSkillImportConfig(t, "\n[[skills.imports]]\nrepository = \"git@example.invalid:org/skills.git\"\nselectors = [\"skills/a\"]\n"); err != nil {
		t.Fatalf("an scp-style remote must be accepted: %v", err)
	}
}

func TestSkillImportBlocksWithDifferentPoliciesCoexist(t *testing.T) {
	t.Parallel()
	cfg, err := parseSkillImportConfig(t, `
[[skills.imports]]
repository = "r"
selectors = ["skills/a"]

[[skills.imports]]
repository = "r"
selectors = ["skills/b"]
ref = "v1"
write = "branch"
push_branch = "contrib"
`)
	if err != nil {
		t.Fatalf("repeating a repository with a different policy must be allowed: %v", err)
	}
	if cfg.Skills.Imports[0].PolicyKey() == cfg.Skills.Imports[1].PolicyKey() {
		t.Fatal("policy keys must differ when any policy field differs")
	}
}

func TestFindSkillImportSelectorDistinguishesExclusions(t *testing.T) {
	t.Parallel()
	cfg, err := parseSkillImportConfig(t, `
[[skills.imports]]
repository = "r"
selectors = ["skills/*", "!skills/internal"]
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	positive, ok := FindSkillImportSelector(cfg.Skills, "r", "skills/*")
	if !ok || positive.Exclusion {
		t.Fatalf("positive selector lookup = %+v ok=%v", positive, ok)
	}
	exclusion, ok := FindSkillImportSelector(cfg.Skills, "r", "!skills/internal")
	if !ok || !exclusion.Exclusion {
		t.Fatalf("exclusion lookup = %+v ok=%v", exclusion, ok)
	}
	// A path spelled as a positive selector must not match the exclusion entry,
	// or remove would delete the wrong one.
	if _, ok := FindSkillImportSelector(cfg.Skills, "r", "skills/internal"); ok {
		t.Fatal("a positive selector must not match a configured exclusion")
	}
}

func TestParseSkillSelectorNormalizesAndClassifies(t *testing.T) {
	t.Parallel()
	body, exclusion, err := ParseSkillSelector("  ! skills/./alpha/  ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !exclusion {
		t.Fatal("a leading '!' marks an exclusion")
	}
	if body != "skills/alpha" {
		t.Fatalf("normalized selector = %q", body)
	}
	if !IsSkillSelectorWildcard("skills/*") || IsSkillSelectorWildcard("skills/alpha") {
		t.Fatal("wildcard detection is wrong")
	}
	if _, _, err := ParseSkillSelector("skills/.git/hooks"); err == nil {
		t.Fatal("a selector must not reach into Git internals")
	}
}

func TestMarshalSkillImportLockIsDeterministic(t *testing.T) {
	t.Parallel()
	entry := func(repository string, path string, name string) SkillImportLockEntry {
		return SkillImportLockEntry{
			Repository: repository, SourcePath: path, RefOmitted: true,
			ResolvedRefName: "main", ResolvedRefType: SkillRefBranch,
			SourceCommit: strings.Repeat("a", 40), UpstreamTreeHash: skilltree.Hash(nil), Tracking: SkillTrackingTracked,
			Write: SkillWriteNone, PushRepository: repository, SkillName: name,
		}
	}
	first, err := MarshalSkillImportLock(&SkillImportLock{
		Version: SkillImportLockVersion,
		Entries: []SkillImportLockEntry{entry("b", "skills/two", "two"), entry("a", "skills/one", "one")},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	second, err := MarshalSkillImportLock(&SkillImportLock{
		Version: SkillImportLockVersion,
		Entries: []SkillImportLockEntry{entry("a", "skills/one", "one"), entry("b", "skills/two", "two")},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// A lock the developer may commit must not churn just because entries were
	// discovered in a different order.
	if string(first) != string(second) {
		t.Fatalf("lock serialization is order-dependent:\n%s\n---\n%s", first, second)
	}

	round, err := ParseSkillImportLock(first, "lock")
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if len(round.Entries) != 2 {
		t.Fatalf("entries = %d", len(round.Entries))
	}
}

func TestParseSkillImportLockRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	document := `{"version":1,"entries":[],"future_field":true}`
	if _, err := ParseSkillImportLock([]byte(document), "lock"); err == nil {
		t.Fatal("a lock with fields this release does not understand must fail rather than be partially honored")
	}
}

func TestParseSkillImportLockRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()
	if _, err := ParseSkillImportLock([]byte(`{"version":2,"entries":[]}`), "lock"); err == nil {
		t.Fatal("a lock from an unsupported schema version must not be guessed at")
	}
	if _, err := ParseSkillImportLock([]byte(`{"version":1,"entries":[]} {"version":1,"entries":[]}`), "lock"); err == nil {
		t.Fatal("trailing JSON must not be silently ignored")
	}
}

func TestParseSkillImportLockRejectsUnsafeManagedName(t *testing.T) {
	t.Parallel()
	entry := SkillImportLockEntry{
		Repository: "https://example.invalid/repo.git", SourcePath: "skills/alpha", RefOmitted: true,
		ResolvedRefName: "main", ResolvedRefType: SkillRefBranch, SourceCommit: strings.Repeat("a", 40),
		UpstreamTreeHash: skilltree.Hash(nil), Tracking: SkillTrackingTracked, Write: SkillWriteNone,
		PushRepository: "https://example.invalid/repo.git", SkillName: "../../victim",
	}
	data, err := MarshalSkillImportLock(&SkillImportLock{Version: SkillImportLockVersion, Entries: []SkillImportLockEntry{entry}})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if _, err := ParseSkillImportLock(data, "lock"); err == nil {
		t.Fatal("an unsafe lock skill name could escape the managed directory")
	}
}

func TestParseSkillImportLockRejectsInconsistentSecurityState(t *testing.T) {
	t.Parallel()
	base := SkillImportLockEntry{
		Repository: "https://example.invalid/repo.git", SourcePath: "skills/alpha", RefOmitted: true,
		ResolvedRefName: "main", ResolvedRefType: SkillRefBranch, SourceCommit: strings.Repeat("a", 40),
		UpstreamTreeHash: skilltree.Hash(nil), Tracking: SkillTrackingTracked, Write: SkillWriteNone,
		PushRepository: "https://example.invalid/repo.git", SkillName: "alpha",
	}
	type testCase struct {
		mutate func(*SkillImportLockEntry)
		want   string
	}
	cases := map[string]testCase{
		"missing required field":       {func(e *SkillImportLockEntry) { e.Repository = "" }, "repository is required"},
		"unnormalized repository":      {func(e *SkillImportLockEntry) { e.Repository += " " }, "repository is not normalized"},
		"unsafe repository":            {func(e *SkillImportLockEntry) { e.Repository = "--upload-pack=evil" }, "must not start with"},
		"unnormalized push repository": {func(e *SkillImportLockEntry) { e.PushRepository += " " }, "push_repository is not normalized"},
		"unsafe push repository": {func(e *SkillImportLockEntry) {
			e.PushRepository = "https://user:secret@example.invalid/repo.git" // #nosec G101 -- rejected test input, not a credential.
		}, "must not embed credentials"},
		"abbreviated commit": {func(e *SkillImportLockEntry) { e.SourceCommit = "abc123" }, "full hexadecimal object id"},
		"unversioned tree hash": {func(e *SkillImportLockEntry) {
			e.UpstreamTreeHash = strings.Repeat("b", 64)
		}, "versioned SHA-256"},
		"unknown ref type": {func(e *SkillImportLockEntry) { e.ResolvedRefType = "note" }, "resolved_ref_type"},
		"unknown tracking": {func(e *SkillImportLockEntry) { e.Tracking = "sometimes" }, "tracking"},
		"tracked tag": {func(e *SkillImportLockEntry) {
			e.ResolvedRefType, e.Tracking = SkillRefTag, SkillTrackingTracked
		}, "only branches can be tracked"},
		"unknown write": {func(e *SkillImportLockEntry) { e.Write = "force" }, "write"},
		"branch without destination": {func(e *SkillImportLockEntry) {
			e.Write = SkillWriteBranch
		}, "push_branch is required"},
		"destination on non-branch write": {func(e *SkillImportLockEntry) {
			e.PushBranch = "contrib"
		}, "push_branch is only valid"},
		"omitted and configured ref": {func(e *SkillImportLockEntry) {
			e.ConfiguredRef = "main"
		}, "both an omitted and a configured ref"},
		"missing configured ref": {func(e *SkillImportLockEntry) {
			e.RefOmitted = false
		}, "configured_ref is required"},
		"unsafe configured ref": {func(e *SkillImportLockEntry) {
			e.RefOmitted, e.ConfiguredRef = false, "--exec=evil"
		}, "configured_ref"},
		"unsafe resolved ref": {func(e *SkillImportLockEntry) {
			e.ResolvedRefName = "feature branch"
		}, "resolved_ref_name"},
		"unsafe push branch": {func(e *SkillImportLockEntry) {
			e.Write, e.PushBranch = SkillWriteBranch, "--exec=evil"
		}, "push_branch"},
		"invalid source path":      {func(e *SkillImportLockEntry) { e.SourcePath = "../alpha" }, "source_path"},
		"unnormalized source path": {func(e *SkillImportLockEntry) { e.SourcePath = "skills/./alpha" }, "is not normalized"},
		"wildcard source path":     {func(e *SkillImportLockEntry) { e.SourcePath = "skills/*" }, "record resolved paths"},
		"unsafe normalized name":   {func(e *SkillImportLockEntry) { e.SkillName = "Alpha" }, "safe normalized skill name"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			entry := base
			testCase.mutate(&entry)
			data, err := MarshalSkillImportLock(&SkillImportLock{Version: SkillImportLockVersion, Entries: []SkillImportLockEntry{entry}})
			if err != nil {
				t.Fatalf("marshal fixture: %v", err)
			}
			_, err = ParseSkillImportLock(data, "lock")
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want rejection mentioning %q", err, testCase.want)
			}
		})
	}

	duplicateKey := base
	duplicateKey.SkillName = "beta"
	duplicateName := base
	duplicateName.SourcePath = "skills/beta"
	for name, entries := range map[string][]SkillImportLockEntry{
		"duplicate source owner": {base, duplicateKey},
		"duplicate local name":   {base, duplicateName},
	} {
		t.Run(name, func(t *testing.T) {
			data, err := MarshalSkillImportLock(&SkillImportLock{Version: SkillImportLockVersion, Entries: entries})
			if err != nil {
				t.Fatalf("marshal fixture: %v", err)
			}
			if _, err := ParseSkillImportLock(data, "lock"); err == nil {
				t.Fatal("duplicate ownership must be rejected")
			}
		})
	}
}

func TestNormalizeSkillImportNameFoldsUnicodeHomographs(t *testing.T) {
	t.Parallel()
	// A fullwidth "a" normalizes to the same directory name, so it must be
	// detected as a collision rather than silently overwriting on disk.
	if NormalizeSkillImportName("ａlpha") != NormalizeSkillImportName("alpha") {
		t.Fatal("Unicode-equivalent skill names must normalize to one comparison form")
	}
}
