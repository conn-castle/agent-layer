package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/templates"
)

// CatalogGitFixture provides minimal local Git content for remote product tests.
// The production URL is redirected only through Git's inherited configuration.
func CatalogGitFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		cmd := exec.CommandContext(t.Context(), "git", args...) // #nosec G204 -- test fixture uses explicit Git arguments and a test-owned cwd.
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "--quiet", "--initial-branch=main")
	catalog, err := templates.LoadCLISkillCatalog()
	require.NoError(t, err)
	// A wildcard also selects the unrelated tree; exact catalog imports must exclude it.
	selectors := []string{"skills/development/pr-retrospective"}
	for _, entry := range catalog {
		selectors = append(selectors, entry.Selectors...)
	}
	for _, selector := range selectors {
		name := filepath.Base(selector)
		dir := filepath.Join(root, selector, "resources")
		require.NoError(t, os.MkdirAll(dir, 0o750))
		text := fmt.Sprintf("---\nname: %s\ndescription: Hermetic catalog fixture.\n---\n\nFixture upstream %s.\n", name, name)
		require.NoError(t, os.WriteFile(filepath.Join(root, selector, "SKILL.md"), []byte(text), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "example.txt"), []byte("resource\n"), 0o644))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "instructions"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "instructions", "rules.md"), []byte("Follow project conventions. Validate your changes.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "instructions", "memory.md"), []byte("Read docs/agent-layer/CONTEXT.md.\n"), 0o644))
	run("add", ".")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "fixture")
	count, _ := strconv.Atoi(os.Getenv("GIT_CONFIG_COUNT"))
	t.Setenv(fmt.Sprintf("GIT_CONFIG_KEY_%d", count), "url."+root+".insteadOf")
	t.Setenv(fmt.Sprintf("GIT_CONFIG_VALUE_%d", count), templates.GeneralSkillsRepository)
	t.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(count+1))
	return root
}
