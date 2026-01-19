package wizard

// AgentID constants matching config keys
const (
	AgentGemini      = "gemini"
	AgentClaude      = "claude"
	AgentCodex       = "codex"
	AgentVSCode      = "vscode"
	AgentAntigravity = "antigravity"
)

// SupportedAgents is the list of agents the wizard can configure.
var SupportedAgents = []string{
	AgentGemini,
	AgentClaude,
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
)

// ApprovalModes lists available approval modes.
var ApprovalModes = []string{
	ApprovalAll,
	ApprovalMCP,
	ApprovalCommands,
	ApprovalNone,
}

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

// DefaultMCPServer defines a known default server.
type DefaultMCPServer struct {
	ID          string
	RequiredEnv string
}

// KnownDefaultMCPServers lists the servers the wizard manages.
var KnownDefaultMCPServers = []DefaultMCPServer{
	{ID: "github", RequiredEnv: "GITHUB_PERSONAL_ACCESS_TOKEN"},
	{ID: "context7", RequiredEnv: "CONTEXT7_API_KEY"},
	{ID: "tavily", RequiredEnv: "TAVILY_API_KEY"},
}
