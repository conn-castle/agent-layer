package projection

import (
	"sort"
	"strings"

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
//
// Claude matches a rule's server part exactly against the server name in its
// tool names, which it normalizes first, so the rule must use that spelling.
func ClaudeMCPRule(serverID string) string {
	return "mcp__" + claudeMCPServerName(serverID) + "__*"
}

// grokMCPRule renders one enabled MCP server as a Grok permission rule. Grok
// accepts Claude's rule syntax, but its server-name normalization is
// unconfirmed, so the configured ID is used verbatim.
func grokMCPRule(serverID string) string {
	return "mcp__" + serverID + "__*"
}

// claudeMCPServerName returns the server name Claude Code uses in MCP tool
// names: each UTF-16 code unit outside [A-Za-z0-9_-] becomes "_". This mirrors
// Claude Code's own normalizer, which runs a non-Unicode regular expression and
// therefore replaces a supplementary-plane rune twice. Its extra collapsing for
// "claude.ai " connector names is omitted; it targets claude.ai connectors.
func claudeMCPServerName(serverID string) string {
	var builder strings.Builder
	for _, r := range serverID {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			builder.WriteRune(r)
		case r > 0xFFFF:
			builder.WriteString("__")
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String()
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
	return allowRules(cfg, commandsAllow, enabledServerIDs, ClaudeMCPRule)
}

// GrokAllowRules resolves approvals into the ordered Grok allow rules. Grok
// shares Claude's command rule syntax but renders MCP rules with grokMCPRule.
func GrokAllowRules(cfg config.Config, commandsAllow []string, enabledServerIDs []string) []string {
	return allowRules(cfg, commandsAllow, enabledServerIDs, grokMCPRule)
}

func allowRules(cfg config.Config, commandsAllow []string, enabledServerIDs []string, mcpRule func(string) string) []string {
	approvals := BuildApprovals(cfg, commandsAllow)
	var rules []string

	if approvals.AllowCommands {
		for _, command := range approvals.Commands {
			rules = append(rules, ClaudeCommandRule(command))
		}
	}

	if approvals.AllowMCP {
		rules = append(rules, MCPRules(enabledServerIDs, mcpRule)...)
	}

	return rules
}

// MCPRules renders one rule per enabled server in sorted ID order and drops a
// rendered rule that repeats an earlier one, as ClaudeMCPRule does for distinct
// IDs such as "a.b" and "a_b".
func MCPRules(enabledServerIDs []string, render func(string) string) []string {
	ids := append([]string(nil), enabledServerIDs...)
	sort.Strings(ids)
	seen := make(map[string]bool, len(ids))
	var rules []string
	for _, id := range ids {
		rule := render(id)
		if seen[rule] {
			continue
		}
		seen[rule] = true
		rules = append(rules, rule)
	}
	return rules
}
