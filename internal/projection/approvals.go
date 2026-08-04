package projection

import (
	"sort"

	"github.com/conn-castle/agent-layer/internal/config"
)

// Approvals captures the resolved approvals policy and allowlist.
type Approvals struct {
	AllowCommands bool
	AllowMCP      bool
	Commands      []string
}

// BuildApprovals resolves approvals.mode into per-feature flags.
func BuildApprovals(cfg config.Config, commands []string) Approvals {
	mode := cfg.Approvals.Mode
	allowCommands := mode == config.ApprovalModeAll || mode == config.ApprovalModeCommands || mode == config.ApprovalModeYOLO
	allowMCP := mode == config.ApprovalModeAll || mode == config.ApprovalModeMCP || mode == config.ApprovalModeYOLO

	return Approvals{
		AllowCommands: allowCommands,
		AllowMCP:      allowMCP,
		Commands:      commands,
	}
}

// ClaudeCommandRule renders one allowlisted command as a Claude permission rule.
func ClaudeCommandRule(pattern string) string {
	return "Bash(" + pattern + ":*)"
}

// ClaudeMCPRule renders one enabled MCP server as a Claude permission rule.
func ClaudeMCPRule(serverID string) string {
	return "mcp__" + serverID + "__*"
}

// ClaudeAllowRules resolves approvals into the ordered Claude allow rules that
// grant the configured commands and MCP servers.
//
// Claude Code applies a project's `permissions.allow` rules only after the
// workspace trust dialog is accepted, and that dialog never appears under
// `claude -p`. Headless dispatch therefore cannot rely on the generated
// settings file and must pass these rules on the command line instead, so both
// callers resolve them here rather than rendering the strings independently.
func ClaudeAllowRules(cfg config.Config, commandsAllow []string, enabledServerIDs []string) []string {
	approvals := BuildApprovals(cfg, commandsAllow)
	var rules []string

	if approvals.AllowCommands {
		for _, command := range approvals.Commands {
			rules = append(rules, ClaudeCommandRule(command))
		}
	}

	if approvals.AllowMCP {
		ids := append([]string(nil), enabledServerIDs...)
		sort.Strings(ids)
		for _, id := range ids {
			rules = append(rules, ClaudeMCPRule(id))
		}
	}

	return rules
}
