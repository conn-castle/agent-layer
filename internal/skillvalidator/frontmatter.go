package skillvalidator

import (
	"fmt"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

const (
	yamlTagStr  = "!!str"
	yamlTagNull = "!!null"
)

type frontMatter struct {
	keys          []string
	name          *string
	description   *string
	compatibility *string
}

func parseFrontMatter(content string) (frontMatter, error) {
	out := frontMatter{
		keys: make([]string, 0),
	}
	if strings.TrimSpace(content) == "" {
		return out, nil
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return out, err
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return out, fmt.Errorf("frontmatter must be a YAML mapping")
	}

	mapping := root.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode := mapping.Content[i]
		valueNode := mapping.Content[i+1]
		key := strings.TrimSpace(keyNode.Value)
		if key == "" {
			continue
		}
		out.keys = append(out.keys, key)
		switch key {
		case "name":
			value, err := parseScalarString(valueNode, "name")
			if err != nil {
				return out, err
			}
			out.name = value
		case "description":
			value, err := parseScalarString(valueNode, "description")
			if err != nil {
				return out, err
			}
			out.description = value
		case "compatibility":
			value, err := parseScalarString(valueNode, "compatibility")
			if err != nil {
				return out, err
			}
			out.compatibility = value
		case "metadata":
			if err := ensureMetadataMap(valueNode); err != nil {
				return out, err
			}
		case "license", "allowed-tools":
			if _, err := parseScalarString(valueNode, key); err != nil {
				return out, err
			}
		}
	}
	sort.Strings(out.keys)
	return out, nil
}

func parseScalarString(node *yaml.Node, field string) (*string, error) {
	if node.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("frontmatter field %q must be a string scalar", field)
	}
	if node.Tag == yamlTagNull {
		return nil, nil
	}
	if node.Tag != "" && node.Tag != yamlTagStr {
		return nil, fmt.Errorf("frontmatter field %q must be a string scalar", field)
	}
	value := node.Value
	return &value, nil
}

func ensureMetadataMap(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.Tag == yamlTagNull {
		return nil
	}
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("frontmatter field %q must be a mapping", "metadata")
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode || (keyNode.Tag != "" && keyNode.Tag != yamlTagStr) {
			return fmt.Errorf("frontmatter field %q must have string keys", "metadata")
		}
		if valueNode.Kind != yaml.ScalarNode || (valueNode.Tag != "" && valueNode.Tag != yamlTagStr) {
			return fmt.Errorf("frontmatter field %q must have string values", "metadata")
		}
	}
	return nil
}
