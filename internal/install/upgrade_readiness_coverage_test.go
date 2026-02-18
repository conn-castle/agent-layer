package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestBuildUpgradeReadinessChecks_ConfigMissing(t *testing.T) {
	inst := &installer{root: t.TempDir(), sys: RealSystem{}}

	checks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		t.Fatalf("buildUpgradeReadinessChecks: %v", err)
	}
	check := findReadinessCheckByID(checks, readinessCheckUnrecognizedConfigKeys)
	if check == nil {
		t.Fatalf("expected %s check", readinessCheckUnrecognizedConfigKeys)
	}
	if !strings.Contains(check.Summary, "missing") {
		t.Fatalf("expected missing-config summary, got %q", check.Summary)
	}
}

func TestBuildUpgradeReadinessChecks_ConfigReadError(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	sys := newFaultSystem(RealSystem{})
	sys.readErrs[normalizePath(configPath)] = errors.New("read boom")

	inst := &installer{root: root, sys: sys}
	_, err := buildUpgradeReadinessChecks(inst)
	if err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected config read error, got %v", err)
	}
}

func TestBuildUpgradeReadinessChecks_ConfigStatError(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(configPath)] = errors.New("stat boom")

	inst := &installer{root: root, sys: sys}
	_, err := buildUpgradeReadinessChecks(inst)
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected config stat error, got %v", err)
	}
}

func TestBuildUpgradeReadinessChecks_ConfigParseFailure(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("[agents]\ninvalid = ["), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	checks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		t.Fatalf("buildUpgradeReadinessChecks: %v", err)
	}

	check := findReadinessCheckByID(checks, readinessCheckUnrecognizedConfigKeys)
	if check == nil {
		t.Fatalf("expected %s check", readinessCheckUnrecognizedConfigKeys)
	}
	if !strings.Contains(strings.Join(check.Details, "\n"), "toml:") {
		t.Fatalf("expected parse detail, got %q", strings.Join(check.Details, "\n"))
	}
}

func TestBuildUpgradeReadinessChecks_VSCodeDetectorErrorPropagates(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	mcpPath := filepath.Join(root, ".vscode", "mcp.json")
	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(mcpPath)] = errors.New("stat boom")
	inst := &installer{root: root, sys: sys}

	_, err := buildUpgradeReadinessChecks(inst)
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected vscode detector error, got %v", err)
	}
}

func TestBuildUpgradeReadinessChecks_DisabledArtifactErrorPropagates(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(cfg), "[agents.claude]\nenabled = true", "[agents.claude]\nenabled = false", 1)
	if updated == string(cfg) {
		t.Fatal("failed to disable claude in config")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	claudePath := filepath.Join(root, ".mcp.json")
	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(claudePath)] = errors.New("stat boom")
	inst := &installer{root: root, sys: sys}

	_, err = buildUpgradeReadinessChecks(inst)
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected disabled-artifact error, got %v", err)
	}
}

func TestDetectVSCodeNoSyncStaleness_SettingsReadError(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write settings file: %v", err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.readErrs[normalizePath(settingsPath)] = errors.New("read boom")
	inst := &installer{root: root, sys: sys}

	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.AgentConfig{Enabled: boolPtr(true)}}}
	_, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected settings read error, got %v", err)
	}
}

func TestDetectVSCodeNoSyncStaleness_VSCodeDisabledNoFinding(t *testing.T) {
	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{
		VSCode:       config.AgentConfig{Enabled: boolPtr(false)},
		ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(false)},
	}}

	check, err := detectVSCodeNoSyncStaleness(inst, &cfg, "config.toml", time.Now())
	if err != nil {
		t.Fatalf("detectVSCodeNoSyncStaleness: %v", err)
	}
	if check != nil {
		t.Fatalf("expected no finding for disabled vscode, got %#v", check)
	}
}

func TestDetectVSCodeNoSyncStaleness_ClaudeVSCodeOnlyEnabled(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("{\"editor.tabSize\":2}\n"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{
		VSCode:       config.AgentConfig{Enabled: boolPtr(false)},
		ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(true)},
	}}
	check, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err != nil {
		t.Fatalf("detectVSCodeNoSyncStaleness: %v", err)
	}
	if check == nil {
		t.Fatal("expected readiness finding for claude-vscode-only config with missing managed block")
	}
	joined := strings.Join(check.Details, "\n")
	if !strings.Contains(joined, "missing Agent Layer managed block") {
		t.Fatalf("expected missing managed block detail, got %#v", check.Details)
	}
	// .vscode/mcp.json and prompts are only generated for agents.vscode, not claude-vscode.
	if strings.Contains(joined, ".vscode/mcp.json") {
		t.Fatalf("should not flag .vscode/mcp.json for claude-vscode-only config, got %#v", check.Details)
	}
	if strings.Contains(joined, "prompts") {
		t.Fatalf("should not flag .vscode/prompts for claude-vscode-only config, got %#v", check.Details)
	}
	// Claude outputs (.mcp.json, .claude/settings.json) should be flagged when claude-vscode is enabled.
	if !strings.Contains(joined, ".mcp.json") {
		t.Fatalf("expected .mcp.json to be flagged for claude-vscode-only config, got %#v", check.Details)
	}
	if !strings.Contains(joined, ".claude/settings.json") {
		t.Fatalf("expected .claude/settings.json to be flagged for claude-vscode-only config, got %#v", check.Details)
	}
}

