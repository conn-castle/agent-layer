//go:build tools
// +build tools

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogSkillPathPrefixesDeriveFromCatalog(t *testing.T) {
	root := t.TempDir()
	catalogPath := filepath.Join(root, "internal", "templates", "cli-skills-catalog.toml")
	require.NoError(t, os.MkdirAll(filepath.Dir(catalogPath), 0o750))
	require.NoError(t, os.WriteFile(catalogPath, []byte(`
[[cli_skills]]
id = "custom-cli"
name = "Custom CLI"

[[cli_skills]]
id = "another-tool"
name = "Another Tool"
`), 0o600))

	prefixes, err := catalogSkillPathPrefixes(root)
	require.NoError(t, err)

	assert.Equal(t, []string{
		".agent-layer/skills/another-tool/",
		".agent-layer/skills/custom-cli/",
	}, prefixes)
}

// repoRootForTest walks up from the test's working directory until it finds the
// repository root (the directory containing go.mod), so the completeness test
// can read the real embedded template tree the generator ships.
func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root (go.mod) from working directory")
		}
		dir = parent
	}
}

// TestCollectTemplateSourcesCoversManagedPartition guards the generator's
// hardcoded managed-file set against drift from the embedded template tree.
//
// The manifest only governs upgrade-managed templates; user-owned seed files
// (config.toml, env) and agent-internal files are deliberately excluded. Today
// that partition lives implicitly inside collectTemplateSources' hardcoded
// rootFiles/dirs lists, with nothing asserting it stays exhaustive. A newly
// added upgrade-managed template that someone forgets to wire in would silently
// degrade to the runtime OwnershipUnknownNoBaseline fallback (a user-facing
// "unknown" prompt) instead of being tracked.
//
// This test independently re-derives the partition by walking the real template
// tree and classifying every template file as managed or excluded via an
// explicit allow/deny list maintained here. It then asserts collectTemplateSources
// returns exactly the managed set. The check fails loudly when:
//   - a new template file matches neither partition (forces a conscious
//     managed-vs-excluded decision), or
//   - the generator's collected set diverges from the walk-derived managed set
//     (a forgotten or extra wiring).
func TestCollectTemplateSourcesCoversManagedPartition(t *testing.T) {
	root := repoRootForTest(t)
	templateRoot := filepath.Join(root, "internal", "templates")

	// Managed partition: must match collectTemplateSources' rootFiles + dirs.
	managedRootFiles := map[string]struct{}{
		"commands.allow":  {},
		"gitignore.block": {},
	}
	managedDirPrefixes := []string{
		"instructions/",
		"skills/",
		"skills-catalog/",
		"docs/agent-layer/",
	}

	// Excluded partition: every template file intentionally kept out of the
	// manifest. User-owned seed files and agent-internal/runtime-only files.
	excludedRootFiles := map[string]struct{}{
		"agent-layer.gitignore":   {},
		"claude-statusline.sh":    {},
		"cli-skills-catalog.toml": {},
		"codex-statusline.toml":   {},
		"config.toml":             {},
		"env":                     {},
		"mcp-catalog.toml":        {},
	}
	excludedDirPrefixes := []string{
		"launchers/",
		"manifests/",
		"migrations/",
	}

	isManaged := func(rel string) bool {
		if _, ok := managedRootFiles[rel]; ok {
			return true
		}
		for _, prefix := range managedDirPrefixes {
			if strings.HasPrefix(rel, prefix) {
				return true
			}
		}
		return false
	}
	isExcluded := func(rel string) bool {
		if _, ok := excludedRootFiles[rel]; ok {
			return true
		}
		for _, prefix := range excludedDirPrefixes {
			if strings.HasPrefix(rel, prefix) {
				return true
			}
		}
		// docs/ holds only docs/agent-layer/ as managed; any other docs/ path is
		// excluded.
		if strings.HasPrefix(rel, "docs/") && !strings.HasPrefix(rel, "docs/agent-layer/") {
			return true
		}
		return false
	}

	var walkManaged []string
	err := filepath.WalkDir(templateRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(templateRoot, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		// .go files are package source, not templates.
		if strings.HasSuffix(rel, ".go") {
			return nil
		}
		managed := isManaged(rel)
		excluded := isExcluded(rel)
		switch {
		case managed && excluded:
			t.Fatalf("template %q is classified as both managed and excluded; fix the partition lists", rel)
		case !managed && !excluded:
			t.Fatalf("template %q matches neither the managed nor excluded partition; decide whether it is upgrade-managed and update collectTemplateSources plus this test's partition lists", rel)
		case managed:
			walkManaged = append(walkManaged, rel)
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, walkManaged, "expected at least one managed template")
	sort.Strings(walkManaged)

	sources, err := collectTemplateSources(root)
	require.NoError(t, err)
	collected := make([]string, 0, len(sources))
	for _, source := range sources {
		collected = append(collected, source.templatePath)
	}
	sort.Strings(collected)

	assert.Equal(t, walkManaged, collected, "collectTemplateSources must collect exactly the walk-derived managed template set")
}

func TestBuildManifestEntriesClassifiesCatalogSkillsFromDerivedPrefixes(t *testing.T) {
	entries, err := buildManifestEntries([]templateSource{{
		templatePath: "skills-catalog/custom-cli/SKILL.md",
		content:      []byte("skill body"),
		dests:        []string{".agent-layer/skills/custom-cli/SKILL.md"},
	}}, []string{".agent-layer/skills/custom-cli/"})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, policyCatalogSkills, entries[0].PolicyID)
}
