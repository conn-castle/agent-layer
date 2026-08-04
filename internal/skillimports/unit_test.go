package skillimports

import (
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestMatchSelectorPathWildcardScope(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern   string
		candidate string
		want      bool
		why       string
	}{
		{"skills/*", "skills/alpha", true, "a single star matches one segment"},
		{"skills/*", "skills/alpha/nested", false, "a single star must not cross a path separator"},
		{"skills/**", "skills/alpha/nested", true, "a double star crosses separators"},
		{"skills/**", "skills", true, "a double star matches zero segments"},
		{"skills/alpha", "skills/alpha", true, "an exact selector matches itself"},
		{"skills/alpha", "skills/alphabet", false, "an exact selector is not a prefix match"},
		{"skills/a?pha", "skills/alpha", true, "a question mark matches one character"},
		{"*/alpha", "skills/alpha", true, "a leading star matches the first segment"},
	}
	for _, testCase := range cases {
		if got := matchSelectorPath(testCase.pattern, testCase.candidate); got != testCase.want {
			t.Errorf("matchSelectorPath(%q, %q) = %v, want %v: %s",
				testCase.pattern, testCase.candidate, got, testCase.want, testCase.why)
		}
	}
}

func TestRejectOverlappingPathsDetectsNesting(t *testing.T) {
	t.Parallel()
	if err := rejectOverlappingPaths([]string{"a/one", "b/two"}); err != nil {
		t.Fatalf("sibling paths must be allowed: %v", err)
	}
	if err := rejectOverlappingPaths([]string{"a", "a/inner"}); err == nil {
		t.Fatal("a skill nested inside another must be rejected")
	}
	if err := rejectOverlappingPaths([]string{"a", "ab"}); err != nil {
		t.Fatalf("a shared name prefix is not nesting: %v", err)
	}
}

func TestRedactSecretsKeepsHostAndPathVisible(t *testing.T) {
	t.Parallel()
	redacted := RedactSecrets("fatal: could not read https://user:ghp_supersecret@github.com/org/repo.git")
	if strings.Contains(redacted, "ghp_supersecret") {
		t.Fatalf("a credential survived redaction: %q", redacted)
	}
	// The host and path must remain so the message is still actionable.
	requireContains(t, redacted, "github.com/org/repo.git")

	// A token is routinely the whole userinfo, with no password separator at all.
	tokenOnly := RedactSecrets("fatal: could not read https://ghp_supersecret@github.com/org/repo.git")
	if strings.Contains(tokenOnly, "ghp_supersecret") {
		t.Fatalf("userinfo without a colon survived redaction: %q", tokenOnly)
	}
	requireContains(t, tokenOnly, "github.com/org/repo.git")

	// A URL with no userinfo keeps every character, and scp-style SSH remotes
	// carry no secret, so neither may be mangled into an unrecognizable message.
	plain := "fatal: could not read https://github.com/org/repo.git"
	if RedactSecrets(plain) != plain {
		t.Fatalf("a credential-free URL was rewritten: %q", RedactSecrets(plain))
	}
	scp := "fatal: could not read git@github.com:org/repo.git"
	if RedactSecrets(scp) != scp {
		t.Fatalf("an scp-style remote was rewritten: %q", RedactSecrets(scp))
	}

	header := RedactSecrets("Authorization: Bearer abc123def")
	if strings.Contains(header, "abc123def") {
		t.Fatalf("a bearer token survived redaction: %q", header)
	}
}

func TestParseRemoteRefsResolvesDefaultBranchAndPeeledTags(t *testing.T) {
	t.Parallel()
	output := strings.Join([]string{
		"ref: refs/heads/trunk\tHEAD",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\tHEAD",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\trefs/heads/trunk",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\trefs/tags/v1",
		"cccccccccccccccccccccccccccccccccccccccc\trefs/tags/v1^{}",
	}, "\n")
	refs := parseRemoteRefs(output)
	if refs.defaultBranch != "trunk" {
		t.Fatalf("default branch = %q, want trunk", refs.defaultBranch)
	}
	if refs.branches["trunk"] != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("branch commit = %q", refs.branches["trunk"])
	}
	// An annotated tag must resolve to the commit it names, not the tag object,
	// or the locked commit would not be a commit at all.
	if refs.tags["v1"] != "cccccccccccccccccccccccccccccccccccccccc" {
		t.Fatalf("tag commit = %q, want the peeled commit", refs.tags["v1"])
	}
}

