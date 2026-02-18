package wizard

import "github.com/conn-castle/agent-layer/internal/messages"

// AgentID constants matching config keys
const (
	AgentGemini       = "gemini"
	AgentClaude       = "claude"
	AgentClaudeVSCode = "claude-vscode"
	AgentCodex        = "codex"
	AgentVSCode       = "vscode"
	AgentAntigravity  = "antigravity"
)

// SupportedAgents is the list of agents the wizard can configure.
var SupportedAgents = []string{
	AgentGemini,
	AgentClaude,
	AgentClaudeVSCode,
	AgentCodex,
	AgentVSCode,
	AgentAntigravity,
}

// ApprovalMode constants
const (
	ApprovalAll      = "all"
	ApprovalMCP      = "mcp"
	ApprovalCommands = "commands"
	ApprovalNone     = "none"
	ApprovalYOLO     = "yolo"
)

// ApprovalModeOption describes a selectable approval mode.
// Value is the canonical config value; Description explains behavior.
type ApprovalModeOption struct {
	Value       string
	Description string
}

// ApprovalModeOptions lists available approval modes and their descriptions.
var ApprovalModeOptions = []ApprovalModeOption{
	{Value: ApprovalAll, Description: messages.WizardApprovalAllDescription},
	{Value: ApprovalMCP, Description: messages.WizardApprovalMCPDescription},
	{Value: ApprovalCommands, Description: messages.WizardApprovalCommandsDescription},
	{Value: ApprovalNone, Description: messages.WizardApprovalNoneDescription},
	{Value: ApprovalYOLO, Description: messages.WizardApprovalYOLODescription},
}

// ApprovalModes lists the canonical approval mode values.
var ApprovalModes = approvalModeValues()

// Model catalogs

// GeminiModels lists supported Gemini model values for the wizard.
var GeminiModels = []string{
	// Auto
	"auto",
	"auto-gemini-2.5",
	"auto-gemini-3",
	// Gemini 2.5
	"gemini-2.5-pro",
	"gemini-2.5-flash",
	"gemini-2.5-flash-lite",
	// Gemini 3 Preview
	"gemini-3-pro-preview",
	"gemini-3-flash-preview",
}

// ClaudeModels lists supported Claude model values for the wizard.
var ClaudeModels = []string{
	"default",
	"sonnet",
	"sonnet[1m]",
	"haiku",
	"opus",
}

// CodexModels lists supported Codex model values for the wizard.
var CodexModels = []string{
	"gpt-5.2-codex",
	"gpt-5.1-codex-mini",
	"gpt-5.1-codex-max",
	"gpt-5.1-codex",
	"gpt-5-codex",
	"gpt-5-codex-mini",
	"gpt-5.2",
	"gpt-5.1",
	"gpt-5",
}

// CodexReasoningEfforts lists supported reasoning effort values for Codex.
var CodexReasoningEfforts = []string{
	"minimal",
	"low",
	"medium",
	"high",
	"xhigh",
}
