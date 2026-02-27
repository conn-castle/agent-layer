package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/testutil"
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

	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}}}
	_, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "read boom") {
		t.Fatalf("expected settings read error, got %v", err)
	}
}

func TestDetectVSCodeNoSyncStaleness_VSCodeDisabledNoFinding(t *testing.T) {
	inst := &installer{root: t.TempDir(), sys: RealSystem{}}
	cfg := config.Config{Agents: config.AgentsConfig{
		VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
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
		VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
		ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)},
	}}
	check, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err != nil {
		t.Fatalf("detectVSCodeNoSyncStaleness: %v", err)
	}
	if check == nil {
		t.Fatal("expected readiness finding for claude_vscode-only config with missing managed block")
	}
	joined := strings.Join(check.Details, "\n")
	if !strings.Contains(joined, "missing Agent Layer managed block") {
		t.Fatalf("expected missing managed block detail, got %#v", check.Details)
	}
	// .vscode/mcp.json and prompts are only generated for agents.vscode, not claude_vscode.
	if strings.Contains(joined, ".vscode/mcp.json") {
		t.Fatalf("should not flag .vscode/mcp.json for claude_vscode-only config, got %#v", check.Details)
	}
	if strings.Contains(joined, "prompts") {
		t.Fatalf("should not flag .vscode/prompts for claude_vscode-only config, got %#v", check.Details)
	}
	// Claude outputs (.mcp.json, .claude/settings.json) should be flagged when claude_vscode is enabled.
	if !strings.Contains(joined, ".mcp.json") {
		t.Fatalf("expected .mcp.json to be flagged for claude_vscode-only config, got %#v", check.Details)
	}
	if !strings.Contains(joined, ".claude/settings.json") {
		t.Fatalf("expected .claude/settings.json to be flagged for claude_vscode-only config, got %#v", check.Details)
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
	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}}}
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

	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}}}
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

	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}}}
	_, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected settings stat error, got %v", err)
	}
}

func TestDetectVSCodeNoSyncStaleness_SkillsStatError(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, ".agent-layer", "skills")
	sys := newFaultSystem(RealSystem{})
	sys.statErrs[normalizePath(skillsRoot)] = errors.New("stat boom")
	inst := &installer{root: root, sys: sys}

	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}}}
	_, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "stat boom") {
		t.Fatalf("expected skills stat error, got %v", err)
	}
}

func TestDetectVSCodeNoSyncStaleness_PromptWalkError(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, ".agent-layer", "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("mkdir skills root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsRoot, "alpha.md"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	promptRoot := filepath.Join(root, ".vscode", "prompts")
	if err := os.MkdirAll(promptRoot, 0o755); err != nil {
		t.Fatalf("mkdir prompt root: %v", err)
	}

	sys := newFaultSystem(RealSystem{})
	sys.walkErrs[normalizePath(promptRoot)] = errors.New("walk boom")
	inst := &installer{root: root, sys: sys}

	cfg := config.Config{Agents: config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}}}
	_, err := detectVSCodeNoSyncStaleness(inst, &cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
	if err == nil || !strings.Contains(err.Error(), "walk boom") {
		t.Fatalf("expected prompt walk error, got %v", err)
	}
}
