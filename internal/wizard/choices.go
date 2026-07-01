package wizard

// Choices tracks user selections in the wizard.
type Choices struct {
	// Approvals
	ApprovalMode        string
	ApprovalModeTouched bool

	// Agents
	EnabledAgents        map[string]bool
	EnabledAgentsTouched bool

	// Models
	AntigravityModel        string
	AntigravityModelTouched bool

	ClaudeModel        string
	ClaudeModelTouched bool

	ClaudeReasoning        string
	ClaudeReasoningTouched bool

	ClaudeLocalConfigDir        bool
	ClaudeLocalConfigDirTouched bool

	// Claude "disable" toggles (disable-intent: true = disable the feature).
	// Each lands in .claude/settings.json via agent_specific passthrough. A key
	// is written only when the toggle is on; leaving it off keeps the client's
	// native default. Gated on Claude || ClaudeVSCode (both write settings.json).
	ClaudeDisableIDEReading        bool // CLAUDE_CODE_AUTO_CONNECT_IDE = "false"
	ClaudeDisableIDEReadingTouched bool

	ClaudeDisableMemory        bool // autoMemoryEnabled = false
	ClaudeDisableMemoryTouched bool

	ClaudeDisableConnectors        bool // ENABLE_CLAUDEAI_MCP_SERVERS = "false"
	ClaudeDisableConnectorsTouched bool

	ClaudeDisableQuestionTool        bool // disable_question_tool flag; sync injects deny + PreToolUse hook
	ClaudeDisableQuestionToolTouched bool

	ClaudeStatusline        bool
	ClaudeStatuslineTouched bool

	CodexModel        string
	CodexModelTouched bool

	CodexReasoning        string
	CodexReasoningTouched bool

	CodexApps        bool
	CodexAppsTouched bool

	// CodexDisableBrowser disables Codex browser/computer-use features
	// (browser_use/in_app_browser/computer_use = false). Disable-intent like the
	// Claude toggles above; gated on Codex enabled.
	CodexDisableBrowser        bool
	CodexDisableBrowserTouched bool

	CodexStatusline        bool
	CodexStatuslineTouched bool

	CopilotCLIModel        string
	CopilotCLIModelTouched bool

	// Agent Layer workflow bundle install/refresh action (Q1).
	// InstallWorkflowBundle=true refreshes managed bundled instruction files and
	// workflow skill dirs, then creates missing conventions and memory files.
	// false is a no-op for workflow-bundle files.
	InstallWorkflowBundle        bool
	InstallWorkflowBundleTouched bool

	// Catalog CLI skills (Q2).
	// EnabledCLISkills is keyed by catalog entry id. Apply copies the matching
	// embedded skill directory into `.agent-layer/skills/<id>/` for ids set true
	// and removes the on-disk directory for ids set false.
	EnabledCLISkills map[string]bool
	CLISkillsCatalog []CLISkillCatalogEntry

	// Git tracking for Agent Layer-owned folders. The managed source of truth is
	// `.agent-layer/gitignore.block`; these fields are derived from that file at
	// wizard startup and written back to it before sync.
	TrackAgentLayerDir     bool
	TrackDocsAgentLayerDir bool
	GitTrackingTouched     bool

	// MCP
	EnabledMCPServers        map[string]bool
	EnabledMCPServersTouched bool
	DisabledMCPServers       map[string]bool
	DefaultMCPServers        []DefaultMCPServer

	// Custom (non-catalog) MCP servers found in config.toml.
	// CustomMCPServers holds their ids in config order; the wizard surfaces them
	// for keep/disable separately from catalog defaults. CustomMCPServersEnabled
	// records the per-id decision: true keeps the server enabled, false sets
	// enabled = false while preserving the entry (a custom server has no catalog
	// template to restore from, so it is never deleted). CustomMCPServersTouched
	// is true once the user has answered the custom-server step.
	CustomMCPServers        []string
	CustomMCPServersEnabled map[string]bool
	CustomMCPServersTouched bool

	// Secrets (Env vars)
	Secrets map[string]string

	// Warnings
	WarningsEnabled                bool
	WarningsEnabledTouched         bool
	InstructionTokenThreshold      int
	MCPServerThreshold             int
	MCPToolsTotalThreshold         int
	MCPServerToolsThreshold        int
	MCPSchemaTokensTotalThreshold  int
	MCPSchemaTokensServerThreshold int
}

// NewChoices returns a Choices struct initialized with defaults.
func NewChoices() *Choices {
	return &Choices{
		EnabledAgents:           make(map[string]bool),
		EnabledCLISkills:        make(map[string]bool),
		EnabledMCPServers:       make(map[string]bool),
		DisabledMCPServers:      make(map[string]bool),
		CustomMCPServersEnabled: make(map[string]bool),
		Secrets:                 make(map[string]string),
	}
}

// Clone returns a deep copy of the choices state for step-level rollback.
func (c *Choices) Clone() *Choices {
	if c == nil {
		return nil
	}
	clone := *c
	clone.EnabledAgents = cloneBoolMap(c.EnabledAgents)
	clone.EnabledCLISkills = cloneBoolMap(c.EnabledCLISkills)
	clone.EnabledMCPServers = cloneBoolMap(c.EnabledMCPServers)
	clone.DisabledMCPServers = cloneBoolMap(c.DisabledMCPServers)
	clone.Secrets = cloneStringMap(c.Secrets)
	clone.DefaultMCPServers = cloneDefaultMCPServers(c.DefaultMCPServers)
	clone.CustomMCPServers = cloneStringSlice(c.CustomMCPServers)
	clone.CustomMCPServersEnabled = cloneBoolMap(c.CustomMCPServersEnabled)
	clone.CLISkillsCatalog = cloneCLISkillCatalog(c.CLISkillsCatalog)
	return &clone
}

func cloneCLISkillCatalog(in []CLISkillCatalogEntry) []CLISkillCatalogEntry {
	if len(in) == 0 {
		return nil
	}
	out := make([]CLISkillCatalogEntry, len(in))
	copy(out, in)
	return out
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return make(map[string]bool)
	}
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return make(map[string]string)
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneDefaultMCPServers(in []DefaultMCPServer) []DefaultMCPServer {
	if len(in) == 0 {
		return nil
	}
	out := make([]DefaultMCPServer, len(in))
	for i := range in {
		out[i] = DefaultMCPServer{
			ID:          in[i].ID,
			RequiredEnv: cloneStringSlice(in[i].RequiredEnv),
		}
	}
	return out
}
