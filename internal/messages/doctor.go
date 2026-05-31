package messages

// Doctor messages for the doctor command.
const (
	// DoctorUse is the doctor command name.
	DoctorUse   = "doctor"
	DoctorShort = "Check for missing secrets, disabled servers, and common misconfigurations"

	DoctorHealthCheckFmt = "🏥 Checking Agent Layer health in %s...\n"

	DoctorCheckNameStructure = "Structure"
	DoctorCheckNameConfig    = "Config"
	DoctorCheckNameSecrets   = "Secrets"
	DoctorCheckNameAgents    = "Agents"
	DoctorCheckNameSkills    = "Skills"
	DoctorCheckNameUpdate    = "Update"

	DoctorMissingRequiredDirFmt       = "Missing required directory: %s"
	DoctorMissingRequiredDirRecommend = "Run `al init` to initialize this repository."
	DoctorMissingOptionalDirFmt       = "Missing optional directory: %s"
	DoctorMissingOptionalDirRecommend = "No action needed unless this repo uses committed/shared project-memory docs. Create %s if it does."
	DoctorPathNotDirFmt               = "%s exists but is not a directory"
	DoctorPathNotDirRecommend         = "Ensure the path is a directory, then run `al init` (fresh repo) or `al upgrade` (existing repo)."
	DoctorDirExistsFmt                = "Directory exists: %s"

	DoctorConfigLoadFailedFmt         = "Failed to load configuration: %v"
	DoctorConfigLoadRecommend         = "Check .agent-layer/ for missing or malformed files (config.toml, .env, instructions/, skills/, commands.allow)."
	DoctorConfigLoadLenientRecommend  = "Run 'al wizard' to fix or 'al upgrade' to apply missing fields."
	DoctorConfigNeedsUpgradeRecommend = "Run `al upgrade` to migrate config.toml, then re-run `al doctor`."
	DoctorConfigLoaded                = "Configuration loaded successfully"

	DoctorMissingSecretFmt          = "Missing secret: %s"
	DoctorMissingSecretRecommendFmt = "Add %s to .agent-layer/.env or your environment."
	DoctorSecretFoundEnvFmt         = "Secret found in environment: %s"
	DoctorSecretFoundEnvFileFmt     = "Secret found in .agent-layer/.env: %s"
	DoctorNoRequiredSecrets         = "No required secrets found in configuration."

	DoctorAgentEnabledFmt              = "Agent enabled: %s"
	DoctorAgentDisabledFmt             = "Agent disabled: %s"
	DoctorAntigravityNotFound          = "Antigravity binary not found: agy"
	DoctorAntigravityInstallRecommend  = "Install Antigravity (https://antigravity.google) and ensure `agy` (>= 1.0.0) is on PATH; run `al probe agy` to verify."
	DoctorAntigravityVersionFailedFmt  = "Failed to read Antigravity version: %v"
	DoctorAntigravityVersionUnknownFmt = "Could not parse Antigravity version from %q"
	DoctorAntigravityVersionTooOldFmt  = "Antigravity version %s is below required 1.0.0"
	DoctorAntigravityVersionOKFmt      = "Antigravity version OK: %s"

	DoctorSkillsValidatedFmt       = "Skills validated successfully (%d checked)"
	DoctorSkillsNoneConfigured     = "No skills configured for validation."
	DoctorSkillValidationWarnFmt   = "%s: %s"
	DoctorSkillValidationRecommend = "Update skill frontmatter/path conventions in .agent-layer/skills to match agentskills.io recommendations."
	DoctorSkillValidationFailedFmt = "Failed to validate skill %s: %v"
	DoctorSkillsLoadFailedFmt      = "Failed to load skills from %s: %v"
	DoctorSkillCatalogTooLargeFmt  = "Skill catalog metadata exceeds %d tokens (%d across %d skills)"

	DoctorCheckNameFlatSkills = "FlatSkills"

	DoctorSkillFlatFormatDetectedFmt   = "Found flat-format skill file %q in .agent-layer/skills/; flat format is no longer supported."
	DoctorSkillFlatFormatRecommend     = "Run 'al upgrade' to migrate flat-format skills to directory format (<name>/SKILL.md)."
	DoctorSkillFlatFormatScanFailedFmt = "Failed to scan %s for flat-format skills: %v"
	DoctorSkillFlatFormatScanRecommend = "Ensure .agent-layer/skills/ exists and is readable, then run 'al doctor' again."

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

	// Size summary: informational context-size report. Doctor always prints it (regardless
	// of noise mode or --quiet) so size visibility never depends on whether warnings show.
	// Thresholds set to nil render as "(no limit set)" rather than assuming a default.
	DoctorSizeSummaryHeader              = "\n📊 Context size summary"
	DoctorSizeInstructionsFmt            = "  - Instructions (%s): %d / %d tokens\n"
	DoctorSizeInstructionsNoLimitFmt     = "  - Instructions (%s): %d tokens (no limit set)\n"
	DoctorSizeInstructionsUnavailableFmt = "  - Instructions: size unavailable (%v)\n"
	DoctorSizeSkillsFmt                  = "  - Skills (always-loaded descriptions): %d / %d tokens\n"
	DoctorSizeMCPServersFmt              = "  - MCP servers enabled: %d / %d\n"
	DoctorSizeMCPServersNoLimitFmt       = "  - MCP servers enabled: %d (no limit set)\n"
	DoctorSizeMCPToolsFmt                = "  - MCP tools (total): %d / %d\n"
	DoctorSizeMCPToolsNoLimitFmt         = "  - MCP tools (total): %d (no limit set)\n"
	DoctorSizeMCPSchemaFmt               = "  - MCP tool schemas (total): %d / %d tokens\n"
	DoctorSizeMCPSchemaNoLimitFmt        = "  - MCP tool schemas (total): %d tokens (no limit set)\n"
	DoctorSizeMCPUnavailable             = "  - MCP servers: size unavailable (server discovery failed)"
	DoctorSizeMCPPartialFmt              = "  - Note: %d of %d enabled MCP server(s) unreachable; tool and schema totals exclude them.\n"
	DoctorSizeTotalFmt                   = "  - Total always-loaded (estimated): ~%d tokens\n"
	DoctorSizeTotalExcludesFmt           = "  - Total always-loaded (estimated): ~%d tokens (excludes %s)\n"

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

	DoctorCheckNameCLISkills             = "CLISkills"
	DoctorCLISkillCatalogLoadFailedFmt   = "Failed to load CLI skill catalog: %v"
	DoctorCLISkillCatalogLoadRecommend   = "Reinstall Agent Layer; the embedded CLI skill catalog could not be read."
	DoctorCLISkillBinaryMissingFmt       = "CLI skill %s is installed but its binary %q is not on PATH"
	DoctorCLISkillBinaryMissingRecommend = "Install %q (see https://agent-layer.dev/best-practices/cli-skill-design) or run `al wizard` to remove the skill."
	DoctorCLISkillBinaryOKFmt            = "CLI skill %s found %q on PATH"
)
