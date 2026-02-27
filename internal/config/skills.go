package config

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	yaml "go.yaml.in/yaml/v3"
	"golang.org/x/text/unicode/norm"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	yamlTagStr  = "!!str"
	yamlTagNull = "!!null"
)

const (
	skillScannerInitialBufferSize = 64 * 1024
	skillScannerMaxTokenSize      = 8 * 1024 * 1024
)

type skillDirEntry struct {
	name  string
	isDir bool
}

type skillReadDir func(dir string) ([]skillDirEntry, error)

type skillReadFile func(path string) ([]byte, error)

type parsedSkill struct {
	description   string
	license       string
	compatibility string
	metadata      map[string]string
	allowedTools  string
	body          string
	name          string
}

type skillFrontMatter struct {
	Name          *string           `yaml:"name"`
	Description   *string           `yaml:"description"`
	License       *string           `yaml:"license"`
	Compatibility *string           `yaml:"compatibility"`
	Metadata      map[string]string `yaml:"metadata"`
	AllowedTools  *string           `yaml:"allowed-tools"`
	NameMultiline bool
}

type skillSource struct {
	path  string
	skill Skill
}

// LoadSkills reads .agent-layer/skills from disk and supports both source formats:
// - .agent-layer/skills/<name>.md
// - .agent-layer/skills/<name>/SKILL.md (canonical; fallback to skill.md for compatibility)
func LoadSkills(dir string) ([]Skill, error) {
	return loadSkills(dir,
		func(path string) ([]skillDirEntry, error) {
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			out := make([]skillDirEntry, 0, len(entries))
			for _, entry := range entries {
				out = append(out, skillDirEntry{name: entry.Name(), isDir: entry.IsDir()})
			}
			return out, nil
		},
		func(path string) ([]byte, error) {
			return osReadFileFunc(path)
		},
	)
}

func loadSkills(dir string, readDir skillReadDir, readFile skillReadFile) ([]Skill, error) {
	entries, err := readDir(dir)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingSkillsDirFmt, dir, err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	byName := make(map[string]skillSource)
	for _, entry := range entries {
		if strings.HasPrefix(entry.name, ".") {
			continue
		}
		if entry.isDir {
			if err := loadDirectorySkill(byName, dir, entry.name, readDir, readFile); err != nil {
				return nil, err
			}
			continue
		}
		if !strings.HasSuffix(entry.name, ".md") {
			continue
		}
		if err := loadFlatSkill(byName, dir, entry.name, readFile); err != nil {
			return nil, err
		}
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)

	skills := make([]Skill, 0, len(names))
	for _, name := range names {
		skills = append(skills, byName[name].skill)
	}
	return skills, nil
}

func loadFlatSkill(byName map[string]skillSource, root string, fileName string, readFile skillReadFile) error {
	path := filepath.Join(root, fileName)
	data, err := readFile(path)
	if err != nil {
		return fmt.Errorf(messages.ConfigFailedReadSkillFmt, path, err)
	}

	parsed, err := parseSkill(string(bytes.TrimPrefix(data, utf8BOM)))
	if err != nil {
		return fmt.Errorf(messages.ConfigInvalidSkillFmt, path, err)
	}

	name := strings.TrimSuffix(fileName, ".md")
	if parsed.name != "" && !skillNamesEqual(parsed.name, name) {
		return fmt.Errorf(messages.ConfigSkillNameMismatchFmt, path, parsed.name, name)
	}

	skill := Skill{
		Name:          name,
		Description:   parsed.description,
		License:       parsed.license,
		Compatibility: parsed.compatibility,
		Metadata:      parsed.metadata,
		AllowedTools:  parsed.allowedTools,
		Body:          parsed.body,
		SourcePath:    path,
	}
	return registerSkill(byName, skill)
}

