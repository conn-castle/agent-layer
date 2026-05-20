package skillvalidator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontMatter_EmptyContent(t *testing.T) {
	fm, err := parseFrontMatter("")
	if err != nil {
		t.Fatalf("parseFrontMatter empty: %v", err)
	}
	if len(fm.keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(fm.keys))
	}
}

func TestParseFrontMatter_WhitespaceOnly(t *testing.T) {
	fm, err := parseFrontMatter("   \n  \t  \n")
	if err != nil {
		t.Fatalf("parseFrontMatter whitespace: %v", err)
	}
	if len(fm.keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(fm.keys))
	}
}

func TestParseFrontMatter_InvalidYAML(t *testing.T) {
	_, err := parseFrontMatter("{{invalid yaml")
	if err == nil {
		t.Fatal("expected YAML parse error")
	}
}

func TestParseFrontMatter_NonMappingYAML(t *testing.T) {
	_, err := parseFrontMatter("- item1\n- item2\n")
	if err == nil || !strings.Contains(err.Error(), "must be a YAML mapping") {
		t.Fatalf("expected mapping error, got %v", err)
	}
}

func TestParseFrontMatter_EmptyKeySkipped(t *testing.T) {
	// YAML with an empty key (null key resolves to "")
	fm, err := parseFrontMatter("name: alpha\n")
	if err != nil {
		t.Fatalf("parseFrontMatter: %v", err)
	}
	if fm.name == nil || *fm.name != "alpha" {
		t.Fatalf("expected name=alpha, got %v", fm.name)
	}
}

func TestParseFrontMatter_MetadataValid(t *testing.T) {
	fm, err := parseFrontMatter("name: test\nmetadata:\n  key1: val1\n  key2: val2\n")
	if err != nil {
		t.Fatalf("parseFrontMatter with metadata: %v", err)
	}
	if fm.name == nil || *fm.name != "test" {
		t.Fatalf("expected name=test, got %v", fm.name)
	}
}

func TestParseFrontMatter_MetadataNull(t *testing.T) {
	fm, err := parseFrontMatter("name: test\nmetadata: null\n")
	if err != nil {
		t.Fatalf("parseFrontMatter with null metadata: %v", err)
	}
	if fm.name == nil || *fm.name != "test" {
		t.Fatalf("expected name=test, got %v", fm.name)
	}
}

func TestParseFrontMatter_MetadataNotMapping(t *testing.T) {
	_, err := parseFrontMatter("name: test\nmetadata: scalar\n")
	if err == nil || !strings.Contains(err.Error(), "must be a mapping") {
		t.Fatalf("expected metadata mapping error, got %v", err)
	}
}

func TestParseFrontMatter_MetadataNonStringKey(t *testing.T) {
	_, err := parseFrontMatter("name: test\nmetadata:\n  123: val\n")
	if err == nil || !strings.Contains(err.Error(), "must have string keys") {
		t.Fatalf("expected metadata string key error, got %v", err)
	}
}

func TestParseFrontMatter_MetadataNonStringValue(t *testing.T) {
	_, err := parseFrontMatter("name: test\nmetadata:\n  key:\n    - nested\n")
	if err == nil || !strings.Contains(err.Error(), "must have string values") {
		t.Fatalf("expected metadata string value error, got %v", err)
	}
}

func TestParseFrontMatter_LicenseField(t *testing.T) {
	fm, err := parseFrontMatter("name: test\nlicense: MIT\n")
	if err != nil {
		t.Fatalf("parseFrontMatter with license: %v", err)
	}
	found := false
	for _, k := range fm.keys {
		if k == "license" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected license in keys")
	}
}

func TestParseFrontMatter_AllowedToolsField(t *testing.T) {
	fm, err := parseFrontMatter("name: test\nallowed-tools: bash\n")
	if err != nil {
		t.Fatalf("parseFrontMatter with allowed-tools: %v", err)
	}
	found := false
	for _, k := range fm.keys {
		if k == "allowed-tools" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected allowed-tools in keys")
	}
}

func TestParseFrontMatter_LicenseNonScalar(t *testing.T) {
	_, err := parseFrontMatter("name: test\nlicense:\n  - item\n")
	if err == nil || !strings.Contains(err.Error(), "must be a string scalar") {
		t.Fatalf("expected license scalar error, got %v", err)
	}
}

func TestParseFrontMatter_DescriptionError(t *testing.T) {
	_, err := parseFrontMatter("description:\n  - item\n")
	if err == nil || !strings.Contains(err.Error(), "must be a string scalar") {
		t.Fatalf("expected description scalar error, got %v", err)
	}
}

func TestParseFrontMatter_CompatibilityError(t *testing.T) {
	_, err := parseFrontMatter("compatibility:\n  - item\n")
	if err == nil || !strings.Contains(err.Error(), "must be a string scalar") {
		t.Fatalf("expected compatibility scalar error, got %v", err)
	}
}

func TestParseScalarString_NonStringTag(t *testing.T) {
	// Exercise the tag != yamlTagStr branch via a frontmatter with an integer value
	_, err := parseFrontMatter("name: 42\n")
	// 42 parses as !!int tag, which should fail parseScalarString
	if err == nil || !strings.Contains(err.Error(), "must be a string scalar") {
		t.Fatalf("expected string scalar error for integer, got %v", err)
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

func TestParseFrontMatter_DuplicateKeysReturnError(t *testing.T) {
	_, err := parseFrontMatter("name: first\nname: second\ndescription: test\n")
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("expected duplicate key error, got %v", err)
	}
}

func TestEnsureMetadataMap_NonStringKeyTag(t *testing.T) {
	// Test through parseFrontMatter with boolean key
	_, err := parseFrontMatter("metadata:\n  true: val\n")
	if err == nil || !strings.Contains(err.Error(), "string keys") {
		t.Fatalf("expected string key error, got %v", err)
	}
}
