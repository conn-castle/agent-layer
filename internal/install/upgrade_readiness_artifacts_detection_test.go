package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestDetectDisabledAgentArtifacts_IgnoresUserFileWithoutEvidence(t *testing.T) {
	root := t.TempDir()
	geminiPath := filepath.Join(root, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(geminiPath), 0o700); err != nil {
		t.Fatalf("mkdir gemini dir: %v", err)
	}
	if err := os.WriteFile(geminiPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write gemini settings: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{Antigravity: config.AntigravityConfig{Enabled: testutil.BoolPtr(true)}, Claude: config.ClaudeConfig{Enabled: testutil.BoolPtr(true)}, ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}, VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}, Codex: config.CodexConfig{Enabled: testutil.BoolPtr(true)}, CopilotCLI: config.AgentConfig{Enabled: testutil.BoolPtr(true)}}}
	check, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err != nil {
		t.Fatalf("detectDisabledAgentArtifacts: %v", err)
	}
	if check != nil {
		t.Fatalf("expected no finding for user-owned gemini file, got %#v", check)
	}
}

func TestDetectDisabledAgentArtifacts_IgnoresDirectories(t *testing.T) {
	root := t.TempDir()
	codexConfigPath := filepath.Join(root, ".codex", "config.toml")
	if err := os.MkdirAll(codexConfigPath, 0o700); err != nil {
		t.Fatalf("mkdir codex config directory: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{
		Antigravity:  config.AntigravityConfig{Enabled: testutil.BoolPtr(true)},
		Claude:       config.ClaudeConfig{Enabled: testutil.BoolPtr(true)},
		ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)},
		Codex:        config.CodexConfig{Enabled: testutil.BoolPtr(false)},
		VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)},
		CopilotCLI:   config.AgentConfig{Enabled: testutil.BoolPtr(true)},
	}}
	check, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err != nil {
		t.Fatalf("detectDisabledAgentArtifacts: %v", err)
	}
	if check != nil {
		t.Fatalf("expected no finding for directory placeholders, got %#v", check)
	}
}

func TestDetectDisabledAgentArtifacts_ClaudeStatError(t *testing.T) {
	root := t.TempDir()
	claudePath := filepath.Join(root, ".mcp.json")
	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(claudePath)] = errors.New("stat boom")
	inst := &installer{root: root, sys: sys}

	// Both Claude and ClaudeVSCode must be disabled for the claude rule to fire.
	cfg := config.Config{Agents: config.AgentsConfig{Antigravity: config.AntigravityConfig{Enabled: testutil.BoolPtr(true)}, Claude: config.ClaudeConfig{Enabled: testutil.BoolPtr(false)}, ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)}, VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}, Codex: config.CodexConfig{Enabled: testutil.BoolPtr(true)}, CopilotCLI: config.AgentConfig{Enabled: testutil.BoolPtr(true)}}}
	_, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected claude stat error, got %v", err)
	}
}

func TestDetectDisabledAgentArtifacts_ClaudeSettingsStatError(t *testing.T) {
	root := t.TempDir()
	claudeSettingsPath := filepath.Join(root, ".claude", "settings.json")
	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(claudeSettingsPath)] = errors.New("stat boom")
	inst := &installer{root: root, sys: sys}

	// Both Claude and ClaudeVSCode must be disabled for the claude rule to fire.
	cfg := config.Config{Agents: config.AgentsConfig{Antigravity: config.AntigravityConfig{Enabled: testutil.BoolPtr(true)}, Claude: config.ClaudeConfig{Enabled: testutil.BoolPtr(false)}, ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)}, VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}, Codex: config.CodexConfig{Enabled: testutil.BoolPtr(true)}, CopilotCLI: config.AgentConfig{Enabled: testutil.BoolPtr(true)}}}
	_, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected claude settings stat error, got %v", err)
	}
}

