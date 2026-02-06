package messages

// Wizard template parsing and validation errors.
const (
	WizardTemplateNoMCPServers               = "template config contains no MCP servers"
	WizardMissingDefaultMCPServerTemplateFmt = "missing default MCP server template for %q"
	WizardMCPServersUnexpectedTypeFmt        = "mcp.servers has unexpected type %T"
	WizardReadConfigTemplateFailedFmt        = "failed to read config template: %w"
	WizardParseConfigTemplateFailedFmt       = "failed to parse config template: %w"
	WizardNoMCPServerBlocksFound             = "no MCP server blocks found in template"
	WizardMissingMCPServerIDInTemplate       = "missing MCP server id in template block"
	WizardDuplicateMCPServerIDInTemplateFmt  = "duplicate MCP server id %q in template"
	WizardTemplateWarningsDefaultsIncomplete = "template config warnings defaults are incomplete"
)
