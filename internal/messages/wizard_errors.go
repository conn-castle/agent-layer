package messages

// Wizard error messages and validation text.
const (
	WizardInstallFailedFmt                = "install failed: %w"
	WizardLoadConfigFailedFmt             = "failed to load config: %w"
	WizardLoadDefaultMCPServersFailedFmt  = "failed to load default MCP servers: %w"
	WizardLoadWarningDefaultsFailedFmt    = "failed to load warning defaults: %w"
	WizardUnknownApprovalModeFmt          = "unknown approval mode: %q"
	WizardUnknownApprovalModeSelectionFmt = "unknown approval mode selection: %q"
	WizardInvalidEnvFileFmt               = "invalid env file %s: %w"
	WizardBackupConfigFailedFmt           = "failed to backup config: %w"
	WizardPatchConfigFailedFmt            = "failed to patch config: %w"
	WizardBackupEnvFailedFmt              = "failed to backup .env: %w"
	WizardWriteConfigFailedFmt            = "failed to write config: %w"
	WizardWriteEnvFailedFmt               = "failed to write .env: %w"
	WizardCustomValueRequiredFmt          = "custom value required for %s"
	WizardPositiveIntRequiredFmt          = "%s must be a positive integer"
	WizardParseConfigFailedFmt            = "parse config: %w"
	WizardDefaultMCPServersRequired       = "default MCP servers are required to patch config"
	WizardRenderConfigFailedFmt           = "render config: %w"
	WizardFormatConfigFailedFmt           = "format config: %w"
	WizardTOMLUnterminatedMultiline       = "unterminated multiline string in TOML output"
)
