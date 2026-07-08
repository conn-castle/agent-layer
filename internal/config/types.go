package config

// Approval mode constants.
const (
	ApprovalModeAll      = "all"
	ApprovalModeCommands = "commands"
	ApprovalModeMCP      = "mcp"
	ApprovalModeNone     = "none"
	ApprovalModeYOLO     = "yolo"
)

// DefaultDispatchMaxDepth is the maximum dispatch recursion depth when unset.
const DefaultDispatchMaxDepth = 2

// Config is the root configuration loaded from .agent-layer/config.toml.
type Config struct {
	Approvals     ApprovalsConfig     `toml:"approvals"`
	Agents        AgentsConfig        `toml:"agents"`
	Dispatch      DispatchLimits      `toml:"dispatch"`
	MCP           MCPConfig           `toml:"mcp"`
	Notifications NotificationsConfig `toml:"notifications"`
	Warnings      WarningsConfig      `toml:"warnings"`
}

// ApprovalsConfig controls auto-approval behavior per client.
type ApprovalsConfig struct {
	Mode string `toml:"mode"`
}

// AgentsConfig holds per-client enablement and model selection.
type AgentsConfig struct {
	Antigravity  AntigravityConfig `toml:"antigravity"`
	Claude       ClaudeConfig      `toml:"claude"`
	ClaudeVSCode EnableOnlyConfig  `toml:"claude_vscode"`
	Codex        CodexConfig       `toml:"codex"`
	VSCode       EnableOnlyConfig  `toml:"vscode"`
	CopilotCLI   AgentConfig       `toml:"copilot_cli"`
}

// DispatchConfig controls Agent Dispatch defaults for a caller agent.
type DispatchConfig struct {
	DefaultAgent string `toml:"default_agent"`
}

// DispatchLimits controls Agent Dispatch recursion limits.
type DispatchLimits struct {
	MaxDepth *int `toml:"max_depth"`
}

// NotificationsConfig controls user-visible local notification behavior.
type NotificationsConfig struct {
	Chime *bool `toml:"chime"`
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
	Dispatch        DispatchConfig `toml:"dispatch"`
	LocalConfigDir  *bool          `toml:"local_config_dir"`
	// DisableQuestionTool, when true, blocks Claude Code's AskUserQuestion tool.
	// Sync injects permissions.deny + a PreToolUse hook into .claude/settings.json
	// (merged with any user agent_specific entries). nil/false leave it allowed.
	DisableQuestionTool *bool `toml:"disable_question_tool"`
	// Statusline controls whether Agent Layer projects the editable
	// .agent-layer/claude-statusline.sh source into .claude/claude-statusline.sh
	// and wires statusLine into .claude/settings.json on sync. It is explicit
	// opt-in: only true enables it. Read via
	// ClaudeStatuslineEnabled.
	Statusline    *bool               `toml:"statusline"`
	AgentSpecific ProviderPassthrough `toml:"agent_specific"`
}

// EnableOnlyConfig is for agents that support enablement but not model selection.
type EnableOnlyConfig struct {
	Enabled *bool `toml:"enabled"`
}

// AntigravityConfig is for the Antigravity (`agy`) client. Model selection is a
// first-class Agent Layer setting and sync projects it into
// .agy/antigravity-cli/settings.json.
type AntigravityConfig struct {
	Enabled       *bool               `toml:"enabled"`
	Model         string              `toml:"model"`
	Dispatch      DispatchConfig      `toml:"dispatch"`
	AgentSpecific ProviderPassthrough `toml:"agent_specific"`
}

// CodexConfig extends AgentConfig with Codex-specific settings.
type CodexConfig struct {
	Enabled         *bool          `toml:"enabled"`
	Model           string         `toml:"model"`
	ReasoningEffort string         `toml:"reasoning_effort"`
	Dispatch        DispatchConfig `toml:"dispatch"`
	LocalConfigDir  *bool          `toml:"local_config_dir"`
	// Statusline controls whether Agent Layer reads the editable
	// .agent-layer/codex-statusline.toml source and injects its native
	// tui.status_line list into .codex/config.toml on sync. It is explicit
	// opt-in: only true enables it. Read via
	// CodexStatuslineEnabled.
	Statusline    *bool               `toml:"statusline"`
	AgentSpecific ProviderPassthrough `toml:"agent_specific"`
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

// ClaudeStatuslineEnabled reports whether the Claude status line should be
// projected and wired. It is explicit opt-in: only true enables it.
func ClaudeStatuslineEnabled(c ClaudeConfig) bool {
	return c.Statusline != nil && *c.Statusline
}

// CodexStatuslineEnabled reports whether the Codex status line should be wired.
// It is explicit opt-in: only true enables it.
func CodexStatuslineEnabled(c CodexConfig) bool {
	return c.Statusline != nil && *c.Statusline
}

// CodexLocalConfigDirEnabled reports whether Agent Layer should set CODEX_HOME
// to the repo-local .codex directory. It is explicit opt-in: only true enables
// it.
func CodexLocalConfigDirEnabled(c CodexConfig) bool {
	return c.LocalConfigDir != nil && *c.LocalConfigDir
}

// NotificationsChimeEnabled reports whether Agent Layer should project a local
// turn-stop chime into supported provider-native hook systems.
func NotificationsChimeEnabled(c Config) bool {
	return c.Notifications.Chime != nil && *c.Notifications.Chime
}

// DispatchMaxDepth returns the configured Agent Dispatch maximum depth.
func DispatchMaxDepth(c Config) int {
	if c.Dispatch.MaxDepth == nil {
		return DefaultDispatchMaxDepth
	}
	return *c.Dispatch.MaxDepth
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