func TestDetectVSCodeNoSyncStaleness_MissingManagedBlockDetail(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("{\"editor.tabSize\":2}\n"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.AgentConfig{Enabled: boolPtr(true)}}}
	check, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err != nil {
		t.Fatalf("detectVSCodeNoSyncStaleness: %v", err)
	}
	if check == nil {
		t.Fatal("expected readiness finding")
	}
	if !strings.Contains(strings.Join(check.Details, "\n"), "missing Agent Layer managed block") {
		t.Fatalf("expected missing managed block detail, got %#v", check.Details)
	}
}

func TestDetectVSCodeNoSyncStaleness_MCPStatError(t *testing.T) {
	root := t.TempDir()
	mcpPath := filepath.Join(root, ".vscode", "mcp.json")
	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(mcpPath)] = errors.New("stat boom")
	inst := &installer{root: root, sys: sys}

	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.AgentConfig{Enabled: boolPtr(true)}}}
	_, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected mcp stat error, got %v", err)
	}
}

func TestDetectVSCodeNoSyncStaleness_SettingsStatError(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(settingsPath)] = errors.New("stat boom")
	inst := &installer{root: root, sys: sys}

	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.AgentConfig{Enabled: boolPtr(true)}}}
	_, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected settings stat error, got %v", err)
	}
}

func TestDetectVSCodeNoSyncStaleness_SlashCommandsStatError(t *testing.T) {
	root := t.TempDir()
	slashRoot := filepath.Join(root, ".agent-layer", "slash-commands")
	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(slashRoot)] = errors.New("stat boom")
	inst := &installer{root: root, sys: sys}

	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.AgentConfig{Enabled: boolPtr(true)}}}
	_, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected slash-commands stat error, got %v", err)
	}
}

func TestDetectVSCodeNoSyncStaleness_PromptWalkError(t *testing.T) {
	root := t.TempDir()
	slashRoot := filepath.Join(root, ".agent-layer", "slash-commands")
	if err := os.MkdirAll(slashRoot, 0o755); err != nil {
		t.Fatalf("mkdir slash root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(slashRoot, "alpha.md"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write slash command: %v", err)
	}
	promptRoot := filepath.Join(root, ".vscode", "prompts")
	if err := os.MkdirAll(promptRoot, 0o755); err != nil {
		t.Fatalf("mkdir prompt root: %v", err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.walkErrs[normalizePath(promptRoot)] = errors.New("walk boom")
	inst := &installer{root: root, sys: sys}

	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.AgentConfig{Enabled: boolPtr(true)}}}
	_, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "walk boom") {
		t.Fatalf("expected prompt walk error, got %v", err)
	}
}

func TestDetectDisabledAgentArtifacts_IgnoresUserFileWithoutEvidence(t *testing.T) {
	root := t.TempDir()
	geminiPath := filepath.Join(root, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(geminiPath), 0o755); err != nil {
		t.Fatalf("mkdir gemini dir: %v", err)
	}
	if err := os.WriteFile(geminiPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write gemini settings: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{Gemini: config.AgentConfig{Enabled: boolPtr(false)}, Claude: config.AgentConfig{Enabled: boolPtr(true)}, ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(true)}, VSCode: config.AgentConfig{Enabled: boolPtr(true)}, Antigravity: config.AgentConfig{Enabled: boolPtr(true)}, Codex: config.CodexConfig{Enabled: boolPtr(true)}}}
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
	if err := os.MkdirAll(codexConfigPath, 0o755); err != nil {
		t.Fatalf("mkdir codex config directory: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{
		Gemini:       config.AgentConfig{Enabled: boolPtr(true)},
		Claude:       config.AgentConfig{Enabled: boolPtr(true)},
		ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(true)},
		Codex:        config.CodexConfig{Enabled: boolPtr(false)},
		VSCode:       config.AgentConfig{Enabled: boolPtr(true)},
		Antigravity:  config.AgentConfig{Enabled: boolPtr(true)},
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
	cfg := config.Config{Agents: config.AgentsConfig{Gemini: config.AgentConfig{Enabled: boolPtr(true)}, Claude: config.AgentConfig{Enabled: boolPtr(false)}, ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(false)}, VSCode: config.AgentConfig{Enabled: boolPtr(true)}, Antigravity: config.AgentConfig{Enabled: boolPtr(true)}, Codex: config.CodexConfig{Enabled: boolPtr(true)}}}
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
	cfg := config.Config{Agents: config.AgentsConfig{Gemini: config.AgentConfig{Enabled: boolPtr(true)}, Claude: config.AgentConfig{Enabled: boolPtr(false)}, ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(false)}, VSCode: config.AgentConfig{Enabled: boolPtr(true)}, Antigravity: config.AgentConfig{Enabled: boolPtr(true)}, Codex: config.CodexConfig{Enabled: boolPtr(true)}}}
	_, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected claude settings stat error, got %v", err)
	}
}

