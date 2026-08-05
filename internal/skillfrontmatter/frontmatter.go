// Package skillfrontmatter parses only the required identity fields from
// SKILL.md YAML front matter. Additional fields remain opaque so callers can
// preserve and project their original bytes without imposing provider policy.
package skillfrontmatter

import (
	"errors"
	"fmt"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

const (
	yamlTagStr  = "!!str"
	yamlTagNull = "!!null"
)

// ErrorKind classifies a structural front-matter parse failure so consumers
// can wrap it in their own message conventions.
type ErrorKind int

const (
	// KindSyntax reports YAML that could not be parsed at all.
	KindSyntax ErrorKind = iota + 1
	// KindType reports a structural or type violation: a non-mapping root,
	// a non-string scalar field, or a malformed metadata map.
	KindType
	// KindDuplicateKey reports a duplicate top-level or metadata key.
	KindDuplicateKey
)

// Error describes why front matter failed structural parsing.
type Error struct {
	// Kind classifies the failure.
	Kind ErrorKind
	// Detail is a human-readable description of the failure.
	Detail string
	// Key is the offending key name for KindDuplicateKey errors.
	Key string
	// Err is the underlying YAML error, if any.
	Err error
}

// Error returns the human-readable failure detail.
func (e *Error) Error() string { return e.Detail }

// Unwrap returns the underlying YAML error, if any.
func (e *Error) Unwrap() error { return e.Err }

// FieldState reports whether a supported scalar field appeared in the front
// matter and, if so, whether it carried a value.
type FieldState int

const (
	// FieldAbsent means the key did not appear in the front matter.
	FieldAbsent FieldState = iota
	// FieldNull means the key appeared with an explicit or implicit null value.
	FieldNull
	// FieldValue means the key appeared with a string value.
	FieldValue
)

// Field is the structural parse result for one supported scalar field.
type Field struct {
	// State reports whether the field was absent, null, or carried a value.
	State FieldState
	// Value is the raw string value; meaningful only when State is FieldValue.
	Value string
	// Multiline reports whether the value used a literal or folded block
	// scalar style. Consumers apply their own policy to this evidence.
	Multiline bool
}

// Document is the structural parse result of SKILL.md YAML front matter.
type Document struct {
	// Keys lists the non-empty top-level keys in document order, including
	// unknown keys, which are tolerated at parse time.
	Keys []string
	// Name is the "name" field.
	Name Field
	// Description is the "description" field.
	Description Field
}

// Parse parses SKILL.md YAML front-matter content into a Document.
// Empty or whitespace-only content yields an empty Document. Structural
// failures are returned as *Error.
func Parse(content string) (Document, error) {
	var doc Document
	if strings.TrimSpace(content) == "" {
		return doc, nil
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		var typeErr *yaml.TypeError
		if errors.As(err, &typeErr) {
			return Document{}, &Error{Kind: KindType, Detail: strings.Join(typeErr.Errors, "; "), Err: err}
		}
		return Document{}, &Error{Kind: KindSyntax, Detail: err.Error(), Err: err}
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return Document{}, &Error{Kind: KindType, Detail: "front matter must be a mapping"}
	}

	mapping := root.Content[0]
	seen := make(map[string]bool)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode := mapping.Content[i]
		valueNode := mapping.Content[i+1]
		key := keyNode.Value
		if key == "" {
			continue
		}
		if seen[key] {
			return Document{}, duplicateKeyError(key)
		}
		seen[key] = true
		doc.Keys = append(doc.Keys, key)

		switch key {
		case "name":
			field, err := parseScalarField(key, valueNode)
			if err != nil {
				return Document{}, err
			}
			doc.Name = field
		case "description":
			field, err := parseScalarField(key, valueNode)
			if err != nil {
				return Document{}, err
			}
			doc.Description = field
		default:
			// Additional fields are intentionally opaque.
		}
	}
	return doc, nil
}

func parseScalarField(field string, node *yaml.Node) (Field, error) {
	if node.Kind != yaml.ScalarNode {
		return Field{}, typeError(fmt.Sprintf("field %q must be a string", field))
	}
	if node.Tag == yamlTagNull {
		return Field{State: FieldNull}, nil
	}
	if node.Tag != "" && node.Tag != yamlTagStr {
		return Field{}, typeError(fmt.Sprintf("field %q must be a string", field))
	}
	return Field{
		State:     FieldValue,
		Value:     node.Value,
		Multiline: node.Style == yaml.LiteralStyle || node.Style == yaml.FoldedStyle,
	}, nil
}

func typeError(detail string) *Error {
	return &Error{Kind: KindType, Detail: detail}
}

func duplicateKeyError(key string) *Error {
	return &Error{Kind: KindDuplicateKey, Key: key, Detail: fmt.Sprintf("duplicate key %q", key)}
}
