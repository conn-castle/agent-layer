package skillvalidator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillSource_Flat(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "alpha.md")
	content := `---
name: alpha
description: test
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write flat skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	if parsed.SourceFormat != SourceFormatFlat {
		t.Fatalf("source format = %q, want %q", parsed.SourceFormat, SourceFormatFlat)
	}
	if parsed.CanonicalName != "alpha" {
		t.Fatalf("canonical name = %q, want %q", parsed.CanonicalName, "alpha")
	}
	if parsed.Name == nil || *parsed.Name != "alpha" {
		t.Fatalf("parsed name = %#v, want alpha", parsed.Name)
	}
	if parsed.Description == nil || *parsed.Description != "test" {
		t.Fatalf("parsed description = %#v, want test", parsed.Description)
	}
}

func TestParseSkillSource_Directory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "beta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	content := `---
name: beta
description: test
compatibility: requires git
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write directory skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	if parsed.SourceFormat != SourceFormatDirectory {
		t.Fatalf("source format = %q, want %q", parsed.SourceFormat, SourceFormatDirectory)
	}
	if parsed.CanonicalName != "beta" {
		t.Fatalf("canonical name = %q, want %q", parsed.CanonicalName, "beta")
	}
	if parsed.Compatibility == nil || *parsed.Compatibility != "requires git" {
		t.Fatalf("compatibility = %#v, want requires git", parsed.Compatibility)
	}
}

func TestParseSkillSource_DirectoryLowercaseSkillFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "beta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}
	path := filepath.Join(dir, "skill.md")
	content := `---
name: beta
description: test
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write directory skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	if parsed.SourceFormat != SourceFormatDirectory {
		t.Fatalf("source format = %q, want %q", parsed.SourceFormat, SourceFormatDirectory)
	}
	if parsed.CanonicalName != "beta" {
		t.Fatalf("canonical name = %q, want %q", parsed.CanonicalName, "beta")
	}
}

func TestValidateParsedSkill_MissingNameWarning(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "alpha.md")
	content := `---
description: test
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeNameMissing) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNameMissing, findings)
	}
}

func TestValidateParsedSkill_NullNameDescriptionWarns(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "alpha.md")
	content := `---
name: null
description: null
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeNameMissing) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNameMissing, findings)
	}
	if !hasFinding(findings, FindingCodeDescriptionMissing) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeDescriptionMissing, findings)
	}
}

func TestValidateParsedSkill_ConsecutiveHyphenOnly(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "my--skill.md")
	content := `---
name: my--skill
description: test
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	findings := ValidateParsedSkill(parsed)
	if hasFinding(findings, FindingCodeNameInvalid) {
		t.Fatalf("consecutive hyphens should not trigger %s (separate finding exists), got %#v", FindingCodeNameInvalid, findings)
	}
	if !hasFinding(findings, FindingCodeNameConsecutiveHyphens) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNameConsecutiveHyphens, findings)
	}
}

func TestValidateParsedSkill_NameAllowsDigits(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pdf-2-text.md")
	content := `---
name: pdf-2-text
description: test
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	findings := ValidateParsedSkill(parsed)
	if hasFinding(findings, FindingCodeNameInvalid) || hasFinding(findings, FindingCodeNameConsecutiveHyphens) {
		t.Fatalf("expected no name-format finding, got %#v", findings)
	}
}

func TestValidateParsedSkill_NameAllowsUnicodeLowercaseLetters(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "naïve-2.md")
	content := `---
name: naïve-2
description: test
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	findings := ValidateParsedSkill(parsed)
	if hasFinding(findings, FindingCodeNameInvalid) {
		t.Fatalf("expected unicode lowercase name to be valid, got %#v", findings)
	}
}

func TestValidateParsedSkill_NameRejectsUppercaseUnicodeLetters(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "éclair.md")
	content := `---
name: Éclair
description: test
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeNameInvalid) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNameInvalid, findings)
	}
}

func TestValidateParsedSkill_UnknownFieldWarning(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "alpha.md")
	content := `---
name: alpha
description: test
foo: bar
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeUnknownField) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeUnknownField, findings)
	}
}

func TestValidateParsedSkill_LengthConstraints(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "alpha.md")
	descriptionTooLong := strings.Repeat("界", MaxDescriptionLength+1)
	compatibilityTooLong := strings.Repeat("漢", MaxCompatibilityLength+1)
	content := `---
name: alpha
description: ` + descriptionTooLong + `
compatibility: ` + compatibilityTooLong + `
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeDescriptionTooLong) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeDescriptionTooLong, findings)
	}
	if !hasFinding(findings, FindingCodeCompatibilityTooLong) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeCompatibilityTooLong, findings)
	}
}

func TestValidateParsedSkill_NameLengthCountsRunes(t *testing.T) {
	longName := strings.Repeat("é", MaxSkillNameLength+1)
	parsed := ParsedSkill{
		SourcePath:      "/tmp/test/" + longName + ".md",
		CanonicalName:   longName,
		SourceFormat:    SourceFormatFlat,
		LineCount:       5,
		FrontMatterKeys: []string{"description", "name"},
		Name:            strPtr(longName),
		Description:     strPtr("test"),
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeNameTooLong) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNameTooLong, findings)
	}
}

func TestValidateParsedSkill_NamePathMismatch(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "beta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir beta: %v", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	content := `---
name: alpha
description: test
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeNamePathMismatch) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNamePathMismatch, findings)
	}
}

