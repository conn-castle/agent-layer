package warnings

import (
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

var secretLikeQueryKeys = []string{
	"token",
	"secret",
	"password",
	"passwd",
	"api_key",
	"apikey",
	"access_token",
	"access_key",
	"auth",
}

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
	known := config.FieldOptionValues("agents.claude.reasoning_effort")
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
	if len(agentSpecific) == 0 {
		return nil
	}
	var keys []string
	for key := range config.CodexReservedKeys {
		value, present := agentSpecific[key]
		if !present {
			continue
		}
		if key == "projects" && !codexProjectsCollides(value, repoRoot) {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	slices.Sort(keys)
	return &Warning{
		Code:     CodePolicyAgentSpecificOverrides,
		Subject:  "agents.codex.agent_specific",
		Message:  fmt.Sprintf(messages.WarningsPolicyAgentSpecificOverridesFmt, "codex"),
		Fix:      messages.WarningsPolicyAgentSpecificOverridesFix,
		Details:  []string{fmt.Sprintf("overridden keys: %s", strings.Join(keys, ", "))},
		Source:   SourceInternal,
		Severity: SeverityWarning,
	}
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
	if len(agentSpecific) == 0 {
		return nil
	}
	var keys []string
	if _, ok := agentSpecific["effortLevel"]; ok {
		keys = append(keys, "effortLevel")
	}
	if permissions, ok := agentSpecific["permissions"]; ok {
		permissionsMap, mapOK := permissions.(map[string]any)
		if !mapOK {
			keys = append(keys, "permissions")
		} else if _, ok := permissionsMap["allow"]; ok {
			keys = append(keys, "permissions.allow")
		}
	}
	if len(keys) == 0 {
		return nil
	}
	slices.Sort(keys)
	return &Warning{
		Code:     CodePolicyAgentSpecificOverrides,
		Subject:  "agents.claude.agent_specific",
		Message:  fmt.Sprintf(messages.WarningsPolicyAgentSpecificOverridesFmt, "claude"),
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

func isExplicitClientTargeted(clients []string, target string) bool {
	if len(clients) == 0 {
		return false
	}
	return slices.Contains(clients, target)
}

func findSecretInURL(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	if parsed.User != nil {
		username := strings.TrimSpace(parsed.User.Username())
		password, hasPassword := parsed.User.Password()
		if username != "" || (hasPassword && strings.TrimSpace(password) != "") {
			return "URL contains inline userinfo credentials", true
		}
	}

	query := parsed.Query()
	for key, values := range query {
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		if !looksLikeSecretQueryKey(lowerKey) {
			continue
		}
		for _, value := range values {
			if strings.TrimSpace(value) == "" {
				continue
			}
			if hasEnvPlaceholder(value) {
				continue
			}
			return fmt.Sprintf("query parameter %q contains a literal secret-like value", key), true
		}
	}

	return "", false
}

func looksLikeSecretQueryKey(key string) bool {
	for _, candidate := range secretLikeQueryKeys {
		if strings.Contains(key, candidate) {
			return true
		}
	}
	return false
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
