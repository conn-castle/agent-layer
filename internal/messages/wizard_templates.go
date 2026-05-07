package messages

// Wizard template parsing and validation errors.
const (
	WizardTemplateNoMCPServers               = "template config contains no MCP servers"
	WizardCatalogNoMCPServers                = "MCP catalog mcp-catalog.toml contains no MCP servers"
	WizardMissingDefaultMCPServerTemplateFmt = "missing default MCP server template for %q"
	WizardReadConfigTemplateFailedFmt        = "failed to read config template: %w"
	WizardLoadMCPCatalogFailedFmt            = "failed to load MCP catalog mcp-catalog.toml: %w"
	WizardTemplateWarningsDefaultsIncomplete = "template config warnings defaults are incomplete"
)
