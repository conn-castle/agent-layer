package warnings

import "fmt"

// Warning codes.
const (
	CodeInstructionsTooLarge     = "INSTRUCTIONS_TOO_LARGE"
	CodeMCPServerUnreachable     = "MCP_SERVER_UNREACHABLE"
	CodeMCPTooManyServers        = "MCP_TOO_MANY_SERVERS_ENABLED"
	CodeMCPTooManyToolsTotal     = "MCP_TOO_MANY_TOOLS_TOTAL"
	CodeMCPServerTooManyTools    = "MCP_SERVER_TOO_MANY_TOOLS"
	CodeMCPToolSchemaBloatTotal  = "MCP_TOOL_SCHEMA_BLOAT_TOTAL"
	CodeMCPToolSchemaBloatServer = "MCP_TOOL_SCHEMA_BLOAT_SERVER"
	CodeMCPToolNameCollision     = "MCP_TOOL_NAME_COLLISION"
	CodeWarningNoiseModeInvalid  = "WARNING_NOISE_MODE_INVALID"
	CodePolicySecretInURL        = "POLICY_SECRET_IN_URL"
	CodePolicyCodexHeaderForm    = "POLICY_CODEX_HEADER_FORM_UNSUPPORTED"
	CodePolicyCapabilityMismatch = "POLICY_CLIENT_CAPABILITY_MISMATCH"
)

// Source labels where a warning originates.
const (
	SourceInternal           = "internal"
	SourceNetwork            = "network"
	SourceExternalDependency = "external dependency"
)

// Severity labels whether a warning should be considered critical.
const (
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// Warning represents a warning message.
type Warning struct {
	Code     string
	Subject  string
	Message  string
	Fix      string
	Details  []string
	Source   string
	Severity string
	// NoiseSuppressible marks warnings that can be hidden by conservative noise controls.
	// Critical warnings are never suppressed even if this flag is true.
	NoiseSuppressible bool
}

func (w Warning) String() string {
	s := "WARNING " + w.Code + ": " + w.Message + "\n"
	s += fmt.Sprintf("  source: %s\n", w.sourceOrDefault())
	s += fmt.Sprintf("  severity: %s\n", w.severityOrDefault())
	s += "  subject: " + w.Subject + "\n"
	s += "  fix: " + w.Fix
	for _, d := range w.Details {
		s += "\n  details: " + d
	}
	return s
}

func (w Warning) sourceOrDefault() string {
	if w.Source == "" {
		return SourceInternal
	}
	return w.Source
}

func (w Warning) severityOrDefault() string {
	if w.Severity == "" {
		return SeverityWarning
	}
	return w.Severity
}
