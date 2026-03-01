package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestCheckConfig_LenientFallback_InjectsBuiltInEnv_NoEnvFile(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a config that fails strict validation.
	partialConfig := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true
[agents.claude]
enabled = true
[agents.codex]
enabled = false
[agents.vscode]
enabled = true
[agents.antigravity]
enabled = false
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(partialConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	// Deliberately omit .env file.
	if err := os.MkdirAll(filepath.Join(configDir, "instructions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "instructions", "00_base.md"), []byte("# Base"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "commands.allow"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	results, cfg := CheckConfig(root)

	configResult := requireResultByCheckName(t, results, messages.DoctorCheckNameConfig)
	if configResult.Status != StatusFail {
		t.Fatalf("expected config FAIL result, got %v", results)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config from lenient fallback")
	}

	// AL_REPO_ROOT must still be injected even when .env is missing.
	got := cfg.Env[config.BuiltinRepoRootEnvVar]
	if got != root {
		t.Fatalf("expected %s=%q even without .env file, got %q", config.BuiltinRepoRootEnvVar, root, got)
	}
}

func TestCheckConfig_LenientFallback_UnknownKeys(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a config with an unknown key (model on an enable-only agent).
	// Strict loading will fail with an ErrConfigValidation-wrapped unknown-key
	// error; lenient loading should succeed.
	unknownKeyConfig := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true
[agents.claude]
enabled = true
[agents.claude_vscode]
enabled = true
model = "some-model"
[agents.codex]
enabled = false
[agents.vscode]
enabled = true
[agents.antigravity]
enabled = false
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(unknownKeyConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "instructions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "instructions", "00_base.md"), []byte("# Base"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "commands.allow"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	results, cfg := CheckConfig(root)

	// Should report a FAIL result for the unknown-key error.
	configResult := requireResultByCheckName(t, results, messages.DoctorCheckNameConfig)
	if configResult.Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", configResult.Status)
	}
	if !strings.Contains(configResult.Message, "unrecognized") {
		t.Fatalf("expected unrecognized key error in message, got: %s", configResult.Message)
	}
	if !strings.Contains(configResult.Recommendation, "agents.claude_vscode.model") {
		t.Fatalf("expected unknown key path in recommendation, got: %s", configResult.Recommendation)
	}
	if !strings.Contains(configResult.Recommendation, "al upgrade") || !strings.Contains(configResult.Recommendation, "al wizard") {
		t.Fatalf("expected upgrade/wizard guidance, got: %s", configResult.Recommendation)
	}

	// Should still return a usable config from lenient loading.
	if cfg == nil {
		t.Fatal("expected non-nil config from lenient fallback")
	}
	if cfg.Config.Approvals.Mode != "all" {
		t.Fatalf("expected approvals.mode = all, got %q", cfg.Config.Approvals.Mode)
	}
}

func TestCheckSecretsNoRequired(t *testing.T) {
	// Config with no MCP servers = no required secrets
	cfg := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{},
			},
		},
		Env: map[string]string{},
	}

	results := CheckSecrets(cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != StatusOK {
		t.Errorf("expected OK status, got %s", results[0].Status)
	}
	if results[0].Message != messages.DoctorNoRequiredSecrets {
		t.Errorf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckAgents(t *testing.T) {
	tBool := true
	fBool := false
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Gemini:       config.AgentConfig{Enabled: &tBool},
				Claude:       config.ClaudeConfig{Enabled: &fBool},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: &fBool},
				Codex:        config.CodexConfig{Enabled: nil},
				VSCode:       config.EnableOnlyConfig{Enabled: &tBool},
				Antigravity:  config.EnableOnlyConfig{Enabled: &fBool},
			},
		},
	}

	results := CheckAgents(cfg)

	statusMap := make(map[string]Status)
	for _, r := range results {
		statusMap[r.Message] = r.Status
	}

	if statusMap["Agent enabled: Gemini"] != StatusOK {
		t.Error("Gemini should be enabled")
	}
	if statusMap["Agent disabled: Claude"] != StatusWarn {
		t.Error("Claude should be disabled")
	}
	if statusMap["Agent disabled: Codex"] != StatusWarn {
		t.Error("Codex should be disabled (nil)")
	}
}

func TestCheckSkills_NoSkills(t *testing.T) {
	cfg := &config.ProjectConfig{}
	results := CheckSkills(cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %#v", len(results), results)
	}
	if results[0].Status != StatusOK {
		t.Fatalf("status = %s, want %s", results[0].Status, StatusOK)
	}
	if results[0].CheckName != messages.DoctorCheckNameSkills {
		t.Fatalf("check name = %q, want %q", results[0].CheckName, messages.DoctorCheckNameSkills)
	}
	if results[0].Message != messages.DoctorSkillsNoneConfigured {
		t.Fatalf("message = %q, want %q", results[0].Message, messages.DoctorSkillsNoneConfigured)
	}
}

func TestCheckSkills_Compliant(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	skillPath := filepath.Join(skillsDir, "alpha.md")
	content := `---
name: alpha
description: test
---
Body.
`
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	cfg := &config.ProjectConfig{
		Root: root,
		Skills: []config.Skill{
			{Name: "alpha", SourcePath: skillPath},
		},
	}
	results := CheckSkills(cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %#v", len(results), results)
	}
	if results[0].Status != StatusOK {
		t.Fatalf("status = %s, want %s", results[0].Status, StatusOK)
	}
	if !strings.Contains(results[0].Message, "Skills validated successfully") {
		t.Fatalf("unexpected message: %q", results[0].Message)
	}
}

func TestCheckSkills_Warnings(t *testing.T) {
	root := t.TempDir()
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	skillPath := filepath.Join(skillsDir, "alpha.md")
	content := `---
description: test
foo: bar
---
Body.
`
	if err := os.WriteFile(skillPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	cfg := &config.ProjectConfig{
		Root: root,
		Skills: []config.Skill{
			{Name: "alpha", SourcePath: skillPath},
		},
	}
	results := CheckSkills(cfg)
	if len(results) < 2 {
		t.Fatalf("expected at least 2 warning results, got %d: %#v", len(results), results)
	}
	for _, result := range results {
		if result.Status != StatusWarn {
			t.Fatalf("status = %s, want %s (%#v)", result.Status, StatusWarn, result)
		}
		if result.CheckName != messages.DoctorCheckNameSkills {
			t.Fatalf("check name = %q, want %q", result.CheckName, messages.DoctorCheckNameSkills)
		}
	}
}

func TestCheckSkills_ParseFailure(t *testing.T) {
	root := t.TempDir()
	cfg := &config.ProjectConfig{
		Root: root,
		Skills: []config.Skill{
			{Name: "missing", SourcePath: filepath.Join(root, ".agent-layer", "skills", "missing.md")},
		},
	}
	results := CheckSkills(cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %#v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("status = %s, want %s", results[0].Status, StatusFail)
	}
	if results[0].Recommendation != messages.DoctorSkillValidationRecommend {
		t.Fatalf("recommendation = %q, want %q", results[0].Recommendation, messages.DoctorSkillValidationRecommend)
	}
}
