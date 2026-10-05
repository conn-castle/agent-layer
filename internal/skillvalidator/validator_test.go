package skillvalidator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillSource_Directory(t *testing.T) {
	path := writeSkill(t, "beta", `---
name: beta
description: test
compatibility: requires git
---
Body.
`)

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	if parsed.CanonicalName != "beta" {
		t.Fatalf("canonical name = %q, want %q", parsed.CanonicalName, "beta")
	}
	if parsed.Name == nil || *parsed.Name != "beta" {
		t.Fatalf("parsed name = %#v, want beta", parsed.Name)
	}
	if parsed.Description == nil || *parsed.Description != "test" {
		t.Fatalf("parsed description = %#v, want test", parsed.Description)
	}
}

func TestParseSkillSource_LongLineDoesNotFail(t *testing.T) {
	longLine := strings.Repeat("a", 70*1024)
	path := writeSkill(t, "alpha", "---\nname: alpha\ndescription: test\n---\n"+longLine+"\n")

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	if parsed.LineCount < 5 {
		t.Fatalf("unexpected line count for long-line skill: %d", parsed.LineCount)
	}
}

func TestParseSkillContentRejectsMalformedFrontMatter(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", want: "is empty"},
		{name: "missing delimiter", content: "name: alpha\n", want: "missing YAML frontmatter"},
		{name: "unterminated", content: "---\nname: alpha\n", want: "unterminated YAML frontmatter"},
		{name: "invalid yaml", content: "---\nname: [\n---\n", want: "parse frontmatter for alpha/SKILL.md: yaml:"},
		{name: "non-mapping root", content: "---\n- item1\n- item2\n---\n", want: "parse frontmatter for alpha/SKILL.md: front matter must be a mapping"},
		{name: "duplicate key", content: "---\nname: first\nname: second\n---\n", want: "parse frontmatter for alpha/SKILL.md: duplicate key \"name\""},
		{name: "non-string name", content: "---\nname: 42\n---\n", want: "parse frontmatter for alpha/SKILL.md: field \"name\" must be a string"},
		{name: "non-string description", content: "---\ndescription: true\n---\n", want: "parse frontmatter for alpha/SKILL.md: field \"description\" must be a string"},
		{name: "non-scalar name", content: "---\nname:\n  - alpha\n---\n", want: "parse frontmatter for alpha/SKILL.md: field \"name\" must be a string"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseSkillContent("alpha/SKILL.md", []byte(test.content))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("malformed skill error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseSkillContentAcceptsUTF8BOM(t *testing.T) {
	parsed, err := ParseSkillContent("alpha/SKILL.md", []byte("\xef\xbb\xbf---\nname: alpha\ndescription: test\n---\nBody\n"))
	if err != nil || parsed.Name == nil || *parsed.Name != "alpha" {
		t.Fatalf("BOM-prefixed skill = %#v, %v", parsed, err)
	}
}

func TestParseSkillContent_AcceptsStringScalarStyles(t *testing.T) {
	for _, test := range []struct {
		name            string
		frontMatter     string
		wantName        string
		wantDescription string
	}{
		{name: "quoted", frontMatter: "name: \"alpha\"\ndescription: 'test'\n", wantName: "alpha", wantDescription: "test"},
		{name: "literal block", frontMatter: "description: |\n  line one\n  line two\nname: |-\n  alpha\n", wantName: "alpha", wantDescription: "line one\nline two\n"},
		{name: "folded block", frontMatter: "name: >-\n  alpha\ndescription: >-\n  a\n  b\n", wantName: "alpha", wantDescription: "a b"},
		{name: "explicit str tag", frontMatter: "name: !!str 42\ndescription: !!str true\n", wantName: "42", wantDescription: "true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := ParseSkillContent("alpha/SKILL.md", []byte("---\n"+test.frontMatter+"---\n"))
			if err != nil {
				t.Fatalf("ParseSkillContent: %v", err)
			}
			if parsed.Name == nil || *parsed.Name != test.wantName {
				t.Fatalf("name = %v, want %q", parsed.Name, test.wantName)
			}
			if parsed.Description == nil || *parsed.Description != test.wantDescription {
				t.Fatalf("description = %v, want %q", parsed.Description, test.wantDescription)
			}
		})
	}
}

func TestParseSkillContent_RequiredFieldKeysMatchExactly(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "empty front matter", content: "---\n---\n"},
		{name: "whitespace-only front matter", content: "---\n   \n\t\n---\n"},
		{name: "whitespace-bearing keys", content: "---\n\" name \": alpha\n\" description \": test\n---\n"},
		{name: "empty keys", content: "---\n\"\": ignored\n---\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := ParseSkillContent("alpha/SKILL.md", []byte(test.content))
			if err != nil {
				t.Fatalf("ParseSkillContent: %v", err)
			}
			if parsed.Name != nil || parsed.Description != nil {
				t.Fatalf("required fields populated: name=%v description=%v", parsed.Name, parsed.Description)
			}
		})
	}
}

