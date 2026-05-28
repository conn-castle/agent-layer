package install

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
			path: ".agent-layer/skills/review-scope/SKILL.md",
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