func TestDetectDisabledAgentArtifacts_FlagsClaudeSettings(t *testing.T) {
	root := t.TempDir()
	claudeSettingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(claudeSettingsPath), 0o700); err != nil {
		t.Fatalf("mkdir claude dir: %v", err)
	}
	if err := os.WriteFile(claudeSettingsPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write claude settings: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{
		Antigravity:  config.AntigravityConfig{Enabled: testutil.BoolPtr(true)},
		Claude:       config.ClaudeConfig{Enabled: testutil.BoolPtr(false)},
		ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)},
		CopilotCLI:   config.AgentConfig{Enabled: testutil.BoolPtr(true)},
		Codex:        config.CodexConfig{Enabled: testutil.BoolPtr(true)},
	}}
	check, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err != nil {
		t.Fatalf("detectDisabledAgentArtifacts: %v", err)
	}
	if check == nil {
		t.Fatal("expected disabled-agent artifacts check for .claude/settings.json")
	}
	joined := strings.Join(check.Details, "\n")
	if !strings.Contains(joined, ".claude/settings.json") {
		t.Fatalf("expected .claude/settings.json in details, got %q", joined)
	}
}

func TestDetectDisabledAgentArtifacts_CodexStatError(t *testing.T) {
	root := t.TempDir()
	codexRulesPath := filepath.Join(root, ".codex", "rules", "default.rules")
	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(codexRulesPath)] = errors.New("stat boom")
	inst := &installer{root: root, sys: sys}

	cfg := config.Config{Agents: config.AgentsConfig{
		Antigravity:  config.AntigravityConfig{Enabled: testutil.BoolPtr(true)},
		Claude:       config.ClaudeConfig{Enabled: testutil.BoolPtr(true)},
		ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)},
		Codex:        config.CodexConfig{Enabled: testutil.BoolPtr(false)},
		VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)},
		CopilotCLI:   config.AgentConfig{Enabled: testutil.BoolPtr(true)},
	}}
	_, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected codex stat error, got %v", err)
	}
}

func TestDetectDisabledAgentArtifacts_VSCodeTemplateReadError(t *testing.T) {
	root := t.TempDir()
	launcherPath := filepath.Join(root, ".agent-layer", "open-vscode.command")
	if err := os.MkdirAll(filepath.Dir(launcherPath), 0o700); err != nil {
		t.Fatalf("mkdir launcher dir: %v", err)
	}
	if err := os.WriteFile(launcherPath, []byte("#!/bin/sh\n"), 0o755); err != nil { // #nosec G306 -- test writes an executable shell stub (PATH-shadowed) for subprocess invocation.
		t.Fatalf("write launcher file: %v", err)
	}

	originalRead := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == "launchers/open-vscode.command" {
			return nil, errors.New("template boom")
		}
		return originalRead(path)
	}
	t.Cleanup(func() {
		templates.ReadFunc = originalRead
	})

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{Antigravity: config.AntigravityConfig{Enabled: testutil.BoolPtr(true)}, Claude: config.ClaudeConfig{Enabled: testutil.BoolPtr(true)}, ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)}, VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)}, Codex: config.CodexConfig{Enabled: testutil.BoolPtr(true)}, CopilotCLI: config.AgentConfig{Enabled: testutil.BoolPtr(true)}}}
	_, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err == nil || !strings.Contains(err.Error(), "template boom") {
		t.Fatalf("expected template read error, got %v", err)
	}
}

func TestDetectDisabledAgentArtifacts_VSCodeSettingsReadError(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	settings := "{\n  // >>> agent-layer\n  // managed\n  // <<< agent-layer\n}\n"
	if err := os.WriteFile(settingsPath, []byte(settings), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.readErrs[normalizePath(settingsPath)] = errors.New("read boom")
	inst := &installer{root: root, sys: sys}
	cfg := config.Config{Agents: config.AgentsConfig{
		Antigravity:  config.AntigravityConfig{Enabled: testutil.BoolPtr(true)},
		Claude:       config.ClaudeConfig{Enabled: testutil.BoolPtr(true)},
		ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		Codex:        config.CodexConfig{Enabled: testutil.BoolPtr(true)},
		VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		CopilotCLI:   config.AgentConfig{Enabled: testutil.BoolPtr(true)},
	}}
	_, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected vscode settings read error, got %v", err)
	}
}

