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
	DispatchChmodCachedBinaryFmt        = "chmod cached binary: %w"
	DispatchMoveCachedBinaryFmt         = "move cached binary into place: %w"
	DispatchUnsupportedOSFmt            = "unsupported OS %q"
	DispatchUnsupportedArchFmt          = "unsupported architecture %q"
	DispatchDownloadFailedFmt           = "download %s: %w"
	DispatchDownloadUnexpectedStatusFmt = "download %s: unexpected status %s"
	DispatchReadFailedFmt               = "read %s: %w"
	DispatchChecksumNotFoundFmt         = "checksum for %s not found in %s"
	DispatchOpenFileFmt                 = "open %s: %w"
	DispatchHashFileFmt                 = "hash %s: %w"
	DispatchChecksumMismatchFmt         = "checksum mismatch for %s (expected %s, got %s)"

	DispatchOpenLockFmt             = "open lock %s: %w"
	DispatchLockFmt                 = "lock %s: %w"
	DispatchReadPinFailedFmt        = "read %s: %w"
	DispatchPinFileEmptyFmt         = "pin file %s is empty"
	DispatchInvalidPinnedVersionFmt = "invalid pinned version in %s: %w"

	// RootStartPathRequired indicates start path is required for root resolution.
	RootStartPathRequired   = "start path is required"
	RootResolvePathFmt      = "resolve path %s: %w"
	RootPathNotDirFmt       = "%s exists but is not a directory"
	RootCheckPathFmt        = "check %s: %w"
	RootPathNotDirOrFileFmt = "%s exists but is not a directory or file"

	// RunRootPathRequired indicates root path is required for run metadata.
	RunRootPathRequired    = "root path is required"
	RunGenerateIDFailedFmt = "failed to generate run id: %w"
	RunCreateDirFailedFmt  = "failed to create run dir %s: %w"

	// EnvfileLineErrorFmt formats envfile line errors.
	EnvfileLineErrorFmt     = "line %d: %w"
	EnvfileReadFailedFmt    = "failed to read env content: %w"
	EnvfileExpectedKeyValue = "expected KEY=VALUE"

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
	WarningsResolveConfigFailedFmt   = "Failed to resolve configuration: %v"
	WarningsResolveConfigFix         = "Correct URL/command/auth or environment variables."
	WarningsTooManyServersFmt        = "enabled server count > %d (%d > %d)"
	WarningsTooManyServersFix        = "disable rarely used servers; consolidate."
	WarningsMCPConnectFailedFmt      = "cannot connect, initialize, or list tools: %v"
	WarningsMCPConnectFix            = "correct URL/command/auth; or disable the server."
	WarningsMCPServerTooManyToolsFmt = "server has > %d tools (%d > %d)"
	WarningsMCPServerTooManyToolsFix = "split the server by domain or reduce exported tools."
	WarningsMCPSchemaBloatServerFmt  = "estimated tokens for tool definitions > %d (%d > %d)"
	WarningsMCPSchemaBloatFix        = "reduce schema verbosity; shorten descriptions; remove huge enums/oneOf; reduce tools."
	WarningsMCPTooManyToolsTotalFmt  = "total discovered tools > %d (%d > %d)"
	WarningsMCPTooManyToolsTotalFix  = "disable servers; reduce tool surface."
	WarningsMCPSchemaBloatTotalFmt   = "estimated tokens for all tool definitions > %d (%d > %d)"
	WarningsMCPToolNameCollisionFmt  = "same tool name appears in more than one server: %v"
	WarningsMCPToolNameCollisionFix  = "namespace tool names per server (recommended pattern: <server>__<action>)."
	WarningsInstructionsTooLargeFmt  = "estimated tokens of the combined instruction payload > %d (%d > %d)"
	WarningsInstructionsTooLargeFix  = "reduce always-on instructions; move reference material into docs/ and link to it; remove repetition."

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
