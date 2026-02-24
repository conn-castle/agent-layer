package messages

// CLI messages for user-facing commands and prompts.
const (
	// RootUse is the CLI command name.
	RootUse = "al"
	// RootShort is the short description for the root command.
	RootShort             = "Agent Layer CLI"
	RootVersionFlag       = "Print version and exit"
	RootQuietFlag         = "Suppress agent-layer informational output"
	RootMissingAgentLayer = "agent layer isn't initialized in this repository (missing .agent-layer); run 'al init' to initialize"

	// VersionCommitFmt formats the commit hash for version display.
	VersionCommitFmt  = "commit %s"
	VersionBuildFmt   = "built %s"
	VersionFullFmt    = "%s (%s)"
	VersionTemplate   = "{{.Version}}\n"
	VersionRequired   = "version is required"
	VersionInvalidFmt = "version %q must be in the form vX.Y.Z or X.Y.Z"

	// InitUse is the init command name.
	InitUse   = "init"
	InitShort = "Initialize Agent Layer in this repository"
	// InitAlreadyInitialized is returned when init is invoked on an already-initialized repo.
	InitAlreadyInitialized = "agent layer is already initialized (or partially initialized) in this repository; run 'al upgrade' to upgrade or repair templates"
	InitRunWizardPrompt    = "Run the setup wizard now? (recommended)"

	InitFlagNoWizard = "Skip prompting to run the setup wizard after init"
	InitFlagVersion  = "Pin the repo to a specific Agent Layer version (vX.Y.Z or X.Y.Z) or latest"

	UpgradeUse                            = "upgrade"
	UpgradeShort                          = "Apply template-managed updates and update the repo pin"
	UpgradePlanUse                        = "plan"
	UpgradePlanShort                      = "Show a dry-run upgrade plan without writing files"
	UpgradePrefetchUse                    = "prefetch"
	UpgradePrefetchShort                  = "Download and cache an Agent Layer release binary"
	UpgradePrefetchVersionFlag            = "Version to prefetch (vX.Y.Z, X.Y.Z, or latest)"
	UpgradePrefetchVersionRequired        = "prefetch requires a release version; pass --version X.Y.Z when running a dev build"
	UpgradePrefetchDoneFmt                = "Prefetched Agent Layer version %s into the local cache.\n"
	UpgradeRepairGitignoreUse             = "repair-gitignore-block"
	UpgradeRepairGitignoreShort           = "Restore `.agent-layer/gitignore.block` and reapply the root `.gitignore` managed block"
	UpgradeRepairGitignoreDone            = "Repaired `.agent-layer/gitignore.block` and updated root `.gitignore`.\n"
	UpgradeRollbackUse                    = "rollback <snapshot-id>"
	UpgradeRollbackShort                  = "Restore a managed-file upgrade snapshot"
	UpgradeRollbackRequiresSnapshotID     = "rollback requires a snapshot id: `al upgrade rollback <snapshot-id>`"
	UpgradeRollbackSuccessFmt             = "Restored snapshot %s.\n"
	UpgradeRollbackFlagList               = "List available upgrade snapshots"
	UpgradeRollbackListHeader             = "Available upgrade snapshots (newest first):"
	UpgradeRollbackNoSnapshots            = "No upgrade snapshots found."
	UpgradeRequiresTerminal               = "upgrade prompts require an interactive terminal; re-run `al upgrade` in a terminal, or run non-interactively with `--yes` and one or more apply flags"
	UpgradeNonInteractiveRequiresYesApply = "non-interactive upgrade requires `--yes` and one or more apply flags: `--apply-managed-updates`, `--apply-memory-updates`, `--apply-deletions`"
	UpgradeYesRequiresApply               = "`--yes` requires one or more apply flags: `--apply-managed-updates`, `--apply-memory-updates`, `--apply-deletions`"
	UpgradeFlagDiffLines                  = "Max number of diff lines shown per file in upgrade previews"
	UpgradeDiffLinesInvalidFmt            = "invalid value for --diff-lines: %d (must be > 0)"
	UpgradeFlagYes                        = "Run non-interactively when used with apply flags"
	UpgradeFlagApplyManagedUpdates        = "Apply managed template updates without prompts"
	UpgradeFlagApplyMemoryUpdates         = "Apply memory file updates without prompts"
	UpgradeFlagApplyDeletions             = "Apply unknown file deletions (requires explicit confirmation unless combined with --yes)"
	UpgradeFlagVersion                    = "Target Agent Layer version for the upgrade (vX.Y.Z, X.Y.Z, or latest)"

	UpgradeOverwritePromptFmt       = "Overwrite %s with the template version?"
	UpgradeOverwriteAllPrompt       = "Overwrite all existing managed files with template versions and update the pin if needed?"
	UpgradeOverwriteManagedHeader   = "Existing managed files that differ from templates:"
	UpgradeOverwriteMemoryHeader    = "Existing memory files in docs/agent-layer that differ from templates:"
	UpgradeOverwriteMemoryAllPrompt = "Overwrite all existing memory files in docs/agent-layer with template versions?"
	UpgradeDeleteUnknownAllPrompt   = "Delete all unknown files under .agent-layer?"
	UpgradeDeleteUnknownPromptFmt   = "Delete %s?"
	UpgradeSkipManagedUpdatesInfo   = "Info: skipping managed template updates (pass --apply-managed-updates to include them)."
	UpgradeSkipMemoryUpdatesInfo    = "Info: skipping memory file updates (pass --apply-memory-updates to include them)."
	UpgradeSkipDeletionsInfo        = "Info: skipping unknown file deletions (pass --apply-deletions to include them)."

	InitWarnUpdateCheckFailedFmt = "Warning: failed to check for updates: %v\n"
	InitWarnDevBuildFmt          = "Warning: running dev build; latest release is %s\n"
	InitResolveLatestVersionFmt  = "resolve latest version: %w"
	InitLatestVersionMissing     = "latest release check returned an empty version"

	InitCreateReleaseValidationRequestFmt = "create release validation request: %w"
	InitValidateReleaseVersionRequestFmt  = "validate requested release v%s: %w"
	InitValidateReleaseVersionStatusFmt   = "validate requested release v%s: unexpected status %s"
	InitReleaseVersionNotFoundFmt         = "requested release v%s not found; check available versions at %s"

	UpdateUpgradeBlock         = "Upgrade:\n  1) Update the CLI:\n     Homebrew: brew upgrade conn-castle/tap/agent-layer\n     macOS/Linux: curl -fsSL https://github.com/conn-castle/agent-layer/releases/latest/download/al-install.sh | bash\n  2) Upgrade this repo:\n     al upgrade plan\n     al upgrade"
	UpdateSafetyBlock          = "Safety:\n  - Back up local changes before upgrading.\n  - `al upgrade` is the recommended default path.\n  - Non-interactive managed-only apply: `al upgrade --yes --apply-managed-updates`.\n  - Include memory updates/deletions only when explicitly selected with apply flags.\n  - Keep secrets only in `.agent-layer/.env` (AL_* keys) or process environment; do not commit generated files with resolved secrets."
	InitWarnUpdateAvailableFmt = "Warning: agent-layer update available: %s (current %s)\n\n" + UpdateUpgradeBlock + "\n\n" + UpdateSafetyBlock + "\n"

	// CompletionUse is the completion command usage.
	CompletionUse                 = "completion [bash|zsh|fish]"
	CompletionShort               = "Generate shell completion scripts"
	CompletionInstall             = "Install the completion script for the specified shell"
	CompletionUnsupportedShellFmt = "unsupported shell %q (supported: bash, zsh, fish)"

	CompletionCreateDirErrFmt   = "create completion dir: %w"
	CompletionWriteFileErrFmt   = "write completion file: %w"
	CompletionInstalledFmt      = "Installed %s completion to %s\n"
	CompletionBashNote          = "Bash completion requires bash-completion to be enabled in your shell."
	CompletionFishNote          = "Restart fish or open a new terminal to enable completions."
	CompletionZshNoteFmt        = "Add this to your .zshrc before compinit:\n  fpath=(%s $fpath)"
	CompletionResolveHomeErrFmt = "resolve home dir: %w"

	// PromptYesDefaultFmt formats yes/no prompts with yes as default.
	PromptYesDefaultFmt   = "%s [Y/n]: "
	PromptNoDefaultFmt    = "%s [y/N]: "
	PromptInvalidResponse = "invalid response %q"
	PromptRetryYesNo      = "Please enter y or n."

	// WizardUse is the wizard command name.
	WizardUse                    = "wizard"
	WizardShort                  = "Interactive setup wizard"
	WizardLong                   = "Run the interactive setup wizard for this repository."
	WizardRequiresTerminal       = "wizard requires an interactive terminal"
	WizardProfileFlagHelp        = "Run wizard in non-interactive profile mode using a profile config TOML file"
	WizardProfileYesFlagHelp     = "Apply profile-mode changes; without this flag profile mode prints a rewrite preview only"
	WizardCleanupBackupsFlagHelp = "Delete wizard backup files (.agent-layer/config.toml.bak and .agent-layer/.env.bak)"
	WizardCleanupBackupsHeader   = "Removed wizard backup files:"
	WizardCleanupBackupsPathFmt  = "  - %s\n"
	WizardCleanupBackupsNone     = "No wizard backup files found."

	// GeminiUse is the gemini command name.
	GeminiUse   = "gemini"
	GeminiShort = "Sync and launch Gemini CLI"

	ClaudeUse   = "claude"
	ClaudeShort = "Sync and launch Claude Code CLI"

	CodexUse   = "codex"
	CodexShort = "Sync and launch Codex CLI"

	VSCodeUse   = "vscode"
	VSCodeShort = "Sync and launch VS Code"

	NoSyncInvalidFmt = "invalid value for --no-sync: %q"
	QuietInvalidFmt  = "invalid value for --quiet: %q"

	AntigravityUse   = "antigravity"
	AntigravityShort = "Sync and launch Antigravity"

	// ClientsGeminiExitErrorFmt formats gemini exit errors.
	ClientsGeminiExitErrorFmt            = "gemini exited with error: %w"
	ClientsClaudeExitErrorFmt            = "claude exited with error: %w"
	ClientsCodexExitErrorFmt             = "codex exited with error: %w"
	ClientsAntigravityExitErrorFmt       = "antigravity exited with error: %w"
	ClientsVSCodeExitErrorFmt            = "vscode exited with error: %w"
	ClientsVSCodeCodeNotFoundFmt         = "vscode preflight failed: 'code' command not found on PATH: %w"
	ClientsVSCodeManagedBlockConflictFmt = "vscode preflight failed: managed settings block conflict in %s (%s); run `al sync` to repair `.vscode/settings.json`"

	ClientsCodexHomeWarningFmt       = "Warning: CODEX_HOME is set to %s; expected %s\n"
	ClientsClaudeConfigDirWarningFmt = "Warning: CLAUDE_CONFIG_DIR is set to %s; expected %s\n"

	// StubShortFmt formats stub command descriptions.
	StubShortFmt          = "%s (not implemented yet)"
	StubNotImplementedFmt = "%s is not implemented in this phase"

	// McpPromptsUse is the mcp-prompts command name.
	McpPromptsUse   = "mcp-prompts"
	McpPromptsShort = "Run the internal MCP prompt server over stdio"
)
