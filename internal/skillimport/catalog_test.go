package skillimport

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skilllock"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func developmentEntry(t *testing.T) templates.CLISkillCatalogEntry {
	t.Helper()
	catalog, err := templates.LoadCLISkillCatalog()
	require.NoError(t, err)
	for _, entry := range catalog {
		if entry.ID == "development-skills" {
			return entry
		}
	}
	t.Fatal("missing development catalog")
	return templates.CLISkillCatalogEntry{}
}

func TestCatalogForeignOwnershipRequiresUnambiguousConfiguration(t *testing.T) {
	block := config.SkillImport{Repository: "https://example.invalid/fork", Selectors: []string{"skills/tools/playwright"}}
	locked := skilllock.Entry{Name: "playwright", Repository: block.Repository, SelectedPath: block.Selectors[0]}
	st := &state{cfg: &config.Config{}, lock: skilllock.New(), local: map[string]localSkill{"playwright": {Name: "playwright", Present: true}}}
	st.lock.Upsert(locked)
	entry := templates.CLISkillCatalogEntry{Repository: templates.GeneralSkillsRepository, Selectors: block.Selectors}
	for _, imports := range [][]config.SkillImport{{block}, nil, {block, {Repository: entry.Repository, Selectors: entry.Selectors}}} {
		st.cfg.Skills.Imports = imports
		require.Equal(t, len(imports) == 1, catalogMember(st, entry.Repository, entry.Selectors[0]).ForeignOwned)
	}
}