func TestValidateParsedSkill_MissingNameWarning(t *testing.T) {
	findings := parseAndValidate(t, "alpha", `---
description: test
---
Body.
`)
	if !hasFinding(findings, FindingCodeNameMissing) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNameMissing, findings)
	}
}

func TestValidateParsedSkill_NullNameDescriptionWarns(t *testing.T) {
	for _, content := range []string{
		"---\nname: null\ndescription: null\n---\nBody.\n",
		"---\nname:\ndescription: ~\n---\nBody.\n",
	} {
		findings := parseAndValidate(t, "alpha", content)
		if !hasFinding(findings, FindingCodeNameMissing) {
			t.Fatalf("expected %s finding for %q, got %#v", FindingCodeNameMissing, content, findings)
		}
		if !hasFinding(findings, FindingCodeDescriptionMissing) {
			t.Fatalf("expected %s finding for %q, got %#v", FindingCodeDescriptionMissing, content, findings)
		}
	}
}

func TestValidateParsedSkill_ConsecutiveHyphenOnly(t *testing.T) {
	findings := parseAndValidate(t, "my--skill", `---
name: my--skill
description: test
---
Body.
`)
	if hasFinding(findings, FindingCodeNameInvalid) {
		t.Fatalf("consecutive hyphens should not trigger %s (separate finding exists), got %#v", FindingCodeNameInvalid, findings)
	}
	if !hasFinding(findings, FindingCodeNameConsecutiveHyphens) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNameConsecutiveHyphens, findings)
	}
}

func TestValidateParsedSkill_NameAllowsDigits(t *testing.T) {
	findings := parseAndValidate(t, "pdf-2-text", `---
name: pdf-2-text
description: test
---
Body.
`)
	if hasFinding(findings, FindingCodeNameInvalid) || hasFinding(findings, FindingCodeNameConsecutiveHyphens) {
		t.Fatalf("expected no name-format finding, got %#v", findings)
	}
}

func TestValidateParsedSkill_NameAllowsUnicodeLowercaseLetters(t *testing.T) {
	findings := parseAndValidate(t, "naïve-2", `---
name: naïve-2
description: test
---
Body.
`)
	if hasFinding(findings, FindingCodeNameInvalid) {
		t.Fatalf("expected unicode lowercase name to be valid, got %#v", findings)
	}
}

func TestValidateParsedSkill_NameRejectsUppercaseUnicodeLetters(t *testing.T) {
	findings := parseAndValidate(t, "éclair", `---
name: Éclair
description: test
---
Body.
`)
	if !hasFinding(findings, FindingCodeNameInvalid) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNameInvalid, findings)
	}
}

func TestValidateParsedSkill_AcceptsUnknownFields(t *testing.T) {
	findings := parseAndValidate(t, "alpha", `---
name: alpha
description: test
foo: bar
license:
  - item
metadata:
  owner:
    nested: true
"": ignored
---
Body.
`)
	if len(findings) != 0 {
		t.Fatalf("unknown fields must pass through without findings, got %#v", findings)
	}
}

func TestValidateParsedSkill_AllowsClaudeInvocationPolicy(t *testing.T) {
	findings := parseAndValidate(t, "alpha", `---
name: alpha
description: test
disable-model-invocation: true
---
Body.
`)
	if len(findings) != 0 {
		t.Fatalf("Claude invocation policy should pass through without findings, got %#v", findings)
	}
}

