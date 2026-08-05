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

func TestParse_NonStringRequiredFieldsRejected(t *testing.T) {
	cases := []string{
		"name: 42\n",
		"description: true\n",
	}
	for _, content := range cases {
		parseErr := parseKindErr(t, content, KindType)
		if !strings.Contains(parseErr.Detail, "must be a string") {
			t.Fatalf("Parse(%q) detail = %q, want string-type violation", content, parseErr.Detail)
		}
	}
}

func TestParse_AdditionalFieldsRemainOpaque(t *testing.T) {
	doc, err := Parse("name: alpha\ndescription: test\nlicense:\n  - item\nmetadata:\n  owner:\n    nested: true\ndisable-model-invocation: maybe\n")
	if err != nil {
		t.Fatalf("Parse opaque fields: %v", err)
	}
	if len(doc.Keys) != 5 {
		t.Fatalf("keys = %v, want all additional keys retained as names", doc.Keys)
	}
}

func TestParse_FieldStateDistinguishesAbsentNullValue(t *testing.T) {
	doc, err := Parse("description: here\n")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if doc.Description.State != FieldValue || doc.Description.Value != "here" {
		t.Fatalf("description = %#v, want present value", doc.Description)
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
