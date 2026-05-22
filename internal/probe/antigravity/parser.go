package antigravity

import (
	"regexp"
	"strings"
)

var (
	permissionsLoadedRE = regexp.MustCompile(`cli_setting_manager\.go:\d+\] CLI settings initialized: permissions=.*PROBEALLOWMARKER`)
	mcpConfigMigratedRE = regexp.MustCompile(`migrate\.go:\d+\] Migrating file .*antigravity-cli/mcp_config\.json to .*config/mcp_config\.json`)
	// mcpRuntimeDiscoveryRE matches a positive MCP runtime event from agy's
	// discovery log. The keyword is required to be word-bounded so messages
	// like "mcp server foo failed to register" cannot satisfy the check.
	mcpRuntimeDiscoveryRE      = regexp.MustCompile(`(?i)discovery\.go:\d+\][^\n]*\bMCP\b[^\n]*\b(registered|connected)\b`)
	workspacePermissionsReadRE = regexp.MustCompile(`(?i)(workspace permissions=|WORKSPACEALLOWMARKER|DOTANTIGRAVITYWORKSPACEMARKER)`)
)

// ParseCapabilities extracts a capability matrix from agy's cli.log and stdout.
//
// The evidence slice is intentionally ordered to follow the capability-matrix
// declaration order (PermissionsLoaded, MCPConfigMigrated, MCPRuntimeDiscovery,
// WorkspacePermissionsRead, InstructionsLoaded, SkillNamesVisible,
// MCPConfigNamesVisible, SharedSkillDedupObserved). Downstream consumers
// (probe JSON output, regression diffs) rely on this ordering.
func ParseCapabilities(logText string, stdoutText string) (CapabilityMatrix, []string) {
	var capabilities CapabilityMatrix
	var evidence []string

	if permissionsLoadedRE.MatchString(logText) {
		capabilities.PermissionsLoaded = true
		evidence = append(evidence, "cli.log: loaded antigravity-cli/settings.json permissions")
	}
	if mcpConfigMigratedRE.MatchString(logText) {
		capabilities.MCPConfigMigrated = true
		evidence = append(evidence, "cli.log: migrated antigravity-cli/mcp_config.json to config/mcp_config.json")
	}
	if mcpRuntimeDiscoveryRE.MatchString(logText) {
		capabilities.MCPRuntimeDiscovery = true
		evidence = append(evidence, "cli.log: MCP runtime discovery or registration was logged")
	}
	if workspacePermissionsReadRE.MatchString(logText) || workspacePermissionsReadRE.MatchString(stdoutText) {
		capabilities.WorkspacePermissionsRead = true
		evidence = append(evidence, "workspace permission marker observed")
	}
	if strings.Contains(stdoutText, "INSTRUCTIONMARKER88") {
		capabilities.InstructionsLoaded = true
		evidence = append(evidence, "stdout: AGENTS.md instruction marker observed")
	}
	// Note: `shared-tier-dup` is also the marker for SharedSkillDedupObserved
	// below — a change to one matcher must consider the other (tests rely on
	// the coupling).
	if strings.Contains(stdoutText, "global-only-skill") || strings.Contains(stdoutText, "shared-tier-dup") || strings.Contains(stdoutText, "probe-marker-skill") {
		capabilities.SkillNamesVisible = true
		evidence = append(evidence, "stdout: skill name marker observed")
	}
	if strings.Contains(stdoutText, "probe-mcp-antigravity-tier") || strings.Contains(stdoutText, "probe-mcp") {
		capabilities.MCPConfigNamesVisible = true
		evidence = append(evidence, "stdout: MCP config server name observed")
	}
	// Require the duplicated skill to surface at least once before claiming
	// dedup is "observed" — otherwise a regression that drops the skill
	// entirely (count=0) flips the bit for an unrelated reason.
	if strings.Contains(stdoutText, "shared-tier-dup") && strings.Count(stdoutText, "shared-tier-dup") == 1 {
		capabilities.SharedSkillDedupObserved = true
		evidence = append(evidence, "stdout: duplicate shared skill surfaced once")
	}

	return capabilities, evidence
}
