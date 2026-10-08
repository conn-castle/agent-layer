package templates

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCLISkillCatalog_EmbeddedHasExpectedEntries(t *testing.T) {
	entries, err := LoadCLISkillCatalog()
	require.NoError(t, err)
	require.Len(t, entries, 7)

	ids := make(map[string]CLISkillCatalogEntry, len(entries))
	for _, entry := range entries {
		ids[entry.ID] = entry
	}
	for _, want := range []string{"tavily-web", "playwright", "find-docs", "dispatch-agent", "skill-sync", "benchmark", "development-skills"} {
		_, ok := ids[want]
		assert.True(t, ok, "catalog should declare %s", want)
	}
	assert.Equal(t, "<!-- agent-layer-catalog-skill: skill-sync -->", ids["skill-sync"].OwnershipMarker)
	assert.Equal(t, "tvly", ids["tavily-web"].Binary)
	assert.Equal(t, "playwright-cli", ids["playwright"].Binary)
	assert.Equal(t, "npx", ids["find-docs"].Binary)
	assert.Empty(t, ids["dispatch-agent"].Binary)
	assert.Equal(t, "Agent dispatch (cross agent conversations)", ids["dispatch-agent"].Name)
	assert.Equal(t, "Skill sync (import and update skills from Git)", ids["skill-sync"].Name)
	assert.Equal(t, "Agent Layer benchmark", ids["benchmark"].Name)
	assert.Equal(t, "Development skills (/implement, /ship-pr, etc.)", ids["development-skills"].Name)
	assert.Equal(t, []string{
		"implement",
		"ship-pr",
		"auto-skill-loop",
		"audit-documentation",
		"audit-memory",
		"audit-tests",
		"interface-audit",
	}, ids["development-skills"].SkillNames())
}

func TestCatalogExternalSkillsHaveExactCoordinatesAndNoEmbeddedContent(t *testing.T) {
	entries, err := LoadCLISkillCatalog()
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.Repository == "" {
			_, err := Read("skills-catalog/" + entry.ID + "/SKILL.md")
			require.NoError(t, err)
			continue
		}
		assert.Equal(t, GeneralSkillsRepository, entry.Repository)
		for i, name := range entry.SkillNames() {
			group := "tools"
			if entry.ID == "development-skills" {
				group = "development"
			}
			assert.Equal(t, "skills/"+group+"/"+name, entry.Selectors[i])
			_, err := Read("skills/" + name + "/SKILL.md")
			require.Error(t, err)
			_, err = Read("skills-catalog/" + name + "/SKILL.md")
			require.Error(t, err)
		}
	}
}

func TestLoadCLISkillCatalog_ReadError(t *testing.T) {
	original := ReadFunc
	ReadFunc = func(string) ([]byte, error) {
		return nil, errors.New("mock read failure")
	}
	t.Cleanup(func() { ReadFunc = original })

	_, err := LoadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cli-skills-catalog.toml")
}

func TestCatalogRejectsInvalidRowsAndDerivedNameCollisions(t *testing.T) {
	for _, tc := range []struct {
		name, catalog, problem string
	}{
		{"empty", "# empty\n", "no entries"},
		{"missing id", "[[cli_skills]]\nname=\"X\"\n", "id"},
		{"missing name", "[[cli_skills]]\nid=\"x\"\n", "name"},
		{"invalid id", "[[cli_skills]]\nid=\"../escape\"\nname=\"Escape\"\n", "invalid id"},
		{"duplicate id", "[[cli_skills]]\nid=\"x\"\nname=\"X\"\n[[cli_skills]]\nid=\"x\"\nname=\"Other\"\n", "duplicates id"},
		{"duplicate name", "[[cli_skills]]\nid=\"x\"\nname=\"X\"\n[[cli_skills]]\nid=\"y\"\nname=\" X \"\n", "duplicates name"},
		{"duplicate derived name", "[[cli_skills]]\nid=\"pack\"\nname=\"Pack\"\nrepository=\"https://example.test/skills.git\"\nselectors=[\"a/x\",\"b/x\"]\n", "duplicates member"},
		{"derived name collides with row", "[[cli_skills]]\nid=\"x\"\nname=\"X\"\nrepository=\"https://example.test/skills.git\"\nselectors=[\"tools/x\"]\n[[cli_skills]]\nid=\"pack\"\nname=\"Pack\"\nrepository=\"https://example.test/skills.git\"\nselectors=[\"development/x\"]\n", "collides with catalog id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseCLISkillCatalog([]byte(tc.catalog), Read)
			require.ErrorContains(t, err, tc.problem)
		})
	}
}

func TestCatalogRejectsIncompleteAndMixedSources(t *testing.T) {
	for _, coordinates := range []string{
		`repository="https://github.com/nicholasjconn/skills.git"`,
		`selectors=["skills/tools/x"]`,
		`repository="../local"` + "\n" + `selectors=["skills/tools/x"]`,
		`repository="https://example.test/skills.git"` + "\n" + `selectors=["skills/*"]`,
		`repository="https://example.test/skills.git"` + "\n" + `selectors=["skills/tools/INVALID"]`,
		`repository="https://example.test/skills.git"` + "\n" + `selectors=["skills/tools/x"]` + "\n" + `ownership_marker="mixed"`,
	} {
		_, err := ParseCLISkillCatalog([]byte("[[cli_skills]]\nid=\"x\"\nname=\"X\"\n"+coordinates+"\n"), Read)
		require.Error(t, err, coordinates)
	}
}
