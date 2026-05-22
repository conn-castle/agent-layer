package config

// Approval mode constants.
const (
	ApprovalModeAll      = "all"
	ApprovalModeCommands = "commands"
	ApprovalModeMCP      = "mcp"
	ApprovalModeNone     = "none"
	ApprovalModeYOLO     = "yolo"
)

// Config is the root configuration loaded from .agent-layer/config.toml.
type Config struct {
	Approvals ApprovalsConfig `toml:"approvals"`
	Agents    AgentsConfig    `toml:"agents"`
	MCP       MCPConfig       `toml:"mcp"`
	Warnings  WarningsConfig  `toml:"warnings"`
}

// ApprovalsConfig controls auto-approval behavior per client.
type ApprovalsConfig struct {
	Mode string `toml:"mode"`
}

// AgentsConfig holds per-client enablement and model selection.
type AgentsConfig struct {
	Antigravity  EnableOnlyConfig `toml:"antigravity"`
	Claude       ClaudeConfig     `toml:"claude"`
	ClaudeVSCode EnableOnlyConfig `toml:"claude_vscode"`
	Codex        CodexConfig      `toml:"codex"`
	VSCode       EnableOnlyConfig `toml:"vscode"`
	CopilotCLI   AgentConfig      `toml:"copilot_cli"`
}

// AgentConfig is for agents that support enablement and model selection.
// ReasoningEffort is present so the TOML decoder accepts the key without
// raising an unknown-key error; the validator then provides a specific
// error message for agents that do not support reasoning effort.
type AgentConfig struct {
	Enabled         *bool  `toml:"enabled"`
	Model           string `toml:"model"`
	ReasoningEffort string `toml:"reasoning_effort"`
}

// ClaudeConfig extends AgentConfig with Claude-specific settings.
type ClaudeConfig struct {
	Enabled         *bool          `toml:"enabled"`
	Model           string         `toml:"model"`
	ReasoningEffort string         `toml:"reasoning_effort"`
	LocalConfigDir  *bool          `toml:"local_config_dir"`
	AgentSpecific   map[string]any `toml:"agent_specific"`
}

// EnableOnlyConfig is for agents that support enablement but not model selection.
type EnableOnlyConfig struct {
	Enabled *bool `toml:"enabled"`
}

// CodexConfig extends AgentConfig with Codex-specific settings.
type CodexConfig struct {
	Enabled         *bool          `toml:"enabled"`
	Model           string         `toml:"model"`
	ReasoningEffort string         `toml:"reasoning_effort"`
	AgentSpecific   map[string]any `toml:"agent_specific"`
}

// MCPConfig contains the external MCP servers configuration.
type MCPConfig struct {
	Servers []MCPServer `toml:"servers"`
}

// WarningsConfig configures optional warning thresholds. Nil fields disable their warnings.
type WarningsConfig struct {
	VersionUpdateOnSync            *bool  `toml:"version_update_on_sync"`
	NoiseMode                      string `toml:"noise_mode"`
	InstructionTokenThreshold      *int   `toml:"instruction_token_threshold"`
	MCPServerThreshold             *int   `toml:"mcp_server_threshold"`
	MCPToolsTotalThreshold         *int   `toml:"mcp_tools_total_threshold"`
	MCPServerToolsThreshold        *int   `toml:"mcp_server_tools_threshold"`
	MCPSchemaTokensTotalThreshold  *int   `toml:"mcp_schema_tokens_total_threshold"`
	MCPSchemaTokensServerThreshold *int   `toml:"mcp_schema_tokens_server_threshold"`
}

// MCPServer defines a single MCP server entry.
type MCPServer struct {
	ID            string            `toml:"id"`
	Enabled       *bool             `toml:"enabled"`
	Clients       []string          `toml:"clients"`
	Transport     string            `toml:"transport"`
	HTTPTransport string            `toml:"http_transport"`
	URL           string            `toml:"url"`
	Headers       map[string]string `toml:"headers"`
	Command       string            `toml:"command"`
	Args          []string          `toml:"args"`
	Env           map[string]string `toml:"env"`
}

// IsAgentEnabled returns true if the agent-enabled pointer is non-nil and true.
func IsAgentEnabled(p *bool) bool {
	return p != nil && *p
}

// SharedAgentSkillsEnabled reports whether any agent that consumes the shared
// `.agents/skills/` projection is enabled. Adding a new shared-skill consumer
// means updating this function in one place; sync writers and readiness checks
// both read from it.
func SharedAgentSkillsEnabled(agents AgentsConfig) bool {
	return IsAgentEnabled(agents.Codex.Enabled) ||
		IsAgentEnabled(agents.Antigravity.Enabled) ||
		IsAgentEnabled(agents.VSCode.Enabled) ||
		IsAgentEnabled(agents.CopilotCLI.Enabled)
}

// LegacySkillProjection names a retired client-side directory that Agent Layer
// claims exclusive ownership of and removes during every sync. The Suffix is
// the file extension used to locate generated artifacts during readiness
// detection. See docs/SKILL-CLIENT-SPEC.md "Ownership of legacy projection
// paths" for the rationale.
type LegacySkillProjection struct {
	Dir    []string
	Suffix string
}

// LegacySkillProjections is the canonical list of retired projection paths.
// It is the single source of truth consumed by both the sync cleanup helper
// and the upgrade-readiness check.
var LegacySkillProjections = []LegacySkillProjection{
	{Dir: []string{".codex", "skills"}, Suffix: "SKILL.md"},
	{Dir: []string{".agent", "skills"}, Suffix: "SKILL.md"},
	{Dir: []string{".gemini", "skills"}, Suffix: "SKILL.md"},
	{Dir: []string{".github", "skills"}, Suffix: "SKILL.md"},
	{Dir: []string{".vscode", "prompts"}, Suffix: ".prompt.md"},
}

// InstructionFile holds a single instruction fragment.
type InstructionFile struct {
	Name    string
	Content string
}

// Skill represents a parsed skill with metadata and body.
type Skill struct {
	Name          string
	Description   string
	License       string
	Compatibility string
	Metadata      map[string]string
	AllowedTools  string
	Body          string
	SourcePath    string
	SourceDir     string // Absolute path to the skill directory (parent of SKILL.md)
}

// ProjectConfig is the fully loaded configuration state for sync and launch.
type ProjectConfig struct {
	Config        Config
	Env           map[string]string
	Instructions  []InstructionFile
	Skills        []Skill
	CommandsAllow []string
	Root          string
}
