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

	CodexModel        string
	CodexModelTouched bool

	CodexReasoning        string
	CodexReasoningTouched bool

	// MCP
	EnabledMCPServers        map[string]bool
	EnabledMCPServersTouched bool

	// Secrets (Env vars)
	Secrets map[string]string
}

// NewChoices returns a Choices struct initialized with defaults.
func NewChoices() *Choices {
	return &Choices{
		EnabledAgents:     make(map[string]bool),
		EnabledMCPServers: make(map[string]bool),
		Secrets:           make(map[string]string),
	}
}
