package messages

// CLI messages for user-facing commands and prompts.
const (
	// RootUse is the CLI command name.
	RootUse = "al"
	// RootShort is the short description for the root command.
	RootShort             = "Agent Layer CLI"
	RootVersionFlag       = "Print version and exit"
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

	InitOverwriteRequiresTerminal = "init overwrite prompts require an interactive terminal; re-run with --force to overwrite without prompts"
	InitOverwritePromptFmt        = "Overwrite %s with the template version?"
	InitOverwriteAllPrompt        = "Overwrite all existing managed files with template versions and update the pin if needed?"
	InitOverwriteManagedHeader    = "Existing managed files (and pin) that differ from templates:"
	InitOverwriteMemoryHeader     = "Existing memory files in docs/agent-layer that differ from templates:"
	InitOverwriteMemoryAllPrompt  = "Overwrite all existing memory files in docs/agent-layer with template versions?"
	InitDeleteUnknownAllPrompt    = "Delete all unknown files under .agent-layer?"
	InitDeleteUnknownPromptFmt    = "Delete %s?"
	InitRunWizardPrompt           = "Run the setup wizard now? (recommended)"

	InitFlagOverwrite = "Prompt before overwriting existing template files"
	InitFlagForce     = "Overwrite existing template files and delete unknown files under .agent-layer without prompting (implies --overwrite)"
	InitFlagNoWizard  = "Skip prompting to run the setup wizard after init"
	InitFlagVersion   = "Pin the repo to a specific Agent Layer version (vX.Y.Z or X.Y.Z)"

	InitWarnUpdateCheckFailedFmt = "Warning: failed to check for updates: %v\n"
	InitWarnDevBuildFmt          = "Warning: running dev build; latest release is %s\n"
	UpdateUpgradeBlock           = "Upgrade:\n  Homebrew: brew upgrade conn-castle/tap/agent-layer\n  macOS/Linux: curl -fsSL https://github.com/conn-castle/agent-layer/releases/latest/download/al-install.sh | bash\n  Windows (PowerShell): iwr -useb https://github.com/conn-castle/agent-layer/releases/latest/download/al-install.ps1 | iex"
	UpdateSafetyBlock            = "Safety:\n  - Back up local changes before upgrading.\n  - `al init --force` overwrites managed files and deletes unknown files under .agent-layer without prompts."
	InitWarnUpdateAvailableFmt   = "Warning: update available: %s (current %s)\n\n" + UpdateUpgradeBlock + "\n\n" + UpdateSafetyBlock + "\n"

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
	WizardUse              = "wizard"
	WizardShort            = "Interactive setup wizard"
	WizardLong             = "Run the interactive setup wizard for this repository."
	WizardRequiresTerminal = "wizard requires an interactive terminal"

	// GeminiUse is the gemini command name.
	GeminiUse   = "gemini"
	GeminiShort = "Sync and launch Gemini CLI"

	ClaudeUse   = "claude"
	ClaudeShort = "Sync and launch Claude Code CLI"

	CodexUse   = "codex"
	CodexShort = "Sync and launch Codex CLI"

	VSCodeUse   = "vscode"
	VSCodeShort = "Sync and launch VS Code with CODEX_HOME configured"

	AntigravityUse   = "antigravity"
	AntigravityShort = "Sync and launch Antigravity"

	// ClientsGeminiExitErrorFmt formats gemini exit errors.
	ClientsGeminiExitErrorFmt      = "gemini exited with error: %w"
	ClientsClaudeExitErrorFmt      = "claude exited with error: %w"
	ClientsCodexExitErrorFmt       = "codex exited with error: %w"
	ClientsAntigravityExitErrorFmt = "antigravity exited with error: %w"
	ClientsVSCodeExitErrorFmt      = "vscode exited with error: %w"
	ClientsCodexHomeWarningFmt     = "Warning: CODEX_HOME is set to %s; expected %s\n"

	// StubShortFmt formats stub command descriptions.
	StubShortFmt          = "%s (not implemented yet)"
	StubNotImplementedFmt = "%s is not implemented in this phase"

	// McpPromptsUse is the mcp-prompts command name.
	McpPromptsUse   = "mcp-prompts"
	McpPromptsShort = "Run the internal MCP prompt server over stdio"
)