func TestDetectDisabledAgentArtifacts_VSCodePromptWalkError(t *testing.T) {
	root := t.TempDir()
	promptRoot := filepath.Join(root, ".vscode", "prompts")
	if err := os.MkdirAll(promptRoot, 0o700); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	sys := newFaultSystem(RealSystem{})
	sys.walkErrs[normalizePath(promptRoot)] = errors.New("walk boom")
	inst := &installer{root: root, sys: sys}
	cfg := config.Config{Agents: config.AgentsConfig{
		Antigravity:  config.AntigravityConfig{Enabled: testutil.BoolPtr(true)},
		Claude:       config.ClaudeConfig{Enabled: testutil.BoolPtr(true)},
		ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		Codex:        config.CodexConfig{Enabled: testutil.BoolPtr(true)},
		VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		CopilotCLI:   config.AgentConfig{Enabled: testutil.BoolPtr(true)},
	}}
	_, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err == nil || !strings.Contains(err.Error(), "walk boom") {
		t.Fatalf("expected vscode prompt walk error, got %v", err)
	}
}

func TestDetectDisabledAgentArtifacts_FlagsClaudeAndGeminiSkillDirs(t *testing.T) {
	root := t.TempDir()

	skillFiles := map[string]string{
		filepath.Join(root, ".claude", "skills", "deploy", "SKILL.md"): "<!--\n  GENERATED FILE\n-->\n",
		filepath.Join(root, ".gemini", "skills", "deploy", "SKILL.md"): "<!--\n  GENERATED FILE\n-->\n",
	}
	for absPath, content := range skillFiles {
		if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", absPath, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", absPath, err)
		}
	}

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{
		Antigravity:  config.AntigravityConfig{Enabled: testutil.BoolPtr(false)},
		Claude:       config.ClaudeConfig{Enabled: testutil.BoolPtr(false)},
		ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		Codex:        config.CodexConfig{Enabled: testutil.BoolPtr(true)},
		VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)},
		CopilotCLI:   config.AgentConfig{Enabled: testutil.BoolPtr(true)},
	}}
	check, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err != nil {
		t.Fatalf("detectDisabledAgentArtifacts: %v", err)
	}
	if check == nil {
		t.Fatal("expected disabled-agent artifacts check for skill dirs")
	}
	joined := strings.Join(check.Details, "\n")
	for _, expected := range []string{
		".claude/skills/deploy/SKILL.md",
		".gemini/skills/deploy/SKILL.md",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected %q in details, got %q", expected, joined)
		}
	}
}

func TestDetectDisabledAgentArtifacts_FindsManagedArtifacts(t *testing.T) {
	root := t.TempDir()

	codexFiles := map[string]string{
		filepath.Join(root, ".codex", "config.toml"):                 "# GENERATED FILE\n",
		filepath.Join(root, ".codex", "rules", "default.rules"):      "# GENERATED FILE\n",
		filepath.Join(root, ".codex", "skills", "alpha", "SKILL.md"): "<!--\n  GENERATED FILE\n-->\n",
		filepath.Join(root, ".agent", "skills", "beta", "SKILL.md"):  "<!--\n  GENERATED FILE\n-->\n",
	}
	for absPath, content := range codexFiles {
		if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", absPath, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", absPath, err)
		}
	}

	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	promptPath := filepath.Join(root, ".vscode", "prompts", "alpha.prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o700); err != nil {
		t.Fatalf("mkdir vscode prompt dir: %v", err)
	}
	settings := "{\n  // >>> agent-layer\n  // managed\n  // <<< agent-layer\n}\n"
	if err := os.WriteFile(settingsPath, []byte(settings), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("<!--\n  GENERATED FILE\n-->\n"), 0o600); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	launcherTemplate, err := templates.Read("launchers/open-vscode.command")
	if err != nil {
		t.Fatalf("read launcher template: %v", err)
	}
	launcherPath := filepath.Join(root, ".agent-layer", "open-vscode.command")
	if err := os.MkdirAll(filepath.Dir(launcherPath), 0o700); err != nil {
		t.Fatalf("mkdir launcher dir: %v", err)
	}
	if err := os.WriteFile(launcherPath, launcherTemplate, 0o755); err != nil { // #nosec G306 -- test writes an executable shell stub (PATH-shadowed) for subprocess invocation.
		t.Fatalf("write launcher: %v", err)
	}

	cfg := config.Config{
		Agents: config.AgentsConfig{
			Antigravity:  config.AntigravityConfig{Enabled: testutil.BoolPtr(true)},
			Claude:       config.ClaudeConfig{Enabled: testutil.BoolPtr(true)},
			ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)},
			Codex:        config.CodexConfig{Enabled: testutil.BoolPtr(false)},
			VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
			CopilotCLI:   config.AgentConfig{Enabled: testutil.BoolPtr(false)},
		},
	}
	inst := &installer{root: root, sys: RealSystem{}}
	check, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err != nil {
		t.Fatalf("detectDisabledAgentArtifacts: %v", err)
	}
	if check == nil {
		t.Fatal("expected disabled-agent artifacts check")
	}
	joined := strings.Join(check.Details, "\n")
	// Codex/antigravity disabled artifacts should be flagged.
	for _, expected := range []string{
		".codex/rules/default.rules",
		".codex/skills/alpha/SKILL.md",
		".agent/skills/beta/SKILL.md",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected detail %q, got %q", expected, joined)
		}
	}
	if strings.Contains(joined, ".codex/config.toml") {
		t.Fatalf("unexpected shared Codex config in disabled details, got %q", joined)
	}
	// .vscode/settings.json is shared (generated when either agent is enabled),
	// so it should NOT be flagged when claude_vscode is enabled.
	if strings.Contains(joined, ".vscode/settings.json") {
		t.Fatalf("unexpected .vscode/settings.json in disabled details when claude_vscode is enabled, got %q", joined)
	}
	// Prompts and launchers are vscode-only, so they SHOULD be flagged
	// even when claude_vscode is enabled (vscode is disabled).
	for _, expected := range []string{
		".vscode/prompts/alpha.prompt.md",
		".agent-layer/open-vscode.command",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected vscode-only artifact %q to be flagged, got %q", expected, joined)
		}
	}
}

