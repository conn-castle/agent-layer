package messages

// Wizard template parsing and validation errors.
const (
	WizardTemplateNoMCPServers               = "template config contains no MCP servers"
	WizardMissingDefaultMCPServerTemplateFmt = "missing default MCP server template for %q"
	WizardReadConfigTemplateFailedFmt        = "failed to read config template: %w"
	WizardTemplateWarningsDefaultsIncomplete = "template config warnings defaults are incomplete"
)