func TestDetectDisabledAgentArtifacts_FlagsClaudeSettings(t *testing.T) {
	root := t.TempDir()
	claudeSettingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(claudeSettingsPath), 0o755); err != nil {
		t.Fatalf("mkdir claude dir: %v", err)
	}
	if err := os.WriteFile(claudeSettingsPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write claude settings: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{
		Gemini:       config.AgentConfig{Enabled: boolPtr(true)},
		Claude:       config.AgentConfig{Enabled: boolPtr(false)},
		ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(false)},
		VSCode:       config.AgentConfig{Enabled: boolPtr(true)},
		Antigravity:  config.AgentConfig{Enabled: boolPtr(true)},
		Codex:        config.CodexConfig{Enabled: boolPtr(true)},
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
	codexAgentsPath := filepath.Join(root, ".codex", "AGENTS.md")
	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(codexAgentsPath)] = errors.New("stat boom")
	inst := &installer{root: root, sys: sys}

	cfg := config.Config{Agents: config.AgentsConfig{
		Gemini:       config.AgentConfig{Enabled: boolPtr(true)},
		Claude:       config.AgentConfig{Enabled: boolPtr(true)},
		ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(true)},
		Codex:        config.CodexConfig{Enabled: boolPtr(false)},
		VSCode:       config.AgentConfig{Enabled: boolPtr(true)},
		Antigravity:  config.AgentConfig{Enabled: boolPtr(true)},
	}}
	_, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected codex stat error, got %v", err)
	}
}

func TestDetectDisabledAgentArtifacts_VSCodeTemplateReadError(t *testing.T) {
	root := t.TempDir()
	launcherPath := filepath.Join(root, ".agent-layer", "open-vscode.command")
	if err := os.MkdirAll(filepath.Dir(launcherPath), 0o755); err != nil {
		t.Fatalf("mkdir launcher dir: %v", err)
	}
	if err := os.WriteFile(launcherPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
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
	cfg := config.Config{Agents: config.AgentsConfig{Gemini: config.AgentConfig{Enabled: boolPtr(true)}, Claude: config.AgentConfig{Enabled: boolPtr(true)}, ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(false)}, VSCode: config.AgentConfig{Enabled: boolPtr(false)}, Antigravity: config.AgentConfig{Enabled: boolPtr(true)}, Codex: config.CodexConfig{Enabled: boolPtr(true)}}}
	_, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err == nil || !strings.Contains(err.Error(), "template boom") {
		t.Fatalf("expected template read error, got %v", err)
	}
}

func TestDetectDisabledAgentArtifacts_VSCodeSettingsReadError(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	settings := "{\n  // >>> agent-layer\n  // managed\n  // <<< agent-layer\n}\n"
	if err := os.WriteFile(settingsPath, []byte(settings), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.readErrs[normalizePath(settingsPath)] = errors.New("read boom")
	inst := &installer{root: root, sys: sys}
	cfg := config.Config{Agents: config.AgentsConfig{
		Gemini:       config.AgentConfig{Enabled: boolPtr(true)},
		Claude:       config.AgentConfig{Enabled: boolPtr(true)},
		ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(false)},
		Codex:        config.CodexConfig{Enabled: boolPtr(true)},
		VSCode:       config.AgentConfig{Enabled: boolPtr(false)},
		Antigravity:  config.AgentConfig{Enabled: boolPtr(true)},
	}}
	_, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected vscode settings read error, got %v", err)
	}
}

