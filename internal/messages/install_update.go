package messages

// Install and update messages.
const (
	// InstallRootRequired indicates root path is required for install.
	InstallRootRequired = "root path is required"
	// InstallSystemRequired indicates system is required for install.
	InstallSystemRequired = "install system is required"
	// InstallOverwritePromptRequired indicates overwrite prompts need a handler.
	InstallOverwritePromptRequired                   = "overwrite prompts require a prompt handler; run in an interactive terminal or use `al upgrade --yes` with explicit apply flags"
	InstallInvalidPinVersionFmt                      = "invalid pin version: %w"
	InstallCreateDirFailedFmt                        = "failed to create directory %s: %w"
	InstallAutoRepairPinWarningFmt                   = "Auto-repairing invalid pin file %s (was %q, now %s)\n"
	InstallFailedReadFmt                             = "failed to read %s: %w"
	InstallFailedReadTemplateFmt                     = "failed to read template %s: %w"
	InstallFailedCreateDirForFmt                     = "failed to create directory for %s: %w"
	InstallFailedWriteFmt                            = "failed to write %s: %w"
	InstallFailedStatFmt                             = "failed to stat %s: %w"
	InstallFailedReadGitignoreBlockFmt               = "failed to read gitignore block %s: %w"
	InstallInvalidGitignoreBlockFmt                  = "gitignore block %s must not include managed markers or template hash; run `al upgrade` to review regenerating it"
	InstallUnexpectedTemplatePathFmt                 = "unexpected template path %s"
	InstallDiffHeader                                = "Found existing files that differ from the templates:"
	InstallDiffLineFmt                               = "  - %s\n"
	InstallDiffFooter                                = "Run `al upgrade` to review each file. Non-interactive managed apply: `al upgrade --yes --apply-managed-updates`."
	InstallUnknownHeader                             = "Found files in .agent-layer not tracked by Agent Layer:"
	InstallUnknownFooter                             = "Run `al upgrade` to review deleting them. Non-interactive deletion apply: `al upgrade --yes --apply-deletions`."
	InstallDeleteUnknownPromptRequired               = "delete prompts require a prompt handler; run in an interactive terminal or include `--apply-deletions` with explicit confirmation settings"
	InstallDeleteUnknownFailedFmt                    = "failed to delete %s: %w"
	InstallUpgradeSnapshotCreatedFmt                 = "Created upgrade snapshot: %s\nIf the upgrade completes, restore with: al upgrade rollback %s\n"
	InstallUpgradeSnapshotRolledBackFmt              = "Upgrade failed during %s. Changes were rolled back using snapshot %s.\n"
	InstallUpgradeSnapshotRollbackFailedFmt          = "Upgrade failed during %s. Rollback using snapshot %s failed: %v\n"
	InstallUpgradeRollbackSnapshotIDRequired         = "upgrade rollback requires a snapshot id"
	InstallUpgradeRollbackSnapshotIDInvalid          = "invalid snapshot id %q: must not contain path separators"
	InstallUpgradeRollbackSnapshotNotFoundFmt        = "upgrade snapshot %s not found under %s"
	InstallUpgradeRollbackSnapshotNotRollbackableFmt = "upgrade snapshot %s is not rollbackable (status %s; expected %s): snapshots are only rollbackable after a completed upgrade writes changes"
	InstallUpgradeRollbackFailedFmt                  = "rollback snapshot %s failed: %w"
	InstallUpgradeSnapshotLargeWarningFmt            = "Warning: upgrade snapshot %s is large (%d MB); consider cleaning old snapshots under .agent-layer/state/upgrade-snapshots (threshold: %d MB)\n"
	InstallDiffPreviewPathRequired                   = "diff preview path is required"
	InstallMissingTemplatePathMappingFmt             = "missing template path mapping for %s"
	InstallSectionAwareMarkerDuplicateFmt            = "section-aware marker %q appears multiple times in %s"
	InstallSectionAwareMarkerMissingFmt              = "section-aware marker %q missing in %s"
	InstallUnknownPlanDiffModeFmt                    = "unknown plan diff mode %q"

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