func TestValidateParsedSkill_NamePathMatchUsesNFKCNormalization(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "café")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir café: %v", err)
	}
	path := filepath.Join(dir, "SKILL.md")
	content := `---
name: café
description: test
---
Body.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	findings := ValidateParsedSkill(parsed)
	if hasFinding(findings, FindingCodeNamePathMismatch) {
		t.Fatalf("expected no %s finding for canonically equivalent names, got %#v", FindingCodeNamePathMismatch, findings)
	}
}

func TestValidateParsedSkill_SizeRecommendation(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "alpha.md")
	var bodyBuilder strings.Builder
	for i := 0; i < MaxRecommendedSkillLines+1; i++ {
		bodyBuilder.WriteString("line\n")
	}
	content := `---
name: alpha
description: test
---
` + bodyBuilder.String()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeSizeRecommendation) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeSizeRecommendation, findings)
	}
}

func TestValidateParsedSkill_NameTooLong(t *testing.T) {
	longName := strings.Repeat("a", MaxSkillNameLength+1)
	parsed := ParsedSkill{
		SourcePath:      "/tmp/test/" + longName + ".md",
		CanonicalName:   longName,
		SourceFormat:    SourceFormatFlat,
		LineCount:       5,
		FrontMatterKeys: []string{"description", "name"},
		Name:            strPtr(longName),
		Description:     strPtr("test"),
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeNameTooLong) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeNameTooLong, findings)
	}
}

func TestValidateParsedSkill_DescriptionMissing(t *testing.T) {
	parsed := ParsedSkill{
		SourcePath:      "/tmp/test/alpha.md",
		CanonicalName:   "alpha",
		SourceFormat:    SourceFormatFlat,
		LineCount:       5,
		FrontMatterKeys: []string{"name"},
		Name:            strPtr("alpha"),
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeDescriptionMissing) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeDescriptionMissing, findings)
	}
}

func TestValidateParsedSkill_DirectorySkillFileName(t *testing.T) {
	parsed := ParsedSkill{
		SourcePath:      "/tmp/test/alpha/skill.md",
		CanonicalName:   "alpha",
		SourceFormat:    SourceFormatDirectory,
		LineCount:       5,
		FrontMatterKeys: []string{"description", "name"},
		Name:            strPtr("alpha"),
		Description:     strPtr("test"),
	}
	findings := ValidateParsedSkill(parsed)
	if !hasFinding(findings, FindingCodeDirectorySkillFileName) {
		t.Fatalf("expected %s finding, got %#v", FindingCodeDirectorySkillFileName, findings)
	}
}

func TestValidateParsedSkill_DeterministicOrder(t *testing.T) {
	parsed := ParsedSkill{
		SourcePath:      "/tmp/test/alpha.md",
		CanonicalName:   "alpha",
		SourceFormat:    SourceFormatFlat,
		LineCount:       MaxRecommendedSkillLines + 1,
		FrontMatterKeys: []string{"description", "foo"},
		Description:     strPtr("test"),
	}
	findings := ValidateParsedSkill(parsed)
	if len(findings) < 3 {
		t.Fatalf("expected multiple findings, got %#v", findings)
	}
	for i := 1; i < len(findings); i++ {
		if findings[i-1].Code > findings[i].Code {
			t.Fatalf("findings are not sorted by code: %#v", findings)
		}
	}
}

func TestValidateMetadata_DeterministicOrder(t *testing.T) {
	parsed := ParsedSkill{
		SourcePath:      "/tmp/test/alpha.md",
		CanonicalName:   "alpha",
		SourceFormat:    SourceFormatFlat,
		FrontMatterKeys: []string{"zeta", "description", "name", "alpha"},
		Name:            strPtr(""),
		Description:     strPtr(""),
	}
	findings := ValidateMetadata(parsed)
	if len(findings) < 4 {
		t.Fatalf("expected multiple findings, got %#v", findings)
	}
	for i := 1; i < len(findings); i++ {
		prev := findings[i-1]
		next := findings[i]
		if prev.Path > next.Path {
			t.Fatalf("findings are not sorted by path: %#v", findings)
		}
		if prev.Path == next.Path && prev.Code > next.Code {
			t.Fatalf("findings are not sorted by code: %#v", findings)
		}
		if prev.Path == next.Path && prev.Code == next.Code && prev.Message > next.Message {
			t.Fatalf("findings are not sorted by message: %#v", findings)
		}
	}
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
