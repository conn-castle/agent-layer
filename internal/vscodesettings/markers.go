// Package vscodesettings identifies Agent Layer's root-level block in VS Code settings.
package vscodesettings

import "strings"

// ManagedBlockStart and ManagedBlockEnd delimit Agent Layer's settings block.
const (
	ManagedBlockStart = "// >>> agent-layer"
	ManagedBlockEnd   = "// <<< agent-layer"
)

// ManagedMarkerLines returns the indices of complete marker lines directly in
// the root object of normalized JSONC lines. Marker text inside strings, block
// comments, nested objects or arrays, and comments outside the root object does
// not count as a managed boundary. Callers still validate the document grammar.
func ManagedMarkerLines(lines []string) (starts, ends []int) {
	depth := 0
	blockComment, quoted, escaped := false, false, false
	for index, line := range lines {
		if depth == 1 && !blockComment && !quoted {
			switch strings.TrimSpace(line) {
			case ManagedBlockStart:
				starts = append(starts, index)
			case ManagedBlockEnd:
				ends = append(ends, index)
			}
		}
	scan:
		for i := 0; i < len(line); i++ {
			ch := line[i]
			switch {
			case blockComment:
				if ch == '*' && i+1 < len(line) && line[i+1] == '/' {
					blockComment = false
					i++
				}
			case quoted:
				switch {
				case escaped:
					escaped = false
				case ch == '\\':
					escaped = true
				case ch == '"':
					quoted = false
				}
			case ch == '/' && i+1 < len(line) && line[i+1] == '/':
				break scan
			case ch == '/' && i+1 < len(line) && line[i+1] == '*':
				blockComment = true
				i++
			case ch == '"':
				quoted = true
			case ch == '{' || ch == '[':
				depth++
			case ch == '}' || ch == ']':
				depth--
			}
		}
	}
	return starts, ends
}
