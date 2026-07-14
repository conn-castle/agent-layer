package config

// ProviderPassthrough stores raw provider-native config from an `agent_specific`
// TOML table. It is an escape hatch for settings Agent Layer does not model;
// Agent Layer-owned settings belong in typed fields.
type ProviderPassthrough = map[string]any

const (
	// CodexApprovalPolicyKey is the top-level Codex config key for approval mode.
	CodexApprovalPolicyKey = "approval_policy"
	// CodexMCPServersKey is the top-level Codex config key for MCP servers.
	CodexMCPServersKey = "mcp_servers"
	// CodexModelKey is the top-level Codex config key for model selection.
	CodexModelKey = "model"
	// CodexReasoningEffortKey is the top-level Codex config key for reasoning effort.
	CodexReasoningEffortKey = "model_reasoning_effort"
	// CodexProjectsKey is the top-level Codex config key for trusted projects.
	CodexProjectsKey = "projects"
	// CodexSandboxModeKey is the top-level Codex config key for sandbox mode.
	CodexSandboxModeKey = "sandbox_mode"
	// CodexWebSearchKey is the top-level Codex config key for web search.
	CodexWebSearchKey = "web_search"
	// CodexFeatureAppsKey is the Codex [features] key controlling built-in apps.
	CodexFeatureAppsKey = "apps"
	// CodexFeaturePluginsKey is the Codex [features] key controlling plugins.
	CodexFeaturePluginsKey = "plugins"
)

var codexBrowserFeatureKeys = []string{browserUseFeatureKey, "in_app_browser", "computer_use"}

// CodexManagedTopLevelKeys returns top-level .codex/config.toml keys managed by
// Agent Layer. The returned slice is caller-owned.
func CodexManagedTopLevelKeys() []string {
	return []string{
		CodexApprovalPolicyKey,
		CodexMCPServersKey,
		CodexModelKey,
		CodexReasoningEffortKey,
		CodexProjectsKey,
		CodexSandboxModeKey,
		CodexWebSearchKey,
	}
}

// CodexBrowserFeatureKeys returns the Codex [features] keys controlled by the
// browser/computer-use wizard toggle.
func CodexBrowserFeatureKeys() []string {
	return append([]string(nil), codexBrowserFeatureKeys...)
}

// CodexKnownManagedFeatureKeys returns Codex [features] keys Agent Layer knows
// how to remove when absent from the current projection.
func CodexKnownManagedFeatureKeys() []string {
	keys := make([]string, 0, 2+len(codexBrowserFeatureKeys))
	keys = append(keys, CodexFeatureAppsKey)
	keys = append(keys, CodexFeaturePluginsKey)
	keys = append(keys, codexBrowserFeatureKeys...)
	return keys
}

// HasProviderPassthroughKey returns true when passthrough defines a top-level key.
func HasProviderPassthroughKey(passthrough map[string]any, key string) bool {
	if len(passthrough) == 0 {
		return false
	}
	_, ok := passthrough[key]
	return ok
}
