//go:build tools
// +build tools

package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
// be absent from the release manifest used for offline upgrade source-version
// inference.
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

func TestManifestFilesRejectsDuplicateDestinations(t *testing.T) {
	_, err := manifestFiles([]templateSource{
		{templatePath: "skills/x/SKILL.md", content: []byte("a"), dests: []string{".agent-layer/skills/x/SKILL.md"}},
		{templatePath: "skills-catalog/x/SKILL.md", content: []byte("b"), dests: []string{".agent-layer/skills/x/SKILL.md"}},
	})
	require.ErrorContains(t, err, "duplicate destination path .agent-layer/skills/x/SKILL.md")
}

func TestCatalogSkillPathPrefixesDeriveFromCatalog(t *testing.T) {
	root := t.TempDir()
	writeRootFile(t, root, "internal/templates/cli-skills-catalog.toml", `
[[cli_skills]]
id = "custom-cli"
name = "Custom CLI"

[[cli_skills]]
id = "another-tool"
name = "Another Tool"

[[cli_skills]]
id = "development-skills"
name = "Development skills"
members = ["implement", "ship-pr"]
`)

	prefixes, err := catalogSkillPathPrefixes(root)
	require.NoError(t, err)

	assert.Equal(t, []string{
		".agent-layer/skills/another-tool/",
		".agent-layer/skills/custom-cli/",
	}, prefixes)
}

func TestCatalogSkillPathPrefixesRejectsInvalidCatalogs(t *testing.T) {
	cases := []struct {
		name    string
		catalog *string
		want    string
	}{
		{name: "missing"},
		{name: "malformed", catalog: ptr("[[cli_skills]\n"), want: "toml"},
		{name: "empty", catalog: ptr(""), want: "CLI skills catalog contains no entries"},
		{name: "invalid id", catalog: ptr("[[cli_skills]]\nid = \"INVALID ID\"\n"), want: `entry 0 has invalid id "INVALID ID"`},
		{name: "duplicate id", catalog: ptr("[[cli_skills]]\nid = \"a\"\n[[cli_skills]]\nid = \"a\"\n"), want: `entry 1 duplicates id "a"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.catalog != nil {
				writeRootFile(t, root, "internal/templates/cli-skills-catalog.toml", *tc.catalog)
			}
			_, err := catalogSkillPathPrefixes(root)
			if tc.catalog == nil {
				require.ErrorIs(t, err, fs.ErrNotExist)
				return
			}
			require.ErrorContains(t, err, tc.want)
		})
	}
}

// TestBuildManifestClassifiesFromRootCatalog pins that --repo-root supplies
// catalog membership as well as template bytes: only the root catalog's
// non-group ids are catalog skills, with no fallback to the catalog or legacy
// prefixes compiled into install.
func TestBuildManifestClassifiesFromRootCatalog(t *testing.T) {
	cases := []struct {
		name    string
		catalog string
		want    map[string]string
	}{
		{
			name:    "custom catalog",
			catalog: "[[cli_skills]]\nid = \"custom-cli\"\n[[cli_skills]]\nid = \"group\"\nmembers = [\"x\"]\n",
			want: map[string]string{
				".agent-layer/skills/custom-cli/SKILL.md":     "catalog_skills_v1",
				".agent-layer/skills/agent-dispatch/SKILL.md": "",
				".agent-layer/skills/tavily-web/SKILL.md":     "",
				".agent-layer/commands.allow":                 "allowlist_lines_v1",
			},
		},
		{
			name:    "groups only",
			catalog: "[[cli_skills]]\nid = \"group\"\nmembers = [\"custom-cli\"]\n",
			want: map[string]string{
				".agent-layer/skills/custom-cli/SKILL.md":     "",
				".agent-layer/skills/agent-dispatch/SKILL.md": "",
				".agent-layer/skills/tavily-web/SKILL.md":     "",
				".agent-layer/commands.allow":                 "allowlist_lines_v1",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeRootFile(t, root, "internal/templates/cli-skills-catalog.toml", tc.catalog)
			writeRootFile(t, root, "internal/templates/commands.allow", "git status\n")
			writeRootFile(t, root, "internal/templates/skills-catalog/custom-cli/SKILL.md", "custom")
			// Core skills under prefixes install still classifies at runtime
			// (a legacy id and a current catalog id) but absent from this catalog.
			writeRootFile(t, root, "internal/templates/skills/agent-dispatch/SKILL.md", "core")
			writeRootFile(t, root, "internal/templates/skills/tavily-web/SKILL.md", "core")

			data, err := buildManifest(root, "1.0.0", time.Unix(0, 0))
			require.NoError(t, err)
			assert.Equal(t, tc.want, manifestPolicies(t, data))
		})
	}
}

func TestBuildManifestRequiresRootCatalog(t *testing.T) {
	root := t.TempDir()
	writeRootFile(t, root, "internal/templates/commands.allow", "git status\n")

	_, err := buildManifest(root, "1.0.0", time.Unix(0, 0))
	require.ErrorContains(t, err, "load CLI skills catalog prefixes")
}

func TestReleaseManifestClassifiesCollectedTemplates(t *testing.T) {
	root := repoRootForTest(t)
	sources, err := collectTemplateSources(root)
	require.NoError(t, err)
	data, err := buildManifest(root, "1.0.0", time.Unix(0, 0))
	require.NoError(t, err)

	policies := manifestPolicies(t, data)
	for _, source := range sources {
		var wantPolicy string
		switch {
		case strings.HasPrefix(source.templatePath, "skills-catalog/"):
			wantPolicy = "catalog_skills_v1"
		case strings.HasPrefix(source.templatePath, "skills/"):
			wantPolicy = ""
		default:
			continue
		}
		for _, dest := range source.dests {
			gotPolicy, ok := policies[dest]
			require.True(t, ok, "manifest missing %s", dest)
			assert.Equal(t, wantPolicy, gotPolicy, "policy for %s (from %s)", dest, source.templatePath)
		}
	}
	assert.Equal(t, "allowlist_lines_v1", policies[".agent-layer/commands.allow"])
	assert.Equal(t, "memory_entries_v1", policies["docs/agent-layer/ISSUES.md"])
}

// manifestPolicies decodes an encoded manifest into path -> policy_id.
func manifestPolicies(t *testing.T, data []byte) map[string]string {
	t.Helper()
	var manifest struct {
		Files []struct {
			Path     string `json:"path"`
			PolicyID string `json:"policy_id"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal(data, &manifest))
	policies := make(map[string]string, len(manifest.Files))
	for _, file := range manifest.Files {
		policies[file.Path] = file.PolicyID
	}
	return policies
}

func writeRootFile(t *testing.T, root string, relPath string, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relPath))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func ptr(s string) *string { return &s }
