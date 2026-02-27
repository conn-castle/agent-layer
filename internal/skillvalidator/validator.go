package skillvalidator

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"
	"golang.org/x/text/unicode/norm"
)

var (
	utf8BOM = []byte{0xEF, 0xBB, 0xBF}
)

const (
	yamlTagStr  = "!!str"
	yamlTagNull = "!!null"
)

const (
	// MaxSkillNameLength is the maximum accepted length for skill frontmatter name.
	MaxSkillNameLength = 64
	// MaxDescriptionLength is the maximum accepted length for skill description.
	MaxDescriptionLength = 1024
	// MaxCompatibilityLength is the maximum accepted length for compatibility text.
	MaxCompatibilityLength = 500
	// MaxRecommendedSkillLines is the recommended upper bound for SKILL.md lines.
	MaxRecommendedSkillLines = 500
)

const (
	// FindingCodeNameMissing reports a missing required name field.
	FindingCodeNameMissing = "SKILL_NAME_MISSING"
	// FindingCodeNameInvalid reports an invalid skill name format.
	FindingCodeNameInvalid = "SKILL_NAME_INVALID"
	// FindingCodeNameTooLong reports skill names that exceed MaxSkillNameLength.
	FindingCodeNameTooLong = "SKILL_NAME_TOO_LONG"
	// FindingCodeNameConsecutiveHyphens reports names containing "--".
	FindingCodeNameConsecutiveHyphens = "SKILL_NAME_CONSECUTIVE_HYPHENS"
	// FindingCodeNamePathMismatch reports skill names that do not match canonical source names.
	FindingCodeNamePathMismatch = "SKILL_NAME_PATH_MISMATCH"
	// FindingCodeDescriptionMissing reports a missing required description field.
	FindingCodeDescriptionMissing = "SKILL_DESCRIPTION_MISSING"
	// FindingCodeDescriptionTooLong reports descriptions that exceed MaxDescriptionLength.
	FindingCodeDescriptionTooLong = "SKILL_DESCRIPTION_TOO_LONG"
	// FindingCodeCompatibilityTooLong reports compatibility values that exceed MaxCompatibilityLength.
	FindingCodeCompatibilityTooLong = "SKILL_COMPATIBILITY_TOO_LONG"
	// FindingCodeUnknownField reports unknown frontmatter fields.
	FindingCodeUnknownField = "SKILL_FRONTMATTER_UNKNOWN_FIELD"
	// FindingCodeDirectorySkillFileName reports non-canonical directory skill filenames.
	FindingCodeDirectorySkillFileName = "SKILL_DIRECTORY_FILENAME"
	// FindingCodeSizeRecommendation reports SKILL.md files that exceed MaxRecommendedSkillLines.
	FindingCodeSizeRecommendation = "SKILL_SIZE_RECOMMENDATION"
)

// Severity indicates validation finding severity.
type Severity string

const (
	// SeverityWarn indicates a non-blocking standards warning.
	SeverityWarn Severity = "warn"
)

// SourceFormat describes how a source skill is represented on disk.
type SourceFormat string

const (
	// SourceFormatFlat is `.agent-layer/skills/<name>.md`.
	SourceFormatFlat SourceFormat = "flat"
	// SourceFormatDirectory is `.agent-layer/skills/<name>/SKILL.md`.
	SourceFormatDirectory SourceFormat = "directory"
)

// Finding is a single deterministic validator diagnostic.
type Finding struct {
	Code     string
	Severity Severity
	Path     string
	Message  string
}

// ParsedSkill is a parsed skill source used as validation input.
type ParsedSkill struct {
	SourcePath      string
	CanonicalName   string
	SourceFormat    SourceFormat
	LineCount       int
	FrontMatterKeys []string
	Name            *string
	Description     *string
	Compatibility   *string
}

// allowedFrontMatterFields is the strict validator allowlist for skill frontmatter fields.
var allowedFrontMatterFields = map[string]struct{}{
	"name":          {},
	"description":   {},
	"license":       {},
	"compatibility": {},
	"metadata":      {},
	"allowed-tools": {},
}

