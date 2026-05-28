package wizard

import (
	"fmt"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// cliSkillsCatalogTemplatePath is the embedded CLI skills catalog used by the wizard.
// It is internal-only: read from the embedded FS, never written to a user repo.
const cliSkillsCatalogTemplatePath = "cli-skills-catalog.toml"

// CLISkillCatalogEntry describes one wizard-managed CLI skill option.
type CLISkillCatalogEntry struct {
	ID   string
	Name string
}

var cliSkillCatalogIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// isSafeCLISkillCatalogID returns true for single-segment catalog ids that can
// be used as .agent-layer/skills/<id>/ directory names.
func isSafeCLISkillCatalogID(id string) bool {
	return cliSkillCatalogIDPattern.MatchString(id) && !strings.Contains(id, "/") && !strings.Contains(id, `\`)
}

// loadCLISkillCatalog parses the embedded cli-skills-catalog.toml file into typed
// catalog entries. Errors loudly when the catalog is missing, malformed, contains
// no entries, or contains an entry with an empty id or name. The catalog is the
// authoritative source for the wizard's CLI-skill multiselect.
func loadCLISkillCatalog() ([]CLISkillCatalogEntry, error) {
	data, err := templates.Read(cliSkillsCatalogTemplatePath)
	if err != nil {
		return nil, fmt.Errorf(messages.WizardLoadCLISkillsCatalogFailedFmt, err)
	}
	var doc struct {
		CLISkills []CLISkillCatalogEntry `toml:"cli_skills"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf(messages.WizardLoadCLISkillsCatalogFailedFmt, err)
	}
	if len(doc.CLISkills) == 0 {
		return nil, fmt.Errorf(messages.WizardCatalogNoCLISkills)
	}
	seen := make(map[string]struct{}, len(doc.CLISkills))
	seenNames := make(map[string]struct{}, len(doc.CLISkills))
	for idx, entry := range doc.CLISkills {
		if entry.ID == "" {
			return nil, fmt.Errorf(messages.WizardCLISkillCatalogEntryMissingIDFmt, idx)
		}
		if !isSafeCLISkillCatalogID(entry.ID) {
			return nil, fmt.Errorf(messages.WizardCLISkillCatalogEntryInvalidIDFmt, idx, entry.ID)
		}
		if _, ok := seen[entry.ID]; ok {
			return nil, fmt.Errorf(messages.WizardCLISkillCatalogEntryDuplicateIDFmt, idx, entry.ID)
		}
		seen[entry.ID] = struct{}{}
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			return nil, fmt.Errorf(messages.WizardCLISkillCatalogEntryMissingNameFmt, entry.ID)
		}
		if _, ok := seenNames[name]; ok {
			return nil, fmt.Errorf(messages.WizardCLISkillCatalogEntryDuplicateNameFmt, idx, name)
		}
		seenNames[name] = struct{}{}
	}
	return doc.CLISkills, nil
}
