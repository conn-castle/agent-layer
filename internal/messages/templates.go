package messages

// Embedded template catalog parsing and validation errors.
const (
	TemplatesLoadCLISkillsCatalogFailedFmt                = "failed to load CLI skills catalog cli-skills-catalog.toml: %w"
	TemplatesCatalogNoCLISkills                           = "CLI skills catalog cli-skills-catalog.toml contains no entries"
	TemplatesCLISkillCatalogEntryMissingIDFmt             = "CLI skills catalog entry %d is missing required id"
	TemplatesCLISkillCatalogEntryMissingNameFmt           = "CLI skills catalog entry %q is missing required name"
	TemplatesCLISkillCatalogEntryInvalidIDFmt             = "CLI skills catalog entry %d has invalid id %q"
	TemplatesCLISkillCatalogEntryDuplicateIDFmt           = "CLI skills catalog entry %d duplicates id %q"
	TemplatesCLISkillCatalogEntryDuplicateNameFmt         = "CLI skills catalog entry %d duplicates name %q"
	TemplatesCLISkillCatalogEntryInvalidMemberFmt         = "CLI skills catalog entry %q has invalid member %q"
	TemplatesCLISkillCatalogEntryDuplicateMemberFmt       = "CLI skills catalog entry %q duplicates member %q"
	TemplatesCLISkillCatalogEntryMemberCollidesIDFmt      = "CLI skills catalog member %q collides with catalog id %q"
	TemplatesCLISkillCatalogEntryMemberMissingTemplateFmt = "CLI skills catalog member %q has no embedded skills/%s/ files"
)
