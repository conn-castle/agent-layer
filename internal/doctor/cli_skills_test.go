package doctor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skillimport"
	"github.com/conn-castle/agent-layer/internal/skilljournal"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestCheckCLISkillsRemoteOwnershipOfflineAndConflicts(t *testing.T) {
	source := testutil.CatalogGitFixture(t)
	for _, repository := range []string{templates.GeneralSkillsRepository, source} {
		t.Run(repository, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700))
			raw, err := templates.Read("config.toml")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "config.toml"), raw, 0o600))
			_, err = skillimport.New(root).Add(context.Background(), skillimport.AddOptions{Repository: repository, Selectors: []string{"skills/tools/playwright"}})
			require.NoError(t, err)
			originalLook := lookPathFunc
			lookPathFunc = func(string) (string, error) { return "", exec.ErrNotFound }
			t.Cleanup(func() { lookPathFunc = originalLook })
			t.Setenv("GIT_CONFIG_KEY_0", "url."+filepath.Join(t.TempDir(), "absent")+".insteadOf")
			cfg := &config.ProjectConfig{Root: root}
			results := CheckCLISkills(cfg)
			require.Len(t, results, 1)
			require.Contains(t, results[0].Message, "playwright")
			for _, dangling := range []bool{false, true} {
				stage := skilljournal.StagingRoot(filepath.Join(root, ".agent-layer", "skills-imported"))
				if dangling {
					require.NoError(t, os.Symlink(filepath.Join(root, "absent"), stage))
				} else {
					require.NoError(t, os.Mkdir(stage, 0o750))
					require.NoError(t, skilljournal.Write(stage, skilljournal.Document{Committed: true}))
				}
				before := testutil.SnapshotEvidence(t, root)
				pending := CheckCLISkills(cfg)
				require.Len(t, pending, 1)
				require.Contains(t, pending[0].Message, "pending recovery")
				require.Contains(t, pending[0].Recommendation, "al sync")
				require.Equal(t, before, testutil.SnapshotEvidence(t, root))
				require.NoError(t, os.RemoveAll(stage))
			}
			// A direct older installer can recreate this duplicate. Neither tree is removed.
			local := filepath.Join(root, ".agent-layer", "skills", "playwright")
			require.NoError(t, os.MkdirAll(local, 0o700))
			results = CheckCLISkills(cfg)
			require.Len(t, results, 2)
			require.Contains(t, results[0].Message, "both local and imported")
			require.DirExists(t, local)
			require.FileExists(t, filepath.Join(root, ".agent-layer", "skills-imported", "playwright", "SKILL.md"))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer", "skills.lock.json"), []byte("invalid"), 0o600))
			results = CheckCLISkills(cfg)
			require.Len(t, results, 1)
			require.Contains(t, results[0].Recommendation, "does not fetch")
		})
	}
}

func TestCheckCLISkills_NilConfigReturnsNil(t *testing.T) {
	results := CheckCLISkills(nil)
	assert.Nil(t, results)
}

func TestCheckCLISkills_CatalogLoadFailureEmitsSingleFail(t *testing.T) {
	original := loadCLISkillCatalogFunc
	loadCLISkillCatalogFunc = func() ([]templates.CLISkillCatalogEntry, error) {
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
	loadCLISkillCatalogFunc = func() ([]templates.CLISkillCatalogEntry, error) {
		return []templates.CLISkillCatalogEntry{{ID: "tavily-web", Binary: "tvly"}}, nil
	}
	t.Cleanup(func() { loadCLISkillCatalogFunc = originalCatalog })

	cfg := &config.ProjectConfig{Root: t.TempDir()}
	results := CheckCLISkills(cfg)
	assert.Empty(t, results, "absent catalog skill dir should produce no doctor results")
}

func TestCheckCLISkills_NoBinaryEntrySkipped(t *testing.T) {
	originalCatalog := loadCLISkillCatalogFunc
	loadCLISkillCatalogFunc = func() ([]templates.CLISkillCatalogEntry, error) {
		return []templates.CLISkillCatalogEntry{{ID: "dispatch-agent"}}, nil
	}
	t.Cleanup(func() { loadCLISkillCatalogFunc = originalCatalog })

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer", "skills", "dispatch-agent"), 0o750))
	cfg := &config.ProjectConfig{Root: root}
	results := CheckCLISkills(cfg)
	assert.Empty(t, results, "catalog entries without a binary contract are skipped")
}

func TestCheckCLISkills_BinaryFoundEmitsOK(t *testing.T) {
	originalCatalog := loadCLISkillCatalogFunc
	loadCLISkillCatalogFunc = func() ([]templates.CLISkillCatalogEntry, error) {
		return []templates.CLISkillCatalogEntry{{ID: "tavily-web", Binary: "tvly"}}, nil
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
	loadCLISkillCatalogFunc = func() ([]templates.CLISkillCatalogEntry, error) {
		return []templates.CLISkillCatalogEntry{{ID: "tavily-web", Binary: "tvly"}}, nil
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

func TestCheckCLISkills_PathPointsAtFileEmitsNoResult(t *testing.T) {
	originalCatalog := loadCLISkillCatalogFunc
	loadCLISkillCatalogFunc = func() ([]templates.CLISkillCatalogEntry, error) {
		return []templates.CLISkillCatalogEntry{{ID: "tavily-web", Binary: "tvly"}}, nil
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
