package wizard

import (
	"strings"
)

// tomlStringState tracks the parser position relative to TOML string literals.
type tomlStringState int

const (
	tomlStateNone tomlStringState = iota
	tomlStateBasic
	tomlStateLiteral
	tomlStateMultiBasic
	tomlStateMultiLiteral
)

// ScanTomlLineForComment scans a TOML line and returns the position of any inline comment
// (or -1 if none) along with the next parser state for multiline string tracking.
// state is the current parser state from the previous line; line is the TOML line to scan.
func ScanTomlLineForComment(line string, state tomlStringState) (commentPos int, nextState tomlStringState) {
	i := 0
	for i < len(line) {
		ch := line[i]

		// Handle escape sequences in basic strings
		if state == tomlStateBasic || state == tomlStateMultiBasic {
			if ch == '\\' && i+1 < len(line) {
				i += 2
				continue
			}
		}

		switch state {
		case tomlStateNone:
			if ch == '#' {
				return i, state
			}
			if ch == '"' {
				if len(line) > i+2 && line[i:i+3] == `"""` {
					state = tomlStateMultiBasic
					i += 3
					continue
				}
				state = tomlStateBasic
			} else if ch == '\'' {
				if len(line) > i+2 && line[i:i+3] == `'''` {
					state = tomlStateMultiLiteral
					i += 3
					continue
				}
				state = tomlStateLiteral
			}

		case tomlStateBasic:
			if ch == '"' {
				state = tomlStateNone
			}

		case tomlStateLiteral:
			if ch == '\'' {
				state = tomlStateNone
			}

		case tomlStateMultiBasic:
			if ch == '"' && len(line) > i+2 && line[i:i+3] == `"""` {
				state = tomlStateNone
				i += 3
				continue
			}

		case tomlStateMultiLiteral:
			if ch == '\'' && len(line) > i+2 && line[i:i+3] == `'''` {
				state = tomlStateNone
				i += 3
				continue
			}
		}
		i++
	}
	return -1, state
}

// IsTomlStateInMultiline returns true if the state indicates we're inside a multiline string.
func IsTomlStateInMultiline(state tomlStringState) bool {
	return state == tomlStateMultiBasic || state == tomlStateMultiLiteral
}

// inlineCommentForLine extracts a TOML inline comment on a specific line, tracking multiline strings.
// lines is the full TOML content split by line; lineIndex is the target line (0-based).
func inlineCommentForLine(lines []string, lineIndex int) string {
	if lineIndex < 0 || lineIndex >= len(lines) {
		return ""
	}
	state := tomlStateNone
	for i, line := range lines {
		commentPos, nextState := ScanTomlLineForComment(line, state)
		if i == lineIndex && commentPos >= 0 {
			return strings.TrimSpace(line[commentPos+1:])
		}
		state = nextState
	}
	return ""
}

// commentForLine collects contiguous leading comment lines and any inline comment on a line.
// lines is the original content split by line; lineIndex is the 0-based line position.
func commentForLine(lines []string, lineIndex int) string {
	if lineIndex < 0 || lineIndex >= len(lines) {
		return ""
	}
	var commentLines []string
	for i := lineIndex - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			break
		}
		if !strings.HasPrefix(trimmed, "#") {
			break
		}
		commentLines = append(commentLines, strings.TrimSpace(strings.TrimPrefix(trimmed, "#")))
	}
	for i, j := 0, len(commentLines)-1; i < j; i, j = i+1, j-1 {
		commentLines[i], commentLines[j] = commentLines[j], commentLines[i]
	}
	if inline := inlineCommentForLine(lines, lineIndex); inline != "" {
		commentLines = append(commentLines, inline)
	}
	if len(commentLines) == 0 {
		return ""
	}
	return strings.Join(commentLines, "\n")
}
