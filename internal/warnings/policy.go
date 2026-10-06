package warnings

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/envref"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// CheckPolicy returns static policy warnings that do not require network calls.
func CheckPolicy(project *config.ProjectConfig) []Warning {
	if project == nil {
		return nil
	}

	results := make([]Warning, 0)

	if agentSpecificWarning := codexAgentSpecificOverrideWarning(project.Root, project.Config.Agents.Codex.AgentSpecific); agentSpecificWarning != nil {
		results = append(results, *agentSpecificWarning)
	}
	if agentSpecificWarning := claudeAgentSpecificOverrideWarning(project.Config.Agents.Claude.AgentSpecific); agentSpecificWarning != nil {
		results = append(results, *agentSpecificWarning)
	}
	if agentSpecificWarning := antigravityAgentSpecificOverrideWarning(project.Config.Agents.Antigravity.AgentSpecific); agentSpecificWarning != nil {
		results = append(results, *agentSpecificWarning)
	}
	if w := claudeReasoningEffortUnknownWarning(project.Config.Agents.Claude.ReasoningEffort); w != nil {
		results = append(results, *w)
	}

	for _, server := range project.Config.MCP.Servers {
		if server.Enabled == nil || !*server.Enabled {
			continue
		}

		if detail, ok := findSecretInURL(server.URL); ok {
			results = append(results, Warning{
				Code:     CodePolicySecretInURL,
				Subject:  server.ID,
				Message:  messages.WarningsPolicySecretInURL,
				Fix:      messages.WarningsPolicySecretInURLFix,
				Details:  []string{detail},
				Source:   SourceInternal,
				Severity: SeverityCritical,
			})
		}

		if isClientTargeted(server.Clients, "codex") && isEnabled(project.Config.Agents.Codex.Enabled) {
			if detail, ok := findUnsupportedCodexHeaderForm(server.Headers); ok {
				results = append(results, Warning{
					Code:     CodePolicyCodexHeaderForm,
					Subject:  server.ID,
					Message:  messages.WarningsPolicyCodexHeaderForm,
					Fix:      messages.WarningsPolicyCodexHeaderFormFix,
					Details:  []string{detail},
					Source:   SourceInternal,
					Severity: SeverityWarning,
				})
			}
		}

	}

	return dedupePolicyWarnings(results)
}

func claudeReasoningEffortUnknownWarning(effort string) *Warning {
	trimmed := strings.TrimSpace(effort)
	known := config.FieldOptionValues(config.ClaudeReasoningEffortFieldKey)
	if trimmed == "" || slices.Contains(known, trimmed) {
		return nil
	}
	return &Warning{
		Code:     CodePolicyClaudeReasoningUnknown,
		Subject:  "agents.claude.reasoning_effort",
		Message:  fmt.Sprintf(messages.WarningsPolicyClaudeReasoningUnknownFmt, trimmed, strings.Join(known, ", ")),
		Fix:      messages.WarningsPolicyClaudeReasoningUnknownFix,
		Source:   SourceInternal,
		Severity: SeverityWarning,
	}
}

// codexAgentSpecificOverrideWarning reports when codex agent-specific config overrides
// managed keys. The `projects` key only collides when the user's map contains the
// managed exact-path entry (the resolved repo root); other paths coexist without override.
func codexAgentSpecificOverrideWarning(repoRoot string, agentSpecific map[string]any) *Warning {
	var keys []string
	for _, key := range config.CodexManagedTopLevelKeys() {
		value, present := agentSpecific[key]
		if !present {
			continue
		}
		if key == config.CodexProjectsKey && !codexProjectsCollides(value, repoRoot) {
			continue
		}
		keys = append(keys, key)
	}
	return agentSpecificOverrideWarning("codex", keys)
}

// codexProjectsCollides reports whether the user's agent_specific.projects value
// actually overrides the managed exact-path trust entry for the current repo root.
func codexProjectsCollides(value any, repoRoot string) bool {
	projectsMap, ok := value.(map[string]any)
	if !ok {
		// Non-map projects fully replaces the managed entry at sync time.
		return true
	}
	if strings.TrimSpace(repoRoot) == "" {
		return false
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return false
	}
	_, ok = projectsMap[absRoot]
	return ok
}

func claudeAgentSpecificOverrideWarning(agentSpecific map[string]any) *Warning {
	var keys []string
	if _, ok := agentSpecific["effortLevel"]; ok {
		keys = append(keys, "effortLevel")
	}
	if key, ok := permissionsAllowOverride(agentSpecific); ok {
		keys = append(keys, key)
	}
	return agentSpecificOverrideWarning("claude", keys)
}

