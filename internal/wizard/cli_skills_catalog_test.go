package wizard

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestLoadCLISkillCatalog_EmbeddedHasFourEntries(t *testing.T) {
	entries, err := loadCLISkillCatalog()
	require.NoError(t, err)
	require.Len(t, entries, 4)

	ids := make(map[string]CLISkillCatalogEntry, len(entries))
	for _, entry := range entries {
		ids[entry.ID] = entry
	}
	for _, want := range []string{"tavily-web", "playwright-cli", "find-docs", "agent-dispatch"} {
		_, ok := ids[want]
		assert.True(t, ok, "catalog should declare %s", want)
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
