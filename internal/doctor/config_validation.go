package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
)

type configUnknownKeyDetail struct {
	Path       string
	Allowed    []string
	Suggestion string
}

type configSchemaNode struct {
	children   map[string]*configSchemaNode
	mapChild   *configSchemaNode
	arrayChild *configSchemaNode
	allowAny   bool
}

var (
	configSchemaOnce sync.Once
	configSchemaRoot *configSchemaNode
)

// summarizeUnknownKeys returns a compact summary suitable for single-line error output.
func summarizeUnknownKeys(details []configUnknownKeyDetail) string {
	if len(details) == 0 {
		return "unrecognized config keys"
	}
	paths := make([]string, 0, len(details))
	for _, detail := range details {
		paths = append(paths, detail.Path)
	}
	sort.Strings(paths)
	return fmt.Sprintf("unrecognized config keys: %s", strings.Join(paths, ", "))
}

// formatUnknownKeyRecommendation renders a multi-line recommendation for unknown keys.
func formatUnknownKeyRecommendation(configPath string, details []configUnknownKeyDetail) string {
	if len(details) == 0 {
		return ""
	}
	lines := []string{
		"Unrecognized config keys are not supported by this release's schema.",
		fmt.Sprintf("Edit %s to remove or rename them.", configPath),
		"",
		"Detected keys:",
	}
	for _, detail := range details {
		line := fmt.Sprintf("- %s", detail.Path)
		if len(detail.Allowed) > 0 {
			line = fmt.Sprintf("%s (allowed keys: %s)", line, strings.Join(detail.Allowed, ", "))
		} else {
			line = fmt.Sprintf("%s (no nested keys are allowed here)", line)
		}
		if detail.Suggestion != "" {
			line = fmt.Sprintf("%s (did you mean %s?)", line, detail.Suggestion)
		}
		lines = append(lines, line)
	}
	lines = append(lines,
		"",
		"Fix options:",
		"1) Remove the unknown keys above.",
		"2) If these keys came from an older release, run `al upgrade`.",
		"3) Run `al wizard` to regenerate a valid config.",
	)
	return strings.Join(lines, "\n")
}

// configUnknownKeys returns detected unknown config keys using the current schema.
func configUnknownKeys(configPath string) ([]configUnknownKeyDetail, error) {
	data, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	schema := configSchema()
	var details []configUnknownKeyDetail
	findUnknownConfigKeys(raw, schema, "", &details)
	sort.Slice(details, func(i, j int) bool {
		return details[i].Path < details[j].Path
	})
	return details, nil
}

// configSchema builds and caches the config schema derived from config.Config.
func configSchema() *configSchemaNode {
	configSchemaOnce.Do(func() {
		configSchemaRoot = buildSchema(reflect.TypeOf(config.Config{}))
	})
	return configSchemaRoot
}

// buildSchema constructs a schema tree from a reflected type using toml tags.
func buildSchema(t reflect.Type) *configSchemaNode {
	t = derefType(t)
	switch t.Kind() {
	case reflect.Struct:
		node := &configSchemaNode{children: make(map[string]*configSchemaNode)}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			tag := strings.TrimSpace(field.Tag.Get("toml"))
			if tag == "" || tag == "-" {
				continue
			}
			key := strings.Split(tag, ",")[0]
			if key == "" {
				continue
			}
			node.children[key] = buildSchema(field.Type)
		}
		return node
	case reflect.Map:
		elemType := derefType(t.Elem())
		if elemType.Kind() == reflect.Interface {
			return &configSchemaNode{allowAny: true}
		}
		return &configSchemaNode{allowAny: true, mapChild: buildSchema(elemType)}
	case reflect.Slice, reflect.Array:
		return &configSchemaNode{arrayChild: buildSchema(t.Elem())}
	default:
		return &configSchemaNode{}
	}
}

// derefType strips pointer indirection to reach the base type.
func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// findUnknownConfigKeys populates details with any unknown keys found under schema.
func findUnknownConfigKeys(raw any, schema *configSchemaNode, path string, details *[]configUnknownKeyDetail) {
	if schema == nil || raw == nil {
		return
	}
	switch typed := raw.(type) {
	case map[string]any:
		if schema.allowAny {
			if schema.mapChild != nil {
				for key, value := range typed {
					findUnknownConfigKeys(value, schema.mapChild, joinConfigPath(path, key), details)
				}
			}
			return
		}
		allowed := schema.allowedKeys()
		for key, value := range typed {
			child, ok := schema.children[key]
			if !ok {
				*details = append(*details, configUnknownKeyDetail{
					Path:       joinConfigPath(path, key),
					Allowed:    allowed,
					Suggestion: suggestKeyRename(key, schema, path),
				})
				continue
			}
			if child.allowAny {
				findUnknownConfigKeys(value, child, joinConfigPath(path, key), details)
				continue
			}
			if child.arrayChild != nil {
				if list, ok := value.([]any); ok {
					for i, item := range list {
						findUnknownConfigKeys(item, child.arrayChild, joinConfigIndexedPath(path, key, i), details)
					}
				}
				continue
			}
			if len(child.children) > 0 {
				findUnknownConfigKeys(value, child, joinConfigPath(path, key), details)
			}
		}
	case []any:
		if schema.arrayChild == nil || (schema.arrayChild.allowAny && schema.arrayChild.mapChild == nil) {
			return
		}
		for i, item := range typed {
			findUnknownConfigKeys(item, schema.arrayChild, fmt.Sprintf("%s[%d]", path, i), details)
		}
	}
}

func (n *configSchemaNode) allowedKeys() []string {
	if n == nil || len(n.children) == 0 {
		return nil
	}
	keys := make([]string, 0, len(n.children))
	for key := range n.children {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func joinConfigPath(path string, key string) string {
	segment := formatConfigPathSegment(key)
	if path == "" {
		return segment
	}
	if strings.HasPrefix(segment, "[") {
		return path + segment
	}
	return path + "." + segment
}

func joinConfigIndexedPath(path string, key string, index int) string {
	base := joinConfigPath(path, key)
	return fmt.Sprintf("%s[%d]", base, index)
}

func formatConfigPathSegment(key string) string {
	if key != "" && strings.IndexFunc(key, func(r rune) bool {
		return r != '_' &&
			(r < 'a' || r > 'z') &&
			(r < 'A' || r > 'Z') &&
			(r < '0' || r > '9')
	}) == -1 {
		return key
	}
	return fmt.Sprintf("[%q]", key)
}

// suggestKeyRename attempts to map an unknown key to a known key in the same schema scope.
func suggestKeyRename(key string, schema *configSchemaNode, path string) string {
	if schema == nil || len(schema.children) == 0 {
		return ""
	}
	for allowed := range schema.children {
		if strings.EqualFold(key, allowed) {
			return joinConfigPath(path, allowed)
		}
	}
	normalized := strings.ReplaceAll(key, "-", "_")
	if normalized != key {
		if _, ok := schema.children[normalized]; ok {
			return joinConfigPath(path, normalized)
		}
	}
	return ""
}
