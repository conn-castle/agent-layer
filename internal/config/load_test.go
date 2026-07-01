package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

func TestLoadProjectConfig(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)

	if err := os.MkdirAll(paths.InstructionsDir, 0o700); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SkillsDir, 0o700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}

	config := `
[approvals]
mode = "all"

[agents.antigravity]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = false
`
	if err := os.WriteFile(paths.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_rules.md"), []byte("base"), 0o600); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	cmdContent := `---
description: test command
---

Do it.`
	helloDir := filepath.Join(paths.SkillsDir, "hello")
	if err := os.MkdirAll(helloDir, 0o700); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(helloDir, "SKILL.md"), []byte(cmdContent), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o600); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}

	project, err := LoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig error: %v", err)
	}
	if project.Root != root {
		t.Fatalf("expected root %s, got %s", root, project.Root)
	}
	if len(project.Instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(project.Instructions))
	}
	if len(project.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(project.Skills))
	}
	if len(project.CommandsAllow) != 1 || project.CommandsAllow[0] != "git status" {
		t.Fatalf("unexpected commands allow: %v", project.CommandsAllow)
	}
}

func TestLoadProjectConfigMissingConfig(t *testing.T) {
	_, err := LoadProjectConfig(t.TempDir())
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "missing config file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadProjectConfigMissingEnv(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)

	if err := os.MkdirAll(paths.InstructionsDir, 0o700); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SkillsDir, 0o700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	config := `
[approvals]
mode = "all"

[agents.antigravity]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = false
`
	if err := os.WriteFile(paths.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_rules.md"), []byte("base"), 0o600); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	cmdContent := `---
description: test command
---

Do it.`
	if err := os.WriteFile(filepath.Join(paths.SkillsDir, "hello.md"), []byte(cmdContent), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o600); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}

	_, err := LoadProjectConfig(root)
	if err == nil {
		t.Fatalf("expected missing env error")
	}
	if !strings.Contains(err.Error(), "missing env file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadProjectConfigMissingInstructions(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)

	if err := os.MkdirAll(paths.SkillsDir, 0o700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	config := `
[approvals]
mode = "all"

[agents.antigravity]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = false
`
	if err := os.WriteFile(paths.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	cmdContent := `---
description: test command
---

Do it.`
	if err := os.WriteFile(filepath.Join(paths.SkillsDir, "hello.md"), []byte(cmdContent), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o600); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}

	_, err := LoadProjectConfig(root)
	if err == nil || !strings.Contains(err.Error(), "missing instructions directory") {
		t.Fatalf("expected missing instructions error, got %v", err)
	}
}

func TestLoadProjectConfigMissingSkills(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)

	if err := os.MkdirAll(paths.InstructionsDir, 0o700); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	config := `
[approvals]
mode = "all"

[agents.antigravity]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = false
`
	if err := os.WriteFile(paths.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_rules.md"), []byte("base"), 0o600); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o600); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}

	_, err := LoadProjectConfig(root)
	if err == nil || !strings.Contains(err.Error(), "missing skills directory") {
		t.Fatalf("expected missing skills error, got %v", err)
	}
}

func TestLoadProjectConfigMissingCommandsAllow(t *testing.T) {
	root := t.TempDir()
	paths := DefaultPaths(root)

	if err := os.MkdirAll(paths.InstructionsDir, 0o700); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SkillsDir, 0o700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	config := `
[approvals]
mode = "all"

[agents.antigravity]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = false
`
	if err := os.WriteFile(paths.ConfigPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_rules.md"), []byte("base"), 0o600); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	cmdContent := `---
description: test command
---

Do it.`
	helloDir := filepath.Join(paths.SkillsDir, "hello")
	if err := os.MkdirAll(helloDir, 0o700); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(helloDir, "SKILL.md"), []byte(cmdContent), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	_, err := LoadProjectConfig(root)
	if err == nil || !strings.Contains(err.Error(), "missing commands allowlist") {
		t.Fatalf("expected missing commands allowlist error, got %v", err)
	}
}

func TestLoadEnvInvalidFormat(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	// Invalid env file - line without equals sign
	if err := os.WriteFile(path, []byte("INVALID"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	_, err := LoadEnv(path)
	if err == nil {
		t.Fatalf("expected error for invalid env file")
	}
	if !strings.Contains(err.Error(), "invalid env file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadTemplateConfig(t *testing.T) {
	cfg, err := LoadTemplateConfig()
	if err != nil {
		t.Fatalf("LoadTemplateConfig error: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected config, got nil")
	}
	// The slim install seed deliberately ships no [[mcp.servers]] entries — those
	// live in the wizard-only mcp-catalog.toml. Verify the seed parses cleanly and
	// retains other top-level sections.
	if len(cfg.MCP.Servers) != 0 {
		t.Fatalf("expected slim seed to ship zero MCP servers, got %d", len(cfg.MCP.Servers))
	}
	if cfg.Approvals.Mode == "" {
		t.Fatalf("expected approvals.mode to be present in template config")
	}
	// AskUserQuestion is allowed by default now: the shipped template sets no
	// disable_question_tool flag and carries no permissions.deny. The opt-in
	// `al wizard` toggle sets disable_question_tool = true, after which sync
	// injects the deny plus a PreToolUse hook.
	if cfg.Agents.Claude.DisableQuestionTool != nil {
		t.Fatalf("expected template to leave disable_question_tool unset, got %v", *cfg.Agents.Claude.DisableQuestionTool)
	}
	if permissions, ok := cfg.Agents.Claude.AgentSpecific["permissions"].(map[string]any); ok {
		if stringSliceValueContains(permissions["deny"], "AskUserQuestion") {
			t.Fatalf("expected template not to deny AskUserQuestion by default, got %v", permissions["deny"])
		}
	}
	// A default PreToolUse matcher would also block the tool, so assert the
	// template ships none (the deny check alone would miss a hook-only block).
	if hooks, ok := cfg.Agents.Claude.AgentSpecific["hooks"].(map[string]any); ok {
		if pre, ok := hooks["PreToolUse"].([]any); ok {
			for _, entry := range pre {
				if m, ok := entry.(map[string]any); ok && m["matcher"] == "AskUserQuestion" {
					t.Fatalf("expected template not to block AskUserQuestion via PreToolUse hook")
				}
			}
		}
	}
}

func stringSliceValueContains(value any, want string) bool {
	switch values := value.(type) {
	case []string:
		for _, value := range values {
			if value == want {
				return true
			}
		}
	case []any:
		for _, value := range values {
			if value == want {
				return true
			}
		}
	}
	return false
}

func TestLoadTemplateConfigReadError(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		return nil, errors.New("mock read error")
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := LoadTemplateConfig()
	if err == nil {
		t.Fatalf("expected error when template read fails")
	}
	if !strings.Contains(err.Error(), "failed to read template") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseConfigLenient_ValidTOMLMissingRequiredFields(t *testing.T) {
	// A pre-v0.8.1 config missing [agents.claude_vscode] should succeed
	// with lenient parsing even though strict ParseConfig would reject it.
	toml := `
[approvals]
mode = "all"

[agents.antigravity]
enabled = true

[agents.claude]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = false
`
	cfg, err := ParseConfigLenient([]byte(toml), "test")
	if err != nil {
		t.Fatalf("expected lenient parse to succeed, got: %v", err)
	}
	if cfg.Agents.ClaudeVSCode.Enabled != nil {
		t.Fatal("expected claude_vscode.enabled to be nil (missing from config)")
	}
	// Strict parse should fail on the same input.
	_, strictErr := ParseConfig([]byte(toml), "test")
	if strictErr == nil {
		t.Fatal("expected strict ParseConfig to fail for missing claude_vscode.enabled")
	}
}

func TestParseConfigLenient_InvalidTOMLSyntax(t *testing.T) {
	_, err := ParseConfigLenient([]byte("invalid toml [[["), "test")
	if err == nil {
		t.Fatal("expected error for invalid TOML syntax")
	}
	if !strings.Contains(err.Error(), "invalid config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfigLenient(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")

	// Write a config missing required fields.
	toml := `
[approvals]
mode = "all"

[agents.antigravity]
enabled = true
`
	if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfigLenient(path)
	if err != nil {
		t.Fatalf("expected lenient load to succeed, got: %v", err)
	}
	if cfg.Approvals.Mode != ApprovalModeAll {
		t.Fatalf("expected approvals.mode = %s, got %q", ApprovalModeAll, cfg.Approvals.Mode)
	}
}

func TestParseConfigLenient_LegacyClaudeVSCodeAlias(t *testing.T) {
	// Pre-migration config uses the legacy kebab-case key.
	tomlData := `
[approvals]
mode = "all"

[agents.antigravity]
enabled = false

[agents.claude]
enabled = true

[agents.claude-vscode]
enabled = true

[agents.codex]
enabled = false

[agents.vscode]
enabled = false

[agents.copilot_cli]
enabled = false
`
	cfg, err := ParseConfigLenient([]byte(tomlData), "test")
	if err != nil {
		t.Fatalf("ParseConfigLenient: %v", err)
	}
	if cfg.Agents.ClaudeVSCode.Enabled == nil {
		t.Fatal("expected claude_vscode.enabled to be carried from legacy claude-vscode key")
	}
	if !*cfg.Agents.ClaudeVSCode.Enabled {
		t.Fatal("expected claude_vscode.enabled = true")
	}
}

func TestParseConfigLenient_LegacyAliasDoesNotOverrideNewKey(t *testing.T) {
	// When both old and new keys exist, the new key takes precedence.
	tomlData := `
[agents.claude-vscode]
enabled = true

[agents.claude_vscode]
enabled = false
`
	cfg, err := ParseConfigLenient([]byte(tomlData), "test")
	if err != nil {
		t.Fatalf("ParseConfigLenient: %v", err)
	}
	if cfg.Agents.ClaudeVSCode.Enabled == nil {
		t.Fatal("expected claude_vscode.enabled to be set")
	}
	if *cfg.Agents.ClaudeVSCode.Enabled {
		t.Fatal("expected new key (false) to take precedence over legacy key (true)")
	}
}

func TestLoadConfigLenient_MissingFile(t *testing.T) {
	_, err := LoadConfigLenient("/nonexistent/config.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "missing config file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseConfig_ValidationErrorIncludesGuidance(t *testing.T) {
	// A config missing required fields should produce an error with guidance text.
	toml := `
[approvals]
mode = "all"
`
	_, err := ParseConfig([]byte(toml), "test")
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "al wizard") || !strings.Contains(err.Error(), "al doctor") {
		t.Fatalf("expected error to contain guidance about wizard/doctor, got: %v", err)
	}
	if !errors.Is(err, ErrConfigValidation) {
		t.Fatalf("expected error to wrap ErrConfigValidation, got: %v", err)
	}
}

func TestParseConfig_RejectsUnknownKeys(t *testing.T) {
	// agents.claude_vscode uses EnableOnlyConfig (no Model field),
	// so "model" is an unknown key that strict decode must reject.
	data := `
[approvals]
mode = "all"
[agents.antigravity]
enabled = true
[agents.claude]
enabled = true
[agents.claude_vscode]
enabled = true
model = "some-model"
[agents.codex]
enabled = true
[agents.vscode]
enabled = true
[agents.copilot_cli]
enabled = true
`
	_, err := ParseConfig([]byte(data), "test")
	if err == nil {
		t.Fatal("expected error for unknown key agents.claude_vscode.model")
	}
	if !strings.Contains(err.Error(), "unrecognized") {
		t.Fatalf("expected unrecognized key error, got: %v", err)
	}
	// Must be a validation error so wizard/doctor lenient fallback triggers.
	if !errors.Is(err, ErrConfigValidation) {
		t.Fatalf("unrecognized key error should match ErrConfigValidation, got: %v", err)
	}
	// Must include repair guidance.
	if !strings.Contains(err.Error(), "al wizard") || !strings.Contains(err.Error(), "al doctor") {
		t.Fatalf("expected error to contain guidance about wizard/doctor, got: %v", err)
	}
}

func TestParseConfig_LegacyGeminiPointsToUpgrade(t *testing.T) {
	data := `
[approvals]
mode = "all"
[agents.gemini]
enabled = true
[agents.claude]
enabled = true
[agents.claude_vscode]
enabled = true
[agents.codex]
enabled = true
[agents.vscode]
enabled = true
[agents.copilot_cli]
enabled = true
`
	_, err := ParseConfig([]byte(data), "test")
	if err == nil {
		t.Fatal("expected error for legacy agents.gemini")
	}
	// Assert the legacy-Gemini specific marker, not just "al upgrade" guidance
	// (the generic ConfigValidationGuidance message also contains "al wizard"
	// and could mention agents.antigravity via suggestRename, masking a
	// regression that removes the legacy-Gemini detection path).
	if !strings.Contains(err.Error(), "agents.gemini is no longer supported") {
		t.Fatalf("expected legacy-Gemini specific marker in error, got: %v", err)
	}
	if !errors.Is(err, ErrConfigValidation) {
		t.Fatalf("legacy Gemini error should match ErrConfigValidation, got: %v", err)
	}
	// Must also match ErrConfigNeedsUpgrade so repair tools (the wizard) redirect
	// to `al upgrade` instead of attempting an in-place fix that dead-ends at sync.
	if !errors.Is(err, ErrConfigNeedsUpgrade) {
		t.Fatalf("legacy Gemini error should match ErrConfigNeedsUpgrade, got: %v", err)
	}
}

func TestParseConfig_AntigravityAgentSpecificModelPointsToUpgrade(t *testing.T) {
	data := `
[approvals]
mode = "all"
[agents.antigravity]
enabled = true
[agents.antigravity.agent_specific]
model = "Gemini 3.5 Flash (High)"
[agents.claude]
enabled = true
[agents.claude_vscode]
enabled = true
[agents.codex]
enabled = true
[agents.vscode]
enabled = true
[agents.copilot_cli]
enabled = true
`
	_, err := ParseConfig([]byte(data), "test")
	if err == nil {
		t.Fatal("expected error for legacy agents.antigravity.agent_specific.model")
	}
	if !strings.Contains(err.Error(), "agents.antigravity.agent_specific.model is not supported") {
		t.Fatalf("expected Antigravity agent_specific model marker in error, got: %v", err)
	}
	if !errors.Is(err, ErrConfigValidation) {
		t.Fatalf("legacy Antigravity model error should match ErrConfigValidation, got: %v", err)
	}
	if !errors.Is(err, ErrConfigNeedsUpgrade) {
		t.Fatalf("legacy Antigravity model error should match ErrConfigNeedsUpgrade, got: %v", err)
	}
	if strings.Contains(err.Error(), "al wizard") {
		t.Fatalf("legacy Antigravity model error should not route to wizard guidance, got: %v", err)
	}
}

func TestHasLegacyAntigravityAgentSpecificModel(t *testing.T) {
	data := []byte(`
[agents.antigravity]
enabled = true
[agents.antigravity.agent_specific]
model = "Gemini 3.5 Flash (High)"
`)
	if !HasLegacyAntigravityAgentSpecificModel(data) {
		t.Fatal("expected legacy Antigravity agent_specific model to be detected")
	}
	if HasLegacyAntigravityAgentSpecificModel([]byte("# model = \"Gemini\"\n[agents.antigravity]\nmodel = \"Gemini\"\n")) {
		t.Fatal("typed model or commented text must not be detected as legacy agent_specific model")
	}
}

func TestHasLegacyGeminiConfig_TabIndentedHeader(t *testing.T) {
	// Tab-indented TOML headers are valid; the previous space-strip helper
	// missed them and skipped the legacy-Gemini diagnostic. The map-based
	// helper relies on the TOML parser, which handles tabs natively.
	data := []byte("\t[agents.gemini]\n\tenabled = true\n")
	if !HasLegacyGeminiConfig(data) {
		t.Fatal("expected HasLegacyGeminiConfig to detect tab-indented [agents.gemini]")
	}
}

func TestHasLegacyGeminiConfig_CommentLineDoesNotFalsePositive(t *testing.T) {
	// A comment line mentioning [agents.gemini] is not a real table — the
	// substring-based helper used to false-positive here.
	data := []byte("# legacy: [agents.gemini] removed in 0.10.2\n[agents.antigravity]\nenabled = true\n")
	if HasLegacyGeminiConfig(data) {
		t.Fatal("expected comment mentioning [agents.gemini] to not be detected as a real table")
	}
}

func TestParseConfigLenient_AliasesLegacyGeminiEnabled(t *testing.T) {
	// Repair tools (wizard, doctor) need to see the user's intended
	// Antigravity enablement on a pre-migration repo so CheckAntigravityBinary
	// gates correctly. Without the alias, lenient parse would treat
	// Antigravity as unset and skip the binary check.
	data := []byte(`
[approvals]
mode = "all"
[agents.gemini]
enabled = true
[agents.claude]
enabled = true
[agents.claude_vscode]
enabled = true
[agents.codex]
enabled = true
[agents.vscode]
enabled = true
[agents.copilot_cli]
enabled = false
`)
	cfg, err := ParseConfigLenient(data, "test")
	if err != nil {
		t.Fatalf("ParseConfigLenient: %v", err)
	}
	if !IsAgentEnabled(cfg.Agents.Antigravity.Enabled) {
		t.Fatal("expected Antigravity.Enabled to be aliased from agents.gemini.enabled")
	}
}

func TestParseConfig_AllowsCustomAgentConfig(t *testing.T) {
	data := `
[approvals]
mode = "all"
[agents.antigravity]
enabled = true
[agents.antigravity.agent_specific]
permissions.deny = ["command(rm:*)"]
[agents.antigravity.agent_specific.features]
example_feature = true
[agents.claude]
enabled = true
[agents.claude.agent_specific]
features.example_feature = true
[agents.claude_vscode]
enabled = true
[agents.codex]
enabled = true
[agents.codex.agent_specific]
features.prevent_idle_sleep = true
[agents.vscode]
enabled = true
[agents.copilot_cli]
enabled = true
	`
	cfg, err := ParseConfig([]byte(data), "test")
	if err != nil {
		t.Fatalf("expected agent-specific config to parse, got: %v", err)
	}
	features, ok := cfg.Agents.Antigravity.AgentSpecific["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected antigravity agent-specific features map, got %#v", cfg.Agents.Antigravity.AgentSpecific["features"])
	}
	if value, ok := features["example_feature"].(bool); !ok || !value {
		t.Fatalf("expected antigravity features.example_feature=true, got %#v", features["example_feature"])
	}
	permissions, ok := cfg.Agents.Antigravity.AgentSpecific["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("expected antigravity agent-specific permissions map, got %#v", cfg.Agents.Antigravity.AgentSpecific["permissions"])
	}
	if !stringSliceValueContains(permissions["deny"], "command(rm:*)") {
		t.Fatalf("expected antigravity permissions.deny to include command(rm:*), got %#v", permissions["deny"])
	}
}

func TestParseConfig_TOMLSyntaxErrorIsNotValidationError(t *testing.T) {
	_, err := ParseConfig([]byte(`{{{`), "test")
	if err == nil {
		t.Fatal("expected TOML syntax error")
	}
	if errors.Is(err, ErrConfigValidation) {
		t.Fatalf("TOML syntax error should not match ErrConfigValidation, got: %v", err)
	}
}