func TestIsCommitIDRejectsAbbreviations(t *testing.T) {
	t.Parallel()
	full := strings.Repeat("a", 40)
	if !isCommitID(full) {
		t.Fatal("a full object id must be recognized")
	}
	// Expanding an abbreviation would record state the user never wrote.
	if isCommitID(strings.Repeat("a", 8)) {
		t.Fatal("an abbreviated id must not be accepted as a commit ref")
	}
}

func TestResolveTrackingFollowsRefKindAndRejectsImpossibleCombinations(t *testing.T) {
	t.Parallel()
	branch := ResolvedRef{Name: "main", Type: config.SkillRefBranch}
	tag := ResolvedRef{Name: "v1", Type: config.SkillRefTag}

	if got, err := resolveTracking("", branch); err != nil || got != config.SkillTrackingTracked {
		t.Fatalf("an omitted mode on a branch = (%q, %v), want tracked", got, err)
	}
	if got, err := resolveTracking("", tag); err != nil || got != config.SkillTrackingPinned {
		t.Fatalf("an omitted mode on a tag = (%q, %v), want pinned", got, err)
	}
	if got, err := resolveTracking(config.SkillTrackingPinned, branch); err != nil || got != config.SkillTrackingPinned {
		t.Fatalf("a branch can be pinned explicitly = (%q, %v)", got, err)
	}
	if _, err := resolveTracking(config.SkillTrackingTracked, tag); err == nil {
		t.Fatal("a tag cannot be tracked; silently downgrading would hide the user's mistake")
	}
}

func TestValidateImportedTreeAcceptsACompleteSkill(t *testing.T) {
	t.Parallel()
	built := tree(t,
		file(SkillManifestName, "---\nname: code-review\ndescription: Review code.\nlicense: MIT\n---\n\nBody\n"),
		file("references/notes.md", "notes"),
	)
	identity, err := ValidateImportedTree(built, "skills/code-review")
	if err != nil {
		t.Fatalf("valid skill rejected: %v", err)
	}
	if identity.Name != "code-review" || identity.Description != "Review code." {
		t.Fatalf("identity = %+v", identity)
	}
}

func TestValidateImportedTreeRejections(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		manifest   string
		sourcePath string
		want       string
	}{
		{
			name:       "empty description",
			manifest:   "---\nname: alpha\ndescription: \"  \"\n---\n",
			sourcePath: "skills/alpha",
			want:       "must be non-empty",
		},
		{
			name:       "missing name",
			manifest:   "---\ndescription: Something.\n---\n",
			sourcePath: "skills/alpha",
			want:       `missing required frontmatter field "name"`,
		},
		{
			name:       "unsafe name",
			manifest:   "---\nname: Alpha Skill\ndescription: Something.\n---\n",
			sourcePath: "skills/Alpha Skill",
			want:       "lowercase letters, digits, and hyphens",
		},
		{
			name:       "consecutive hyphens",
			manifest:   "---\nname: a--b\ndescription: Something.\n---\n",
			sourcePath: "skills/a--b",
			want:       "consecutive hyphens",
		},
		{
			name:       "no frontmatter",
			manifest:   "just a body\n",
			sourcePath: "skills/alpha",
			want:       "missing YAML frontmatter",
		},
		{
			name:       "unterminated frontmatter",
			manifest:   "---\nname: alpha\n",
			sourcePath: "skills/alpha",
			want:       "unterminated YAML frontmatter",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			built := tree(t, file(SkillManifestName, testCase.manifest))
			_, err := ValidateImportedTree(built, testCase.sourcePath)
			if err == nil {
				t.Fatalf("expected %q to be rejected", testCase.name)
			}
			requireContains(t, err.Error(), testCase.want)
		})
	}
}

func TestValidateImportedTreeRequiresCanonicalManifestName(t *testing.T) {
	t.Parallel()
	built := tree(t, file("skill.md", "---\nname: alpha\ndescription: Alpha.\n---\n"))
	_, err := ValidateImportedTree(built, "skills/alpha")
	if err == nil {
		t.Fatal("a non-canonical manifest name must be rejected for imports")
	}
	requireContains(t, err.Error(), "has no "+SkillManifestName)
}

