package skillvalidator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkillFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "alpha.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write skill fixture: %v", err)
	}
	return path
}

func TestParseSkillSource_EmptyFrontMatterSection(t *testing.T) {
	parsed, err := ParseSkillSource(writeSkillFixture(t, "---\n---\nBody.\n"))
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	if len(parsed.FrontMatterKeys) != 0 {
		t.Fatalf("expected 0 keys, got %v", parsed.FrontMatterKeys)
	}
	if parsed.Name != nil || parsed.Description != nil {
		t.Fatalf("expected nil fields for empty front matter, got %#v", parsed)
	}
}

func TestParseSkillSource_NonMappingFrontMatterRejected(t *testing.T) {
	_, err := ParseSkillSource(writeSkillFixture(t, "---\n- item1\n- item2\n---\nBody.\n"))
	if err == nil || !strings.Contains(err.Error(), "must be a mapping") {
		t.Fatalf("expected mapping error, got %v", err)
	}
}

func TestParseSkillSource_KeysSortedAcrossAllFields(t *testing.T) {
	content := "---\nname: alpha\nlicense: MIT\nallowed-tools: bash\nmetadata:\n  k: v\ndescription: test\n---\nBody.\n"
	parsed, err := ParseSkillSource(writeSkillFixture(t, content))
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	want := []string{"allowed-tools", "description", "license", "metadata", "name"}
	if len(parsed.FrontMatterKeys) != len(want) {
		t.Fatalf("keys = %v, want %v", parsed.FrontMatterKeys, want)
	}
	for i, key := range want {
		if parsed.FrontMatterKeys[i] != key {
			t.Fatalf("keys = %v, want %v", parsed.FrontMatterKeys, want)
		}
	}
}

func TestParseSkillSource_MalformedMetadataRejected(t *testing.T) {
	cases := []string{
		"---\nname: alpha\nmetadata: scalar\n---\nBody.\n",
		"---\nname: alpha\nmetadata:\n  123: val\n---\nBody.\n",
		"---\nname: alpha\nmetadata:\n  key:\n    - nested\n---\nBody.\n",
	}
	for _, content := range cases {
		_, err := ParseSkillSource(writeSkillFixture(t, content))
		if err == nil || !strings.Contains(err.Error(), "metadata") {
			t.Fatalf("expected metadata error for %q, got %v", content, err)
		}
	}
}

func TestParseSkillSource_NonStringScalarFieldRejected(t *testing.T) {
	_, err := ParseSkillSource(writeSkillFixture(t, "---\nname: 42\ndescription: test\n---\nBody.\n"))
	if err == nil || !strings.Contains(err.Error(), "must be a string") {
		t.Fatalf("expected string-type error for integer name, got %v", err)
	}
}

func TestParseSkillSource_DuplicateMetadataKeyRejected(t *testing.T) {
	// Doctor now fails loudly on duplicate metadata keys, matching config
	// loading, instead of silently tolerating last-value-wins.
	_, err := ParseSkillSource(writeSkillFixture(t, "---\nname: alpha\nmetadata:\n  owner: a\n  owner: b\n---\nBody.\n"))
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("expected duplicate metadata key error, got %v", err)
	}
}

func TestParseSkillSource_MultilineNameStillParsed(t *testing.T) {
	// Doctor keeps its policy of accepting block-scalar names at parse time;
	// only config loading rejects multiline names.
	parsed, err := ParseSkillSource(writeSkillFixture(t, "---\nname: |-\n  alpha\ndescription: test\n---\nBody.\n"))
	if err != nil {
		t.Fatalf("ParseSkillSource: %v", err)
	}
	if parsed.Name == nil || *parsed.Name != "alpha" {
		t.Fatalf("expected name=alpha for multiline scalar, got %#v", parsed.Name)
	}
}

func TestIsValidSkillName_Cases(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"", false},
		{"-leading", false},
		{"trailing-", false},
		{"UPPER", false},
		{"has space", false},
		{"has_underscore", false},
		{"valid", true},
		{"with-hyphen", true},
		{"with-123", true},
		{"a", true},
		{"1", true},
	}
	for _, tt := range tests {
		if got := isValidSkillName(tt.name); got != tt.valid {
			t.Errorf("isValidSkillName(%q) = %v, want %v", tt.name, got, tt.valid)
		}
	}
}

func TestCountLines_Cases(t *testing.T) {
	tests := []struct {
		content string
		want    int
	}{
		{"", 0},
		{"hello", 1},
		{"hello\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
		{"a\nb\nc", 3},
		{"\n", 1},
		{"\n\n", 2},
	}
	for _, tt := range tests {
		if got := countLines(tt.content); got != tt.want {
			t.Errorf("countLines(%q) = %d, want %d", tt.content, got, tt.want)
		}
	}
}

func TestSortFindings_SortsByPathThenCodeThenMessage(t *testing.T) {
	findings := []Finding{
		{Path: "b.md", Code: "Z", Message: "m2"},
		{Path: "a.md", Code: "A", Message: "m1"},
		{Path: "a.md", Code: "A", Message: "m0"},
		{Path: "b.md", Code: "A", Message: "m3"},
	}
	sortFindings(findings)
	if findings[0].Message != "m0" || findings[1].Message != "m1" || findings[2].Message != "m3" || findings[3].Message != "m2" {
		t.Fatalf("unexpected sort order: %#v", findings)
	}
}

func TestParseSkillSource_MissingFile(t *testing.T) {
	_, err := ParseSkillSource("/nonexistent/path.md")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestParseSkillSource_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.md")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ParseSkillSource(path)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty error, got %v", err)
	}
}

func TestParseSkillSource_MissingFrontMatterDelimiter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-fm.md")
	if err := os.WriteFile(path, []byte("no frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ParseSkillSource(path)
	if err == nil || !strings.Contains(err.Error(), "missing YAML frontmatter") {
		t.Fatalf("expected frontmatter error, got %v", err)
	}
}

func TestParseSkillSource_UnterminatedFrontMatter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unterm.md")
	if err := os.WriteFile(path, []byte("---\nname: test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ParseSkillSource(path)
	if err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("expected unterminated error, got %v", err)
	}
}

func TestParseSkillSource_InvalidFrontMatterYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad-yaml.md")
	content := "---\n{{invalid yaml\n---\nBody.\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ParseSkillSource(path)
	if err == nil || !strings.Contains(err.Error(), "parse frontmatter") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestParseSkillSource_UTF8BOMStripped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bom.md")
	content := append([]byte{0xEF, 0xBB, 0xBF}, []byte("---\nname: bom\ndescription: test\n---\nBody.\n")...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseSkillSource(path)
	if err != nil {
		t.Fatalf("ParseSkillSource with BOM: %v", err)
	}
	if parsed.Name == nil || *parsed.Name != "bom" {
		t.Fatalf("expected name=bom, got %v", parsed.Name)
	}
}

func TestParseSkillSource_DuplicateTopLevelKeyRejected(t *testing.T) {
	_, err := ParseSkillSource(writeSkillFixture(t, "---\nname: first\nname: second\ndescription: test\n---\nBody.\n"))
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("expected duplicate key error, got %v", err)
	}
}
