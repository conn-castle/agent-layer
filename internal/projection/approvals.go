package projection

import "github.com/conn-castle/agent-layer/internal/config"

// Approval mode constants.
const (
	approvalModeAll      = "all"
	approvalModeCommands = "commands"
	approvalModeMCP      = "mcp"
)

// Approvals captures the resolved approvals policy and allowlist.
type Approvals struct {
	AllowCommands bool
	AllowMCP      bool
	Commands      []string
}

// BuildApprovals resolves approvals.mode into per-feature flags.
func BuildApprovals(cfg config.Config, commands []string) Approvals {
	allowCommands := cfg.Approvals.Mode == approvalModeAll || cfg.Approvals.Mode == approvalModeCommands
	allowMCP := cfg.Approvals.Mode == approvalModeAll || cfg.Approvals.Mode == approvalModeMCP

	return Approvals{
		AllowCommands: allowCommands,
		AllowMCP:      allowMCP,
		Commands:      commands,
	}
}
