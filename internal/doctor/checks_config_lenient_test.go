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
	skillPath := filepath.Join(configDir, "skills", "alpha.md")
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
	if len(results) != 1 || results[0].CheckName != messages.DoctorCheckNameConfig || results[0].Status != StatusFail {
		t.Fatalf("expected single config fail result, got %#v", results)
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
