package config

import (
	"regexp"
	"strings"
)

var (
	secretAssignmentPattern = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|authorization)\s*[:=]\s*["'][^"']{8,}["']`)
	bearerPattern           = regexp.MustCompile(`(?i)bearer\s+[a-z0-9_\-\.\/+=]{8,}`)
)

// ContainsPotentialSecretLiteral reports whether content includes likely secret literals.
func ContainsPotentialSecretLiteral(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(trimmed, "${") {
			continue
		}
		if secretAssignmentPattern.MatchString(trimmed) || bearerPattern.MatchString(trimmed) {
			return true
		}
	}
	return false
}
