package templates

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// TestEmbeddedDispatchAgentSkillEncodesAsyncConversationWorkflow proves the
// agent-facing workflow drives the five MCP tools. It also proves no CLI
// polling fallback survives: a fallback would let an agent burn coordinator
// turns on terminal waits exactly when the MCP server is misconfigured, hiding
// the capability problem instead of surfacing it.
func TestEmbeddedDispatchAgentSkillEncodesAsyncConversationWorkflow(t *testing.T) {
	dispatchTemplate, err := Read("skills-catalog/dispatch-agent/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	dispatchSkill := string(dispatchTemplate)
	for _, required := range []string{
		"dispatch_options", "dispatch_start", "dispatch_wait", "dispatch_continue", "dispatch_cancel",
		"mcp__agent-layer__dispatch_start", "result_path",
	} {
		if !strings.Contains(dispatchSkill, required) {
			t.Fatalf("dispatch-agent skill lacks %q", required)
		}
	}
	for _, forbidden := range []string{"al dispatch options", "al dispatch start", "al dispatch wait", "al dispatch continue", "al dispatch cancel"} {
		if strings.Contains(dispatchSkill, forbidden) {
			t.Fatalf("dispatch-agent skill still instructs the CLI path %q", forbidden)
		}
	}
}

// TestEmbeddedDispatchAgentSkillUsesRenamedID pins the catalog id to the
// directory name and to the frontmatter that clients project as the invocable
// skill name. A mismatch between the two would ship a skill whose directory the
// wizard installs and removes under one id while agents invoke it under
// another, and the workflow skills that instruct `/dispatch-agent` would have
// no matching skill to call.
func TestEmbeddedDispatchAgentSkillUsesRenamedID(t *testing.T) {
	data, err := Read("skills-catalog/dispatch-agent/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\nname: dispatch-agent\n") {
		t.Fatal("dispatch-agent skill frontmatter does not declare the dispatch-agent id")
	}
	if _, err := Read("skills-catalog/agent-dispatch/SKILL.md"); err == nil {
		t.Fatal("superseded agent-dispatch skill template should be absent")
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

func TestEmbeddedSkillSyncNarrowsToolsAndUsesConfirmedDestructiveCommands(t *testing.T) {
	data, err := Read("skills-catalog/skill-sync/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	skill := string(data)
	if !strings.Contains(skill, "\nallowed-tools: Bash(al skills *) Bash(al sync)\n") {
		t.Fatal("skill-sync does not limit its pre-approved shell commands to al skills and al sync")
	}
	if strings.Contains(skill, "Bash(al:*)") {
		t.Fatal("skill-sync retains the unrestricted al command grant")
	}
	for _, required := range []string{
		"al skills add <repository> <selector>... --yes",
		"<selector> --yes",
		"al skills reset <name> --yes",
		"al skills push --yes",
	} {
		if !strings.Contains(skill, required) {
			t.Fatalf("skill-sync lacks confirmed mutation form %q", required)
		}
	}
	mutatingCommand := regexp.MustCompile(`\bal skills (?:add|remove|reset|push)\b`)
	commandSpan := regexp.MustCompile("(?s)`(al skills (?:add|remove|reset|push)\\b[^`]*)`")
	commands := commandSpan.FindAllStringSubmatch(skill, -1)
	if got, want := len(commands), len(mutatingCommand.FindAllString(skill, -1)); got != want {
		t.Fatalf("skill-sync has %d parsed mutation command examples for %d mutation occurrences", got, want)
	}
	for _, match := range commands {
		command := strings.Join(strings.Fields(match[1]), " ")
		tokens := strings.Fields(command)
		if slices.Contains(tokens, "--help") {
			continue
		}
		if !slices.Contains(tokens, "--yes") {
			t.Fatalf("skill-sync mutation command %q lacks an exact --yes token", command)
		}
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

func TestMigrationsDoNotDeleteCurrentlyEmbeddedSkills(t *testing.T) {
	type manifest struct {
		Operations []struct {
			Kind string `json:"kind"`
			Path string `json:"path"`
		} `json:"operations"`
	}
	err := Walk("migrations", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || filepath.Ext(path) != ".json" {
			return walkErr
		}
		data, err := Read(path)
		if err != nil {
			return err
		}
		var migration manifest
		if err := json.Unmarshal(data, &migration); err != nil {
			return err
		}
		for _, operation := range migration.Operations {
			if operation.Kind != "delete_file" && operation.Kind != "delete_generated_artifact" {
				continue
			}
			if !strings.HasPrefix(operation.Path, ".agent-layer/skills/") {
				continue
			}
			skillTemplate := strings.TrimPrefix(operation.Path, ".agent-layer/") + "/SKILL.md"
			if _, err := Read(skillTemplate); err == nil {
				t.Fatalf("%s deletes currently embedded skill %s", path, skillTemplate)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect migration manifests: %v", err)
	}
}
