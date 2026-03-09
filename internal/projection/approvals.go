package projection

import "github.com/conn-castle/agent-layer/internal/config"

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