func TestDetectDisabledAgentArtifacts_VSCodePromptWalkError(t *testing.T) {
	root := t.TempDir()
	promptRoot := filepath.Join(root, ".vscode", "prompts")
	if err := os.MkdirAll(promptRoot, 0o755); err != nil {
		t.Fatalf("mkdir prompt dir: %v", err)
	}
	sys := newFaultSystem(RealSystem{})
	sys.walkErrs[normalizePath(promptRoot)] = errors.New("walk boom")
	inst := &installer{root: root, sys: sys}
	cfg := config.Config{Agents: config.AgentsConfig{
		Gemini:       config.AgentConfig{Enabled: boolPtr(true)},
		Claude:       config.AgentConfig{Enabled: boolPtr(true)},
		ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(false)},
		Codex:        config.CodexConfig{Enabled: boolPtr(true)},
		VSCode:       config.AgentConfig{Enabled: boolPtr(false)},
		Antigravity:  config.AgentConfig{Enabled: boolPtr(true)},
	}}
	_, err := detectDisabledAgentArtifacts(inst, &cfg)
	if err == nil || !strings.Contains(err.Error(), "walk boom") {
		t.Fatalf("expected vscode prompt walk error, got %v", err)
	}
}

func TestCountMarkdownFiles_ErrorBranches(t *testing.T) {
	root := t.TempDir()
	markdownRoot := filepath.Join(root, ".agent-layer", "slash-commands")
	if err := os.MkdirAll(markdownRoot, 0o755); err != nil {
		t.Fatalf("mkdir markdown root: %v", err)
	}

	t.Run("stat error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.statErrs[normalizePath(markdownRoot)] = errors.New("stat boom")
		inst := &installer{root: root, sys: sys}

		_, err := countMarkdownFiles(inst, markdownRoot)
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})

	t.Run("walk error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.walkErrs[normalizePath(markdownRoot)] = errors.New("walk boom")
		inst := &installer{root: root, sys: sys}

		_, err := countMarkdownFiles(inst, markdownRoot)
		if err == nil || !strings.Contains(err.Error(), "walk boom") {
			t.Fatalf("expected walk error, got %v", err)
		}
	})

	t.Run("missing root", func(t *testing.T) {
		inst := &installer{root: root, sys: RealSystem{}}
		count, err := countMarkdownFiles(inst, filepath.Join(root, "does-not-exist"))
		if err != nil {
			t.Fatalf("countMarkdownFiles: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected zero count for missing root, got %d", count)
		}
	})
}

func TestListGeneratedFilesWithSuffix_ErrorBranches(t *testing.T) {
	root := t.TempDir()
	promptRoot := filepath.Join(root, ".vscode", "prompts")
	if err := os.MkdirAll(promptRoot, 0o755); err != nil {
		t.Fatalf("mkdir prompts root: %v", err)
	}
	promptPath := filepath.Join(promptRoot, "alpha.prompt.md")
	if err := os.WriteFile(promptPath, []byte("<!--\n  "+generatedFileMarker+"\n-->\n"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	t.Run("read error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.readErrs[normalizePath(promptPath)] = errors.New("read boom")
		inst := &installer{root: root, sys: sys}

		_, _, err := listGeneratedFilesWithSuffix(inst, promptRoot, ".prompt.md")
		if err == nil || !strings.Contains(err.Error(), "read boom") {
			t.Fatalf("expected read error, got %v", err)
		}
	})

	t.Run("stat error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.statErrs[normalizePath(promptPath)] = errors.New("stat boom")
		inst := &installer{root: root, sys: sys}

		_, _, err := listGeneratedFilesWithSuffix(inst, promptRoot, ".prompt.md")
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})

	t.Run("walk error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.walkErrs[normalizePath(promptRoot)] = errors.New("walk boom")
		inst := &installer{root: root, sys: sys}

		_, _, err := listGeneratedFilesWithSuffix(inst, promptRoot, ".prompt.md")
		if err == nil || !strings.Contains(err.Error(), "walk boom") {
			t.Fatalf("expected walk error, got %v", err)
		}
	})

	t.Run("missing root", func(t *testing.T) {
		inst := &installer{root: root, sys: RealSystem{}}
		paths, latest, err := listGeneratedFilesWithSuffix(inst, filepath.Join(root, ".vscode", "missing-prompts"), ".prompt.md")
		if err != nil {
			t.Fatalf("listGeneratedFilesWithSuffix: %v", err)
		}
		if len(paths) != 0 {
			t.Fatalf("expected no paths for missing root, got %#v", paths)
		}
		if !latest.IsZero() {
			t.Fatalf("expected zero latest time for missing root, got %s", latest)
		}
	})

	t.Run("root stat error", func(t *testing.T) {
		sys := newFaultSystem(RealSystem{})
		sys.statErrs[normalizePath(promptRoot)] = errors.New("stat boom")
		inst := &installer{root: root, sys: sys}
		_, _, err := listGeneratedFilesWithSuffix(inst, promptRoot, ".prompt.md")
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected root stat error, got %v", err)
		}
	})

	t.Run("ignores non-generated files", func(t *testing.T) {
		manualPromptPath := filepath.Join(promptRoot, "manual.prompt.md")
		otherPath := filepath.Join(promptRoot, "notes.txt")
		if err := os.WriteFile(manualPromptPath, []byte("manual\n"), 0o644); err != nil {
			t.Fatalf("write manual prompt: %v", err)
		}
		if err := os.WriteFile(otherPath, []byte("notes\n"), 0o644); err != nil {
			t.Fatalf("write notes: %v", err)
		}

		if err := os.Remove(promptPath); err != nil {
			t.Fatalf("remove generated prompt: %v", err)
		}

		inst := &installer{root: root, sys: RealSystem{}}
		paths, latest, err := listGeneratedFilesWithSuffix(inst, promptRoot, ".prompt.md")
		if err != nil {
			t.Fatalf("listGeneratedFilesWithSuffix: %v", err)
		}
		if len(paths) != 0 {
			t.Fatalf("expected no generated prompt paths, got %#v", paths)
		}
		if !latest.IsZero() {
			t.Fatalf("expected zero latest time for non-generated prompts, got %s", latest)
		}
	})
}

