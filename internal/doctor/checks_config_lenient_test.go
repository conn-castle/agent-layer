package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

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
	result := requireResultByCheckName(t, results, messages.DoctorCheckNameConfig)
	if result.Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", result.Status)
	}
	if !strings.Contains(result.Message, "claude_vscode") {
		t.Fatalf("expected validation error about claude_vscode, got: %s", result.Message)
	}
	if result.Recommendation != messages.DoctorConfigLoadLenientRecommend {
		t.Fatalf("expected lenient recommendation, got: %s", result.Recommendation)
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

func TestCheckConfig_LenientFallback_UnknownKeysGuidance(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	partialConfig := `
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
model = "vscode-model-not-supported"
reasoning_effort = "nope"
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
	if cfg == nil {
		t.Fatal("expected non-nil config from lenient fallback")
	}
	configPath := filepath.Join(configDir, "config.toml")
	details, detailsErr := configUnknownKeys(configPath)
	if detailsErr != nil {
		t.Fatalf("expected unknown-key details, got error: %v", detailsErr)
	}
	var sawModel, sawReasoning bool
	for _, detail := range details {
		if detail.Path == "agents.vscode.model" {
			sawModel = true
		}
		if detail.Path == "agents.vscode.reasoning_effort" {
			sawReasoning = true
		}
	}
	if !sawModel || !sawReasoning {
		t.Fatalf("expected unknown-key details for model+reasoning, got: %#v", details)
	}
	result := requireResultByCheckName(t, results, messages.DoctorCheckNameConfig)
	if result.CheckName != messages.DoctorCheckNameConfig {
		t.Fatalf("expected Config check name, got %s", result.CheckName)
	}
	if result.Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", result.Status)
	}
	if !strings.Contains(result.Message, "Failed to load configuration:") {
		t.Fatalf("expected load failure prefix, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "unrecognized config keys") {
		t.Fatalf("expected unrecognized keys message, got: %s", result.Message)
	}
	expectedSummary := summarizeUnknownKeys(details)
	if !strings.Contains(result.Message, expectedSummary) {
		t.Fatalf("expected unknown-key summary %q in message, got: %s", expectedSummary, result.Message)
	}
	expectedRecommendation := formatUnknownKeyRecommendation(relPathForDoctor(root, configPath), details)
	if result.Recommendation != expectedRecommendation {
		t.Fatalf("unexpected recommendation:\n--- got ---\n%s\n--- want ---\n%s", result.Recommendation, expectedRecommendation)
	}
}

func TestCheckConfig_LenientFallback_UnknownKeysGuidance_SuggestsRename(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	legacyKeyConfig := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true
[agents.claude]
enabled = true
[agents.claude-vscode]
enabled = true
model = "legacy-key-not-supported"
[agents.codex]
enabled = false
[agents.vscode]
enabled = true
[agents.antigravity]
enabled = false
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(legacyKeyConfig), 0o644); err != nil {
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
	if cfg == nil {
		t.Fatal("expected non-nil config from lenient fallback")
	}
	result := requireResultByCheckName(t, results, messages.DoctorCheckNameConfig)
	if result.Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", result.Status)
	}
	if !strings.Contains(result.Message, `unrecognized config keys: agents["claude-vscode"]`) {
		t.Fatalf("expected unknown key summary in message, got: %s", result.Message)
	}
	if !strings.Contains(result.Recommendation, "did you mean agents.claude_vscode?") {
		t.Fatalf("expected key-rename suggestion, got: %s", result.Recommendation)
	}
}

func TestCheckConfig_LenientFallback_LoadsSkillsForDoctor(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

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
	alphaDir := filepath.Join(configDir, "skills", "alpha")
	if err := os.MkdirAll(alphaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(alphaDir, "SKILL.md")
	skillContent := `---
name: alpha
description: test
---
Body.
`
	if err := os.WriteFile(skillPath, []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "commands.allow"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	_, cfg := CheckConfig(root)
	if cfg == nil {
		t.Fatal("expected non-nil config from lenient fallback")
	}
	if len(cfg.Skills) != 1 {
		t.Fatalf("expected 1 loaded skill, got %d", len(cfg.Skills))
	}

	skillResults := CheckSkills(cfg)
	if len(skillResults) != 1 {
		t.Fatalf("expected 1 skill result, got %d: %#v", len(skillResults), skillResults)
	}
	if skillResults[0].Message == messages.DoctorSkillsNoneConfigured {
		t.Fatalf("unexpected no-skills message while skill files exist: %#v", skillResults)
	}
}

func TestCheckConfig_LenientFallback_MissingSkillsDir_DoesNotAddSkillsFailure(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

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

	results, cfg := CheckConfig(root)
	if cfg == nil {
		t.Fatal("expected non-nil config from lenient fallback")
	}
	for _, result := range results {
		if result.CheckName == messages.DoctorCheckNameSkills {
			t.Fatalf("did not expect skills failure for missing skills directory: %#v", results)
		}
	}
	configResult := requireResultByCheckName(t, results, messages.DoctorCheckNameConfig)
	if configResult.Status != StatusFail {
		t.Fatalf("expected config fail result, got %#v", results)
	}

	skillResults := CheckSkills(cfg)
	if len(skillResults) != 1 || skillResults[0].Message != messages.DoctorSkillsNoneConfigured {
		t.Fatalf("expected no-skills configured result, got %#v", skillResults)
	}
}

func TestCheckConfig_LenientFallback_SkillsPathIsFile_ReportsLoadFailure(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

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
	if err := os.WriteFile(filepath.Join(configDir, "skills"), []byte("not-a-directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, cfg := CheckConfig(root)
	if cfg == nil {
		t.Fatal("expected non-nil config from lenient fallback")
	}

	var foundSkillsLoadFailure bool
	for _, result := range results {
		if result.CheckName != messages.DoctorCheckNameSkills {
			continue
		}
		foundSkillsLoadFailure = true
		if !strings.Contains(result.Message, "Failed to load skills from .agent-layer/skills") {
			t.Fatalf("unexpected skills load failure message: %q", result.Message)
		}
		if strings.Contains(result.Message, "Failed to validate skill") {
			t.Fatalf("unexpected per-skill validation wording for directory load failure: %q", result.Message)
		}
	}
	if !foundSkillsLoadFailure {
		t.Fatalf("expected skills load failure result, got %#v", results)
	}
}

func TestCheckConfig_LenientFallback_InvalidSkillFile_ReportsLoadFailure(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

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
	// Malformed YAML frontmatter should make LoadSkills fail.
	brokenDir := filepath.Join(configDir, "skills", "broken")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatal(err)
	}
	badSkill := "---\nname: [\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(brokenDir, "SKILL.md"), []byte(badSkill), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "commands.allow"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	results, cfg := CheckConfig(root)
	if cfg == nil {
		t.Fatal("expected non-nil config from lenient fallback")
	}

	skillsResult := requireResultByCheckName(t, results, messages.DoctorCheckNameSkills)
	if skillsResult.Status != StatusFail {
		t.Fatalf("expected skills FAIL status, got %s", skillsResult.Status)
	}
	if !strings.Contains(skillsResult.Message, "Failed to load skills from .agent-layer/skills") {
		t.Fatalf("unexpected skills load failure message: %q", skillsResult.Message)
	}
	if skillsResult.Recommendation != messages.DoctorSkillValidationRecommend {
		t.Fatalf("expected skills recommendation %q, got %q", messages.DoctorSkillValidationRecommend, skillsResult.Recommendation)
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
	configResult := requireResultByCheckName(t, results, messages.DoctorCheckNameConfig)
	if configResult.Status != StatusFail {
		t.Fatalf("expected config FAIL result, got %v", results)
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
