package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestSyncRetiresOnlyGeneratedClaudeRulesCopy(t *testing.T) {
	const oldGenerated = "<!--\n  GENERATED FILE\n  Source: .agent-layer/instructions/*.md\n  Regenerate: al sync\n-->\n\nOld project guidance.\n"
	const handwritten = "# My Claude rules\nMentions GENERATED FILE and Regenerate: al sync.\n"
	for _, test := range []struct {
		name    string
		content string
		removed bool
		symlink bool
	}{
		{"generated", oldGenerated, true, false},
		{"handwritten", handwritten, false, false},
		{"symlinked-rules", oldGenerated, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeGitignoreBlockForSkillsDedupTest(t, root)
			rules := filepath.Join(root, ".claude", "rules")
			if err := os.MkdirAll(rules, 0o700); err != nil {
				t.Fatal(err)
			}
			if test.symlink {
				external := t.TempDir()
				if err := os.Remove(rules); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, rules); err != nil {
					t.Fatal(err)
				}
			}
			legacy := filepath.Join(rules, "agent-layer.md")
			if err := os.WriteFile(legacy, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			sibling := filepath.Join(rules, "team.md")
			if err := os.WriteFile(sibling, []byte(handwritten), 0o600); err != nil {
				t.Fatal(err)
			}
			project := &config.ProjectConfig{
				Root: root,
				Config: config.Config{
					Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
					Agents:    agentsForSkillsTest("claude", "grok"),
				},
				Instructions: []config.InstructionFile{{Name: "00_rules.md", Content: "Current project guidance.\n"}},
			}
			if _, err := RunWithProject(RealSystem{}, root, project); err != nil {
				t.Fatal(err)
			}
			if test.removed {
				if _, err := os.Stat(legacy); !os.IsNotExist(err) {
					t.Fatalf("generated duplicate survived sync: %v", err)
				}
			} else if got, err := os.ReadFile(legacy); err != nil || string(got) != test.content { // #nosec G304 -- legacy is beneath the test-owned temporary root.
				t.Fatalf("handwritten rules changed: %q, %v", got, err)
			}
			if got, err := os.ReadFile(sibling); err != nil || string(got) != handwritten { // #nosec G304 -- sibling is beneath the test-owned temporary root.
				t.Fatalf("unrelated rules changed: %q, %v", got, err)
			}
			for _, rel := range []string{"AGENTS.md", ".claude/CLAUDE.md"} {
				got, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- fixed output paths beneath the test-owned temporary root.
				if err != nil || !strings.Contains(string(got), "Current project guidance.") {
					t.Fatalf("current instructions unavailable at %s: %q, %v", rel, got, err)
				}
			}
		})
	}
}