func TestDetectDisabledAgentArtifacts_FindsManagedArtifacts(t *testing.T) {
	root := t.TempDir()

	codexFiles := map[string]string{
		filepath.Join(root, ".codex", "AGENTS.md"):                   "GENERATED FILE\n",
		filepath.Join(root, ".codex", "config.toml"):                 "# GENERATED FILE\n",
		filepath.Join(root, ".codex", "rules", "default.rules"):      "# GENERATED FILE\n",
		filepath.Join(root, ".codex", "skills", "alpha", "SKILL.md"): "<!--\n  GENERATED FILE\n-->\n",
		filepath.Join(root, ".agent", "skills", "beta", "SKILL.md"):  "<!--\n  GENERATED FILE\n-->\n",
	}
	for absPath, content := range codexFiles {
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", absPath, err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", absPath, err)
		}
	}

	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	promptPath := filepath.Join(root, ".vscode", "prompts", "alpha.prompt.md")
	if err := os.MkdirAll(filepath.Dir(promptPath), 0o755); err != nil {
		t.Fatalf("mkdir vscode prompt dir: %v", err)
	}
	settings := "{\n  // >>> agent-layer\n  // managed\n  // <<< agent-layer\n}\n"
	if err := os.WriteFile(settingsPath, []byte(settings), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
	if err := os.WriteFile(promptPath, []byte("<!--\n  GENERATED FILE\n-->\n"), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	launcherTemplate, err := templates.Read("launchers/open-vscode.command")
	if err != nil {
		t.Fatalf("read launcher template: %v", err)
	}
	launcherPath := filepath.Join(root, ".agent-layer", "open-vscode.command")
	if err := os.MkdirAll(filepath.Dir(launcherPath), 0o755); err != nil {
		t.Fatalf("mkdir launcher dir: %v", err)
	}
	if err := os.WriteFile(launcherPath, launcherTemplate, 0o755); err != nil {
		t.Fatalf("write launcher: %v", err)
	}

	cfg := config.Config{
		Agents: config.AgentsConfig{
			Gemini:       config.AgentConfig{Enabled: boolPtr(true)},
			Claude:       config.AgentConfig{Enabled: boolPtr(true)},
			ClaudeVSCode: config.AgentConfig{Enabled: boolPtr(true)},
			Codex:        config.CodexConfig{Enabled: boolPtr(false)},
			VSCode:       config.AgentConfig{Enabled: boolPtr(false)},
			Antigravity:  config.AgentConfig{Enabled: boolPtr(false)},
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
		".codex/AGENTS.md",
		".codex/config.toml",
		".codex/rules/default.rules",
		".codex/skills/alpha/SKILL.md",
		".agent/skills/beta/SKILL.md",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected detail %q, got %q", expected, joined)
		}
	}
	// .vscode/settings.json is shared (generated when either agent is enabled),
	// so it should NOT be flagged when claude-vscode is enabled.
	if strings.Contains(joined, ".vscode/settings.json") {
		t.Fatalf("unexpected .vscode/settings.json in disabled details when claude-vscode is enabled, got %q", joined)
	}
	// Prompts and launchers are vscode-only, so they SHOULD be flagged
	// even when claude-vscode is enabled (vscode is disabled).
	for _, expected := range []string{
		".vscode/prompts/alpha.prompt.md",
		".agent-layer/open-vscode.command",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected vscode-only artifact %q to be flagged, got %q", expected, joined)
		}
	}
}

