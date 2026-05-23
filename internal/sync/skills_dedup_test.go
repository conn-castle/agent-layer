package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestSkillProjectionDoesNotDuplicateEnabledClientDiscovery(t *testing.T) {
	tests := []struct {
		name        string
		agents      config.AgentsConfig
		clientPaths map[string][]string
	}{
		{
			name:   "antigravity only",
			agents: agentsForSkillsTest("antigravity"),
			clientPaths: map[string][]string{
				"antigravity": {
					filepath.Join(".agents", "skills"),
					filepath.Join(".agy", "antigravity-cli", "skills"),
					filepath.Join(".agy", "skills"),
				},
			},
		},
		{
			name:   "claude only",
			agents: agentsForSkillsTest("claude"),
			clientPaths: map[string][]string{
				"claude": {filepath.Join(".claude", "skills")},
			},
		},
		{
			name:   "claude vscode only",
			agents: agentsForSkillsTest("claude_vscode"),
			clientPaths: map[string][]string{
				"claude_vscode": {filepath.Join(".claude", "skills")},
			},
		},
		{
			name:   "codex only",
			agents: agentsForSkillsTest("codex"),
			clientPaths: map[string][]string{
				"codex": {
					filepath.Join(".agents", "skills"),
					filepath.Join(".codex", "skills"),
				},
			},
		},
		{
			name:   "vscode only",
			agents: agentsForSkillsTest("vscode"),
			clientPaths: map[string][]string{
				"vscode": {
					filepath.Join(".agents", "skills"),
					filepath.Join(".github", "skills"),
					filepath.Join(".vscode", "prompts"),
				},
			},
		},
		{
			name:   "copilot cli only",
			agents: agentsForSkillsTest("copilot_cli"),
			clientPaths: map[string][]string{
				"copilot_cli": {
					filepath.Join(".agents", "skills"),
					filepath.Join(".github", "skills"),
				},
			},
		},
		{
			name:   "claude plus copilot chat",
			agents: agentsForSkillsTest("claude", "vscode"),
			clientPaths: map[string][]string{
				"claude": {filepath.Join(".claude", "skills")},
				"vscode": {
					filepath.Join(".agents", "skills"),
					filepath.Join(".github", "skills"),
					filepath.Join(".vscode", "prompts"),
				},
			},
		},
		{
			name:   "codex plus antigravity",
			agents: agentsForSkillsTest("codex", "antigravity"),
			clientPaths: map[string][]string{
				"codex": {
					filepath.Join(".agents", "skills"),
					filepath.Join(".codex", "skills"),
				},
				"antigravity": {
					filepath.Join(".agents", "skills"),
					filepath.Join(".agy", "antigravity-cli", "skills"),
					filepath.Join(".agy", "skills"),
				},
			},
		},
		{
			name:   "all enabled",
			agents: agentsForSkillsTest("antigravity", "claude", "claude_vscode", "codex", "vscode", "copilot_cli"),
			clientPaths: map[string][]string{
				"antigravity": {
					filepath.Join(".agents", "skills"),
					filepath.Join(".agy", "antigravity-cli", "skills"),
					filepath.Join(".agy", "skills"),
				},
				"claude":        {filepath.Join(".claude", "skills")},
				"claude_vscode": {filepath.Join(".claude", "skills")},
				"codex": {
					filepath.Join(".agents", "skills"),
					filepath.Join(".codex", "skills"),
				},
				"vscode": {
					filepath.Join(".agents", "skills"),
					filepath.Join(".github", "skills"),
					filepath.Join(".vscode", "prompts"),
				},
				"copilot_cli": {
					filepath.Join(".agents", "skills"),
					filepath.Join(".github", "skills"),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeGitignoreBlockForSkillsDedupTest(t, root)
			project := &config.ProjectConfig{
				Root: root,
				Config: config.Config{
					Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
					Agents:    tt.agents,
				},
				Instructions: []config.InstructionFile{{Name: "00_rules.md", Content: "rules"}},
				Skills: []config.Skill{
					{Name: "alpha", Description: "Alpha skill", Body: "Alpha body", SourcePath: filepath.Join(root, ".agent-layer", "skills", "alpha", "SKILL.md")},
					{Name: "beta", Description: "Beta skill", Body: "Beta body", SourcePath: filepath.Join(root, ".agent-layer", "skills", "beta", "SKILL.md")},
				},
			}
			if _, err := RunWithProject(RealSystem{}, root, project); err != nil {
				t.Fatalf("RunWithProject: %v", err)
			}

			for client, paths := range tt.clientPaths {
				counts := countDiscoveredSkills(t, root, paths)
				for _, skill := range []string{"alpha", "beta"} {
					if counts[skill] != 1 {
						t.Fatalf("%s discovered skill %s %d times through paths %v", client, skill, counts[skill], paths)
					}
				}
			}
		})
	}
}

func writeGitignoreBlockForSkillsDedupTest(t *testing.T, root string) {
	t.Helper()
	block, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read gitignore block template: %v", err)
	}
	path := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.WriteFile(path, block, 0o600); err != nil {
		t.Fatalf("write gitignore block: %v", err)
	}
}

func agentsForSkillsTest(enabled ...string) config.AgentsConfig {
	falseVal := false
	agents := config.AgentsConfig{
		Antigravity:  config.AntigravityConfig{Enabled: &falseVal},
		Claude:       config.ClaudeConfig{Enabled: &falseVal},
		ClaudeVSCode: config.EnableOnlyConfig{Enabled: &falseVal},
		Codex:        config.CodexConfig{Enabled: &falseVal},
		VSCode:       config.EnableOnlyConfig{Enabled: &falseVal},
		CopilotCLI:   config.AgentConfig{Enabled: &falseVal},
	}
	for _, name := range enabled {
		trueVal := true
		switch name {
		case "antigravity":
			agents.Antigravity.Enabled = &trueVal
		case "claude":
			agents.Claude.Enabled = &trueVal
		case "claude_vscode":
			agents.ClaudeVSCode.Enabled = &trueVal
		case "codex":
			agents.Codex.Enabled = &trueVal
		case "vscode":
			agents.VSCode.Enabled = &trueVal
		case "copilot_cli":
			agents.CopilotCLI.Enabled = &trueVal
		}
	}
	return agents
}

func countDiscoveredSkills(t *testing.T, root string, paths []string) map[string]int {
	t.Helper()
	counts := make(map[string]int)
	for _, relDir := range paths {
		fullDir := filepath.Join(root, relDir)
		entries, err := os.ReadDir(fullDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("read %s: %v", relDir, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillPath := filepath.Join(fullDir, entry.Name(), "SKILL.md")
			if _, err := os.Stat(skillPath); err == nil {
				counts[entry.Name()]++
			} else if !os.IsNotExist(err) {
				t.Fatalf("stat %s: %v", skillPath, err)
			}
		}
	}
	return counts
}
