package templates

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedAgentDispatchSkillEncodesAsyncConversationWorkflow proves the
// agent-facing workflow drives the five MCP tools. It also proves no CLI
// polling fallback survives: a fallback would let an agent burn coordinator
// turns on terminal waits exactly when the MCP server is misconfigured, hiding
// the capability problem instead of surfacing it.
func TestEmbeddedAgentDispatchSkillEncodesAsyncConversationWorkflow(t *testing.T) {
	dispatchTemplate, err := Read("skills-catalog/agent-dispatch/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	dispatchSkill := string(dispatchTemplate)
	for _, required := range []string{
		"dispatch_options", "dispatch_start", "dispatch_wait", "dispatch_continue", "dispatch_cancel",
		"mcp__agent-layer__dispatch_start", "result_path",
	} {
		if !strings.Contains(dispatchSkill, required) {
			t.Fatalf("agent-dispatch skill lacks %q", required)
		}
	}
	for _, forbidden := range []string{"al dispatch options", "al dispatch start", "al dispatch wait", "al dispatch continue", "al dispatch cancel"} {
		if strings.Contains(dispatchSkill, forbidden) {
			t.Fatalf("agent-dispatch skill still instructs the CLI path %q", forbidden)
		}
	}
}

func TestReadTemplate(t *testing.T) {
	data, err := Read("config.toml")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected template content")
	}
}

func TestReadTemplateMissing(t *testing.T) {
	_, err := Read("missing.txt")
	if err == nil {
		t.Fatalf("expected error for missing template")
	}
}

func TestEmbeddedPlaywrightSkillUsesDistinctIDAndCLICommand(t *testing.T) {
	data, err := Read("skills-catalog/playwright/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	skill := string(data)
	if !strings.Contains(skill, "\nname: playwright\n") {
		t.Fatal("playwright skill frontmatter does not use the distinct playwright id")
	}
	if !strings.Contains(skill, "playwright-cli --help") {
		t.Fatal("playwright skill does not preserve the playwright-cli command surface")
	}
	if _, err := Read("skills-catalog/playwright-cli/SKILL.md"); err == nil {
		t.Fatal("colliding playwright-cli skill template should be absent")
	}
}

func TestEmbeddedAutoSkillLoopRequiresExplicitInvocation(t *testing.T) {
	skillData, err := Read("skills/auto-skill-loop/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(skillData), "disable-model-invocation: true") {
		t.Fatal("auto-skill-loop does not disable Claude model invocation")
	}

	metadata, err := Read("skills/auto-skill-loop/agents/openai.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metadata), "allow_implicit_invocation: false") {
		t.Fatal("auto-skill-loop does not disable Codex implicit invocation")
	}
}

func TestReadLauncherTemplate(t *testing.T) {
	data, err := Read("launchers/open-vscode.command")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected launcher template content")
	}
}

func TestReadManifestTemplate(t *testing.T) {
	data, err := Read("manifests/0.7.0.json")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected manifest content")
	}
}

func TestReadMigrationManifestTemplate(t *testing.T) {
	data, err := Read("migrations/0.7.0.json")
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected migration manifest content")
	}
}

func TestWalkTemplates(t *testing.T) {
	var seen bool
	err := Walk("instructions", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".md") {
			seen = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
	if !seen {
		t.Fatalf("expected to see at least one instruction template")
	}
}

func TestSkillTemplatesAllowResourceFiles(t *testing.T) {
	err := Walk("skills", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Base(path) == "SKILL.md" {
			return nil
		}
		if _, err := Read(path); err != nil {
			t.Fatalf("expected embedded skill resource %s to be readable: %v", path, err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk error: %v", err)
	}
}

func TestRemovedSkillTemplatesStayRemoved(t *testing.T) {
	for _, path := range []string{
		"skills/continue-roadmap/SKILL.md",
		"skills/complete-current-phase/SKILL.md",
		"skills/find-issues/SKILL.md",
		"skills/fix-issues/SKILL.md",
		"skills/finish-task/SKILL.md",
		"skills/loop-clean-and-fix/SKILL.md",
		"skills/mechanical-cleanup/SKILL.md",
		"skills/audit-and-fix-uncommitted/SKILL.md",
		"skills/audit-and-fix-uncommitted-changes/SKILL.md",
		"skills/prune-new-tests/SKILL.md",
		"skills/prune-new-tests/reviewer-prompt.md",
		"skills/repair-checks/SKILL.md",
		"skills/run-all-checks/SKILL.md",
		"skills/run-and-fix-checks/SKILL.md",
		"skills/simplify-new-code/SKILL.md",
		"skills/simplify-new-code/reviewer-prompt.md",
		"skills/simplify-code/SKILL.md",
		"skills/resolve-findings/SKILL.md",
		"skills/address-pr-comments/SKILL.md",
		"skills/fix-ci/SKILL.md",
		"skills/full-workflow/SKILL.md",
		"skills/fully-implement-plan/SKILL.md",
		"skills/plan-work/SKILL.md",
		"skills/review-uncommitted-code/SKILL.md",
		"skills/run-and-fix-all-checks/SKILL.md",
		"skills/schedule-backlog/SKILL.md",
	} {
		if _, err := Read(path); err == nil {
			t.Fatalf("expected removed skill template %s to stay absent", path)
		}
	}
}