func TestExactTemplateMatcher_Branches(t *testing.T) {
	matcher := exactTemplateMatcher("launchers/open-vscode.command")
	templateData, err := templates.Read("launchers/open-vscode.command")
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	matched, err := matcher(templateData)
	if err != nil {
		t.Fatalf("matcher error: %v", err)
	}
	if !matched {
		t.Fatal("expected exact template match")
	}
	matched, err = matcher([]byte("not-template"))
	if err != nil {
		t.Fatalf("matcher error: %v", err)
	}
	if matched {
		t.Fatal("expected non-template content to fail match")
	}
}

func TestSortedMapKeys_Sorts(t *testing.T) {
	keys := sortedMapKeys(map[string]string{
		"beta":  "2",
		"alpha": "1",
	})
	if len(keys) != 2 || keys[0] != "alpha" || keys[1] != "beta" {
		t.Fatalf("unexpected key order: %#v", keys)
	}
}

func TestDetectFloatingDependencies_EnvAndURL(t *testing.T) {
	cfg := config.Config{
		MCP: config.MCPConfig{
			Servers: []config.MCPServer{
				{
					ID:      "sample",
					Enabled: boolPtr(true),
					URL:     "https://example.com/tool@next",
					Env: map[string]string{
						"PACKAGE_REF": "tool@canary",
					},
				},
			},
		},
	}
	check := detectFloatingDependencies(&cfg)
	if check == nil {
		t.Fatal("expected floating dependencies readiness check")
	}
	joined := strings.Join(check.Details, "\n")
	if !strings.Contains(joined, "url=") || !strings.Contains(joined, "env.PACKAGE_REF") {
		t.Fatalf("expected url/env floating details, got %q", joined)
	}
}

func TestReadAgentLayerEnvForReadiness_Branches(t *testing.T) {
	t.Run("missing env file returns empty map", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		values, err := readAgentLayerEnvForReadiness(inst)
		if err != nil {
			t.Fatalf("readAgentLayerEnvForReadiness: %v", err)
		}
		if len(values) != 0 {
			t.Fatalf("expected empty env values, got %#v", values)
		}
	})

	t.Run("read error is surfaced", func(t *testing.T) {
		root := t.TempDir()
		envPath := filepath.Join(root, ".agent-layer", ".env")
		if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
			t.Fatalf("mkdir env dir: %v", err)
		}
		if err := os.WriteFile(envPath, []byte("AL_SAMPLE=value\n"), 0o644); err != nil {
			t.Fatalf("write env file: %v", err)
		}

		sys := newFaultSystem(RealSystem{})
		sys.readErrs[normalizePath(envPath)] = errors.New("read boom")
		inst := &installer{root: root, sys: sys}
		_, err := readAgentLayerEnvForReadiness(inst)
		if err == nil || !strings.Contains(err.Error(), "read boom") {
			t.Fatalf("expected read error, got %v", err)
		}
	})

	t.Run("parse error is surfaced", func(t *testing.T) {
		root := t.TempDir()
		envPath := filepath.Join(root, ".agent-layer", ".env")
		if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
			t.Fatalf("mkdir env dir: %v", err)
		}
		if err := os.WriteFile(envPath, []byte("BROKEN_LINE\n"), 0o644); err != nil {
			t.Fatalf("write invalid env file: %v", err)
		}

		inst := &installer{root: root, sys: RealSystem{}}
		_, err := readAgentLayerEnvForReadiness(inst)
		if err == nil || !strings.Contains(err.Error(), "parse") {
			t.Fatalf("expected parse error, got %v", err)
		}
	})

	t.Run("only AL_ keys are retained", func(t *testing.T) {
		root := t.TempDir()
		envPath := filepath.Join(root, ".agent-layer", ".env")
		if err := os.MkdirAll(filepath.Dir(envPath), 0o755); err != nil {
			t.Fatalf("mkdir env dir: %v", err)
		}
		if err := os.WriteFile(envPath, []byte("AL_KEEP=1\nOTHER=2\nAL_EMPTY=\n"), 0o644); err != nil {
			t.Fatalf("write env file: %v", err)
		}

		inst := &installer{root: root, sys: RealSystem{}}
		values, err := readAgentLayerEnvForReadiness(inst)
		if err != nil {
			t.Fatalf("readAgentLayerEnvForReadiness: %v", err)
		}
		if _, ok := values["AL_KEEP"]; !ok {
			t.Fatalf("expected AL_KEEP in filtered env, got %#v", values)
		}
		if _, ok := values["OTHER"]; ok {
			t.Fatalf("did not expect OTHER in filtered env, got %#v", values)
		}
	})
}

