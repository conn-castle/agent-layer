package skillvalidator

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"
)

var (
	utf8BOM = []byte{0xEF, 0xBB, 0xBF}
)

const (
	skillSourceScannerInitialBufferSize = 64 * 1024
	skillSourceScannerMaxTokenSize      = 8 * 1024 * 1024
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
	// FindingCodeSizeRecommendation reports SKILL.md files that exceed MaxRecommendedSkillLines.
	FindingCodeSizeRecommendation = "SKILL_SIZE_RECOMMENDATION"
)

// Finding is a single deterministic validator diagnostic.
type Finding struct {
	Code    string
	Path    string
	Message string
}

// ParsedSkill is a parsed skill source used as validation input.
type ParsedSkill struct {
	SourcePath    string
	CanonicalName string
	LineCount     int
	// Name and Description are nil when the field is absent or null.
	Name        *string
	Description *string
}

// ParseSkillSource reads and parses a <name>/SKILL.md source file into
// validator input.
func ParseSkillSource(path string) (ParsedSkill, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- skill source path is provided by the al CLI which resolved it from a configured templates directory, not user input.
	if err != nil {
		return ParsedSkill{}, fmt.Errorf("read skill source %s: %w", path, err)
	}
	return ParseSkillContent(path, raw)
}

// ParseSkillContent parses already-read <name>/SKILL.md bytes into validator
// input. path supplies the canonical name from its parent directory and is
// used for error context; it does not have to exist on the local filesystem,
// so callers holding a skill tree in memory (imports, merges, upstream
// comparisons) validate through exactly the same rules as on-disk sources.
func ParseSkillContent(path string, raw []byte) (ParsedSkill, error) {
	content := string(bytes.TrimPrefix(raw, utf8BOM))
	lineCount := countLines(content)

	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, skillSourceScannerInitialBufferSize), skillSourceScannerMaxTokenSize)
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

	name, description, err := parseFrontMatter(strings.Join(fmLines, "\n"))
	if err != nil {
		return ParsedSkill{}, fmt.Errorf("parse frontmatter for %s: %w", path, err)
	}

	return ParsedSkill{
		SourcePath:    path,
		CanonicalName: canonicalNameForPath(path),
		LineCount:     lineCount,
		Name:          name,
		Description:   description,
	}, nil
}

// parseFrontMatter extracts the required name and description fields from
// SKILL.md YAML front matter. Additional fields remain opaque so callers can
// preserve and project their original bytes without imposing provider policy.
// Absent and null fields are both returned as nil.
func parseFrontMatter(content string) (name *string, description *string, err error) {
	if strings.TrimSpace(content) == "" {
		return nil, nil, nil
	}

	var root yaml.Node
	// Decoding into a yaml.Node never reports *yaml.TypeError; only syntax
	// errors reach this branch.
	if err := yaml.Unmarshal([]byte(content), &root); err != nil {
		return nil, nil, err
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, nil, errors.New("front matter must be a mapping")
	}

	mapping := root.Content[0]
	seen := make(map[string]bool)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i].Value
		valueNode := mapping.Content[i+1]
		if key == "" {
			continue
		}
		if seen[key] {
			return nil, nil, fmt.Errorf("duplicate key %q", key)
		}
		seen[key] = true

		switch key {
		case "name":
			if name, err = stringField(key, valueNode); err != nil {
				return nil, nil, err
			}
		case "description":
			if description, err = stringField(key, valueNode); err != nil {
				return nil, nil, err
			}
		}
	}
	return name, description, nil
}

func stringField(key string, node *yaml.Node) (*string, error) {
	if node.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("field %q must be a string", key)
	}
	if node.Tag == yamlTagNull {
		return nil, nil
	}
	if node.Tag != "" && node.Tag != yamlTagStr {
		return nil, fmt.Errorf("field %q must be a string", key)
	}
	value := node.Value
	return &value, nil
}

// ValidateParsedSkill validates all configured skill rules for a parsed source.
func ValidateParsedSkill(parsed ParsedSkill) []Finding {
	findings := make([]Finding, 0)
	if parsed.Name == nil {
		findings = append(findings, warning(FindingCodeNameMissing, parsed.SourcePath, "missing required frontmatter field \"name\""))
	} else {
		name := NormalizeName(*parsed.Name)
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
			if name != NormalizeName(parsed.CanonicalName) {
				findings = append(findings, warning(
					FindingCodeNamePathMismatch,
					parsed.SourcePath,
					fmt.Sprintf("frontmatter field \"name\" (%q) must match canonical source name %q", strings.TrimSpace(*parsed.Name), parsed.CanonicalName),
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

	if parsed.LineCount > MaxRecommendedSkillLines {
		findings = append(findings, warning(
			FindingCodeSizeRecommendation,
			parsed.SourcePath,
			fmt.Sprintf("skill source is %d lines; keep skill instructions under %d lines when possible", parsed.LineCount, MaxRecommendedSkillLines),
		))
	}
	sortFindings(findings)
	return findings
}
