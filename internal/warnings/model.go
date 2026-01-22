package warnings

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
)

// Warning represents a warning message.
type Warning struct {
	Code    string
	Subject string
	Message string
	Fix     string
	Details []string
}

func (w Warning) String() string {
	s := "WARNING " + w.Code + ": " + w.Message + "\n"
	s += "  subject: " + w.Subject + "\n"
	s += "  fix: " + w.Fix
	for _, d := range w.Details {
		s += "\n  details: " + d
	}
	return s
}
