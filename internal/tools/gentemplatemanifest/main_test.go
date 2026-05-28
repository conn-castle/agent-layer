//go:build tools
// +build tools

package main

import (
	"os"
	"path/filepath"
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
