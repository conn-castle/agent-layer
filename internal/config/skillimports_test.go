package config

import (
	"strings"
	"testing"
)

// baseConfigTOML is a minimally valid configuration used to exercise the
// skills.imports rules without repeating unrelated required fields.
const baseConfigTOML = `[approvals]
mode = "none"

[agents.antigravity]
enabled = false

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = false

[agents.codex]
enabled = false

[agents.vscode]
enabled = false

[agents.copilot_cli]
enabled = false
`

func parseWithImports(t *testing.T, imports string) (*Config, error) {
	t.Helper()
	return ParseConfig([]byte(baseConfigTOML+imports), "config.toml")
}

// TestSkillImportValidationRejectsUnsafePolicy proves each configuration rule
// that protects a user from an import Agent Layer could not honor.
func TestSkillImportValidationRejectsUnsafePolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		imports string
		want    string
	}{
		{
			name:    "missing repository",
			imports: "\n[[skills.imports]]\nselectors = [\"skills/a\"]\n",
			want:    "repository is required",
		},
		{
			name:    "no selectors",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = []\n",
			want:    "at least one selector",
		},
		{
			name:    "exclusion only",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"!skills/a\"]\n",
			want:    "at least one positive selector",
		},
		{
			name:    "selector escapes the repository",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"../outside\"]\n",
			want:    "normalized repository-relative path",
		},
		{
			name:    "absolute selector",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"/skills/a\"]\n",
			want:    "repository-relative path",
		},
		{
			name:    "invalid tracking",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\ntracking = \"latest\"\n",
			want:    "tracking",
		},
		{
			name:    "invalid write policy",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\nwrite_policy = \"force\"\n",
			want:    "write_policy",
		},
		{
			name:    "branch policy without a branch",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\nwrite_policy = \"branch\"\n",
			want:    "push_branch is required",
		},
		{
			name:    "branch policy targeting a primary branch",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\nwrite_policy = \"branch\"\npush_branch = \"main\"\n",
			want:    "primary branch name",
		},
		{
			name:    "push branch without branch policy",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\nwrite_policy = \"direct\"\npush_branch = \"topic\"\n",
			want:    "only supported when write_policy",
		},
		{
			name:    "push repository without a write policy",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\npush_repository = \"fork\"\n",
			want:    "push_repository requires",
		},
		{
			name: "duplicate block identity",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\n" +
				"\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/b\"]\n",
			want: "duplicates the repository",
		},
		{
			name: "duplicate repository and selector pair",
			imports: "\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\n" +
				"\n[[skills.imports]]\nrepository = \"r\"\nselectors = [\"skills/a\"]\nref = \"v1\"\n",
			want: "each repository and selector pair must be unique",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseWithImports(t, tt.imports)
			if err == nil {
				t.Fatalf("expected an error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

// TestSkillImportValidationAcceptsSupportedPolicies proves the supported
// combinations load and expose their documented defaults.
func TestSkillImportValidationAcceptsSupportedPolicies(t *testing.T) {
	t.Parallel()
	cfg, err := parseWithImports(t, `
[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = ["skills/*", "!skills/internal"]

[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = ["tools/one"]
ref = "v1.2.3"
tracking = "pinned"
write_policy = "branch"
push_repository = "https://example.test/fork.git"
push_branch = "skill-updates"
`)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if len(cfg.Skills.Imports) != 2 {
		t.Fatalf("imports = %d, want 2", len(cfg.Skills.Imports))
	}

	first := cfg.Skills.Imports[0]
	if first.EffectiveWritePolicy() != SkillWritePolicyNone || first.WriteEnabled() {
		t.Fatalf("omitted write_policy did not default to none: %+v", first)
	}
	if first.EffectivePushRepository() != "https://example.test/skills.git" {
		t.Fatalf("push repository fallback = %q", first.EffectivePushRepository())
	}
	if got := strings.Join(first.PositiveSelectors(), ","); got != "skills/*" {
		t.Fatalf("positive selectors = %q", got)
	}
	if got := strings.Join(first.ExclusionSelectors(), ","); got != "skills/internal" {
		t.Fatalf("exclusion selectors = %q", got)
	}

	second := cfg.Skills.Imports[1]
	if !second.WriteEnabled() || second.EffectivePushRepository() != "https://example.test/fork.git" {
		t.Fatalf("fork destination not honored: %+v", second)
	}
	if first.Identity() == second.Identity() {
		t.Fatal("different policies produced the same block identity")
	}
}

// TestSkillImportIdentityUsesTheEffectivePushRepository proves an omitted
// destination and the equivalent explicit source destination identify the same
// policy block, preventing duplicate blocks and misdirected selector edits.
func TestSkillImportIdentityUsesTheEffectivePushRepository(t *testing.T) {
	t.Parallel()
	omitted := SkillImport{
		Repository:  "https://example.test/skills.git",
		WritePolicy: SkillWritePolicyDirect,
	}
	explicit := omitted
	explicit.PushRepository = "https://example.test/skills.git/"
	if omitted.Identity() != explicit.Identity() {
		t.Fatalf("equivalent push destinations have different identities: omitted=%+v explicit=%+v", omitted.Identity(), explicit.Identity())
	}

	_, err := parseWithImports(t, `
[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = ["skills/a"]
write_policy = "direct"

[[skills.imports]]
repository = "https://example.test/skills.git"
selectors = ["skills/b"]
write_policy = "direct"
push_repository = "https://example.test/skills.git"
`)
	if err == nil || !strings.Contains(err.Error(), "duplicates the repository") {
		t.Fatalf("equivalent duplicate block error = %v", err)
	}
}

// TestNormalizeSkillSelectorProducesStableIdentity proves selector identity
// does not depend on incidental formatting, which is what makes `al skills
// remove` able to find exactly one selector.
func TestNormalizeSkillSelectorProducesStableIdentity(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"  skills/a  ": "skills/a",
		"skills/a/":    "skills/a",
		"skills\\a":    "skills/a",
		"! skills/a":   "!skills/a",
		"!skills/a/":   "!skills/a",
		"./skills/a":   "skills/a",
		"skills//a":    "skills/a",
	}
	for input, want := range cases {
		if got := NormalizeSkillSelector(input); got != want {
			t.Fatalf("NormalizeSkillSelector(%q) = %q, want %q", input, got, want)
		}
	}
	if got := NormalizeSkillRepository(" https://example.test/repo.git/ "); got != "https://example.test/repo.git" {
		t.Fatalf("NormalizeSkillRepository = %q", got)
	}
}

// TestValidateSkillSelectorPathRejectsUnsafePaths proves the shared selector
// syntax check used by configuration validation and `al skills add` refuses
// every form that could escape or misidentify a repository path.
func TestValidateSkillSelectorPathRejectsUnsafePaths(t *testing.T) {
	t.Parallel()
	rejected := map[string]string{
		`skills\a`:  "path separator",
		"/skills/a": "repository-relative",
		"../a":      "normalized",
		"a/../b":    "normalized",
		"a/./b":     "normalized",
		"a//b":      "normalized",
	}
	for value, want := range rejected {
		err := ValidateSkillSelectorPath(value)
		if err == nil {
			t.Fatalf("selector %q was accepted", value)
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("selector %q error %q does not contain %q", value, err, want)
		}
	}
	for _, value := range []string{"a", "skills/a", "skills/*", "a/b/c"} {
		if err := ValidateSkillSelectorPath(value); err != nil {
			t.Fatalf("selector %q was rejected: %v", value, err)
		}
	}
}

// TestSkillExclusionHelpersRoundTrip proves exclusion detection and stripping
// agree, which is what keeps a `!` selector out of the desired set while still
// matching skill roots.
func TestSkillExclusionHelpersRoundTrip(t *testing.T) {
	t.Parallel()
	if !IsSkillExclusionSelector(" !skills/a ") || IsSkillExclusionSelector("skills/a") {
		t.Fatal("exclusion detection does not tolerate surrounding whitespace")
	}
	if got := SkillExclusionPath("! skills/a "); got != "skills/a" {
		t.Fatalf("SkillExclusionPath = %q", got)
	}
	if got := SkillExclusionPath("skills/a"); got != "skills/a" {
		t.Fatalf("SkillExclusionPath changed a positive selector: %q", got)
	}
	if got := NormalizeSkillSelector("  "); got != "" {
		t.Fatalf("an empty selector normalized to %q", got)
	}
	if got := NormalizeSkillSelector("!"); got != "!" {
		t.Fatalf("a bare exclusion prefix normalized to %q", got)
	}
}
