package templates

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/messages"
)

// cliSkillsCatalogPath is the embedded CLI skills catalog shared by the wizard
// and doctor. It is internal-only: read from the embedded FS, never written to
// a user repo.
const cliSkillsCatalogPath = "cli-skills-catalog.toml"

// CLISkillCatalogEntry describes one CLI skill catalog option.
// Git entries derive destination names from their exact source paths;
// the catalog ID identifies the checkbox, including grouped selections.
// Binary names the external CLI the skill requires, if any.
type CLISkillCatalogEntry struct {
	ID              string
	Name            string
	Binary          string   `toml:"binary"`
	OwnershipMarker string   `toml:"ownership_marker"`
	Repository      string   `toml:"repository"`
	Selectors       []string `toml:"selectors"`
}

var cliSkillCatalogIDPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// IsSafeCLISkillCatalogID returns true for single-segment catalog ids that can
// be used as .agent-layer/skills/<id>/ directory names.
func IsSafeCLISkillCatalogID(id string) bool {
	return cliSkillCatalogIDPattern.MatchString(id) && !strings.Contains(id, "/") && !strings.Contains(id, `\`)
}

// LoadCLISkillCatalog parses the embedded cli-skills-catalog.toml file into typed
// catalog entries. Errors loudly when the catalog is missing, malformed, contains
// no entries, or contains an entry with an invalid or duplicate id, name, or
// member.
func LoadCLISkillCatalog() ([]CLISkillCatalogEntry, error) {
	data, err := Read(cliSkillsCatalogPath)
	if err != nil {
		return nil, fmt.Errorf(messages.TemplatesLoadCLISkillsCatalogFailedFmt, err)
	}
	return ParseCLISkillCatalog(data, Read)
}

// ParseCLISkillCatalog validates source coordinates and retained embedded trees.
// The release generator supplies its filesystem reader to share runtime validation.
func ParseCLISkillCatalog(data []byte, read func(string) ([]byte, error)) ([]CLISkillCatalogEntry, error) {
	var doc struct {
		CLISkills []CLISkillCatalogEntry `toml:"cli_skills"`
	}
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf(messages.TemplatesLoadCLISkillsCatalogFailedFmt, err)
	}
	if len(doc.CLISkills) == 0 {
		return nil, fmt.Errorf(messages.TemplatesCatalogNoCLISkills)
	}
	seen := make(map[string]struct{}, len(doc.CLISkills))
	seenNames := make(map[string]struct{}, len(doc.CLISkills))
	for idx, entry := range doc.CLISkills {
		if entry.ID == "" {
			return nil, fmt.Errorf(messages.TemplatesCLISkillCatalogEntryMissingIDFmt, idx)
		}
		if !IsSafeCLISkillCatalogID(entry.ID) {
			return nil, fmt.Errorf(messages.TemplatesCLISkillCatalogEntryInvalidIDFmt, idx, entry.ID)
		}
		if _, ok := seen[entry.ID]; ok {
			return nil, fmt.Errorf(messages.TemplatesCLISkillCatalogEntryDuplicateIDFmt, idx, entry.ID)
		}
		seen[entry.ID] = struct{}{}
		name := strings.TrimSpace(entry.Name)
		if name == "" {
			return nil, fmt.Errorf(messages.TemplatesCLISkillCatalogEntryMissingNameFmt, entry.ID)
		}
		if _, ok := seenNames[name]; ok {
			return nil, fmt.Errorf(messages.TemplatesCLISkillCatalogEntryDuplicateNameFmt, idx, name)
		}
		seenNames[name] = struct{}{}
	}
	seenSkills := map[string]bool{}
	for _, entry := range doc.CLISkills {
		if err := validateCatalogSource(entry, read); err != nil {
			return nil, err
		}
		for _, name := range entry.SkillNames() {
			if name != entry.ID {
				if _, collision := seen[name]; collision {
					return nil, fmt.Errorf(messages.TemplatesCLISkillCatalogEntryMemberCollidesIDFmt, name, name)
				}
			}
			if seenSkills[name] {
				return nil, fmt.Errorf(messages.TemplatesCLISkillCatalogEntryDuplicateMemberFmt, entry.ID, name)
			}
			seenSkills[name] = true
		}
	}
	return doc.CLISkills, nil
}

// SkillNames returns the declared destination names in selector order.
func (e CLISkillCatalogEntry) SkillNames() []string {
	if e.Repository != "" {
		names := make([]string, len(e.Selectors))
		for i, selector := range e.Selectors {
			names[i] = path.Base(selector)
		}
		return names
	}
	return []string{e.ID}
}

// GeneralSkillsRepository is the canonical source of the external catalog.
const GeneralSkillsRepository = "https://github.com/nicholasjconn/skills.git"

// RetiredSkillNames identifies protected legacy local slots; it contains no content.
var RetiredSkillNames = []string{"implement", "ship-pr", "auto-skill-loop", "audit-documentation", "audit-memory", "audit-tests", "interface-audit", "find-docs", "playwright", "tavily-web"}

// IsRetiredSkill reports names whose former bundled local slot must be preserved.
func IsRetiredSkill(name string) bool {
	for _, candidate := range RetiredSkillNames {
		if name == candidate {
			return true
		}
	}
	return false
}

func validateCatalogSource(e CLISkillCatalogEntry, read func(string) ([]byte, error)) error {
	if e.Repository == "" {
		if len(e.Selectors) > 0 {
			return fmt.Errorf("catalog %s mixes embedded and Git coordinates", e.ID)
		}
		if _, err := read("skills-catalog/" + e.ID + "/SKILL.md"); err != nil {
			return fmt.Errorf("catalog %s embedded skill: %w", e.ID, err)
		}
		return nil
	}
	u, err := url.Parse(e.Repository)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("catalog %s has invalid repository", e.ID)
	}
	if e.OwnershipMarker != "" || len(e.Selectors) == 0 {
		return fmt.Errorf("catalog %s has incomplete or mixed source coordinates", e.ID)
	}
	for _, selector := range e.Selectors {
		if selector == "" || path.IsAbs(selector) || path.Clean(selector) != selector || strings.ContainsAny(selector, "*?!\\[]\x00\r\n") || strings.Contains(selector, "..") || !IsSafeCLISkillCatalogID(path.Base(selector)) {
			return fmt.Errorf("catalog %s has unsafe or mismatched exact selector %q", e.ID, selector)
		}
	}
	return nil
}
