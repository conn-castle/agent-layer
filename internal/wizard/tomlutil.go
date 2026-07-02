package wizard

import "github.com/conn-castle/agent-layer/internal/tomlpatch"

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
	commentPos, next := tomlpatch.ScanLineForComment(line, tomlpatch.StringState(state))
	return commentPos, tomlStringState(next)
}

// inlineCommentForLine extracts a TOML inline comment on a specific line, tracking multiline strings.
// lines is the full TOML content split by line; lineIndex is the target line (0-based).
func inlineCommentForLine(lines []string, lineIndex int) string {
	return tomlpatch.InlineCommentForLine(lines, lineIndex)
}

// commentForLine collects contiguous leading comment lines and any inline comment on a line.
// lines is the original content split by line; lineIndex is the 0-based line position.
func commentForLine(lines []string, lineIndex int) string {
	return tomlpatch.CommentForLine(lines, lineIndex)
}
