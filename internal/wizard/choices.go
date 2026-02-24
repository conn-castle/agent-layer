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
	GeminiModel        string
	GeminiModelTouched bool

	ClaudeModel        string
	ClaudeModelTouched bool

	ClaudeReasoning        string
	ClaudeReasoningTouched bool

	ClaudeLocalConfigDir        bool
	ClaudeLocalConfigDirTouched bool

	CodexModel        string
	CodexModelTouched bool

	CodexReasoning        string
	CodexReasoningTouched bool

	// MCP
	EnabledMCPServers        map[string]bool
	EnabledMCPServersTouched bool
	DisabledMCPServers       map[string]bool
	MissingDefaultMCPServers []string
	RestoreMissingMCPServers bool
	DefaultMCPServers        []DefaultMCPServer

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
		EnabledAgents:      make(map[string]bool),
		EnabledMCPServers:  make(map[string]bool),
		DisabledMCPServers: make(map[string]bool),
		Secrets:            make(map[string]string),
	}
}

// Clone returns a deep copy of the choices state for step-level rollback.
func (c *Choices) Clone() *Choices {
	if c == nil {
		return nil
	}
	clone := *c
	clone.EnabledAgents = cloneBoolMap(c.EnabledAgents)
	clone.EnabledMCPServers = cloneBoolMap(c.EnabledMCPServers)
	clone.DisabledMCPServers = cloneBoolMap(c.DisabledMCPServers)
	clone.Secrets = cloneStringMap(c.Secrets)
	clone.MissingDefaultMCPServers = cloneStringSlice(c.MissingDefaultMCPServers)
	clone.DefaultMCPServers = cloneDefaultMCPServers(c.DefaultMCPServers)
	return &clone
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
