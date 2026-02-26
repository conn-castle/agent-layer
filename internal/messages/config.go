package messages

// Config messages for configuration loading and validation.
//
// Naming convention: Config* messages validate configuration inputs (filesystem,
// root path, file contents) rather than a unified System interface, so they use
// descriptive names like ConfigFSRequired and ConfigRootRequired instead of
// ConfigSystemRequired.
const (
	// ConfigMissingFileFmt formats missing config file errors.
	ConfigMissingFileFmt        = "missing config file %s: %w"
	ConfigFailedReadTemplateFmt = "failed to read template config.toml: %w"
	ConfigMissingEnvFileFmt     = "missing env file %s: %w"
	ConfigInvalidEnvFileFmt     = "invalid env file %s: %w"
	ConfigInvalidConfigFmt      = "invalid config %s: %w"
	ConfigFSRequired            = "config filesystem is required"
	ConfigRootRequired          = "config root path is required"
	ConfigPathOutsideRootFmt    = "path %s is outside repo root %s"

	ConfigMissingCommandsAllowlistFmt    = "missing commands allowlist %s: %w"
	ConfigFailedReadCommandsAllowlistFmt = "failed to read commands allowlist %s: %w"

	ConfigApprovalsModeInvalidFmt                  = "%s: approvals.mode must be one of all, mcp, commands, none, yolo"
	ConfigGeminiEnabledRequiredFmt                 = "%s: agents.gemini.enabled is required"
	ConfigGeminiReasoningEffortUnsupportedFmt      = "%s: agents.gemini.reasoning_effort is not supported in this release"
	ConfigClaudeEnabledRequiredFmt                 = "%s: agents.claude.enabled is required"
	ConfigClaudeVSCodeEnabledRequiredFmt           = "%s: agents.claude_vscode.enabled is required"
	ConfigCodexEnabledRequiredFmt                  = "%s: agents.codex.enabled is required"
	ConfigVSCodeEnabledRequiredFmt                 = "%s: agents.vscode.enabled is required"
	ConfigAntigravityEnabledRequiredFmt            = "%s: agents.antigravity.enabled is required"
	ConfigClaudeReasoningEffortInvalidFmt          = "%s: agents.claude.reasoning_effort must be one of low, medium, high"
	ConfigClaudeReasoningEffortModelUnsupportedFmt = "%s: agents.claude.reasoning_effort requires an Opus model; set agents.claude.model to an opus variant (current: %q)"
	ConfigMcpServerIDRequiredFmt                   = "%s: mcp.servers[%d].id is required"
	ConfigMcpServerIDReservedFmt                   = "%s: mcp.servers[%d].id is reserved for the internal prompt server"
	ConfigMcpServerIDDuplicateFmt                  = "%s: mcp.servers[%d].id %q duplicates mcp.servers[%d].id"
	ConfigMcpServerEnabledRequiredFmt              = "%s: mcp.servers[%d].enabled is required"
	ConfigMcpServerURLRequiredFmt                  = "%s: mcp.servers[%d].url is required for http transport"
	ConfigMcpServerHTTPTransportInvalidFmt         = "%s: mcp.servers[%d].http_transport must be sse or streamable"
	ConfigMcpServerCommandRequiredFmt              = "%s: mcp.servers[%d].command is required for stdio transport"
	ConfigMcpServerTransportInvalidFmt             = "%s: mcp.servers[%d].transport must be http or stdio"
	ConfigMcpServerClientInvalidFmt                = "%s: mcp.servers[%d].clients contains invalid client %q"
	ConfigUnrecognizedKeysFmt                      = "%s: unrecognized config keys: %w"
	ConfigWarningNoiseModeInvalidFmt               = "%s: warnings.noise_mode %q is invalid (allowed: default, reduce, quiet)"
	ConfigWarningThresholdInvalidFmt               = "%s: %s must be greater than zero"

	ConfigMissingSkillsDirFmt          = "missing skills directory %s: %w"
	ConfigFailedReadSkillFmt           = "failed to read skill %s: %w"
	ConfigInvalidSkillFmt              = "invalid skill %s: %w"
	ConfigSkillMissingContent          = "missing content"
	ConfigSkillMissingFrontMatter      = "missing front matter"
	ConfigSkillUnterminatedFrontMatter = "unterminated front matter"
	ConfigSkillFailedReadContentFmt    = "failed to read content: %w"
	ConfigSkillDescriptionEmpty        = "description is empty"
	ConfigSkillMissingDescription      = "missing description in front matter"
	ConfigSkillNameEmpty               = "name is empty"
	ConfigSkillNameInvalidMultiline    = "name must be a single line scalar"
	ConfigSkillNameMismatchFmt         = "skill in %s has name %q, expected %q"
	ConfigSkillDirMissingSkillFileFmt  = "invalid skill directory %s: missing SKILL.md"
	ConfigSkillDuplicateNameFmt        = "duplicate skill name %q from %s and %s"

	ConfigMissingInstructionsDirFmt = "missing instructions directory %s: %w"
	ConfigNoInstructionFilesFmt     = "no instruction files found in %s"
	ConfigFailedReadInstructionFmt  = "failed to read instruction %s: %w"

	ConfigMissingEnvVarsFmt = "missing environment variables: %s"

	// ConfigValidationGuidance is appended to validation errors to direct users to repair tools.
	ConfigValidationGuidance = "(run 'al wizard' to fix or 'al doctor' to diagnose)"

	// ConfigLenientLoadInfoFmt is used when repair tools fall back to lenient config loading.
	ConfigLenientLoadInfoFmt = "Config has validation errors; %s will help you fix them: %v"
)
