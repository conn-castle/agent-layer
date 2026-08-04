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
	ConfigRepoRootRequiredPath  = "repo root required for path expansion"
	ConfigPathOutsideRootFmt    = "path %s is outside repo root %s"

	ConfigMissingCommandsAllowlistFmt    = "missing commands allowlist %s: %w"
	ConfigFailedReadCommandsAllowlistFmt = "failed to read commands allowlist %s: %w"

	ConfigApprovalsModeInvalidFmt                 = "%s: approvals.mode must be one of all, mcp, commands, none, yolo"
	ConfigClaudeEnabledRequiredFmt                = "%s: agents.claude.enabled is required"
	ConfigClaudeVSCodeEnabledRequiredFmt          = "%s: agents.claude_vscode.enabled is required"
	ConfigCodexEnabledRequiredFmt                 = "%s: agents.codex.enabled is required"
	ConfigVSCodeEnabledRequiredFmt                = "%s: agents.vscode.enabled is required"
	ConfigAntigravityEnabledRequiredFmt           = "%s: agents.antigravity.enabled is required"
	ConfigAntigravityAgentSpecificModelInvalidFmt = "%s: agents.antigravity.agent_specific.model is not supported; use agents.antigravity.model for Antigravity model selection"
	ConfigCopilotCLIEnabledRequiredFmt            = "%s: agents.copilot_cli.enabled is required"
	ConfigCopilotCLIReasoningEffortUnsupportedFmt = "%s: agents.copilot_cli.reasoning_effort is not supported in this release"
	ConfigDispatchMaxDepthInvalidFmt              = "%s: dispatch.max_depth must be greater than zero"
	ConfigDispatchMCPWaitTimeoutInvalidFmt        = "%s: dispatch.mcp_wait_timeout_minutes must be greater than zero"
	ConfigDispatchMCPToolTimeoutInvalidFmt        = "%s: dispatch.mcp_tool_timeout_minutes must be greater than zero"
	ConfigDispatchMCPTimeoutOrderInvalidFmt       = "%s: dispatch.mcp_tool_timeout_minutes (%d) must be greater than dispatch.mcp_wait_timeout_minutes (%d)"
	ConfigMcpServerIDRequiredFmt                  = "%s: mcp.servers[%d].id is required"
	ConfigMcpServerIDReservedFmt                  = "%s: mcp.servers[%d].id is reserved"
	ConfigMcpServerIDDuplicateFmt                 = "%s: mcp.servers[%d].id %q duplicates mcp.servers[%d].id"
	ConfigMcpServerEnabledRequiredFmt             = "%s: mcp.servers[%d].enabled is required"
	ConfigMcpServerURLRequiredFmt                 = "%s: mcp.servers[%d].url is required for http transport"
	ConfigMcpServerHTTPTransportInvalidFmt        = "%s: mcp.servers[%d].http_transport must be sse or streamable"
	ConfigMcpServerCommandRequiredFmt             = "%s: mcp.servers[%d].command is required for stdio transport"
	ConfigMcpServerTransportInvalidFmt            = "%s: mcp.servers[%d].transport must be http or stdio"
	ConfigMcpServerClientInvalidFmt               = "%s: mcp.servers[%d].clients contains invalid client %q"
	ConfigUnrecognizedKeysFmt                     = "%s: unrecognized config keys: %w"
	ConfigLegacyGeminiUnsupportedFmt              = "%s: agents.gemini is no longer supported; run 'al upgrade' to migrate to agents.antigravity (renames agents.gemini.enabled, drops legacy gemini.model/reasoning_effort keys, and rewrites mcp.servers[].clients gemini→antigravity)"
	ConfigLegacyDispatchUnsupportedFmt            = "%s: agents.<agent>.dispatch.default_agent is no longer supported; run 'al upgrade' to remove the retired dispatch defaults"
	ConfigWarningNoiseModeInvalidFmt              = "%s: warnings.noise_mode %q is invalid (allowed: default, reduce, quiet)"
	ConfigWarningThresholdInvalidFmt              = "%s: %s must be greater than zero"

	ConfigMissingSkillsDirFmt            = "missing skills directory %s: %w"
	ConfigFailedReadImportedSkillsDirFmt = "failed to read imported skills directory %s: %w"
	ConfigFailedReadSkillFmt             = "failed to read skill %s: %w"
	ConfigInvalidSkillFmt                = "invalid skill %s: %w"
	ConfigSkillMissingContent            = "missing content"
	ConfigSkillMissingFrontMatter        = "missing front matter"
	ConfigSkillUnterminatedFrontMatter   = "unterminated front matter"
	ConfigSkillInvalidFrontMatterFmt     = "invalid front matter: %w"
	ConfigSkillInvalidFrontMatterTypeFmt = "invalid front matter type: %s"
	ConfigSkillDuplicateKeyFmt           = "skill front matter contains duplicate key %q"
	ConfigSkillFailedReadContentFmt      = "failed to read content: %w"
	ConfigSkillDescriptionEmpty          = "description is empty"
	ConfigSkillMissingDescription        = "missing description in front matter"
	ConfigSkillNameEmpty                 = "name is empty"
	ConfigSkillNameInvalidMultiline      = "name must be a single line scalar"
	ConfigSkillNameMismatchFmt           = "skill in %s has name %q, expected %q"
	ConfigSkillDirEmptyFmt               = "skill directory %s has no SKILL.md"
	ConfigSkillDuplicateNameFmt          = "duplicate skill name %q from %s and %s"
	ConfigSkillFlatFormatUnsupportedFmt  = "found flat-format skill %q (%s) in skills directory; flat format is no longer supported -- run 'al upgrade' to migrate to directory format"

	// Skill import configuration validation.
	ConfigSkillImportRepositoryRequiredFmt           = "%s: skills.imports[%d].repository is required"
	ConfigSkillImportSelectorsRequiredFmt            = "%s: skills.imports[%d].selectors must list at least one selector"
	ConfigSkillImportPositiveSelectorRequiredFmt     = "%s: skills.imports[%d].selectors must include at least one positive selector; an exclusion never imports a skill by itself"
	ConfigSkillImportSelectorEmptyFmt                = "%s: skills.imports[%d].selectors contains an empty selector"
	ConfigSkillImportSelectorInvalidFmt              = "%s: skills.imports[%d].selectors contains invalid selector %q: %w"
	ConfigSkillImportTrackingInvalidFmt              = "%s: skills.imports[%d].tracking %q is invalid (allowed: tracked, pinned)"
	ConfigSkillImportWritePolicyInvalidFmt           = "%s: skills.imports[%d].write_policy %q is invalid (allowed: none, branch, direct)"
	ConfigSkillImportPushBranchRequiredFmt           = "%s: skills.imports[%d].push_branch is required when write_policy = \"branch\"; Agent Layer never generates branch names"
	ConfigSkillImportPushBranchPrimaryFmt            = "%s: skills.imports[%d].push_branch %q is a primary branch name; use write_policy = \"direct\" to write to the destination default branch"
	ConfigSkillImportPushBranchUnsupportedFmt        = "%s: skills.imports[%d].push_branch is only supported when write_policy = \"branch\" (write_policy = %q)"
	ConfigSkillImportPushRepositoryUnsupportedFmt    = "%s: skills.imports[%d].push_repository requires write_policy = \"branch\" or \"direct\""
	ConfigSkillImportDuplicateBlockFmt               = "%s: skills.imports[%d] duplicates the repository, ref, tracking, write policy, and push destination of skills.imports[%d]; keep one block per unique policy and list its selectors together"
	ConfigSkillImportDuplicateSelectorFmt            = "%s: skills.imports[%d] repeats selector %q already declared by skills.imports[%d]; each repository and selector pair must be unique across configuration"
	ConfigSkillImportBlockUnparsableFmt              = "failed to parse a [[skills.imports]] block: %w"
	ConfigSkillImportSelectorsAssignmentMissing      = "[[skills.imports]] block has no selectors assignment"
	ConfigSkillImportSelectorsAssignmentUnterminated = "[[skills.imports]] selectors array is unterminated"
	ConfigSkillSelectorBackslash                     = "use '/' as the path separator"
	ConfigSkillSelectorAbsolute                      = "must be a repository-relative path"
	ConfigSkillSelectorNotNormalized                 = "must be a normalized repository-relative path without '.' or '..' segments"

	ConfigMissingInstructionsDirFmt = "missing instructions directory %s: %w"
	ConfigFailedReadInstructionFmt  = "failed to read instruction %s: %w"

	ConfigMissingEnvVarsFmt = "missing environment variables: %s"

	// ConfigValidationGuidance is appended to validation errors to direct users to repair tools.
	ConfigValidationGuidance = "(run 'al wizard' to fix or 'al doctor' to diagnose)"

	// ConfigLenientLoadInfoFmt is used when repair tools fall back to lenient config loading.
	ConfigLenientLoadInfoFmt = "Config has validation errors; %s will help you fix them: %v"
)
