package config

import (
	"strings"
	"testing"
)

// TestSetSkillImportSelectorsPreservesUnrelatedConfiguration proves a selector
// edit rewrites nothing but the targeted block, keeping user comments,
// formatting, and unrelated tables byte-identical.
func TestSetSkillImportSelectorsPreservesUnrelatedConfiguration(t *testing.T) {
	t.Parallel()
	content := `# Top-level comment kept verbatim.
` + baseConfigTOML + `
[[mcp.servers]]
id = "example"      # unrelated array-of-tables
enabled = false
transport = "stdio"
command = "run"

[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = ["skills/a"]

# Trailing comment.
`
	identity := SkillImport{Repository: "https://example.test/skills.git"}.Identity()
	updated, err := SetSkillImportSelectors(content, identity, []string{"skills/a", "skills/b"})
	if err != nil {
		t.Fatalf("SetSkillImportSelectors: %v", err)
	}

	for _, preserved := range []string{
		"# Top-level comment kept verbatim.",
		`mode = "none"`,
		`id = "example"      # unrelated array-of-tables`,
		"# Trailing comment.",
	} {
		if !strings.Contains(updated, preserved) {
			t.Fatalf("edit dropped preserved content %q:\n%s", preserved, updated)
		}
	}
	if !strings.Contains(updated, `"skills/b"`) {
		t.Fatalf("new selector missing:\n%s", updated)
	}

	cfg, err := ParseConfig([]byte(updated), "config.toml")
	if err != nil {
		t.Fatalf("updated config does not parse: %v", err)
	}
	if got := strings.Join(cfg.Skills.Imports[0].Selectors, ","); got != "skills/a,skills/b" {
		t.Fatalf("selectors = %q", got)
	}
}

// TestSetSkillImportSelectorsAppendsAndRemovesBlocks proves a new policy
// creates one block with only the fields it needs, and clearing the selectors
// removes the block entirely.
func TestSetSkillImportSelectorsAppendsAndRemovesBlocks(t *testing.T) {
	t.Parallel()
	content := baseConfigTOML
	identity := SkillImport{
		Repository:  "https://example.test/skills.git",
		Ref:         "v1.0.0",
		Tracking:    SkillTrackingPinned,
		WritePolicy: SkillWritePolicyBranch,
		PushBranch:  "skill-updates",
	}.Identity()

	added, err := SetSkillImportSelectors(content, identity, []string{"skills/a"})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	cfg, err := ParseConfig([]byte(added), "config.toml")
	if err != nil {
		t.Fatalf("appended config does not parse: %v", err)
	}
	if len(cfg.Skills.Imports) != 1 || cfg.Skills.Imports[0].Identity() != identity {
		t.Fatalf("appended block identity mismatch: %+v", cfg.Skills.Imports)
	}
	if strings.Contains(added, "push_repository") {
		t.Fatalf("append emitted an unrequested optional field:\n%s", added)
	}

	removed, err := SetSkillImportSelectors(added, identity, nil)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if strings.Contains(removed, "skills.imports") {
		t.Fatalf("block was not removed:\n%s", removed)
	}
	if _, err := ParseConfig([]byte(removed), "config.toml"); err != nil {
		t.Fatalf("config after removal does not parse: %v", err)
	}
}

// TestSetSkillImportSelectorsTargetsOnlyTheMatchingBlock proves an edit
// distinguishes blocks that share a repository but differ in policy.
func TestSetSkillImportSelectorsTargetsOnlyTheMatchingBlock(t *testing.T) {
	t.Parallel()
	content := baseConfigTOML + `
[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = ["skills/tracked"]

[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = ["skills/pinned"]
ref = "v1.0.0"
tracking = "pinned"
`
	pinned := SkillImport{
		Repository: "https://example.test/skills.git",
		Ref:        "v1.0.0",
		Tracking:   SkillTrackingPinned,
	}.Identity()

	updated, err := SetSkillImportSelectors(content, pinned, []string{"skills/pinned", "skills/extra"})
	if err != nil {
		t.Fatalf("SetSkillImportSelectors: %v", err)
	}
	cfg, err := ParseConfig([]byte(updated), "config.toml")
	if err != nil {
		t.Fatalf("updated config does not parse: %v", err)
	}
	if got := strings.Join(cfg.Skills.Imports[0].Selectors, ","); got != "skills/tracked" {
		t.Fatalf("untouched block changed: %q", got)
	}
	if got := strings.Join(cfg.Skills.Imports[1].Selectors, ","); got != "skills/pinned,skills/extra" {
		t.Fatalf("targeted block = %q", got)
	}
}