func TestDetectDisabledAgentArtifacts_SharedSkillsEnabledByAnyConsumer(t *testing.T) {
	root := t.TempDir()
	sharedSkill := filepath.Join(root, ".agents", "skills", "alpha", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(sharedSkill), 0o700); err != nil {
		t.Fatalf("mkdir shared skill dir: %v", err)
	}
	if err := os.WriteFile(sharedSkill, []byte("<!--\n  GENERATED FILE\n-->\n"), 0o600); err != nil {
		t.Fatalf("write shared skill: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{
		Antigravity:  config.AntigravityConfig{Enabled: testutil.BoolPtr(false)},
		Claude:       config.ClaudeConfig{Enabled: testutil.BoolPtr(false)},
		ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		Codex:        config.CodexConfig{Enabled: testutil.BoolPtr(true)},
		VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		CopilotCLI:   config.AgentConfig{Enabled: testutil.BoolPtr(false)},
	}}
	check, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err != nil {
		t.Fatalf("detectDisabledAgentArtifacts: %v", err)
	}
	if check != nil && strings.Contains(strings.Join(check.Details, "\n"), ".agents/skills/alpha/SKILL.md") {
		t.Fatalf("did not expect shared skills to be stale while Codex is enabled, got %#v", check.Details)
	}
}

func TestDetectDisabledAgentArtifacts_FlagsSharedSkillsWhenNoConsumerEnabled(t *testing.T) {
	root := t.TempDir()
	sharedSkill := filepath.Join(root, ".agents", "skills", "alpha", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(sharedSkill), 0o700); err != nil {
		t.Fatalf("mkdir shared skill dir: %v", err)
	}
	if err := os.WriteFile(sharedSkill, []byte("<!--\n  GENERATED FILE\n-->\n"), 0o600); err != nil {
		t.Fatalf("write shared skill: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{
		Antigravity:  config.AntigravityConfig{Enabled: testutil.BoolPtr(false)},
		Claude:       config.ClaudeConfig{Enabled: testutil.BoolPtr(false)},
		ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		Codex:        config.CodexConfig{Enabled: testutil.BoolPtr(false)},
		VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		CopilotCLI:   config.AgentConfig{Enabled: testutil.BoolPtr(false)},
	}}
	check, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err != nil {
		t.Fatalf("detectDisabledAgentArtifacts: %v", err)
	}
	if check == nil || !strings.Contains(strings.Join(check.Details, "\n"), ".agents/skills/alpha/SKILL.md") {
		t.Fatalf("expected stale shared skill finding, got %#v", check)
	}
}
