package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestCheckStructure(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "doctor-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	// Test missing directories
	results := CheckStructure(tmpDir)
	failCount := 0
	for _, r := range results {
		if r.Status == StatusFail {
			failCount++
		}
	}
	if failCount != 2 {
		t.Errorf("Expected 2 failures for empty directory, got %d", failCount)
	}

	// Test exists but not directory
	if err := os.WriteFile(filepath.Join(tmpDir, ".agent-layer"), []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	results = CheckStructure(tmpDir)
	fileFail := false
	for _, r := range results {
		if r.Message == ".agent-layer exists but is not a directory" {
			fileFail = true
			if r.Status != StatusFail {
				t.Errorf("Expected fail status for file, got %s", r.Status)
			}
		}
	}
	if !fileFail {
		t.Error("Expected failure for file blocking directory")
	}
	_ = os.Remove(filepath.Join(tmpDir, ".agent-layer"))

	// Test existing directories
	if err := os.Mkdir(filepath.Join(tmpDir, ".agent-layer"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "docs/agent-layer"), 0755); err != nil {
		t.Fatal(err)
	}
	results = CheckStructure(tmpDir)
	for _, r := range results {
		if r.Status != StatusOK {
			t.Errorf("Expected OK status for existing directory %s, got %s", r.CheckName, r.Status)
		}
	}
}

func TestCheckSecretsUsesRequiredEnvVars(t *testing.T) {
	t.Setenv("HEADER_TOKEN", "present")

	enabled := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:      "demo",
						Enabled: &enabled,
						URL:     "https://example.com/${URL_TOKEN}",
						Command: "run-${CMD_TOKEN}",
						Args:    []string{"--token", "${ARG_TOKEN}"},
						Headers: map[string]string{"Authorization": "Bearer ${HEADER_TOKEN}"},
						Env:     map[string]string{"API_KEY": "${ENV_TOKEN}"},
					},
				},
			},
		},
		Env: map[string]string{
			"ARG_TOKEN": "set",
		},
	}

	results := CheckSecrets(cfg)
	expected := map[string]Status{
		fmt.Sprintf(messages.DoctorMissingSecretFmt, "URL_TOKEN"):      StatusFail,
		fmt.Sprintf(messages.DoctorMissingSecretFmt, "CMD_TOKEN"):      StatusFail,
		fmt.Sprintf(messages.DoctorMissingSecretFmt, "ENV_TOKEN"):      StatusFail,
		fmt.Sprintf(messages.DoctorSecretFoundEnvFileFmt, "ARG_TOKEN"): StatusOK,
		fmt.Sprintf(messages.DoctorSecretFoundEnvFmt, "HEADER_TOKEN"):  StatusOK,
	}

	for msg, status := range expected {
		found := false
		for _, result := range results {
			if result.Message != msg {
				continue
			}
			if result.Status != status {
				t.Fatalf("expected %q status %s, got %s", msg, status, result.Status)
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("expected result message %q", msg)
		}
	}
}

func TestCheckSecretsSkipsDisabledServers(t *testing.T) {
	enabled := true
	disabled := false

	cfg := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:      "enabled-server",
						Enabled: &enabled,
						URL:     "https://example.com/${ENABLED_TOKEN}",
					},
					{
						ID:      "disabled-server",
						Enabled: &disabled,
						URL:     "https://example.com/${DISABLED_TOKEN}",
					},
					{
						ID:  "nil-enabled-server",
						URL: "https://example.com/${NIL_TOKEN}",
					},
				},
			},
		},
		Env: map[string]string{},
	}

	results := CheckSecrets(cfg)

	// Only the enabled server's secret should appear.
	if len(results) != 1 {
		t.Fatalf("expected 1 result for single enabled server, got %d: %v", len(results), results)
	}
	wantMsg := fmt.Sprintf(messages.DoctorMissingSecretFmt, "ENABLED_TOKEN")
	if results[0].Message != wantMsg {
		t.Fatalf("expected %q, got %q", wantMsg, results[0].Message)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected fail status, got %s", results[0].Status)
	}
}

func TestCheckConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Missing config
	results, cfg := CheckConfig(tmpDir)
	if cfg != nil {
		t.Error("Expected nil config for missing file")
	}
	if len(results) != 1 || results[0].Status != StatusFail {
		t.Error("Expected failure for missing config")
	}

	// Invalid config
	configDir := filepath.Join(tmpDir, ".agent-layer")
	if err := os.Mkdir(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte("invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	results, cfg = CheckConfig(tmpDir)
	if cfg != nil {
		t.Error("Expected nil config for invalid file")
	}
	if len(results) != 1 || results[0].Status != StatusFail {
		t.Error("Expected failure for invalid config")
	}

	// Valid config
	validConfig := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true
[agents.claude]
enabled = true
[agents.claude_vscode]
enabled = true
[agents.codex]
enabled = false
[agents.vscode]
enabled = true
[agents.antigravity]
enabled = false
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(validConfig), 0644); err != nil {
		t.Fatal(err)
	}
	// Create minimal valid setup
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(configDir, "instructions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "instructions", "00_base.md"), []byte("# Base"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(configDir, "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "commands.allow"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	results, cfg = CheckConfig(tmpDir)
	if cfg == nil {
		t.Error("Expected valid config")
	}
	if len(results) != 1 || results[0].Status != StatusOK {
		t.Errorf("Expected success for valid config, got %v", results)
	}
}

func TestCheckConfig_LenientFallback(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a valid TOML config that is missing required fields (e.g. claude_vscode).
	// Strict loading will fail but lenient loading should succeed.
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
	// Create remaining files needed for LoadProjectConfig (env, instructions, etc.).
	// Include an env var so we can verify the lenient fallback loads .env.
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte("AL_TEST_TOKEN=loaded\n"), 0o600); err != nil {
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

	// Should report a FAIL result for the validation error.
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "claude_vscode") {
		t.Fatalf("expected validation error about claude_vscode, got: %s", results[0].Message)
	}
	if results[0].Recommendation != messages.DoctorConfigLoadLenientRecommend {
		t.Fatalf("expected lenient recommendation, got: %s", results[0].Recommendation)
	}

	// Should still return a usable config from lenient loading.
	if cfg == nil {
		t.Fatal("expected non-nil config from lenient fallback")
	}
	if cfg.Config.Approvals.Mode != "all" {
		t.Fatalf("expected approvals.mode = all, got %q", cfg.Config.Approvals.Mode)
	}

	// Env should be loaded so downstream secret checks work correctly.
	if cfg.Env == nil {
		t.Fatal("expected .env to be loaded in lenient fallback")
	}
	if cfg.Env["AL_TEST_TOKEN"] != "loaded" {
		t.Fatalf("expected AL_TEST_TOKEN=loaded, got %q", cfg.Env["AL_TEST_TOKEN"])
	}

	// Downstream checks should work with the lenient config.
	agentResults := CheckAgents(cfg)
	if len(agentResults) == 0 {
		t.Fatal("expected agent results from lenient config")
	}
}

func TestCheckConfig_LenientFallback_InjectsBuiltInEnv(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write a valid TOML config that fails strict validation (missing claude_vscode).
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

	// Should report a FAIL result for the validation error.
	if len(results) != 1 || results[0].Status != StatusFail {
		t.Fatalf("expected 1 FAIL result, got %d: %v", len(results), results)
	}

	// Should still return a usable config from lenient loading.
	if cfg == nil {
		t.Fatal("expected non-nil config from lenient fallback")
	}

	// AL_REPO_ROOT must be injected so downstream MCP resolution doesn't
	// produce false "missing environment variables" warnings.
	got := cfg.Env[config.BuiltinRepoRootEnvVar]
	if got != root {
		t.Fatalf("expected %s=%q in lenient fallback env, got %q", config.BuiltinRepoRootEnvVar, root, got)
	}
}

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

	if len(results) != 1 || results[0].Status != StatusFail {
		t.Fatalf("expected 1 FAIL result, got %d: %v", len(results), results)
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
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "unrecognized") {
		t.Fatalf("expected unrecognized key error in message, got: %s", results[0].Message)
	}
	if results[0].Recommendation != messages.DoctorConfigLoadLenientRecommend {
		t.Fatalf("expected lenient recommendation, got: %s", results[0].Recommendation)
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