func loadDirectorySkill(byName map[string]skillSource, root string, dirName string, readDir skillReadDir, readFile skillReadFile) error {
	skillDirPath := filepath.Join(root, dirName)
	entries, err := readDir(skillDirPath)
	if err != nil {
		return fmt.Errorf(messages.ConfigFailedReadSkillFmt, skillDirPath, err)
	}

	hasCanonical := false
	hasFallback := false
	for _, entry := range entries {
		if entry.isDir {
			continue
		}
		switch entry.name {
		case "SKILL.md":
			hasCanonical = true
		case "skill.md":
			hasFallback = true
		}
	}

	skillPath := ""
	switch {
	case hasCanonical:
		skillPath = filepath.Join(skillDirPath, "SKILL.md")
	case hasFallback:
		skillPath = filepath.Join(skillDirPath, "skill.md")
	default:
		return fmt.Errorf(messages.ConfigSkillDirMissingSkillFileFmt, skillDirPath)
	}

	data, err := readFile(skillPath)
	if err != nil {
		return fmt.Errorf(messages.ConfigFailedReadSkillFmt, skillPath, err)
	}

	parsed, err := parseSkill(string(bytes.TrimPrefix(data, utf8BOM)))
	if err != nil {
		return fmt.Errorf(messages.ConfigInvalidSkillFmt, skillPath, err)
	}

	name := dirName
	if parsed.name != "" && !skillNamesEqual(parsed.name, name) {
		return fmt.Errorf(messages.ConfigSkillNameMismatchFmt, skillPath, parsed.name, name)
	}

	skill := Skill{
		Name:          name,
		Description:   parsed.description,
		License:       parsed.license,
		Compatibility: parsed.compatibility,
		Metadata:      parsed.metadata,
		AllowedTools:  parsed.allowedTools,
		Body:          parsed.body,
		SourcePath:    skillPath,
	}
	return registerSkill(byName, skill)
}

func registerSkill(byName map[string]skillSource, skill Skill) error {
	if existing, ok := byName[skill.Name]; ok {
		return fmt.Errorf(messages.ConfigSkillDuplicateNameFmt, skill.Name, existing.path, skill.SourcePath)
	}
	byName[skill.Name] = skillSource{path: skill.SourcePath, skill: skill}
	return nil
}

func parseSkill(content string) (parsedSkill, error) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, skillScannerInitialBufferSize), skillScannerMaxTokenSize)
	if !scanner.Scan() {
		return parsedSkill{}, fmt.Errorf(messages.ConfigSkillMissingContent)
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return parsedSkill{}, fmt.Errorf(messages.ConfigSkillMissingFrontMatter)
	}

	var fmLines []string
	foundEnd := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "---" {
			foundEnd = true
			break
		}
		fmLines = append(fmLines, line)
	}
	if !foundEnd {
		return parsedSkill{}, fmt.Errorf(messages.ConfigSkillUnterminatedFrontMatter)
	}

	var bodyBuilder strings.Builder
	for scanner.Scan() {
		bodyBuilder.WriteString(scanner.Text())
		bodyBuilder.WriteString("\n")
	}
	if err := scanner.Err(); err != nil {
		return parsedSkill{}, fmt.Errorf(messages.ConfigSkillFailedReadContentFmt, err)
	}

	frontMatter, err := parseSkillFrontMatter(fmLines)
	if err != nil {
		return parsedSkill{}, err
	}

	description, err := parseSkillDescription(frontMatter.Description)
	if err != nil {
		return parsedSkill{}, err
	}

	name, err := parseSkillName(frontMatter.Name, frontMatter.NameMultiline)
	if err != nil {
		return parsedSkill{}, err
	}

	body := strings.TrimPrefix(bodyBuilder.String(), "\n")
	body = strings.TrimRight(body, "\n")
	return parsedSkill{
		description:   description,
		license:       normalizeOptionalSkillValue(frontMatter.License),
		compatibility: normalizeOptionalSkillValue(frontMatter.Compatibility),
		metadata:      normalizeSkillMetadata(frontMatter.Metadata),
		allowedTools:  normalizeOptionalSkillValue(frontMatter.AllowedTools),
		body:          body,
		name:          name,
	}, nil
}