func TestReadinessEnvHelpers_Branches(t *testing.T) {
	filtered := filterAgentLayerEnvForReadiness(map[string]string{
		"AL_ONE": "1",
		"OTHER":  "2",
	})
	if _, ok := filtered["AL_ONE"]; !ok {
		t.Fatalf("expected AL_ONE in filtered env, got %#v", filtered)
	}
	if _, ok := filtered["OTHER"]; ok {
		t.Fatalf("did not expect OTHER in filtered env, got %#v", filtered)
	}
	if len(filterAgentLayerEnvForReadiness(nil)) != 0 {
		t.Fatal("expected empty filtered map for nil input")
	}

	if _, ok := readinessEnvValue("AL_REPO_ROOT", map[string]string{}, "", RealSystem{}); ok {
		t.Fatal("expected built-in AL_REPO_ROOT lookup to fail when repo root is empty")
	}
	if value, ok := readinessEnvValue("AL_REPO_ROOT", map[string]string{}, "/repo", RealSystem{}); !ok || value != "/repo" {
		t.Fatalf("expected built-in AL_REPO_ROOT value, got value=%q ok=%v", value, ok)
	}

	processValue := "from-process"
	sys := newFaultSystem(RealSystem{})
	sys.lookupEnvs["AL_TOKEN"] = &processValue
	if value, ok := readinessEnvValue("AL_TOKEN", map[string]string{"AL_TOKEN": "from-file"}, "/repo", sys); !ok || value != "from-process" {
		t.Fatalf("expected process env precedence, got value=%q ok=%v", value, ok)
	}

	emptyProcess := ""
	sys.lookupEnvs["AL_FALLBACK"] = &emptyProcess
	if value, ok := readinessEnvValue("AL_FALLBACK", map[string]string{"AL_FALLBACK": "from-file"}, "/repo", sys); !ok || value != "from-file" {
		t.Fatalf("expected fallback to file env, got value=%q ok=%v", value, ok)
	}
	if _, ok := readinessEnvValue("AL_MISSING", map[string]string{"AL_MISSING": ""}, "/repo", sys); ok {
		t.Fatal("expected missing readiness env value when both sources are empty")
	}

	if _, ok := processEnvValue(nil, "AL_TOKEN"); ok {
		t.Fatal("expected processEnvValue to return missing for nil system")
	}
	if _, ok := processEnvValue(sys, "AL_UNKNOWN"); ok {
		t.Fatal("expected processEnvValue to return missing for unset key")
	}

	inst := &installer{root: "/repo", sys: sys}
	subst := readinessSubstitutionEnv("${AL_REPO_ROOT}/${AL_TOKEN}/${AL_MISSING}", map[string]string{"AL_MISSING": ""}, inst.root, inst.sys)
	if subst["AL_REPO_ROOT"] != "/repo" || subst["AL_TOKEN"] != "from-process" {
		t.Fatalf("unexpected substitution env values: %#v", subst)
	}
	if _, ok := subst["AL_MISSING"]; ok {
		t.Fatalf("did not expect unresolved env var in substitution map: %#v", subst)
	}
}

