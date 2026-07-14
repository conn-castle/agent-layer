package skillfrontmatter

import (
	"errors"
	"strings"
	"testing"
)

func parseKindErr(t *testing.T, content string, wantKind ErrorKind) *Error {
	t.Helper()
	_, err := Parse(content)
	if err == nil {
		t.Fatalf("Parse(%q) succeeded, want %v error", content, wantKind)
	}
	var parseErr *Error
	if !errors.As(err, &parseErr) {
		t.Fatalf("Parse(%q) returned %T, want *Error", content, err)
	}
	if parseErr.Kind != wantKind {
		t.Fatalf("Parse(%q) kind = %v, want %v (detail %q)", content, parseErr.Kind, wantKind, parseErr.Detail)
	}
	return parseErr
}

func TestParse_EmptyAndWhitespaceContent(t *testing.T) {
	for _, content := range []string{"", "   \n  \t  \n"} {
		doc, err := Parse(content)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", content, err)
		}
		if len(doc.Keys) != 0 {
			t.Fatalf("Parse(%q) keys = %v, want none", content, doc.Keys)
		}
		if doc.Description.State != FieldAbsent {
			t.Fatalf("Parse(%q) description state = %v, want absent", content, doc.Description.State)
		}
	}
}

func TestParse_SyntaxErrorClassified(t *testing.T) {
	parseErr := parseKindErr(t, "{{invalid yaml", KindSyntax)
	if parseErr.Err == nil {
		t.Fatal("expected wrapped underlying YAML error")
	}
}

func TestParse_NonMappingRootRejected(t *testing.T) {
	parseErr := parseKindErr(t, "- item1\n- item2\n", KindType)
	if !strings.Contains(parseErr.Detail, "must be a mapping") {
		t.Fatalf("unexpected detail: %q", parseErr.Detail)
	}
}

func TestParse_DuplicateTopLevelKeyRejected(t *testing.T) {
	parseErr := parseKindErr(t, "name: first\nname: second\n", KindDuplicateKey)
	if parseErr.Key != "name" {
		t.Fatalf("duplicate key = %q, want name", parseErr.Key)
	}
}

func TestParse_DuplicateMetadataKeyRejected(t *testing.T) {
	parseErr := parseKindErr(t, "metadata:\n  owner: a\n  owner: b\n", KindDuplicateKey)
	if parseErr.Key != "owner" {
		t.Fatalf("duplicate key = %q, want owner", parseErr.Key)
	}
}

func TestParse_NonStringScalarFieldsRejected(t *testing.T) {
	cases := []string{
		"name: 42\n",
		"description: true\n",
		"license:\n  - item\n",
		"compatibility:\n  codex: \">=0.1\"\n",
		"allowed-tools:\n  - Read\n",
	}
	for _, content := range cases {
		parseErr := parseKindErr(t, content, KindType)
		if !strings.Contains(parseErr.Detail, "must be a string") {
			t.Fatalf("Parse(%q) detail = %q, want string-type violation", content, parseErr.Detail)
		}
	}
}

func TestParse_MalformedMetadataRejected(t *testing.T) {
	cases := map[string]string{
		"metadata: scalar\n":            "must be a string map",
		"metadata:\n  123: val\n":       "keys must be strings",
		"metadata:\n  true: val\n":      "keys must be strings",
		"metadata:\n  owner: 7\n":       "values must be strings",
		"metadata:\n  k:\n    - nest\n": "values must be strings",
	}
	for content, wantDetail := range cases {
		parseErr := parseKindErr(t, content, KindType)
		if !strings.Contains(parseErr.Detail, wantDetail) {
			t.Fatalf("Parse(%q) detail = %q, want %q", content, parseErr.Detail, wantDetail)
		}
	}
}

func TestParse_MetadataNullAndValues(t *testing.T) {
	for _, content := range []string{"metadata: ~\n", "metadata: null\n"} {
		doc, err := Parse(content)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", content, err)
		}
		if doc.Metadata != nil {
			t.Fatalf("Parse(%q) metadata = %#v, want nil", content, doc.Metadata)
		}
	}

	doc, err := Parse("metadata:\n  owner: team\n  version: \"1.0\"\n")
	if err != nil {
		t.Fatalf("Parse metadata error: %v", err)
	}
	if len(doc.Metadata) != 2 || doc.Metadata["owner"] != "team" || doc.Metadata["version"] != "1.0" {
		t.Fatalf("unexpected metadata: %#v", doc.Metadata)
	}
}

func TestParse_FieldStateDistinguishesAbsentNullValue(t *testing.T) {
	doc, err := Parse("description: here\nlicense: null\n")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if doc.Description.State != FieldValue || doc.Description.Value != "here" {
		t.Fatalf("description = %#v, want present value", doc.Description)
	}
	if doc.License.State != FieldNull {
		t.Fatalf("license state = %v, want null", doc.License.State)
	}
	if doc.Name.State != FieldAbsent {
		t.Fatalf("name state = %v, want absent", doc.Name.State)
	}
}

func TestParse_MultilineStyleReportedNotRejected(t *testing.T) {
	cases := map[string]bool{
		"name: alpha\n":            false,
		"name: \"alpha\"\n":        false,
		"name: |-\n  alpha\n":      true,
		"name: >-\n  a\n  b\n":     true,
		"description: >-\n  d\n":   true,
		"description: plain one\n": false,
	}
	for content, wantMultiline := range cases {
		doc, err := Parse(content)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", content, err)
		}
		field := doc.Name
		if strings.HasPrefix(content, "description") {
			field = doc.Description
		}
		if field.Multiline != wantMultiline {
			t.Fatalf("Parse(%q) multiline = %v, want %v", content, field.Multiline, wantMultiline)
		}
	}
}

func TestParse_UnknownAndEmptyKeysTolerated(t *testing.T) {
	doc, err := Parse("description: d\nfoo: bar\n\"\": ignored\n\" \": also ignored\n")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if len(doc.Keys) != 2 || doc.Keys[0] != "description" || doc.Keys[1] != "foo" {
		t.Fatalf("keys = %v, want [description foo]", doc.Keys)
	}
}
