package projection

import "github.com/conn-castle/agent-layer/internal/config"

// Approval mode constants.
const (
	approvalModeAll      = "all"
	approvalModeCommands = "commands"
	approvalModeMCP      = "mcp"
	approvalModeYOLO     = "yolo"
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
	allowCommands := mode == approvalModeAll || mode == approvalModeCommands || mode == approvalModeYOLO
	allowMCP := mode == approvalModeAll || mode == approvalModeMCP || mode == approvalModeYOLO

	return Approvals{
		AllowCommands: allowCommands,
		AllowMCP:      allowMCP,
		Commands:      commands,
	}
}
