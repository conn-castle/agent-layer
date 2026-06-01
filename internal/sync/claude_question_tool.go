package sync

import "github.com/conn-castle/agent-layer/internal/config"

// askUserQuestionTool is the Claude Code tool blocked when
// agents.claude.disable_question_tool is true.
const askUserQuestionTool = "AskUserQuestion"

// askUserQuestionHookCommand is the PreToolUse command that rejects the tool.
// The hook is the enforcement mechanism under YOLO/bypassPermissions, where
// permissions.deny is skipped.
const askUserQuestionHookCommand = "echo 'BLOCKED: The AskUserQuestion tool is banned.' >&2; exit 2"

// isQuestionToolDisabled reports whether the typed flag opts into blocking the
// AskUserQuestion tool. nil and false both mean "not disabled".
func isQuestionToolDisabled(claude config.ClaudeConfig) bool {
	return claude.DisableQuestionTool != nil && *claude.DisableQuestionTool
}

// injectAskUserQuestionBlock unions "AskUserQuestion" into permissions.deny and
// appends the AskUserQuestion PreToolUse hook in the generated Claude settings,
// preserving any user-supplied deny / PreToolUse entries already merged in.
//
// It runs after mergeAgentSpecificSettings so user agent_specific entries are
// present and get unioned rather than replaced. It is idempotent: the deny is
// deduped by string and the hook by matcher, so repeated sync does not grow the
// arrays. The hook is always added (when absent) regardless of approvals.mode,
// because permissions.deny is ignored under YOLO/bypassPermissions while
// PreToolUse hooks always fire.
func injectAskUserQuestionBlock(settings map[string]any) {
	permissions, ok := settings["permissions"].(map[string]any)
	if !ok {
		permissions = make(map[string]any)
		settings["permissions"] = permissions
	}
	permissions["deny"] = unionStringIntoList(permissions["deny"], askUserQuestionTool)

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		hooks = make(map[string]any)
		settings["hooks"] = hooks
	}
	hooks["PreToolUse"] = appendAskUserQuestionHook(hooks["PreToolUse"])
}

// unionStringIntoList returns existing with want appended unless it is already
// present. It accepts the []any form (TOML-decoded agent_specific) and the
// []string form (managed lists), comparing element values as strings, and
// returns a []any so the result interoperates with user-supplied lists.
func unionStringIntoList(existing any, want string) []any {
	var out []any
	present := false
	switch values := existing.(type) {
	case []any:
		for _, v := range values {
			out = append(out, v)
			if s, ok := v.(string); ok && s == want {
				present = true
			}
		}
	case []string:
		for _, s := range values {
			out = append(out, s)
			if s == want {
				present = true
			}
		}
	}
	if present {
		return out
	}
	return append(out, want)
}

// appendAskUserQuestionHook returns existing with the AskUserQuestion PreToolUse
// matcher entry appended unless an entry with that matcher already exists. It
// accepts the []any form produced by the TOML decoder.
func appendAskUserQuestionHook(existing any) []any {
	var out []any
	if values, ok := existing.([]any); ok {
		out = append(out, values...)
		for _, entry := range values {
			if m, ok := entry.(map[string]any); ok && m["matcher"] == askUserQuestionTool {
				return out
			}
		}
	}
	return append(out, map[string]any{
		"matcher": askUserQuestionTool,
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": askUserQuestionHookCommand,
			},
		},
	})
}