// TestSetSkillImportSelectorsHandlesMultiLineArrays proves an existing
// multi-line selectors array is replaced as a unit rather than leaving orphaned
// array lines behind.
func TestSetSkillImportSelectorsHandlesMultiLineArrays(t *testing.T) {
	t.Parallel()
	content := baseConfigTOML + `
[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = [
  "skills/a",
  "skills/b",
]
ref = "main"
tracking = "tracked"
`
	identity := SkillImport{
		Repository: "https://example.test/skills.git",
		Ref:        "main",
		Tracking:   SkillTrackingTracked,
	}.Identity()

	updated, err := SetSkillImportSelectors(content, identity, []string{"skills/a"})
	if err != nil {
		t.Fatalf("SetSkillImportSelectors: %v", err)
	}
	if strings.Contains(updated, `"skills/b"`) {
		t.Fatalf("removed selector survived:\n%s", updated)
	}
	cfg, err := ParseConfig([]byte(updated), "config.toml")
	if err != nil {
		t.Fatalf("updated config does not parse: %v", err)
	}
	if got := strings.Join(cfg.Skills.Imports[0].Selectors, ","); got != "skills/a" {
		t.Fatalf("selectors = %q", got)
	}
	if !strings.Contains(updated, `ref = "main"`) {
		t.Fatalf("unrelated block fields were dropped:\n%s", updated)
	}
}

// TestSetSkillImportSelectorsIgnoresHeadersInsideStrings proves the line-aware
// editor does not mistake a table header embedded in a multiline string for a
// real block boundary.
func TestSetSkillImportSelectorsIgnoresHeadersInsideStrings(t *testing.T) {
	t.Parallel()
	content := baseConfigTOML + `
[[mcp.servers]]
id = "example"
enabled = false
transport = "stdio"
command = """
[[skills.imports]]
not a real block
"""

[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = ["skills/a"]
`
	identity := SkillImport{Repository: "https://example.test/skills.git"}.Identity()
	updated, err := SetSkillImportSelectors(content, identity, []string{"skills/a", "skills/c"})
	if err != nil {
		t.Fatalf("SetSkillImportSelectors: %v", err)
	}
	cfg, err := ParseConfig([]byte(updated), "config.toml")
	if err != nil {
		t.Fatalf("updated config does not parse: %v", err)
	}
	if len(cfg.Skills.Imports) != 1 {
		t.Fatalf("imports = %d, want 1", len(cfg.Skills.Imports))
	}
	if got := strings.Join(cfg.Skills.Imports[0].Selectors, ","); got != "skills/a,skills/c" {
		t.Fatalf("selectors = %q", got)
	}
	if !strings.Contains(updated, "not a real block") {
		t.Fatalf("multiline string content was damaged:\n%s", updated)
	}
}

// TestSetSkillImportSelectorsIsANoOpForAnAbsentBlock proves clearing selectors
// for a policy that is not configured changes nothing.
func TestSetSkillImportSelectorsIsANoOpForAnAbsentBlock(t *testing.T) {
	t.Parallel()
	identity := SkillImport{Repository: "https://example.test/absent.git"}.Identity()
	updated, err := SetSkillImportSelectors(baseConfigTOML, identity, nil)
	if err != nil {
		t.Fatalf("SetSkillImportSelectors: %v", err)
	}
	if updated != baseConfigTOML {
		t.Fatalf("content changed for an absent block:\n%s", updated)
	}
}

// TestSetSkillImportSelectorsRejectsUnusableBlocks proves a block the editor
// cannot rewrite safely fails loudly instead of producing damaged TOML.
func TestSetSkillImportSelectorsRejectsUnusableBlocks(t *testing.T) {
	t.Parallel()
	identity := SkillImport{Repository: "https://example.test/skills.git"}.Identity()

	noSelectors := baseConfigTOML + `
[[skills.imports]]
repository = "https://example.test/skills.git"
`
	if _, err := SetSkillImportSelectors(noSelectors, identity, []string{"skills/a"}); err == nil {
		t.Fatal("expected a block with no selectors assignment to fail")
	} else if !strings.Contains(err.Error(), "no selectors assignment") {
		t.Fatalf("error = %v", err)
	}

	unterminated := baseConfigTOML + `
[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = [
  "skills/a",
`
	if _, err := SetSkillImportSelectors(unterminated, identity, []string{"skills/a"}); err == nil {
		t.Fatal("expected an unterminated selectors array to fail")
	}

	twoBlocksOneHeader := baseConfigTOML + `
[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = ["skills/a"]
[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = ["skills/b"]
ref = "v1"
`
	updated, err := SetSkillImportSelectors(twoBlocksOneHeader, identity, []string{"skills/a", "skills/c"})
	if err != nil {
		t.Fatalf("adjacent blocks: %v", err)
	}
	if !strings.Contains(updated, `"skills/c"`) || !strings.Contains(updated, `"skills/b"`) {
		t.Fatalf("adjacent blocks were not edited independently:\n%s", updated)
	}
}
