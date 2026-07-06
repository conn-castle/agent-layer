package install

import (
	"sort"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestOwnershipPolicyForPath_CatalogSkills(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "tavily-web SKILL.md classified as catalog_skills_v1",
			path: ".agent-layer/skills/tavily-web/SKILL.md",
			want: ownershipPolicyCatalogSkills,
		},
		{
			name: "playwright-cli nested file classified",
			path: ".agent-layer/skills/playwright-cli/templates/something.md",
			want: ownershipPolicyCatalogSkills,
		},
		{
			name: "find-docs classified",
			path: ".agent-layer/skills/find-docs/SKILL.md",
			want: ownershipPolicyCatalogSkills,
		},
		{
			name: "agent-dispatch classified",
			path: ".agent-layer/skills/agent-dispatch/SKILL.md",
			want: ownershipPolicyCatalogSkills,
		},
		{
			name: "non-catalog workflow skill not classified",
			path: ".agent-layer/skills/review-code/SKILL.md",
			want: "",
		},
		{
			name: "ROADMAP still memory_roadmap_v1",
			path: "docs/agent-layer/ROADMAP.md",
			want: ownershipPolicyMemoryRoadmap,
		},
		{
			name: "ISSUES still memory_entries_v1",
			path: "docs/agent-layer/ISSUES.md",
			want: ownershipPolicyMemoryEntries,
		},
		{
			name: "commands.allow still allowlist_lines_v1",
			path: commandsAllowRelPath,
			want: ownershipPolicyAllowlist,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, ownershipPolicyForPath(c.path))
		})
	}
}

func TestCatalogSkillRelPathPrefixesMatchEmbeddedCatalog(t *testing.T) {
	data, err := templates.Read("cli-skills-catalog.toml")
	require.NoError(t, err)
	var catalog struct {
		CLISkills []struct {
			ID string `toml:"id"`
		} `toml:"cli_skills"`
	}
	require.NoError(t, toml.Unmarshal(data, &catalog))
	require.NotEmpty(t, catalog.CLISkills)

	want := make([]string, 0, len(catalog.CLISkills))
	for _, entry := range catalog.CLISkills {
		require.NotEmpty(t, entry.ID)
		want = append(want, ".agent-layer/skills/"+entry.ID+"/")
	}
	got := append([]string(nil), catalogSkillRelPathPrefixes...)
	sort.Strings(want)
	sort.Strings(got)

	assert.Equal(t, want, got)
	for _, prefix := range got {
		assert.Equal(t, ownershipPolicyCatalogSkills, ownershipPolicyForPath(prefix+"SKILL.md"))
	}
}
