package wizard

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestLoadCLISkillCatalog_EmbeddedHasExpectedEntries(t *testing.T) {
	entries, err := loadCLISkillCatalog()
	require.NoError(t, err)
	require.Len(t, entries, 6)

	ids := make(map[string]CLISkillCatalogEntry, len(entries))
	for _, entry := range entries {
		ids[entry.ID] = entry
	}
	for _, want := range []string{"tavily-web", "playwright", "find-docs", "dispatch-agent", "skill-sync", "development-skills"} {
		_, ok := ids[want]
		assert.True(t, ok, "catalog should declare %s", want)
	}
	assert.Equal(t, "<!-- agent-layer-catalog-skill: skill-sync -->", ids["skill-sync"].OwnershipMarker)
	assert.Equal(t, "Agent dispatch (cross agent conversations)", ids["dispatch-agent"].Name)
	assert.Equal(t, "Skill sync (import and update skills from Git)", ids["skill-sync"].Name)
	assert.Equal(t, "Agent Layer development skills (/implement, /ship-pr, etc.)", ids["development-skills"].Name)
	assert.Equal(t, []string{
		"implement",
		"ship-pr",
		"auto-skill-loop",
		"audit-documentation",
		"audit-memory",
		"audit-tests",
		"interface-audit",
	}, ids["development-skills"].Members)
}

func TestLoadCLISkillCatalog_DevelopmentSkillsMembersMatchEmbeddedWorkflowSkills(t *testing.T) {
	entries, err := loadCLISkillCatalog()
	require.NoError(t, err)
	var members []string
	for _, entry := range entries {
		if entry.ID == "development-skills" {
			members = append(members, entry.Members...)
		}
	}
	embedded, err := embeddedWorkflowSkillIDs()
	require.NoError(t, err)
	assert.Len(t, members, len(embedded))
	for _, member := range members {
		_, ok := embedded[member]
		assert.True(t, ok, "catalog member %s should be an embedded workflow skill", member)
	}
}

func TestLoadCLISkillCatalog_ReadError(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(string) ([]byte, error) {
		return nil, errors.New("mock read failure")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cli-skills-catalog.toml")
}

func TestLoadCLISkillCatalog_EmptyDoc(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillsCatalogTemplatePath {
			return []byte("# empty\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no entries")
}

func TestLoadCLISkillCatalog_MissingID(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillsCatalogTemplatePath {
			return []byte("[[cli_skills]]\nname = \"X\"\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id")
}

func TestLoadCLISkillCatalog_MissingName(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillsCatalogTemplatePath {
			return []byte("[[cli_skills]]\nid = \"x\"\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestLoadCLISkillCatalog_InvalidID(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillsCatalogTemplatePath {
			return []byte("[[cli_skills]]\nid = \"../escape\"\nname = \"Escape\"\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid id")
}

func TestLoadCLISkillCatalog_DuplicateID(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillsCatalogTemplatePath {
			return []byte("[[cli_skills]]\nid = \"find-docs\"\nname = \"Find Docs\"\n\n[[cli_skills]]\nid = \"find-docs\"\nname = \"Duplicate\"\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicates id")
}

func TestLoadCLISkillCatalog_DuplicateName(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillsCatalogTemplatePath {
			return []byte("[[cli_skills]]\nid = \"find-docs\"\nname = \"Find Docs\"\n\n[[cli_skills]]\nid = \"tavily-web\"\nname = \" Find Docs \"\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicates name")
}

func TestLoadCLISkillCatalog_InvalidMember(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillsCatalogTemplatePath {
			return []byte("[[cli_skills]]\nid = \"pack\"\nname = \"Pack\"\nmembers = [\"../escape\"]\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid member")
}

func TestLoadCLISkillCatalog_DuplicateMember(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillsCatalogTemplatePath {
			return []byte("[[cli_skills]]\nid = \"pack\"\nname = \"Pack\"\nmembers = [\"implement\", \"implement\"]\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicates member")
}

func TestLoadCLISkillCatalog_MemberCollidesWithCatalogID(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillsCatalogTemplatePath {
			return []byte("[[cli_skills]]\nid = \"tavily-web\"\nname = \"Tavily\"\n\n[[cli_skills]]\nid = \"pack\"\nname = \"Pack\"\nmembers = [\"tavily-web\"]\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collides with catalog id")
}

func TestLoadCLISkillCatalog_MemberMissingTemplate(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillsCatalogTemplatePath {
			return []byte("[[cli_skills]]\nid = \"pack\"\nname = \"Pack\"\nmembers = [\"not-a-real-skill\"]\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalog()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no embedded")
}
