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

	"golang.org/x/text/unicode/norm"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skillfrontmatter"
)

// utf8BOM is the UTF-8 byte-order-mark trimmed from skill and instruction file
// content before parsing.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// osReadFileFunc is a test seam over os.ReadFile used by LoadSkills.
var osReadFileFunc = os.ReadFile

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

type skillSource struct {
	path  string
	skill Skill
}

// LoadSkills reads .agent-layer/skills from disk.
// Supported source format:
// - .agent-layer/skills/<name>/SKILL.md (canonical; fallback to skill.md for compatibility)
// Flat-format .agent-layer/skills/<name>.md files are rejected with actionable errors.
// Directories without a supported skill file also fail loudly.
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
		if strings.HasSuffix(entry.name, ".md") {
			name := strings.TrimSuffix(entry.name, ".md")
			return nil, fmt.Errorf(messages.ConfigSkillFlatFormatUnsupportedFmt, name, filepath.Join(dir, entry.name))
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
		case skillManifestName:
			hasCanonical = true
		case lowercaseSkillManifestName:
			hasFallback = true
		}
	}

	skillPath := ""
	switch {
	case hasCanonical:
		skillPath = filepath.Join(skillDirPath, skillManifestName)
	case hasFallback:
		skillPath = filepath.Join(skillDirPath, lowercaseSkillManifestName)
	default:
		return fmt.Errorf(messages.ConfigSkillDirEmptyFmt, skillDirPath)
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
		SourceDir:     skillDirPath,
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

	doc, err := skillfrontmatter.Parse(strings.Join(fmLines, "\n"))
	if err != nil {
		return parsedSkill{}, wrapFrontMatterError(err)
	}

	description, err := parseSkillDescription(skillFieldValue(doc.Description))
	if err != nil {
		return parsedSkill{}, err
	}

	name, err := parseSkillName(skillFieldValue(doc.Name), doc.Name.Multiline)
	if err != nil {
		return parsedSkill{}, err
	}

	body := strings.TrimPrefix(bodyBuilder.String(), "\n")
	body = strings.TrimRight(body, "\n")
	return parsedSkill{
		description:   description,
		license:       normalizeOptionalSkillValue(skillFieldValue(doc.License)),
		compatibility: normalizeOptionalSkillValue(skillFieldValue(doc.Compatibility)),
		metadata:      normalizeSkillMetadata(doc.Metadata),
		allowedTools:  normalizeOptionalSkillValue(skillFieldValue(doc.AllowedTools)),
		body:          body,
		name:          name,
	}, nil
}

// wrapFrontMatterError converts a structural front-matter parse failure into
// the config package's existing error message conventions.
func wrapFrontMatterError(err error) error {
	var parseErr *skillfrontmatter.Error
	if errors.As(err, &parseErr) {
		switch parseErr.Kind {
		case skillfrontmatter.KindDuplicateKey:
			return fmt.Errorf(messages.ConfigSkillDuplicateKeyFmt, parseErr.Key)
		case skillfrontmatter.KindSyntax:
			return fmt.Errorf(messages.ConfigSkillInvalidFrontMatterFmt, parseErr)
		default:
			return fmt.Errorf(messages.ConfigSkillInvalidFrontMatterTypeFmt, parseErr.Detail)
		}
	}
	return fmt.Errorf(messages.ConfigSkillInvalidFrontMatterFmt, err)
}

// skillFieldValue maps a structural field to the config policy view: absent
// fields are nil, while present-null fields behave like present empty values.
func skillFieldValue(field skillfrontmatter.Field) *string {
	switch field.State {
	case skillfrontmatter.FieldNull:
		empty := ""
		return &empty
	case skillfrontmatter.FieldValue:
		value := field.Value
		return &value
	default:
		return nil
	}
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
