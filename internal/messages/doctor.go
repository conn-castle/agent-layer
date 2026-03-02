package messages

// Doctor messages for the doctor command.
const (
	// DoctorUse is the doctor command name.
	DoctorUse   = "doctor"
	DoctorShort = "Check for missing secrets, disabled servers, and common misconfigurations"

	DoctorHealthCheckFmt = "🏥 Checking Agent Layer health in %s...\n"

	DoctorCheckNameStructure    = "Structure"
	DoctorCheckNameConfig       = "Config"
	DoctorCheckNameSecrets      = "Secrets"
	DoctorCheckNameAgents       = "Agents"
	DoctorCheckNameSkills       = "Skills"
	DoctorCheckNamePromptServer = "MCPPrompts"
	DoctorCheckNamePromptConfig = "MCPPromptConfig"
	DoctorCheckNameUpdate       = "Update"

	DoctorMissingRequiredDirFmt       = "Missing required directory: %s"
	DoctorMissingRequiredDirRecommend = "Run `al init` to initialize this repository."
	DoctorPathNotDirFmt               = "%s exists but is not a directory"
	DoctorPathNotDirRecommend         = "Ensure the path is a directory, then run `al init` (fresh repo) or `al upgrade` (existing repo)."
	DoctorDirExistsFmt                = "Directory exists: %s"

	DoctorConfigLoadFailedFmt        = "Failed to load configuration: %v"
	DoctorConfigLoadRecommend        = "Check .agent-layer/ for missing or malformed files (config.toml, .env, instructions/, skills/, commands.allow)."
	DoctorConfigLoadLenientRecommend = "Run 'al wizard' to fix or 'al upgrade' to apply missing fields."
	DoctorConfigLoaded               = "Configuration loaded successfully"

	DoctorMissingSecretFmt          = "Missing secret: %s"
	DoctorMissingSecretRecommendFmt = "Add %s to .agent-layer/.env or your environment."
	DoctorSecretFoundEnvFmt         = "Secret found in environment: %s"
	DoctorSecretFoundEnvFileFmt     = "Secret found in .agent-layer/.env: %s"
	DoctorNoRequiredSecrets         = "No required secrets found in configuration."

	DoctorAgentEnabledFmt  = "Agent enabled: %s"
	DoctorAgentDisabledFmt = "Agent disabled: %s"

	DoctorSkillsValidatedFmt       = "Skills validated successfully (%d checked)"
	DoctorSkillsNoneConfigured     = "No skills configured for validation."
	DoctorSkillValidationWarnFmt   = "%s: %s"
	DoctorSkillValidationRecommend = "Update skill frontmatter/path conventions in .agent-layer/skills to match agentskills.io recommendations."
	DoctorSkillValidationFailedFmt = "Failed to validate skill %s: %v"
	DoctorSkillsLoadFailedFmt      = "Failed to load skills from %s: %v"

	DoctorPromptServerConfigMissing    = "Internal MCP prompt server unavailable because configuration failed to load."
	DoctorPromptServerConfigRecommend  = "Fix configuration errors above; the prompt server requires a valid config."
	DoctorPromptServerResolveFailedFmt = "Failed to resolve internal MCP prompt server command: %v"
	DoctorPromptServerResolveRecommend = "Ensure `al` is on PATH or Go is installed, and the repo root is the Agent Layer module."
	DoctorPromptServerEnvFailedFmt     = "Failed to resolve internal MCP prompt server environment: %v"
	DoctorPromptServerEnvRecommend     = "Ensure the repo root is available and readable."
	DoctorPromptServerMissingRepoRoot  = "Internal MCP prompt server environment is missing AL_REPO_ROOT."
	DoctorPromptServerResolvedFmt      = "Internal MCP prompt server resolved: %s"
	DoctorPromptServerNotRequired      = "No MCP prompt server clients enabled."

	DoctorCheckNameFlatSkills = "FlatSkills"

	DoctorSkillFlatFormatDetectedFmt   = "Found flat-format skill file %q in .agent-layer/skills/; flat format is no longer supported."
	DoctorSkillFlatFormatRecommend     = "Run 'al upgrade' to migrate flat-format skills to directory format (<name>/SKILL.md)."
	DoctorSkillFlatFormatScanFailedFmt = "Failed to scan %s for flat-format skills: %v"
	DoctorSkillFlatFormatScanRecommend = "Ensure .agent-layer/skills/ exists and is readable, then run 'al doctor' again."

	DoctorPromptServerConfigNotRequired      = "No MCP prompt server client configs enabled."
	DoctorPromptServerConfigResolveFailedFmt = "Failed to resolve internal MCP prompt server for config comparison: %v"
	DoctorPromptServerConfigMissingFmt       = "Missing %s; internal MCP prompt server will not be configured."
	DoctorPromptServerConfigReadFailedFmt    = "Failed to read %s: %v"
	DoctorPromptServerConfigInvalidFmt       = "Invalid %s: %v"
	DoctorPromptServerConfigMissingServerFmt = "%s is missing the internal prompt server entry."
	DoctorPromptServerConfigMismatchFmt      = "%s internal prompt server entry does not match expected settings: %s"
	DoctorPromptServerConfigMatchFmt         = "%s internal prompt server entry matches expected settings."
	DoctorPromptServerConfigFilesRecommend   = "Run `al sync` (or `al claude`/`al gemini`) to regenerate client configs, or disable the agent if unused."

	DoctorUpdateSkippedFmt          = "Update check skipped because %s is set"
	DoctorUpdateSkippedRecommendFmt = "Unset %s to check for updates."
	DoctorUpdateRateLimited         = "Update check skipped due to GitHub API rate limit (HTTP 403/429)"
	DoctorUpdateFailedFmt           = "Failed to check for updates: %v"
	DoctorUpdateFailedRecommend     = "Verify network access and try again."
	DoctorUpdateDevBuildFmt         = "Running dev build; latest release is %s"
	DoctorUpdateDevBuildRecommend   = "Install a release build to use version pinning and dispatch."
	DoctorUpdateAvailableFmt        = "Agent Layer update available: %s (current %s)"
	DoctorUpdateAvailableRecommend  = UpdateUpgradeBlock + "\n\n" + UpdateSafetyBlock
	DoctorUpToDateFmt               = "Agent Layer is up to date (%s)"

	DoctorWarningSystemHeader        = "\n🔍 Running warning checks..."
	DoctorMCPCheckStartFmt           = "⏳ Checking MCP servers (%d enabled)"
	DoctorMCPCheckDone               = " done"
	DoctorInstructionsCheckFailedFmt = "Failed to check instructions: %v"
	DoctorMCPCheckFailedFmt          = "Failed to check MCP servers: %v"
	DoctorFailureSummary             = "❌ Some checks failed or triggered warnings. Please address the items above."
	DoctorFailureError               = "doctor checks failed"
	DoctorSuccessSummary             = "✅ All systems go. Agent Layer is ready."

	DoctorStatusOKLabel        = "[OK]  "
	DoctorStatusWarnLabel      = "[WARN]"
	DoctorStatusFailLabel      = "[FAIL]"
	DoctorResultLineFmt        = "%s %-10s %s\n"
	DoctorRecommendationPrefix = "       💡 "
	DoctorRecommendationIndent = "         "
)
