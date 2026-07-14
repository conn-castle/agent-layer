// Package skillfrontmatter is the canonical structural parser for SKILL.md
// YAML front matter. It owns root-mapping validation, duplicate-key
// rejection, scalar field typing, and metadata string-map validation.
// Consumer-specific policy (required fields, standards warnings, sync
// semantics) stays with the consumers.
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
	// License is the "license" field.
	License Field
	// Compatibility is the "compatibility" field.
	Compatibility Field
	// AllowedTools is the "allowed-tools" field.
	AllowedTools Field
	// Metadata holds the "metadata" string map; nil when the key is absent
	// or null, possibly empty when an empty map was supplied.
	Metadata map[string]string
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
		key := strings.TrimSpace(keyNode.Value)
		if key == "" {
			continue
		}
		if seen[key] {
			return Document{}, duplicateKeyError(key)
		}
		seen[key] = true
		doc.Keys = append(doc.Keys, key)

		var target *Field
		switch key {
		case "name":
			target = &doc.Name
		case "description":
			target = &doc.Description
		case "license":
			target = &doc.License
		case "compatibility":
			target = &doc.Compatibility
		case "allowed-tools":
			target = &doc.AllowedTools
		case "metadata":
			metadata, err := parseMetadata(valueNode)
			if err != nil {
				return Document{}, err
			}
			doc.Metadata = metadata
			continue
		default:
			// Unknown fields are tolerated at parse time; consumers decide
			// whether to warn on them.
			continue
		}
		field, err := parseScalarField(key, valueNode)
		if err != nil {
			return Document{}, err
		}
		*target = field
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

func parseMetadata(node *yaml.Node) (map[string]string, error) {
	if node.Kind == yaml.ScalarNode && node.Tag == yamlTagNull {
		return nil, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, typeError("metadata must be a string map")
	}
	metadata := make(map[string]string, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode || (keyNode.Tag != "" && keyNode.Tag != yamlTagStr) {
			return nil, typeError("metadata keys must be strings")
		}
		if valueNode.Kind != yaml.ScalarNode || (valueNode.Tag != "" && valueNode.Tag != yamlTagStr) {
			return nil, typeError("metadata values must be strings")
		}
		if _, exists := metadata[keyNode.Value]; exists {
			return nil, duplicateKeyError(keyNode.Value)
		}
		metadata[keyNode.Value] = valueNode.Value
	}
	return metadata, nil
}

func typeError(detail string) *Error {
	return &Error{Kind: KindType, Detail: detail}
}

func duplicateKeyError(key string) *Error {
	return &Error{Kind: KindDuplicateKey, Key: key, Detail: fmt.Sprintf("duplicate key %q", key)}
}
