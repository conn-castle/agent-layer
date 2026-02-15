package messages

// Doctor messages for the doctor command.
const (
	// DoctorUse is the doctor command name.
	DoctorUse   = "doctor"
	DoctorShort = "Check for missing secrets, disabled servers, and common misconfigurations"

	DoctorHealthCheckFmt = "🏥 Checking Agent Layer health in %s...\n"

	DoctorCheckNameStructure  = "Structure"
	DoctorCheckNameConfig     = "Config"
	DoctorCheckNameSecrets    = "Secrets"
	DoctorCheckNameSecretRisk = "SecretRisk"
	DoctorCheckNameAgents     = "Agents"
	DoctorCheckNameUpdate     = "Update"

	DoctorMissingRequiredDirFmt       = "Missing required directory: %s"
	DoctorMissingRequiredDirRecommend = "Run `al init` to initialize this repository."
	DoctorPathNotDirFmt               = "%s exists but is not a directory"
	DoctorPathNotDirRecommend         = "Ensure the path is a directory, then run `al init` (fresh repo) or `al upgrade` (existing repo)."
	DoctorDirExistsFmt                = "Directory exists: %s"

	DoctorConfigLoadFailedFmt = "Failed to load configuration: %v"
	DoctorConfigLoadRecommend = "Check .agent-layer/config.toml for syntax errors."
	DoctorConfigLoaded        = "Configuration loaded successfully"

	DoctorMissingSecretFmt          = "Missing secret: %s"
	DoctorMissingSecretRecommendFmt = "Add %s to .agent-layer/.env or your environment."
	DoctorSecretFoundEnvFmt         = "Secret found in environment: %s"
	DoctorSecretFoundEnvFileFmt     = "Secret found in .agent-layer/.env: %s"
	DoctorNoRequiredSecrets         = "No required secrets found in configuration."
	DoctorSecretRiskDetectedFmt     = "Potential secret literal detected in %s"
	DoctorSecretRiskRecommend       = "Keep secrets only in .agent-layer/.env (AL_* keys) or process environment. Ensure generated files containing resolved values are not committed."
	DoctorSecretRiskNone            = "No obvious secret literals detected in generated artifact surfaces."

	DoctorAgentEnabledFmt  = "Agent enabled: %s"
	DoctorAgentDisabledFmt = "Agent disabled: %s"

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
