package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// errMalformedGitignoreBlock signals that the managed agent-layer block in a
// .gitignore is corrupt (an orphaned or duplicated marker, or an end marker
// before the start). It is wrapped into a user-facing message by callers.
var errMalformedGitignoreBlock = errors.New("malformed agent-layer managed block")

const (
	gitignoreStart = "# >>> agent-layer"
	gitignoreEnd   = "# <<< agent-layer"
)

const (
	// AgentLayerGitignorePattern controls whether the repo-local Agent Layer
	// configuration directory is tracked by git.
	AgentLayerGitignorePattern = "/.agent-layer/"
	// DocsAgentLayerGitignorePattern controls whether Agent Layer project memory
	// is tracked by git.
	DocsAgentLayerGitignorePattern = "/docs/agent-layer/"
)

// GitignoreTrackingSettings captures the user-owned tracking choices stored in
// the managed gitignore block.
type GitignoreTrackingSettings struct {
	TrackAgentLayerDir     bool
	TrackDocsAgentLayerDir bool
}

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

// ParseGitignoreTrackingSettings reads the two user-owned tracking choices from
// a gitignore block. A missing or commented pattern means the path is tracked.
func ParseGitignoreTrackingSettings(content string) (GitignoreTrackingSettings, error) {
	trackAgentLayer, err := gitignorePatternIsTracked(content, AgentLayerGitignorePattern)
	if err != nil {
		return GitignoreTrackingSettings{}, err
	}
	trackDocsAgentLayer, err := gitignorePatternIsTracked(content, DocsAgentLayerGitignorePattern)
	if err != nil {
		return GitignoreTrackingSettings{}, err
	}
	return GitignoreTrackingSettings{
		TrackAgentLayerDir:     trackAgentLayer,
		TrackDocsAgentLayerDir: trackDocsAgentLayer,
	}, nil
}

// ApplyGitignoreTrackingSettings updates the two tracking choices while leaving
// all unrelated template content intact.
func ApplyGitignoreTrackingSettings(content string, settings GitignoreTrackingSettings) (string, error) {
	next, err := setGitignorePatternTracked(content, AgentLayerGitignorePattern, settings.TrackAgentLayerDir)
	if err != nil {
		return "", err
	}
	return setGitignorePatternTracked(next, DocsAgentLayerGitignorePattern, settings.TrackDocsAgentLayerDir)
}

func gitignorePatternIsTracked(content string, pattern string) (bool, error) {
	tracked := true
	matches := 0
	for _, line := range strings.Split(content, "\n") {
		commented, match, err := inspectGitignorePatternLine(line, pattern)
		if err != nil {
			return false, err
		}
		if !match {
			continue
		}
		matches++
		tracked = commented
	}
	if matches > 1 {
		return false, fmt.Errorf(messages.InstallGitignoreDuplicatePatternFmt, pattern)
	}
	return tracked, nil
}

func setGitignorePatternTracked(content string, pattern string, tracked bool) (string, error) {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	matches := 0
	for i, line := range lines {
		commented, match, err := inspectGitignorePatternLine(line, pattern)
		if err != nil {
			return "", err
		}
		if !match {
			continue
		}
		matches++
		if tracked && !commented {
			lines[i] = "# " + pattern
		} else if !tracked && commented {
			lines[i] = pattern
		}
	}
	if matches > 1 {
		return "", fmt.Errorf(messages.InstallGitignoreDuplicatePatternFmt, pattern)
	}
	if matches == 0 && !tracked {
		lines = append(lines, pattern)
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func inspectGitignorePatternLine(line string, pattern string) (commented bool, match bool, err error) {
	commented, exact := gitignorePatternLineStatus(line, pattern)
	if exact {
		return commented, true, nil
	}
	if gitignorePatternHasUnsupportedInlineComment(strings.TrimSpace(line), pattern) {
		return false, false, fmt.Errorf(messages.InstallGitignoreUnsupportedInlineCommentFmt, pattern, pattern, pattern)
	}
	return false, false, nil
}

func gitignorePatternLineStatus(line string, pattern string) (bool, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == pattern {
		return false, true
	}
	if !strings.HasPrefix(trimmed, "#") {
		return false, false
	}
	commented := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
	if commented == pattern {
		return true, true
	}
	after, ok := strings.CutPrefix(commented, pattern)
	if !ok {
		return false, false
	}
	trailing := strings.TrimSpace(after)
	return true, strings.HasPrefix(trailing, "#") || strings.HasPrefix(trailing, "-")
}

func gitignorePatternHasUnsupportedInlineComment(line string, pattern string) bool {
	after, ok := strings.CutPrefix(line, pattern)
	if !ok {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(after), "#")
}

// updateGitignoreContent replaces or appends the managed block in a .gitignore file.
// content is the existing file content; block is the normalized block; returns updated content.
// It fails loud on any malformed managed block (an orphaned/duplicated marker, or an
// end marker before the start): repairing such a file by appending or by replacing only
// the first marker pair would silently delete the user's content between mispaired
// markers on the next sync. This mirrors the VS Code managed-block handling.
func updateGitignoreContent(content string, block string, path string) (string, error) {
	lines := splitLines(content)
	blockLines := splitLines(block)

	start, end, err := findGitignoreBlock(lines)
	if err != nil {
		return "", fmt.Errorf(messages.InstallGitignoreUnterminatedBlockFmt, path, gitignoreStart, gitignoreEnd)
	}
	if start == -1 && end == -1 {
		// No managed block: create or append one.
		if content == "" {
			return strings.Join(blockLines, "\n") + "\n", nil
		}
		separator := ""
		if !strings.HasSuffix(content, "\n") {
			separator = "\n"
		}
		return content + separator + strings.Join(blockLines, "\n") + "\n", nil
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

	return strings.Join(updated, "\n") + "\n", nil
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

// findGitignoreBlock returns the indices of the managed start and end markers.
// lines is the .gitignore content split into lines. It returns (start, end, nil)
// for a well-formed block (both markers present, start before end), (-1, -1, nil)
// when there is no managed block at all, and (-1, -1, err) when the markers are
// malformed — duplicated, only one present, or end before start. It never returns
// a partial-index success: a single dangling marker is an error, not a success
// with one index set. This makes callers fail loud rather than mispairing markers
// and silently deleting user content. This mirrors findVSCodeManagedBlock.
func findGitignoreBlock(lines []string) (int, int, error) {
	start := -1
	end := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == gitignoreStart {
			if start != -1 {
				return -1, -1, errMalformedGitignoreBlock
			}
			start = i
		}
		if trimmed == gitignoreEnd {
			if end != -1 {
				return -1, -1, errMalformedGitignoreBlock
			}
			end = i
		}
	}
	if start == -1 && end == -1 {
		return -1, -1, nil
	}
	if start == -1 || end == -1 || end < start {
		return -1, -1, errMalformedGitignoreBlock
	}
	return start, end, nil
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