// antigravityAgentSpecificOverrideWarning shares the Claude permissions check
// because .agy/antigravity-cli/settings.json shares Claude's permissions shape.
func antigravityAgentSpecificOverrideWarning(agentSpecific map[string]any) *Warning {
	var keys []string
	if key, ok := permissionsAllowOverride(agentSpecific); ok {
		keys = append(keys, key)
	}
	return agentSpecificOverrideWarning("antigravity", keys)
}

// permissionsAllowOverride returns the overridden key when agent-specific
// config collides with the managed `permissions.allow`: a non-map `permissions`
// replaces it outright, while a user `permissions.deny` is additive.
func permissionsAllowOverride(agentSpecific map[string]any) (string, bool) {
	permissions, ok := agentSpecific[permissionsKey]
	if !ok {
		return "", false
	}
	permissionsMap, ok := permissions.(map[string]any)
	if !ok {
		return permissionsKey, true
	}
	if _, ok := permissionsMap["allow"]; ok {
		return permissionsKey + ".allow", true
	}
	return "", false
}

// agentSpecificOverrideWarning reports the overridden managed keys for client,
// or nil when there are none.
func agentSpecificOverrideWarning(client string, keys []string) *Warning {
	if len(keys) == 0 {
		return nil
	}
	slices.Sort(keys)
	return &Warning{
		Code:     CodePolicyAgentSpecificOverrides,
		Subject:  "agents." + client + ".agent_specific",
		Message:  fmt.Sprintf(messages.WarningsPolicyAgentSpecificOverridesFmt, client),
		Fix:      messages.WarningsPolicyAgentSpecificOverridesFix,
		Details:  []string{fmt.Sprintf("overridden keys: %s", strings.Join(keys, ", "))},
		Source:   SourceInternal,
		Severity: SeverityWarning,
	}
}

func isEnabled(enabled *bool) bool {
	return enabled != nil && *enabled
}

func isClientTargeted(clients []string, target string) bool { //nolint:unparam // target is intentionally a parameter for readability and future extensibility
	if len(clients) == 0 {
		return true
	}
	return slices.Contains(clients, target)
}

// findSecretInURL scans the raw URL text rather than parsing it, because
// url.Parse rejects a `${...}` placeholder in the host or userinfo and a parse
// failure would otherwise hide a literal secret elsewhere in the same URL.
func findSecretInURL(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	if hasLiteralUserinfo(trimmed) {
		return "URL contains inline userinfo credentials", true
	}
	if key, found := envref.LiteralSecretQueryKey(trimmed); found {
		return fmt.Sprintf("query parameter %q contains a literal secret-like value", key), true
	}
	return "", false
}

// hasLiteralUserinfo reports whether the URL authority carries a literal
// password, or a literal username without a placeholder password beside it.
func hasLiteralUserinfo(rawURL string) bool {
	rest, ok := strings.CutPrefix(rawURL, "//")
	if !ok {
		_, rest, ok = strings.Cut(rawURL, "://")
		if !ok {
			return false
		}
	}
	authority := rest
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		authority = rest[:end]
	}
	// Userinfo ends at the last "@" in the authority, so a literal "@" inside a
	// password is not mistaken for the host separator.
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return false
	}
	username, password, _ := strings.Cut(authority[:at], ":")
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if password != "" {
		return !envref.IsEntirelyPlaceholders(password)
	}
	return username != "" && !envref.IsEntirelyPlaceholders(username)
}

func findUnsupportedCodexHeaderForm(headers map[string]string) (string, bool) {
	if len(headers) == 0 {
		return "", false
	}
	for key, value := range headers {
		if strings.EqualFold(key, "Authorization") {
			if isLiteralHeaderValue(value) || isExactEnvPlaceholder(value) || isBearerEnvPlaceholder(value) {
				continue
			}
			return fmt.Sprintf("authorization header value %q is unsupported for codex projection", value), true
		}
		if isLiteralHeaderValue(value) || isExactEnvPlaceholder(value) {
			continue
		}
		return fmt.Sprintf("header %q value %q is unsupported for codex projection", key, value), true
	}
	return "", false
}

func isLiteralHeaderValue(value string) bool {
	return !hasEnvPlaceholder(value)
}

func isExactEnvPlaceholder(value string) bool {
	names := config.ExtractEnvVarNames(value)
	if len(names) != 1 {
		return false
	}
	return value == fmt.Sprintf("${%s}", names[0])
}

func isBearerEnvPlaceholder(value string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(strings.ToLower(value), strings.ToLower(prefix)) {
		return false
	}
	token := strings.TrimSpace(value[len(prefix):])
	return isExactEnvPlaceholder(token)
}

func hasEnvPlaceholder(value string) bool {
	return len(config.ExtractEnvVarNames(value)) > 0
}

func dedupePolicyWarnings(items []Warning) []Warning {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]Warning, 0, len(items))
	for _, item := range items {
		key := item.Code + "|" + item.Subject + "|" + item.Message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
