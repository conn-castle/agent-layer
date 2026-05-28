package messages

// Wizard template parsing and validation errors.
const (
	WizardTemplateNoMCPServers               = "template config contains no MCP servers"
	WizardCatalogNoMCPServers                = "MCP catalog mcp-catalog.toml contains no MCP servers"
	WizardMissingDefaultMCPServerTemplateFmt = "missing default MCP server template for %q"
	WizardReadConfigTemplateFailedFmt        = "failed to read config template: %w"
	WizardLoadMCPCatalogFailedFmt            = "failed to load MCP catalog mcp-catalog.toml: %w"
	WizardTemplateWarningsDefaultsIncomplete = "template config warnings defaults are incomplete"

	WizardLoadCLISkillsCatalogFailedFmt      = "failed to load CLI skills catalog cli-skills-catalog.toml: %w"
	WizardCatalogNoCLISkills                 = "CLI skills catalog cli-skills-catalog.toml contains no entries"
	WizardCLISkillCatalogEntryMissingIDFmt   = "CLI skills catalog entry %d is missing required id"
	WizardCLISkillCatalogEntryMissingNameFmt = "CLI skills catalog entry %q is missing required name"
	WizardCLISkillCatalogEntryInvalidIDFmt   = "CLI skills catalog entry %d has invalid id %q"
	WizardCLISkillCatalogEntryDuplicateIDFmt = "CLI skills catalog entry %d duplicates id %q"
)
