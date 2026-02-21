package messages

// System messages for internal operations.
const (
	// DispatchErrDispatched indicates dispatch was executed.
	DispatchErrDispatched = "dispatch executed"
	// DispatchMissingArgv0 indicates argv[0] is missing.
	DispatchMissingArgv0            = "missing argv[0]"
	DispatchWorkingDirRequired      = "working directory is required"
	DispatchExitHandlerRequired     = "exit handler is required"
	DispatchSystemRequired          = "dispatch system is required"
	DispatchAlreadyActiveFmt        = "version dispatch already active (current %s, requested %s)"
	DispatchDevVersionNotAllowedFmt = "cannot dispatch to dev version; set %s to a release version"
	DispatchInvalidBuildVersionFmt  = "invalid build version %q: %w"
	DispatchInvalidEnvVersionFmt    = "invalid %s: %w"
	DispatchResolveUserCacheDirFmt  = "resolve user cache dir: %w"

	DispatchCheckCachedBinaryFmt        = "check cached binary %s: %w"
	DispatchVersionNotCachedFmt         = "version %s is not cached (expected at %s); network access disabled via %s"
	DispatchCreateCacheDirFmt           = "create cache dir: %w"
	DispatchCreateTempFileFmt           = "create temp file: %w"
	DispatchSyncTempFileFmt             = "sync temp file: %w"
	DispatchCloseTempFileFmt            = "close temp file: %w"
	DispatchTruncateTempFileFmt         = "truncate temp file: %w"
	DispatchResetTempFileOffsetFmt      = "reset temp file offset: %w"
	DispatchChmodCachedBinaryFmt        = "chmod cached binary: %w"
	DispatchMoveCachedBinaryFmt         = "move cached binary into place: %w"
	DispatchUnsupportedOSFmt            = "unsupported OS %q"
	DispatchUnsupportedArchFmt          = "unsupported architecture %q"
	DispatchDownloadFailedFmt           = "download %s: %w"
	DispatchDownloadUnexpectedStatusFmt = "download %s: unexpected status %s"
	DispatchDownloadTooLargeFmt         = "download %s: response too large (%d bytes > limit %d bytes)"
	DispatchReadFailedFmt               = "read %s: %w"
	DispatchChecksumNotFoundFmt         = "checksum for %s not found in %s"
	DispatchOpenFileFmt                 = "open %s: %w"
	DispatchHashFileFmt                 = "hash %s: %w"
	DispatchChecksumMismatchFmt         = "checksum mismatch for %s (expected %s, got %s)"

	DispatchDownload404Fmt     = "download %s: release not found (HTTP 404)\n\nThe requested version may not exist or may have been removed.\nRemediation:\n  - Verify the version exists at %s\n  - If this repo is pinned to a bad version, install a valid `al` release and run: al upgrade\n  - Or edit .agent-layer/al.version to a valid version (X.Y.Z)"
	DispatchDownloadTimeoutFmt = "download %s: request timed out\n\nRemediation:\n  - Check your internet connection\n  - If behind a proxy, ensure HTTP_PROXY/HTTPS_PROXY are set\n  - Retry the command\n  - To work offline with a previously cached version, set AL_NO_NETWORK=1"

	DispatchDownloadingFmt = "Downloading al v%s...\n"
	DispatchDownloadedFmt  = "Downloaded al v%s\n"

	DispatchOpenLockFmt      = "open lock %s: %w"
	DispatchLockFmt          = "lock %s: %w"
	DispatchLockTimeoutFmt   = "timed out waiting for lock after %s"
	DispatchReadPinFailedFmt = "read %s: %w"

	DispatchPinFileEmptyWarningFmt         = "warning: pin file %s is empty; ignoring (run al upgrade to repair)\n"
	DispatchInvalidPinnedVersionWarningFmt = "warning: invalid pinned version in %s: %v; ignoring (run al upgrade to repair)\n"
	DispatchVersionSourceFmt               = "Agent Layer version source: %s (%s)\n"
	DispatchVersionOverrideWarningFmt      = "warning: %s overrides repo pin %s from .agent-layer/al.version\n"

	// RootStartPathRequired indicates start path is required for root resolution.
	RootStartPathRequired   = "start path is required"
	RootResolvePathFmt      = "resolve path %s: %w"
	RootPathNotDirFmt       = "%s exists but is not a directory; move or remove it and retry"
	RootCheckPathFmt        = "check %s: %w"
	RootPathNotDirOrFileFmt = "%s exists but is not a directory or file"

	// RunRootPathRequired indicates root path is required for run metadata.
	RunRootPathRequired    = "root path is required"
	RunGenerateIDFailedFmt = "failed to generate run id: %w"
	RunCreateDirFailedFmt  = "failed to create run dir %s: %w"

	// EnvfileLineErrorFmt formats envfile line errors.
	EnvfileLineErrorFmt            = "line %d: %w"
	EnvfileReadFailedFmt           = "failed to read env content: %w"
	EnvfileExpectedKeyValue        = "expected KEY=VALUE"
	EnvfileUnterminatedQuotedValue = "unterminated quoted value"
	EnvfileInvalidQuotedSuffix     = "invalid trailing characters after quoted value"

	// FsutilCreateTempFileFmt formats temp file creation errors.
	FsutilCreateTempFileFmt = "create temp file for %s: %w"
	FsutilSetPermissionsFmt = "set permissions for %s: %w"
	FsutilWriteTempFileFmt  = "write temp file for %s: %w"
	FsutilSyncTempFileFmt   = "sync temp file for %s: %w"
	FsutilCloseTempFileFmt  = "close temp file for %s: %w"
	FsutilRenameTempFileFmt = "rename temp file for %s: %w"
	FsutilOpenDirFmt        = "open dir %s: %w"
	FsutilSyncDirFmt        = "sync dir %s: %w"

	// WarningsResolveConfigFailedFmt formats config resolution failures.
	WarningsResolveConfigFailedFmt        = "Failed to resolve configuration: %v"
	WarningsResolveConfigFix              = "Correct URL/command/auth or environment variables."
	WarningsTooManyServersFmt             = "enabled server count > %d (%d > %d)"
	WarningsTooManyServersFix             = "disable rarely used servers; consolidate."
	WarningsMCPConnectFailedFmt           = "cannot connect, initialize, or list tools: %v"
	WarningsMCPConnectFix                 = "correct URL/command/auth; or disable the server."
	WarningsMCPServerTooManyToolsFmt      = "server has > %d tools (%d > %d)"
	WarningsMCPServerTooManyToolsFix      = "split the server by domain or reduce exported tools."
	WarningsMCPSchemaBloatServerFmt       = "estimated tokens for tool definitions > %d (%d > %d)"
	WarningsMCPSchemaBloatFix             = "reduce schema verbosity; shorten descriptions; remove huge enums/oneOf; reduce tools."
	WarningsMCPTooManyToolsTotalFmt       = "total discovered tools > %d (%d > %d)"
	WarningsMCPTooManyToolsTotalFix       = "disable servers; reduce tool surface."
	WarningsMCPSchemaBloatTotalFmt        = "estimated tokens for all tool definitions > %d (%d > %d)"
	WarningsMCPToolNameCollisionFmt       = "same tool name appears in more than one server: %v"
	WarningsMCPToolNameCollisionFix       = "namespace tool names per server (recommended pattern: <server>__<action>)."
	WarningsInstructionsTooLargeFmt       = "estimated tokens of the combined instruction payload > %d (%d > %d)"
	WarningsInstructionsTooLargeFix       = "reduce always-on instructions; move reference material into docs/ and link to it; remove repetition."
	WarningsPolicySecretInURL             = "mcp server URL appears to contain a literal secret-like value"
	WarningsPolicySecretInURLFix          = "Move secrets out of URL query/userinfo. Use .agent-layer/.env AL_* keys and projected headers/env placeholders instead."
	WarningsPolicyCodexHeaderForm         = "one or more MCP header values are incompatible with Codex header projection"
	WarningsPolicyCodexHeaderFormFix      = "For Codex-targeted servers, use literal values, ${VAR}, or Authorization: Bearer ${VAR} only."
	WarningsPolicyAntigravityMCP          = "MCP server targets antigravity, but antigravity does not support MCP."
	WarningsPolicyAntigravityMCPFix       = "Remove antigravity from mcp.servers[].clients and keep MCP-targeted clients to gemini/claude/codex/vscode."
	WarningsPolicyAntigravityApprovalsFmt = "approvals.mode=%q has no effect for antigravity"
	WarningsPolicyAntigravityApprovalsFix = "Keep approvals.mode for supported clients, but do not expect antigravity to honor approvals settings."
	WarningsPolicyYOLOAck                 = "[yolo] permission prompts disabled for supported clients"
	WarningsNoiseModeInvalidFmt           = "unknown warnings noise mode %q; expected one of: %s, %s"
	WarningsNoiseModeInvalidFix           = "Set warnings.noise_mode to default or reduce."

	WarningsUnsupportedTransportFmt     = "unsupported transport: %s"
	WarningsUnsupportedHTTPTransportFmt = "unsupported http transport: %s"
	WarningsConnectionFailedFmt         = "connection failed: %w"
	WarningsListToolsFailedFmt          = "list tools failed: %w"
	WarningsTooManyTools                = "too many tools or infinite loop"

	// CoverReportProfileFlagUsage describes the profile flag.
	CoverReportProfileFlagUsage      = "path to coverage profile"
	CoverReportThresholdFlagUsage    = "required coverage threshold (optional)"
	CoverReportMissingProfileFlag    = "missing required -profile flag"
	CoverReportParseFailedFmt        = "failed to parse coverage profile: %v\n"
	CoverReportWriteTableFailedFmt   = "failed to write summary table: %v\n"
	CoverReportWriteSummaryFailedFmt = "failed to write coverage summary: %v\n"
	CoverReportTableHeader           = "file\tcover%\tlines_missed"
	CoverReportTableRowFmt           = "%s\t%.2f\t%d\n"
	CoverReportTotalWithThresholdFmt = "total coverage: %.2f%% (threshold %.2f%%) %s\n"
	CoverReportTotalFmt              = "total coverage: %.2f%%\n"
	CoverReportStatusPass            = "PASS"
	CoverReportStatusFail            = "FAIL"

	// ExtractChecksumUsageFmt formats extract-checksum usage.
	ExtractChecksumUsageFmt       = "Usage: %s <checksums-file> <target-filename>\n"
	ExtractChecksumFileMissingFmt = "Error: %s not found\n"
	ExtractChecksumReadFailedFmt  = "Error: failed to read %s: %v\n"
	ExtractChecksumNotFoundFmt    = "Error: %s not found in %s\n"

	UpdateFormulaUsageFmt       = "Usage: %s <formula-file> <new-url> <new-sha256>\n"
	UpdateFormulaFileMissingFmt = "Error: %s not found\n"
	UpdateFormulaStatFailedFmt  = "Error: failed to stat %s: %v\n"
	UpdateFormulaReadFailedFmt  = "Error: failed to read %s: %v\n"
	UpdateFormulaWriteFailedFmt = "Error: failed to write %s: %v\n"
	UpdateFormulaURLCountFmt    = "Error: expected 1 url line, found %d\n"
	UpdateFormulaSHACountFmt    = "Error: expected 1 sha256 line, found %d\n"

	// McpRunPromptServerFailedFmt formats MCP prompt server failures.
	McpRunPromptServerFailedFmt = "failed to run MCP prompt server: %w"
)