// ParseSkillSource reads and parses a skill source file into validator input.
func ParseSkillSource(path string) (ParsedSkill, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ParsedSkill{}, fmt.Errorf("read skill source %s: %w", path, err)
	}
	content := string(bytes.TrimPrefix(raw, utf8BOM))
	lineCount := countLines(content)

	scanner := bufio.NewScanner(strings.NewReader(content))
	if !scanner.Scan() {
		return ParsedSkill{}, fmt.Errorf("skill source %s is empty", path)
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return ParsedSkill{}, fmt.Errorf("skill source %s is missing YAML frontmatter", path)
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
	if err := scanner.Err(); err != nil {
		return ParsedSkill{}, fmt.Errorf("read skill source %s: %w", path, err)
	}
	if !foundEnd {
		return ParsedSkill{}, fmt.Errorf("skill source %s has unterminated YAML frontmatter", path)
	}

	frontmatter, err := parseFrontMatter(strings.Join(fmLines, "\n"))
	if err != nil {
		return ParsedSkill{}, fmt.Errorf("parse frontmatter for %s: %w", path, err)
	}

	name, format := canonicalNameForPath(path)
	return ParsedSkill{
		SourcePath:      path,
		CanonicalName:   name,
		SourceFormat:    format,
		LineCount:       lineCount,
		FrontMatterKeys: frontmatter.keys,
		Name:            frontmatter.name,
		Description:     frontmatter.description,
		Compatibility:   frontmatter.compatibility,
	}, nil
}

// ValidateMetadata validates frontmatter-level skill requirements.
func ValidateMetadata(parsed ParsedSkill) []Finding {
	findings := make([]Finding, 0)
	keySet := make(map[string]struct{}, len(parsed.FrontMatterKeys))
	for _, key := range parsed.FrontMatterKeys {
		keySet[key] = struct{}{}
		if _, ok := allowedFrontMatterFields[key]; !ok {
			findings = append(findings, warning(
				FindingCodeUnknownField,
				parsed.SourcePath,
				fmt.Sprintf("unknown frontmatter field %q (allowed: name, description, license, compatibility, metadata, allowed-tools)", key),
			))
		}
	}

	if parsed.Name == nil {
		findings = append(findings, warning(FindingCodeNameMissing, parsed.SourcePath, "missing required frontmatter field \"name\""))
	} else {
		name := normalizeSkillName(*parsed.Name)
		if name == "" {
			findings = append(findings, warning(FindingCodeNameMissing, parsed.SourcePath, "frontmatter field \"name\" must be non-empty"))
		} else {
			nameRuneCount := utf8.RuneCountInString(name)
			if nameRuneCount > MaxSkillNameLength {
				findings = append(findings, warning(
					FindingCodeNameTooLong,
					parsed.SourcePath,
					fmt.Sprintf("frontmatter field \"name\" exceeds %d characters (%d)", MaxSkillNameLength, nameRuneCount),
				))
			}
			if !isValidSkillName(name) {
				findings = append(findings, warning(
					FindingCodeNameInvalid,
					parsed.SourcePath,
					"frontmatter field \"name\" must contain only lowercase letters, digits, and hyphens; it cannot start or end with a hyphen",
				))
			}
			if strings.Contains(name, "--") {
				findings = append(findings, warning(
					FindingCodeNameConsecutiveHyphens,
					parsed.SourcePath,
					"frontmatter field \"name\" must not contain consecutive hyphens",
				))
			}
		}
	}

	if parsed.Description == nil {
		findings = append(findings, warning(FindingCodeDescriptionMissing, parsed.SourcePath, "missing required frontmatter field \"description\""))
	} else {
		description := strings.TrimSpace(*parsed.Description)
		if description == "" {
			findings = append(findings, warning(FindingCodeDescriptionMissing, parsed.SourcePath, "frontmatter field \"description\" must be non-empty"))
		} else if descriptionRuneCount := utf8.RuneCountInString(description); descriptionRuneCount > MaxDescriptionLength {
			findings = append(findings, warning(
				FindingCodeDescriptionTooLong,
				parsed.SourcePath,
				fmt.Sprintf("frontmatter field \"description\" exceeds %d characters (%d)", MaxDescriptionLength, descriptionRuneCount),
			))
		}
	}

	if _, ok := keySet["compatibility"]; ok && parsed.Compatibility != nil {
		compatibility := strings.TrimSpace(*parsed.Compatibility)
		if compatibilityRuneCount := utf8.RuneCountInString(compatibility); compatibilityRuneCount > MaxCompatibilityLength {
			findings = append(findings, warning(
				FindingCodeCompatibilityTooLong,
				parsed.SourcePath,
				fmt.Sprintf("frontmatter field \"compatibility\" exceeds %d characters (%d)", MaxCompatibilityLength, compatibilityRuneCount),
			))
		}
	}

	sortFindings(findings)
	return findings
}