func TestCheckPathExpansionValue_Branches(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: RealSystem{}}

	if detail, err := checkPathExpansionValue(inst, map[string]string{}, 0, "s1", "command", "npx -y mcp-server", true); err != nil || detail != "" {
		t.Fatalf("expected non-path value to produce no readiness detail, detail=%q err=%v", detail, err)
	}

	detail, err := checkPathExpansionValue(inst, map[string]string{}, 0, "s1", "command", "${AL_REPO_ROOT}/${AL_MISSING}/tool", true)
	if err != nil {
		t.Fatalf("checkPathExpansionValue unresolved placeholder: %v", err)
	}
	if !strings.Contains(detail, "unresolved path placeholder") {
		t.Fatalf("expected unresolved-placeholder detail, got %q", detail)
	}

	weirdDetail, weirdErr := checkPathExpansionValue(inst, map[string]string{"AL_WEIRD": "${UNEXPANDED}"}, 0, "s1", "command", "~/${AL_WEIRD}", false)
	if weirdErr != nil {
		t.Fatalf("checkPathExpansionValue weird expansion: %v", weirdErr)
	}
	if !strings.Contains(weirdDetail, "did not expand cleanly") {
		t.Fatalf("expected dirty-expansion detail, got %q", weirdDetail)
	}

	commandDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatalf("mkdir command dir: %v", err)
	}
	detail, err = checkPathExpansionValue(inst, map[string]string{}, 1, "s2", "command", "${AL_REPO_ROOT}/bin", true)
	if err != nil {
		t.Fatalf("checkPathExpansionValue directory command: %v", err)
	}
	if !strings.Contains(detail, "directory") {
		t.Fatalf("expected directory detail, got %q", detail)
	}

	commandPath := filepath.Join(commandDir, "tool")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write command path: %v", err)
	}
	if detail, err = checkPathExpansionValue(inst, map[string]string{}, 2, "s3", "command", "${AL_REPO_ROOT}/bin/tool", true); err != nil || detail != "" {
		t.Fatalf("expected clean command path, detail=%q err=%v", detail, err)
	}

	statFault := newFaultSystem(RealSystem{})
	statTarget := filepath.Join(root, "broken", "tool")
	statFault.statErrs[normalizePath(statTarget)] = errors.New("stat boom")
	instWithFault := &installer{root: root, sys: statFault}
	if _, err = checkPathExpansionValue(instWithFault, map[string]string{}, 3, "s4", "command", "${AL_REPO_ROOT}/broken/tool", true); err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected stat error, got %v", err)
	}
}

func TestDetectGeneratedSecretRisk_ErrorBranches(t *testing.T) {
	t.Run("stat error", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, ".mcp.json")
		sys := newFaultSystem(RealSystem{})
		sys.statErrs[normalizePath(target)] = errors.New("stat boom")
		inst := &installer{root: root, sys: sys}
		_, err := detectGeneratedSecretRisk(inst, `api_key = "hardcoded-secret"`)
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})

	t.Run("read error", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, ".mcp.json")
		if err := os.WriteFile(target, []byte("{\"token\":\"abc\"}\n"), 0o644); err != nil {
			t.Fatalf("write target: %v", err)
		}
		sys := newFaultSystem(RealSystem{})
		sys.readErrs[normalizePath(target)] = errors.New("read boom")
		inst := &installer{root: root, sys: sys}
		_, err := detectGeneratedSecretRisk(inst, `api_key = "hardcoded-secret"`)
		if err == nil || !strings.Contains(err.Error(), "read boom") {
			t.Fatalf("expected read error, got %v", err)
		}
	})

	t.Run("ignores directories and non-secret files", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, ".codex", "config.toml"), 0o755); err != nil {
			t.Fatalf("mkdir config dir: %v", err)
		}
		target := filepath.Join(root, ".mcp.json")
		if err := os.WriteFile(target, []byte("{\"safe\":true}\n"), 0o644); err != nil {
			t.Fatalf("write target: %v", err)
		}
		inst := &installer{root: root, sys: RealSystem{}}
		check, err := detectGeneratedSecretRisk(inst, `api_key = "hardcoded-secret"`)
		if err != nil {
			t.Fatalf("detectGeneratedSecretRisk: %v", err)
		}
		if check != nil {
			t.Fatalf("expected no secret-risk check, got %#v", check)
		}
	})
}

func TestDetectPathExpansionAnomalies_AdditionalBranches(t *testing.T) {
	t.Run("ignores disabled and non-stdio servers", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		cfg := config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "disabled-stdio",
						Enabled:   boolPtr(false),
						Transport: config.TransportStdio,
						Command:   "${AL_REPO_ROOT}/missing",
					},
					{
						ID:        "enabled-http",
						Enabled:   boolPtr(true),
						Transport: config.TransportHTTP,
						URL:       "https://example.com",
						Command:   "${AL_REPO_ROOT}/missing",
					},
				},
			},
		}
		check, err := detectPathExpansionAnomalies(inst, &cfg, map[string]string{})
		if err != nil {
			t.Fatalf("detectPathExpansionAnomalies: %v", err)
		}
		if check != nil {
			t.Fatalf("expected nil check, got %#v", check)
		}
	})

	t.Run("arg stat error propagates", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "broken", "tool")
		faults := newFaultSystem(RealSystem{})
		faults.statErrs[normalizePath(target)] = errors.New("stat boom")
		inst := &installer{root: root, sys: faults}
		cfg := config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "stdio-server",
						Enabled:   boolPtr(true),
						Transport: config.TransportStdio,
						Command:   "node",
						Args:      []string{"${AL_REPO_ROOT}/broken/tool"},
					},
				},
			},
		}
		_, err := detectPathExpansionAnomalies(inst, &cfg, map[string]string{})
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})
}

func boolPtr(value bool) *bool {
	return &value
}
