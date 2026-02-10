package install

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const (
	gitignoreStart = "# >>> agent-layer"
	gitignoreEnd   = "# <<< agent-layer"
)

const gitignoreHashPrefix = "# Template hash: "
const (
	gitignoreHeaderLine1 = "# Managed by Agent Layer. To customize, edit .agent-layer/gitignore.block"
	gitignoreHeaderLine2 = "# and re-run `al sync` to apply changes."
)

// wrapGitignoreBlock wraps content with agent-layer markers.
// content is the normalized block content without markers; returns the wrapped block.
func wrapGitignoreBlock(content string) string {
	content = strings.TrimRight(content, "\n")
	return gitignoreStart + "\n" + content + "\n" + gitignoreEnd + "\n"
}

// renderGitignoreBlock inserts a hash line and managed header into a gitignore block.
// block is the normalized template block (without markers); returns the rendered block.
func renderGitignoreBlock(block string) string {
	block = normalizeGitignoreBlock(block)
	hashLine := gitignoreHashPrefix + gitignoreBlockHash(block)
	lines := splitLines(block)
	out := make([]string, 0, len(lines)+4)
	out = append(out, hashLine, gitignoreHeaderLine1, gitignoreHeaderLine2, "")
	out = append(out, lines...)
	return strings.Join(out, "\n") + "\n"
}

// normalizeGitignoreBlock normalizes line endings and ensures a trailing newline.
// block is the raw template content; returns the normalized block.
func normalizeGitignoreBlock(block string) string {
	block = strings.ReplaceAll(block, "\r\n", "\n")
	block = strings.ReplaceAll(block, "\r", "\n")
	return strings.TrimRight(block, "\n") + "\n"
}

// gitignoreBlockHash returns the content hash for a gitignore block.
// block is the normalized block content; returns the hash string.
func gitignoreBlockHash(block string) string {
	sum := sha256.Sum256([]byte(block))
	return hex.EncodeToString(sum[:])
}

// gitignoreBlockMatchesHash reports whether the embedded hash matches the block content.
// block is the rendered block; returns true when the hash matches.
func gitignoreBlockMatchesHash(block string) bool {
	hash, stripped := stripGitignoreHash(block)
	if hash == "" {
		return false
	}
	return gitignoreBlockHash(stripped) == hash
}

// stripGitignoreHash removes the hash line and returns the hash and remaining block content.
// block is the rendered block; returns the hash and stripped block.
func stripGitignoreHash(block string) (string, string) {
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	var hash string
	remaining := make([]string, 0, len(lines))
	for _, line := range lines {
		if hash == "" && strings.HasPrefix(line, gitignoreHashPrefix) {
			hash = strings.TrimSpace(strings.TrimPrefix(line, gitignoreHashPrefix))
			continue
		}
		remaining = append(remaining, line)
	}
	return hash, strings.Join(remaining, "\n") + "\n"
}

// ValidateGitignoreBlock normalizes and validates a gitignore block template.
// It returns the normalized block or an error if managed markers or a template hash are present.
func ValidateGitignoreBlock(block string, blockPath string) (string, error) {
	normalized := normalizeGitignoreBlock(block)
	if containsManagedGitignoreMarkers(normalized) {
		return "", fmt.Errorf(messages.InstallInvalidGitignoreBlockFmt, blockPath)
	}
	return normalized, nil
}

// updateGitignoreContent replaces or appends the managed block in a .gitignore file.
// content is the existing file content; block is the normalized block; returns updated content.
func updateGitignoreContent(content string, block string) string {
	lines := splitLines(content)
	blockLines := splitLines(block)

	start, end := findGitignoreBlock(lines)
	if start == -1 || end == -1 || end < start {
		if content == "" {
			return strings.Join(blockLines, "\n") + "\n"
		}
		separator := ""
		if !strings.HasSuffix(content, "\n") {
			separator = "\n"
		}
		return content + separator + strings.Join(blockLines, "\n") + "\n"
	}

	pre := append([]string{}, lines[:start]...)
	post := append([]string{}, lines[end+1:]...)
	blankCount := countLeadingBlankLines(post)
	post = trimLeadingBlankLines(post)

	pre = append(pre, blockLines...)
	updated := pre
	if len(post) > 0 {
		if blankCount > 1 {
			blankCount = 1
		}
		for i := 0; i < blankCount; i++ {
			updated = append(updated, "")
		}
		updated = append(updated, post...)
	}

	return strings.Join(updated, "\n") + "\n"
}

// splitLines normalizes line endings and splits content into lines.
// input is the raw text; returns normalized lines, preserving at most one trailing blank line.
func splitLines(input string) []string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	hasTrailingBlank := strings.HasSuffix(input, "\n\n")
	input = strings.TrimRight(input, "\n")
	if input == "" {
		if hasTrailingBlank {
			return []string{""}
		}
		return []string{}
	}
	lines := strings.Split(input, "\n")
	if hasTrailingBlank {
		lines = append(lines, "")
	}
	return lines
}

// containsManagedGitignoreMarkers reports whether the block includes managed markers or hash lines.
// block is the normalized template block; returns true when managed markers are present.
func containsManagedGitignoreMarkers(block string) bool {
	for _, line := range splitLines(block) {
		trimmed := strings.TrimSpace(line)
		if trimmed == gitignoreStart || trimmed == gitignoreEnd {
			return true
		}
		if strings.HasPrefix(trimmed, gitignoreHashPrefix) {
			return true
		}
	}
	return false
}

// findGitignoreBlock returns the start and end indices of the managed block.
// lines is the .gitignore content split into lines; returns -1 values when missing.
func findGitignoreBlock(lines []string) (int, int) {
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == gitignoreStart {
			start = i
			break
		}
	}
	if start == -1 {
		return -1, -1
	}
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == gitignoreEnd {
			return start, i
		}
	}
	return start, -1
}

// trimLeadingBlankLines removes leading blank lines from input.
// lines is the list to trim; returns the remaining lines.
func trimLeadingBlankLines(lines []string) []string {
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != "" {
			break
		}
		i++
	}
	return lines[i:]
}

// countLeadingBlankLines returns the number of leading blank lines in input.
func countLeadingBlankLines(lines []string) int {
	count := 0
	for count < len(lines) {
		if strings.TrimSpace(lines[count]) != "" {
			break
		}
		count++
	}
	return count
}