// ValidateDirectory validates source-path and directory-format conventions.
func ValidateDirectory(parsed ParsedSkill) []Finding {
	findings := make([]Finding, 0)
	if parsed.SourceFormat == SourceFormatDirectory && filepath.Base(parsed.SourcePath) != "SKILL.md" {
		findings = append(findings, warning(
			FindingCodeDirectorySkillFileName,
			parsed.SourcePath,
			"directory-format skill sources must use SKILL.md",
		))
	}

	if parsed.Name != nil {
		name := normalizeSkillName(*parsed.Name)
		canonical := normalizeSkillName(parsed.CanonicalName)
		if name != "" && name != canonical {
			findings = append(findings, warning(
				FindingCodeNamePathMismatch,
				parsed.SourcePath,
				fmt.Sprintf("frontmatter field \"name\" (%q) must match canonical source name %q", strings.TrimSpace(*parsed.Name), parsed.CanonicalName),
			))
		}
	}

	sortFindings(findings)
	return findings
}

// ValidateParsedSkill validates all configured skill rules for a parsed source.
func ValidateParsedSkill(parsed ParsedSkill) []Finding {
	findings := make([]Finding, 0)
	findings = append(findings, ValidateMetadata(parsed)...)
	findings = append(findings, ValidateDirectory(parsed)...)
	if parsed.LineCount > MaxRecommendedSkillLines {
		findings = append(findings, warning(
			FindingCodeSizeRecommendation,
			parsed.SourcePath,
			fmt.Sprintf("SKILL.md is %d lines; keep skill instructions under %d lines when possible", parsed.LineCount, MaxRecommendedSkillLines),
		))
	}
	sortFindings(findings)
	return findings
}

// ValidateSkillSource parses and validates a skill source path.
func ValidateSkillSource(path string) ([]Finding, error) {
	parsed, err := ParseSkillSource(path)
	if err != nil {
		return nil, err
	}
	return ValidateParsedSkill(parsed), nil
}

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

func canonicalNameForPath(path string) (string, SourceFormat) {
	base := filepath.Base(path)
	if base == "SKILL.md" || base == "skill.md" {
		return filepath.Base(filepath.Dir(path)), SourceFormatDirectory
	}
	return strings.TrimSuffix(base, filepath.Ext(base)), SourceFormatFlat
}

func normalizeSkillName(name string) string {
	return strings.TrimSpace(norm.NFKC.String(name))
}

func isValidSkillName(name string) bool {
	if name == "" {
		return false
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	for _, r := range name {
		if r == '-' || (r >= '0' && r <= '9') || unicode.IsLower(r) {
			continue
		}
		return false
	}
	return true
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		return findings[i].Message < findings[j].Message
	})
}

func warning(code string, path string, message string) Finding {
	return Finding{
		Code:     code,
		Severity: SeverityWarn,
		Path:     path,
		Message:  message,
	}
}

func countLines(content string) int {
	if content == "" {
		return 0
	}
	count := strings.Count(content, "\n")
	if strings.HasSuffix(content, "\n") {
		return count
	}
	return count + 1
}
