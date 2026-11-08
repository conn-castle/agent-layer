package skillimport

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/skillmigration"
)

func TestInstructionsExactFileLifecycleAndNumericProjection(t *testing.T) {
	repo := newGitRepo(t, "main")
	repo.WriteFile("instructions/rules.md", "first\nsecond\nthird\n", 0o644)
	repo.WriteFile("instructions/memory.md", "memory\n", 0o644)
	repo.Commit("instructions")
	proj := newProject(t)
	require.NoError(t, os.Rename(filepath.Join(proj.paths.InstructionsDir, "00_rules.md"), filepath.Join(proj.paths.InstructionsDir, "conventions.md")))
	proj.ReplaceInConfig("00_rules.md", "conventions.md")
	require.NoError(t, os.WriteFile(filepath.Join(proj.paths.InstructionsDir, "notes.txt"), []byte("ignored"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(proj.paths.InstructionsDir, "archive.md"), 0o750))
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(proj.paths.InstructionsDir, "assets")))
	service := NewInstructions(proj.root)
	require.ErrorContains(t, skillmigration.CheckVersion(context.Background(), proj.root, "0.23.1"), "explicitly ordered instructions")
	order := 40
	report, err := service.Add(t.Context(), AddOptions{Repository: repo.dir, Selectors: []string{"instructions/rules.md"}, Order: &order, WritePolicy: config.SkillWritePolicyDirect})
	require.NoError(t, err)
	require.False(t, report.Failed(), report.Render("add"))
	order = 10
	report, err = service.Add(t.Context(), AddOptions{Repository: repo.dir, Selectors: []string{"instructions/memory.md"}, Order: &order})
	require.NoError(t, err)
	require.False(t, report.Failed(), report.Render("add"))
	beforeConfig, err := os.ReadFile(proj.paths.ConfigPath)
	require.NoError(t, err)
	_, err = service.Add(t.Context(), AddOptions{Repository: repo.dir, Selectors: []string{"instructions/extra.md"}, Order: &order})
	require.ErrorContains(t, err, "duplicate instruction order")
	afterConfig, err := os.ReadFile(proj.paths.ConfigPath)
	require.NoError(t, err)
	require.Equal(t, beforeConfig, afterConfig)
	project, err := config.LoadProjectConfig(proj.root)
	require.NoError(t, err)
	require.Equal(t, []string{"conventions.md", "memory.md", "rules.md"}, []string{project.Instructions[0].Name, project.Instructions[1].Name, project.Instructions[2].Name})
	local := filepath.Join(proj.paths.ImportedInstructionsDir, "rules.md")
	require.NoError(t, os.WriteFile(local, []byte("local\nsecond\nthird\n"), 0o600))
	repo.WriteFile("instructions/rules.md", "first\nsecond\nremote\n", 0o644)
	repo.Commit("advance")
	report, err = service.Pull(t.Context())
	require.NoError(t, err)
	require.False(t, report.Failed(), report.Render("pull"))
	bytes, err := os.ReadFile(local) // #nosec G304 -- test-owned instruction paths.
	require.NoError(t, err)
	require.Equal(t, "local\nsecond\nremote\n", string(bytes))
	report, err = service.Push(t.Context())
	require.NoError(t, err)
	require.False(t, report.Failed(), report.Render("push"))
	bytes, err = os.ReadFile(filepath.Join(repo.dir, "instructions", "rules.md"))
	require.NoError(t, err)
	require.Contains(t, string(bytes), "local")
	bytes, err = os.ReadFile(filepath.Join(repo.dir, "README.md"))
	require.NoError(t, err)
	require.Equal(t, "seed\n", string(bytes))
	bytes, err = os.ReadFile(filepath.Join(repo.dir, "instructions", "memory.md"))
	require.NoError(t, err)
	require.Equal(t, "memory\n", string(bytes))
	_, err = os.Stat(filepath.Join(repo.dir, "instructions", "conventions.md"))
	require.True(t, os.IsNotExist(err))
	// One real content conflict uses the existing Git workspace/index resolution.
	require.NoError(t, os.WriteFile(local, []byte("conflicting local\nsecond\nremote\n"), 0o600))
	repo.WriteFile("instructions/rules.md", "conflicting remote\nsecond\nremote\n", 0o644)
	repo.Commit("conflict")
	report, err = service.Pull(t.Context())
	require.NoError(t, err)
	require.True(t, report.Failed())
	status, err := service.Status()
	require.NoError(t, err)
	var workspace string
	for _, entry := range status.Entries {
		if entry.Name == "rules.md" {
			require.Equal(t, ConditionConflicted, entry.Condition)
			workspace = filepath.Join(proj.root, entry.Workspace)
		}
	}
	require.NotEmpty(t, workspace)
	git := &gitRepo{t: t, dir: workspace}
	git.WriteFile("rules.md", "resolved\n", 0o644)
	git.run("add", "rules.md")
	report, err = service.Resolve(t.Context(), "rules.md")
	require.NoError(t, err)
	require.False(t, report.Failed(), report.Render("resolve"))
	linkedRoot, outside := t.TempDir(), t.TempDir()
	require.NoError(t, os.Symlink(outside, filepath.Join(linkedRoot, ".agent-layer")))
	_, err = NewInstructions(linkedRoot).Status()
	require.ErrorContains(t, err, "unlinked")
	_, err = os.Stat(filepath.Join(outside, "sync.lock"))
	require.True(t, os.IsNotExist(err), "unsafe root must fail before even creating a lock")
}
