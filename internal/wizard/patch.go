package wizard

import (
	"fmt"
	"strings"

	toml "github.com/pelletier/go-toml"
)

// PatchConfig applies wizard choices to TOML config content.
// content is the current config; choices holds selections; returns updated content or error.
func PatchConfig(content string, choices *Choices) (string, error) {
	configTree, err := toml.LoadBytes([]byte(content))
	if err != nil {
		return "", fmt.Errorf("parse config: %w", err)
	}
	lines := strings.Split(content, "\n")

	// 1. Approvals
	if choices.ApprovalModeTouched {
		setPathPreservingComment(configTree, lines, []string{"approvals", "mode"}, choices.ApprovalMode)
	}

	// 2. Agents
	if choices.EnabledAgentsTouched {
		for _, agent := range SupportedAgents {
			enabled := choices.EnabledAgents[agent]
			setPathPreservingComment(configTree, lines, []string{"agents", agent, "enabled"}, enabled)
		}
	}

	optionalKeys := []struct {
		path    []string
		value   string
		touched bool
	}{
		{path: []string{"agents", "gemini", "model"}, value: choices.GeminiModel, touched: choices.GeminiModelTouched},
		{path: []string{"agents", "claude", "model"}, value: choices.ClaudeModel, touched: choices.ClaudeModelTouched},
		{path: []string{"agents", "codex", "model"}, value: choices.CodexModel, touched: choices.CodexModelTouched},
		{path: []string{"agents", "codex", "reasoning_effort"}, value: choices.CodexReasoning, touched: choices.CodexReasoningTouched},
	}
	for _, item := range optionalKeys {
		if !item.touched {
			continue
		}
		if item.value == "" {
			if err := deletePath(configTree, item.path); err != nil {
				return "", err
			}
			continue
		}
		setPathPreservingComment(configTree, lines, item.path, item.value)
	}

	if (choices.EnabledMCPServersTouched || choices.RestoreMissingMCPServers) && len(choices.DefaultMCPServers) == 0 {
		return "", fmt.Errorf("default MCP servers are required to patch config")
	}

	restoredServers := map[string]bool{}
	if choices.RestoreMissingMCPServers && len(choices.MissingDefaultMCPServers) > 0 {
		if err := appendMissingDefaultMCPServers(configTree, choices.MissingDefaultMCPServers); err != nil {
			return "", err
		}
		for _, id := range choices.MissingDefaultMCPServers {
			restoredServers[id] = true
		}
	}

	// 3. MCP Servers (only default ones)
	if choices.EnabledMCPServersTouched {
		servers, err := mcpServerTrees(configTree)
		if err != nil {
			return "", err
		}
		for _, server := range choices.DefaultMCPServers {
			enabled := choices.EnabledMCPServers[server.ID]
			preserveComment := !restoredServers[server.ID]
			setMCPServerEnabled(servers, lines, server.ID, enabled, preserveComment)
		}
	}

	updated, err := configTree.ToTomlString()
	if err != nil {
		return "", fmt.Errorf("render config: %w", err)
	}
	return updated, nil
}

// setPathPreservingComment sets a value while retaining existing inline/leading comments when possible.
// tree is the parsed config; lines is the original content split by line; keys is the TOML path; value is the new value.
func setPathPreservingComment(tree *toml.Tree, lines []string, keys []string, value interface{}) {
	comment := commentForPath(tree, lines, keys)
	if comment != "" {
		tree.SetPathWithComment(keys, comment, false, value)
		return
	}
	tree.SetPath(keys, value)
}

// deletePath removes a TOML path if it exists.
// tree is the parsed config; keys is the TOML path to delete; returns an error when deletion fails.
func deletePath(tree *toml.Tree, keys []string) error {
	if !tree.HasPath(keys) {
		return nil
	}
	return tree.DeletePath(keys)
}

// setMCPServerEnabled updates an MCP server's enabled flag when the server is present.
// servers is the parsed MCP server list; lines is the original config; serverID identifies the target server.
// preserveComment controls whether inline/leading comments should be retained.
func setMCPServerEnabled(servers []*toml.Tree, lines []string, serverID string, enabled bool, preserveComment bool) {
	for _, server := range servers {
		id, ok := server.Get("id").(string)
		if !ok || id == "" {
			continue
		}
		if id == serverID {
			if preserveComment {
				setPathPreservingComment(server, lines, []string{"enabled"}, enabled)
				return
			}
			server.SetPath([]string{"enabled"}, enabled)
			return
		}
	}
}

// commentForPath returns the combined comment text for a TOML path, or empty if none found.
// tree is the parsed config; lines is the original content; keys identifies the TOML path.
func commentForPath(tree *toml.Tree, lines []string, keys []string) string {
	if !tree.HasPath(keys) {
		return ""
	}
	pos := tree.GetPositionPath(keys)
	if pos.Invalid() {
		return ""
	}
	return commentForLine(lines, pos.Line-1)
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

const (
	stateNone = iota
	stateBasic
	stateLiteral
	stateMultiBasic
	stateMultiLiteral
)

// inlineCommentForLine extracts a TOML inline comment on a specific line, tracking multiline strings.
// lines is the full TOML content split by line; lineIndex is the target line (0-based).
func inlineCommentForLine(lines []string, lineIndex int) string {
	if lineIndex < 0 || lineIndex >= len(lines) {
		return ""
	}
	state := stateNone
	for i, line := range lines {
		comment, nextState := scanLineForComment(line, state, i == lineIndex)
		state = nextState
		if i == lineIndex {
			return comment
		}
	}
	return ""
}

// scanLineForComment returns an inline comment when capture is true and updates the parser state.
// state tracks whether the parser is inside a TOML string; it persists across lines for multiline strings.
func scanLineForComment(line string, state int, capture bool) (string, int) {
	i := 0
	for i < len(line) {
		ch := line[i]

		if state == stateBasic || state == stateMultiBasic {
			if ch == '\\' {
				i += 2 // Skip escape sequence
				continue
			}
		}

		switch state {
		case stateNone:
			if ch == '#' {
				if capture {
					return strings.TrimSpace(line[i+1:]), state
				}
				return "", state
			}
			if ch == '"' {
				if strings.HasPrefix(line[i:], `"""`) {
					state = stateMultiBasic
					i += 3
					continue
				}
				state = stateBasic
			} else if ch == '\'' {
				if strings.HasPrefix(line[i:], `'''`) {
					state = stateMultiLiteral
					i += 3
					continue
				}
				state = stateLiteral
			}

		case stateBasic:
			if ch == '"' {
				state = stateNone
			}

		case stateLiteral:
			if ch == '\'' {
				state = stateNone
			}

		case stateMultiBasic:
			if ch == '"' && strings.HasPrefix(line[i:], `"""`) {
				state = stateNone
				i += 3
				continue
			}

		case stateMultiLiteral:
			if ch == '\'' && strings.HasPrefix(line[i:], `'''`) {
				state = stateNone
				i += 3
				continue
			}
		}
		i++
	}
	return "", state
}