func TestValidateParsedSkill_LengthConstraints(t *testing.T) {
	descriptionTooLong := strings.Repeat("界", MaxDescriptionLength+1)
	findings := parseAndValidate(t, "alpha", `---
name: alpha
description: `+descriptionTooLong+`
compatibility: `+strings.Repeat("漢", 2000)+`
---
Body.
`)
	if !hasFinding(findings, FindingCodeDescriptionTooLong) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeDescriptionTooLong, findings)
	}
	if len(findings) != 1 {
		t.Fatalf("opaque compatibility field produced findings: %#v", findings)
	}
}

func TestValidateParsedSkill_NameLengthCountsRunes(t *testing.T) {
	longName := strings.Repeat("é", MaxSkillNameLength+1)
	parsed := ParsedSkill{
		SourcePath:    "/tmp/test/" + longName + "/SKILL.md",
		CanonicalName: longName,
		LineCount:     5,
		Name:          strPtr(longName),
		Description:   strPtr("test"),
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeNameTooLong) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNameTooLong, findings)
	}
}

func TestValidateParsedSkill_NamePathMismatch(t *testing.T) {
	findings := parseAndValidate(t, "beta", `---
name: alpha
description: test
---
Body.
`)
	if !hasFinding(findings, FindingCodeNamePathMismatch) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNamePathMismatch, findings)
	}
}

func TestValidateParsedSkill_NamePathMatchUsesNFKCNormalization(t *testing.T) {
	findings := parseAndValidate(t, "caf\u00e9", "---\nname: cafe\u0301\ndescription: test\n---\nBody.\n")
	if hasFinding(findings, FindingCodeNamePathMismatch) {
		t.Fatalf("expected no %s finding for canonically equivalent names, got %#v", FindingCodeNamePathMismatch, findings)
	}
}

func TestValidateParsedSkill_SizeRecommendation(t *testing.T) {
	findings := parseAndValidate(t, "alpha", `---
name: alpha
description: test
---
`+strings.Repeat("line\n", MaxRecommendedSkillLines+1))
	if !hasFinding(findings, FindingCodeSizeRecommendation) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeSizeRecommendation, findings)
	}
}

func TestValidateParsedSkill_NameTooLong(t *testing.T) {
	longName := strings.Repeat("a", MaxSkillNameLength+1)
	parsed := ParsedSkill{
		SourcePath:    "/tmp/test/" + longName + "/SKILL.md",
		CanonicalName: longName,
		LineCount:     5,
		Name:          strPtr(longName),
		Description:   strPtr("test"),
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeNameTooLong) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNameTooLong, findings)
	}
}

func TestValidateParsedSkill_DescriptionMissing(t *testing.T) {
	parsed := ParsedSkill{
		SourcePath:    "/tmp/test/alpha/SKILL.md",
		CanonicalName: "alpha",
		LineCount:     5,
		Name:          strPtr("alpha"),
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeDescriptionMissing) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeDescriptionMissing, findings)
	}
}

func TestValidateParsedSkill_DeterministicOrder(t *testing.T) {
	for _, parsed := range []ParsedSkill{
		{
			SourcePath:    "/tmp/test/alpha/SKILL.md",
			CanonicalName: "alpha",
			LineCount:     MaxRecommendedSkillLines + 1,
			Description:   strPtr("test"),
		},
		{
			SourcePath:    "/tmp/test/alpha/SKILL.md",
			CanonicalName: "alpha",
			Name:          strPtr(""),
			Description:   strPtr(""),
		},
	} {
		findings := ValidateParsedSkill(parsed)
		if len(findings) < 2 {
			t.Fatalf("expected multiple findings, got %#v", findings)
		}
		for i := 1; i < len(findings); i++ {
			prev := findings[i-1]
			next := findings[i]
			if prev.Code > next.Code || (prev.Code == next.Code && prev.Message > next.Message) {
				t.Fatalf("findings are not sorted by code then message: %#v", findings)
			}
		}
	}
}

// writeSkill writes content to <tempdir>/<dirName>/SKILL.md and returns its path.
func writeSkill(t *testing.T, dirName string, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), dirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dirName, err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	return path
}

func parseAndValidate(t *testing.T, dirName string, content string) []Finding {
	t.Helper()
	parsed, err := ParseSkillSource(writeSkill(t, dirName, content))
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	return ValidateParsedSkill(parsed)
}

func hasFinding(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func strPtr(value string) *string {
	return &value
}
