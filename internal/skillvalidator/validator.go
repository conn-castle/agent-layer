package skillvalidator

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

var (
	utf8BOM = []byte{0xEF, 0xBB, 0xBF}
)

const (
	skillSourceScannerInitialBufferSize = 64 * 1024
	skillSourceScannerMaxTokenSize      = 8 * 1024 * 1024
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
			fmt.Sprintf("skill source is %d lines; keep skill instructions under %d lines when possible", parsed.LineCount, MaxRecommendedSkillLines),
		))
	}
	sortFindings(findings)
	return findings
}
