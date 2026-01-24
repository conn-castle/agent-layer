package messages

// Config messages for configuration loading and validation.
const (
	// ConfigMissingFileFmt formats missing config file errors.
	ConfigMissingFileFmt        = "missing config file %s: %w"
	ConfigFailedReadTemplateFmt = "failed to read template config.toml: %w"
	ConfigMissingEnvFileFmt     = "missing env file %s: %w"
	ConfigInvalidEnvFileFmt     = "invalid env file %s: %w"
	ConfigInvalidConfigFmt      = "invalid config %s: %w"

	ConfigMissingCommandsAllowlistFmt    = "missing commands allowlist %s: %w"
	ConfigFailedReadCommandsAllowlistFmt = "failed to read commands allowlist %s: %w"

	ConfigApprovalsModeInvalidFmt       = "%s: approvals.mode must be one of all, mcp, commands, none"
	ConfigGeminiEnabledRequiredFmt      = "%s: agents.gemini.enabled is required"
	ConfigClaudeEnabledRequiredFmt      = "%s: agents.claude.enabled is required"
	ConfigCodexEnabledRequiredFmt       = "%s: agents.codex.enabled is required"
	ConfigVSCodeEnabledRequiredFmt      = "%s: agents.vscode.enabled is required"
	ConfigAntigravityEnabledRequiredFmt = "%s: agents.antigravity.enabled is required"
	ConfigMcpServerIDRequiredFmt        = "%s: mcp.servers[%d].id is required"
	ConfigMcpServerIDReservedFmt        = "%s: mcp.servers[%d].id is reserved for the internal prompt server"
	ConfigMcpServerEnabledRequiredFmt   = "%s: mcp.servers[%d].enabled is required"
	ConfigMcpServerURLRequiredFmt       = "%s: mcp.servers[%d].url is required for http transport"
	ConfigMcpServerCommandNotAllowedFmt = "%s: mcp.servers[%d].command/args are not allowed for http transport"
	ConfigMcpServerEnvNotAllowedFmt     = "%s: mcp.servers[%d].env is not allowed for http transport"
	ConfigMcpServerCommandRequiredFmt   = "%s: mcp.servers[%d].command is required for stdio transport"
	ConfigMcpServerURLNotAllowedFmt     = "%s: mcp.servers[%d].url is not allowed for stdio transport"
	ConfigMcpServerHeadersNotAllowedFmt = "%s: mcp.servers[%d].headers are not allowed for stdio transport"
	ConfigMcpServerTransportInvalidFmt  = "%s: mcp.servers[%d].transport must be http or stdio"
	ConfigMcpServerClientInvalidFmt     = "%s: mcp.servers[%d].clients contains invalid client %q"
	ConfigWarningThresholdInvalidFmt    = "%s: %s must be greater than zero"

	ConfigMissingSlashCommandsDirFmt          = "missing slash commands directory %s: %w"
	ConfigFailedReadSlashCommandFmt           = "failed to read slash command %s: %w"
	ConfigInvalidSlashCommandFmt              = "invalid slash command %s: %w"
	ConfigSlashCommandMissingContent          = "missing content"
	ConfigSlashCommandMissingFrontMatter      = "missing front matter"
	ConfigSlashCommandUnterminatedFrontMatter = "unterminated front matter"
	ConfigSlashCommandFailedReadContentFmt    = "failed to read content: %w"
	ConfigSlashCommandDescriptionEmpty        = "description is empty"
	ConfigSlashCommandMissingDescription      = "missing description in front matter"

	ConfigMissingInstructionsDirFmt = "missing instructions directory %s: %w"
	ConfigNoInstructionFilesFmt     = "no instruction files found in %s"
	ConfigFailedReadInstructionFmt  = "failed to read instruction %s: %w"

	ConfigMissingEnvVarsFmt = "missing environment variables: %s"
)