func TestAppendAndEditImportBlockPreserveSurroundingLines(t *testing.T) {
	t.Parallel()
	original := "# leading comment\n[approvals]\nmode = \"all\" # trailing\n"
	withBlock := AppendImportBlock(original, config.SkillImport{
		Repository: "https://example.invalid/skills.git",
		Selectors:  []string{"skills/alpha"},
		Ref:        "v1",
	})
	requireContains(t, withBlock, "# leading comment")
	requireContains(t, withBlock, `mode = "all" # trailing`)
	requireContains(t, withBlock, "[[skills.imports]]")
	requireContains(t, withBlock, `ref = "v1"`)
	// Fields the user did not choose stay omitted so the documented defaults keep
	// applying rather than being frozen into the file.
	requireNotContains(t, withBlock, "write =")
	requireNotContains(t, withBlock, "tracking =")

	added, err := AddSelectorToBlock(withBlock, 0, "skills/beta")
	if err != nil {
		t.Fatalf("add selector: %v", err)
	}
	requireContains(t, added, "skills/beta")
	requireContains(t, added, "# leading comment")

	removed, err := RemoveSelectorFromBlock(added, 0, "skills/alpha")
	if err != nil {
		t.Fatalf("remove selector: %v", err)
	}
	requireNotContains(t, removed, "skills/alpha")
	requireContains(t, removed, "skills/beta")

	withoutBlock, err := RemoveImportBlock(removed, 0)
	if err != nil {
		t.Fatalf("remove block: %v", err)
	}
	requireNotContains(t, withoutBlock, "skills.imports")
	requireContains(t, withoutBlock, "# leading comment")
	requireContains(t, withoutBlock, `mode = "all" # trailing`)
}

func TestEditSelectorsHandlesMultiLineArrays(t *testing.T) {
	t.Parallel()
	document := strings.Join([]string{
		"[[skills.imports]]",
		`repository = "https://example.invalid/skills.git"`,
		"selectors = [",
		`  "skills/alpha", # keep this note`,
		`  "skills/beta",`,
		"]",
		"",
	}, "\n")

	added, err := AddSelectorToBlock(document, 0, "skills/gamma")
	if err != nil {
		t.Fatalf("add selector: %v", err)
	}
	for _, want := range []string{"skills/alpha", "skills/beta", "skills/gamma"} {
		requireContains(t, added, want)
	}

	removed, err := RemoveSelectorFromBlock(added, 0, "skills/beta")
	if err != nil {
		t.Fatalf("remove selector: %v", err)
	}
	requireNotContains(t, removed, "skills/beta")
	requireContains(t, removed, "skills/alpha")
	requireContains(t, removed, "skills/gamma")
}

func TestRemoveSelectorFromBlockRejectsAnAbsentSelector(t *testing.T) {
	t.Parallel()
	document := "[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/alpha\"]\n"
	if _, err := RemoveSelectorFromBlock(document, 0, "skills/nope"); err == nil {
		t.Fatal("removing a selector that is not configured must fail")
	}
}

func TestSkillImportBlockSpansIgnoreHeadersInsideStrings(t *testing.T) {
	t.Parallel()
	document := strings.Join([]string{
		"[[skills.imports]]",
		`repository = "r"`,
		`selectors = ["skills/alpha"]`,
		"note = \"\"\"",
		"[[skills.imports]]",
		"\"\"\"",
		"",
	}, "\n")
	spans := skillImportBlockSpans(splitLines(document))
	// A header-looking line inside a multiline string is string content; treating
	// it as a block would corrupt the file on the next edit.
	if len(spans) != 1 {
		t.Fatalf("spans = %+v, want exactly one block", spans)
	}
}

func TestReportDistinguishesPartialFromCompleteFailure(t *testing.T) {
	t.Parallel()
	partial := &Report{}
	partial.AddSkill(SkillResult{SkillName: "ok", Action: ActionImported})
	partial.Failf("r", "p", "bad", errInjectedGitFailure)
	requireContains(t, partial.Err().Error(), "some skill imports failed")

	complete := &Report{}
	complete.Failf("r", "p", "bad", errInjectedGitFailure)
	requireContains(t, complete.Err().Error(), "skill import failed")
	requireNotContains(t, complete.Err().Error(), "some skill imports failed")

	clean := &Report{}
	clean.AddSkill(SkillResult{SkillName: "ok", Action: ActionUnchanged})
	if clean.Err() != nil {
		t.Fatalf("a clean report must not produce an error: %v", clean.Err())
	}
}
