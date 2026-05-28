package doctor

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestCheckCLISkills_NilConfigReturnsNil(t *testing.T) {
	results := CheckCLISkills(nil)
	assert.Nil(t, results)
}

func TestCheckCLISkills_CatalogLoadFailureEmitsSingleFail(t *testing.T) {
	original := loadCLISkillCatalogFunc
	loadCLISkillCatalogFunc = func() ([]cliSkillCatalogEntry, error) {
		return nil, errors.New("mock catalog load failure")
	}
	t.Cleanup(func() { loadCLISkillCatalogFunc = original })

	cfg := &config.ProjectConfig{Root: t.TempDir()}
	results := CheckCLISkills(cfg)
	require.Len(t, results, 1)
	assert.Equal(t, StatusFail, results[0].Status)
	assert.Contains(t, results[0].Message, "mock catalog load failure")
}

func TestCheckCLISkills_AbsentSkillDirEmitsNothing(t *testing.T) {
	originalCatalog := loadCLISkillCatalogFunc
	loadCLISkillCatalogFunc = func() ([]cliSkillCatalogEntry, error) {
		return []cliSkillCatalogEntry{{ID: "tavily-web", Binary: "tvly"}}, nil
	}
	t.Cleanup(func() { loadCLISkillCatalogFunc = originalCatalog })

	cfg := &config.ProjectConfig{Root: t.TempDir()}
	results := CheckCLISkills(cfg)
	assert.Empty(t, results, "absent catalog skill dir should produce no doctor results")
}

func TestCheckCLISkills_NoBinaryEntrySkipped(t *testing.T) {
	originalCatalog := loadCLISkillCatalogFunc
	loadCLISkillCatalogFunc = func() ([]cliSkillCatalogEntry, error) {
		return []cliSkillCatalogEntry{{ID: "agent-dispatch"}}, nil
	}
	t.Cleanup(func() { loadCLISkillCatalogFunc = originalCatalog })

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "agent-dispatch"), 0o750))
	cfg := &config.ProjectConfig{Root: root}
	results := CheckCLISkills(cfg)
	assert.Empty(t, results, "catalog entries without a binary contract are skipped")
}

func TestCheckCLISkills_BinaryFoundEmitsOK(t *testing.T) {
	originalCatalog := loadCLISkillCatalogFunc
	loadCLISkillCatalogFunc = func() ([]cliSkillCatalogEntry, error) {
		return []cliSkillCatalogEntry{{ID: "tavily-web", Binary: "tvly"}}, nil
	}
	t.Cleanup(func() { loadCLISkillCatalogFunc = originalCatalog })

	originalLook := lookPathFunc
	lookPathFunc = func(name string) (string, error) {
		if name == "tvly" {
			return "/usr/local/bin/tvly", nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookPathFunc = originalLook })

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "tavily-web"), 0o750))
	cfg := &config.ProjectConfig{Root: root}
	results := CheckCLISkills(cfg)
	require.Len(t, results, 1)
	assert.Equal(t, StatusOK, results[0].Status)
	assert.Contains(t, results[0].Message, "tvly")
}

func TestCheckCLISkills_BinaryMissingEmitsFail(t *testing.T) {
	originalCatalog := loadCLISkillCatalogFunc
	loadCLISkillCatalogFunc = func() ([]cliSkillCatalogEntry, error) {
		return []cliSkillCatalogEntry{{ID: "tavily-web", Binary: "tvly"}}, nil
	}
	t.Cleanup(func() { loadCLISkillCatalogFunc = originalCatalog })

	originalLook := lookPathFunc
	lookPathFunc = func(string) (string, error) {
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookPathFunc = originalLook })

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "tavily-web"), 0o750))
	cfg := &config.ProjectConfig{Root: root}
	results := CheckCLISkills(cfg)
	require.Len(t, results, 1)
	assert.Equal(t, StatusFail, results[0].Status)
	assert.Contains(t, results[0].Message, "tvly")
	assert.NotEmpty(t, results[0].Recommendation)
}

func TestCheckCLISkills_EmbeddedCatalogLoads(t *testing.T) {
	// The default loadCLISkillCatalogFunc reads the embedded TOML. Exercise it
	// here to lift coverage on the default path.
	entries, err := loadCLISkillCatalogFunc()
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
}

func TestLoadCLISkillCatalogForDoctor_InvalidID(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillCatalogTemplatePath {
			return []byte("[[cli_skills]]\nid = \"../escape\"\nbinary = \"tvly\"\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalogFunc()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid id")
}

func TestLoadCLISkillCatalogForDoctor_DuplicateID(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == cliSkillCatalogTemplatePath {
			return []byte("[[cli_skills]]\nid = \"tavily-web\"\nbinary = \"tvly\"\n\n[[cli_skills]]\nid = \"tavily-web\"\nbinary = \"tvly\"\n"), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCLISkillCatalogFunc()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicates id")
}

func TestCheckCLISkills_PathPointsAtFileEmitsNoResult(t *testing.T) {
	originalCatalog := loadCLISkillCatalogFunc
	loadCLISkillCatalogFunc = func() ([]cliSkillCatalogEntry, error) {
		return []cliSkillCatalogEntry{{ID: "tavily-web", Binary: "tvly"}}, nil
	}
	t.Cleanup(func() { loadCLISkillCatalogFunc = originalCatalog })

	root := t.TempDir()
	skillFile := filepath.Join(root, ".agent-layer", "skills", "tavily-web")
	require.NoError(t, os.MkdirAll(filepath.Dir(skillFile), 0o750))
	require.NoError(t, os.WriteFile(skillFile, []byte("x"), 0o600))

	cfg := &config.ProjectConfig{Root: root}
	results := CheckCLISkills(cfg)
	assert.Empty(t, results, "a file (not a directory) at the catalog skill path is treated as absent")
}
