package messages

// Install and update messages.
const (
	// InstallRootRequired indicates root path is required for install.
	InstallRootRequired = "root path is required"
	// InstallSystemRequired indicates system is required for install.
	InstallSystemRequired = "install system is required"
	// InstallOverwritePromptRequired indicates overwrite prompts need a handler.
	InstallOverwritePromptRequired     = "overwrite prompts require a prompt handler; re-run with --force to overwrite without prompts"
	InstallInvalidPinVersionFmt        = "invalid pin version: %w"
	InstallCreateDirFailedFmt          = "failed to create directory %s: %w"
	InstallExistingPinFileEmptyFmt     = "existing pin file %s is empty"
	InstallFailedReadFmt               = "failed to read %s: %w"
	InstallFailedReadTemplateFmt       = "failed to read template %s: %w"
	InstallFailedCreateDirForFmt       = "failed to create directory for %s: %w"
	InstallFailedWriteFmt              = "failed to write %s: %w"
	InstallFailedStatFmt               = "failed to stat %s: %w"
	InstallFailedReadGitignoreBlockFmt = "failed to read gitignore block %s: %w"
	InstallInvalidGitignoreBlockFmt    = "gitignore block %s must not include managed markers or template hash; re-run `al init --overwrite` to regenerate it"
	InstallUnexpectedTemplatePathFmt   = "unexpected template path %s"
	InstallDiffHeader                  = "Found existing files that differ from the templates:"
	InstallDiffLineFmt                 = "  - %s\n"
	InstallDiffFooter                  = "Re-run `al init --overwrite` to review each file, or `al init --force` to replace them without prompts."
	InstallUnknownHeader               = "Found files in .agent-layer not tracked by Agent Layer:"
	InstallUnknownFooter               = "Re-run `al init --overwrite` to review deleting them, or `al init --force` to delete them without prompts."
	InstallDeleteUnknownPromptRequired = "delete prompts require a prompt handler; re-run with --force to delete unknown files without prompts"
	InstallDeleteUnknownFailedFmt      = "failed to delete %s: %w"

	// UpdateCreateRequestErrFmt formats request creation errors.
	UpdateCreateRequestErrFmt         = "create latest release request: %w"
	UpdateFetchLatestReleaseErrFmt    = "fetch latest release: %w"
	UpdateFetchLatestReleaseStatusFmt = "fetch latest release: unexpected status %s"
	UpdateDecodeLatestReleaseErrFmt   = "decode latest release: %w"
	UpdateLatestReleaseMissingTag     = "latest release missing tag_name"
	UpdateInvalidLatestReleaseTagFmt  = "invalid latest release tag %q: %w"
	UpdateInvalidCurrentVersionFmt    = "invalid current version %q: %w"
	UpdateInvalidVersionFmt           = "invalid version %q"
	UpdateInvalidVersionSegmentFmt    = "invalid version segment %q: %w"
)
