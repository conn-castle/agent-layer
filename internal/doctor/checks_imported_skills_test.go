package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/messages"
)

const importedSkillsTestConfig = `
[[instructions.local]]
selectors = ["00_rules.md"]
order = 0

[approvals]
mode = "all"

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
[agents.grok]
enabled = false
`

// newImportedSkillsTestRoot creates a repository root with valid Agent Layer
// scaffolding. When lenient is true the config omits a required agent so
// CheckConfig takes its lenient fallback path.
func newImportedSkillsTestRoot(t *testing.T, lenient bool) string {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	for _, dir := range []string{filepath.Join(configDir, "instructions"), filepath.Join(configDir, "skills")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := importedSkillsTestConfig
	if lenient {
		strict := cfg
		cfg = strings.Replace(cfg, "[agents.claude_vscode]\nenabled = false\n", "", 1)
		if cfg == strict {
			t.Fatal("lenient fixture did not remove a required agent")
		}
	}
	files := map[string]string{
		"config.toml":              cfg,
		".env":                     "",
		"commands.allow":           "",
		"instructions/00_rules.md": "# Base",
		"skills/local/SKILL.md":    "---\nname: local\ndescription: The local skill.\n---\nBody.\n",
	}
	for name, content := range files {
		path := filepath.Join(configDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func writeImportedSkill(t *testing.T, root string, name string, description string) {
	t.Helper()
	dir := filepath.Join(root, ".agent-layer", "skills-imported", name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := "---\nname: " + name + "\ndescription: " + description + "\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeImportedSkillsLock(t *testing.T, root string, names ...string) {
	t.Helper()
	entries := make([]string, 0, len(names))
	for _, name := range names {
		entries = append(entries, `{"name":"`+name+`","repository":"https://example.test/skills.git","selector":"s/`+name+
			`","selected_path":"s/`+name+`","configured_ref":"","resolved_ref":"main","ref_kind":"branch",`+
			`"tracking":"tracked","commit":"`+strings.Repeat("a", 40)+`","tree_hash":"sha256:`+strings.Repeat("b", 64)+`"}`)
	}
	content := `{"version":1,"skills":[` + strings.Join(entries, ",") + `]}`
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "skills.lock.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestCheckConfig_IncludesImportedSkills proves doctor validates and budgets
// the same combined skill set sync projects, on both config load paths.
func TestCheckConfig_IncludesImportedSkills(t *testing.T) {
	for _, lenient := range []bool{false, true} {
		root := newImportedSkillsTestRoot(t, lenient)
		writeImportedSkill(t, root, "remote", "The remote imported skill.")
		writeImportedSkillsLock(t, root, "remote")

		results, cfg := CheckConfig(root)
		if cfg == nil {
			t.Fatalf("lenient=%v: expected a config, results %#v", lenient, results)
		}
		if HasFailResultForCheck(results, messages.DoctorCheckNameSkills) {
			t.Fatalf("lenient=%v: unexpected Skills FAIL: %#v", lenient, results)
		}
		if len(cfg.Skills) != 2 || cfg.Skills[0].Name != "local" || cfg.Skills[1].Name != "remote" || !cfg.Skills[1].Imported {
			t.Fatalf("lenient=%v: skills = %+v, want local and imported remote", lenient, cfg.Skills)
		}
		if text, _ := SkillCatalogMetadata(cfg); !strings.Contains(text, "The remote imported skill.") {
			t.Fatalf("lenient=%v: catalog metadata %q omits the imported skill", lenient, text)
		}
		if got := CheckSkills(cfg); len(got) != 1 || got[0].Message != fmt.Sprintf(messages.DoctorSkillsValidatedFmt, 2) {
			t.Fatalf("lenient=%v: CheckSkills = %#v, want both skills validated", lenient, got)
		}
	}
}

// TestCheckConfig_ReportsImportedSkillFailures proves every imported-tier state
// that makes sync fail is reported as a Skills FAIL instead of a passing check.
func TestCheckConfig_ReportsImportedSkillFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string)
		want  string
	}{
		{
			name: "invalid manifest",
			setup: func(t *testing.T, root string) {
				writeImportedSkill(t, root, "remote", strings.Repeat("d", 1500))
				writeImportedSkillsLock(t, root, "remote")
			},
			want: "exceeds 1024 characters",
		},
		{
			name: "orphan directory",
			setup: func(t *testing.T, root string) {
				writeImportedSkill(t, root, "stray", "Stray.")
			},
			want: "have no entry in .agent-layer/skills.lock.json",
		},
		{
			name: "malformed lock",
			setup: func(t *testing.T, root string) {
				writeImportedSkill(t, root, "remote", "Remote.")
				if err := os.WriteFile(filepath.Join(root, ".agent-layer", "skills.lock.json"), []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: "skills.lock.json",
		},
		{
			name: "name in both tiers",
			setup: func(t *testing.T, root string) {
				writeImportedSkill(t, root, "local", "Imported local.")
				writeImportedSkillsLock(t, root, "local")
			},
			want: "exists in both",
		},
	}
	for _, tt := range tests {
		for _, lenient := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/lenient=%v", tt.name, lenient), func(t *testing.T) {
				root := newImportedSkillsTestRoot(t, lenient)
				tt.setup(t, root)

				results, cfg := CheckConfig(root)
				if cfg == nil {
					t.Fatalf("expected a config, results %#v", results)
				}
				result := requireResultByCheckName(t, results, messages.DoctorCheckNameSkills)
				if result.Status != StatusFail {
					t.Fatalf("Skills status = %s, want FAIL: %#v", result.Status, result)
				}
				if !strings.Contains(result.Message, ".agent-layer/skills-imported") || !strings.Contains(result.Message, tt.want) {
					t.Fatalf("Skills message %q does not name the imported tier and %q", result.Message, tt.want)
				}
				if result.Recommendation != messages.DoctorImportedSkillsRecommend {
					t.Fatalf("recommendation = %q, want imported-skills guidance", result.Recommendation)
				}
				if len(cfg.Skills) != 1 || cfg.Skills[0].Name != "local" {
					t.Fatalf("skills = %+v, want only the user-managed tier", cfg.Skills)
				}
			})
		}
	}
}

// TestCheckSkills_ImportedSkillWarningsUseImportedGuidance proves validation
// warnings for an imported skill do not point at the user-managed tier.
func TestCheckSkills_ImportedSkillWarningsUseImportedGuidance(t *testing.T) {
	root := newImportedSkillsTestRoot(t, false)
	writeImportedSkill(t, root, "remote", "Remote.")
	manifest := filepath.Join(root, ".agent-layer", "skills-imported", "remote", "SKILL.md")
	if err := os.WriteFile(manifest, []byte("---\nname: remote\ndescription: Remote.\n---\n"+strings.Repeat("line\n", 600)), 0o600); err != nil {
		t.Fatal(err)
	}
	writeImportedSkillsLock(t, root, "remote")

	_, cfg := CheckConfig(root)
	if cfg == nil {
		t.Fatal("expected a config")
	}
	results := CheckSkills(cfg)
	if len(results) != 1 || results[0].Status != StatusWarn || !strings.Contains(results[0].Message, "skills-imported/remote") {
		t.Fatalf("CheckSkills = %#v, want one warning for the imported skill", results)
	}
	if results[0].Recommendation != messages.DoctorImportedSkillsRecommend {
		t.Fatalf("recommendation = %q, want imported-skills guidance", results[0].Recommendation)
	}
}
