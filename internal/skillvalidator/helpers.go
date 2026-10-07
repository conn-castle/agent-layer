package skillvalidator

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// canonicalNameForPath returns the skill directory name for a
// <name>/SKILL.md source path.
func canonicalNameForPath(path string) string {
	return filepath.Base(filepath.Dir(path))
}

// NormalizeName applies the same Unicode normalization used when comparing
// skill names across configuration, imports, and user-managed sources.
func NormalizeName(name string) string {
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
		Code:    code,
		Path:    path,
		Message: message,
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