func parseSkillFrontMatter(lines []string) (skillFrontMatter, error) {
	var frontMatter skillFrontMatter
	frontMatterContent := strings.Join(lines, "\n")
	if strings.TrimSpace(frontMatterContent) == "" {
		return frontMatter, nil
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(frontMatterContent), &root); err != nil {
		var typeErr *yaml.TypeError
		if errors.As(err, &typeErr) {
			return skillFrontMatter{}, fmt.Errorf(messages.ConfigSkillInvalidFrontMatterTypeFmt, strings.Join(typeErr.Errors, "; "))
		}
		return skillFrontMatter{}, fmt.Errorf(messages.ConfigSkillInvalidFrontMatterFmt, err)
	}

	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return skillFrontMatter{}, fmt.Errorf(messages.ConfigSkillInvalidFrontMatterTypeFmt, "front matter must be a mapping")
	}

	mapping := root.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode := mapping.Content[i]
		valueNode := mapping.Content[i+1]
		key := strings.TrimSpace(keyNode.Value)

		switch key {
		case "name":
			value, err := parseFrontMatterStringField("name", valueNode)
			if err != nil {
				return skillFrontMatter{}, err
			}
			frontMatter.Name = &value
			frontMatter.NameMultiline = valueNode.Style == yaml.LiteralStyle || valueNode.Style == yaml.FoldedStyle
		case "description":
			value, err := parseFrontMatterStringField("description", valueNode)
			if err != nil {
				return skillFrontMatter{}, err
			}
			frontMatter.Description = &value
		case "license":
			value, err := parseFrontMatterStringField("license", valueNode)
			if err != nil {
				return skillFrontMatter{}, err
			}
			frontMatter.License = &value
		case "compatibility":
			value, err := parseFrontMatterStringField("compatibility", valueNode)
			if err != nil {
				return skillFrontMatter{}, err
			}
			frontMatter.Compatibility = &value
		case "allowed-tools":
			value, err := parseFrontMatterStringField("allowed-tools", valueNode)
			if err != nil {
				return skillFrontMatter{}, err
			}
			frontMatter.AllowedTools = &value
		case "metadata":
			metadata, err := parseFrontMatterMetadata(valueNode)
			if err != nil {
				return skillFrontMatter{}, err
			}
			frontMatter.Metadata = metadata
		default:
			// Unknown fields are intentionally tolerated at parse time.
		}
	}

	return frontMatter, nil
}

func parseSkillDescription(description *string) (string, error) {
	if description == nil {
		return "", fmt.Errorf(messages.ConfigSkillMissingDescription)
	}
	normalized := strings.TrimSpace(*description)
	if normalized == "" {
		return "", fmt.Errorf(messages.ConfigSkillDescriptionEmpty)
	}
	return normalized, nil
}

func parseSkillName(name *string, multiline bool) (string, error) {
	if name == nil {
		return "", nil
	}
	normalized := strings.TrimSpace(*name)
	if normalized == "" {
		return "", fmt.Errorf(messages.ConfigSkillNameEmpty)
	}
	if multiline || strings.Contains(normalized, "\n") {
		return "", fmt.Errorf(messages.ConfigSkillNameInvalidMultiline)
	}
	return normalized, nil
}

func normalizeOptionalSkillValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func normalizeSkillName(value string) string {
	return strings.TrimSpace(norm.NFKC.String(value))
}

func skillNamesEqual(left string, right string) bool {
	return normalizeSkillName(left) == normalizeSkillName(right)
}

func normalizeSkillMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}

	normalized := make(map[string]string, len(metadata))
	for key, value := range metadata {
		normalized[key] = value
	}
	return normalized
}

func parseFrontMatterStringField(field string, node *yaml.Node) (string, error) {
	if node.Kind != yaml.ScalarNode {
		return "", fmt.Errorf(messages.ConfigSkillInvalidFrontMatterTypeFmt, fmt.Sprintf("%s must be a string", field))
	}
	if node.Tag != "" && node.Tag != yamlTagStr && node.Tag != yamlTagNull {
		return "", fmt.Errorf(messages.ConfigSkillInvalidFrontMatterTypeFmt, fmt.Sprintf("%s must be a string", field))
	}
	if node.Tag == yamlTagNull {
		return "", nil
	}
	return node.Value, nil
}

func parseFrontMatterMetadata(node *yaml.Node) (map[string]string, error) {
	if node.Kind == yaml.ScalarNode && node.Tag == yamlTagNull {
		return nil, nil
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf(messages.ConfigSkillInvalidFrontMatterTypeFmt, "metadata must be a string map")
	}

	metadata := make(map[string]string, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valueNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode || (keyNode.Tag != "" && keyNode.Tag != yamlTagStr) {
			return nil, fmt.Errorf(messages.ConfigSkillInvalidFrontMatterTypeFmt, "metadata keys must be strings")
		}
		if valueNode.Kind != yaml.ScalarNode || (valueNode.Tag != "" && valueNode.Tag != yamlTagStr) {
			return nil, fmt.Errorf(messages.ConfigSkillInvalidFrontMatterTypeFmt, "metadata values must be strings")
		}
		metadata[keyNode.Value] = valueNode.Value
	}
	return metadata, nil
}
