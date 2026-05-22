package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestCheckConfig_LenientFallback_InjectsBuiltInEnv_NoEnvFile(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Write a config that fails strict validation.
	partialConfig := `
[approvals]
mode = "all"

[agents.antigravity]
enabled = true
[agents.claude]
enabled = true
[agents.codex]
enabled = false
[agents.vscode]
enabled = true
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(partialConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	// Deliberately omit .env file.
	if err := os.MkdirAll(filepath.Join(configDir, "instructions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "instructions", "00_rules.md"), []byte("# Base"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "commands.allow"), []byte(""), 0o600); err != nil {
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
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Write a config with an unknown key (model on an enable-only agent).
	// Strict loading will fail with an ErrConfigValidation-wrapped unknown-key
	// error; lenient loading should succeed.
	unknownKeyConfig := `
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
enabled = false
[agents.vscode]
enabled = true
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(unknownKeyConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "instructions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "instructions", "00_rules.md"), []byte("# Base"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "commands.allow"), []byte(""), 0o600); err != nil {
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
	if cfg.Config.Approvals.Mode != config.ApprovalModeAll {
		t.Fatalf("expected approvals.mode = %s, got %q", config.ApprovalModeAll, cfg.Config.Approvals.Mode)
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
	originalLookPath := lookPathFunc
	originalCommandOutput := commandOutputFunc
	lookPathFunc = func(file string) (string, error) { return "/test/bin/" + file, nil }
	// Match the real `agy --version` output shape so the tightened
	// agyVersionRE (anchored on `^agy <version>`) finds the version.
	commandOutputFunc = func(name string, args ...string) ([]byte, error) { return []byte("agy 1.0.0\n"), nil }
	t.Cleanup(func() {
		lookPathFunc = originalLookPath
		commandOutputFunc = originalCommandOutput
	})

	tBool := true
	fBool := false
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Antigravity:  config.EnableOnlyConfig{Enabled: &tBool},
				Claude:       config.ClaudeConfig{Enabled: &fBool},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: &fBool},
				Codex:        config.CodexConfig{Enabled: nil},
				VSCode:       config.EnableOnlyConfig{Enabled: &tBool},
				CopilotCLI:   config.AgentConfig{Enabled: &fBool},
			},
		},
	}

	results := CheckAgents(cfg)

	statusMap := make(map[string]Status)
	for _, r := range results {
		statusMap[r.Message] = r.Status
	}

	if statusMap["Agent enabled: Antigravity"] != StatusOK {
		t.Error("Antigravity should be reported enabled with StatusOK")
	}
	if statusMap["Agent disabled: Claude"] != StatusOK {
		t.Error("disabled agents should report StatusOK (informational, not a problem)")
	}
	if statusMap["Agent disabled: Codex"] != StatusOK {
		t.Error("agent with nil enabled flag should report StatusOK")
	}
	if statusMap["Antigravity version OK: 1.0.0"] != StatusOK {
		t.Error("enabled Antigravity should check agy version")
	}
}

func TestCommandOutputWithTimeoutStopsHungCommand(t *testing.T) {
	t.Run("deadline exceeded", func(t *testing.T) {
		scriptPath := filepath.Join(t.TempDir(), "hang.sh")
		script := "#!/bin/sh\nsleep 5\n"
		if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(scriptPath, 0o700); err != nil { // #nosec G302 -- test needs an executable shell stub for subprocess timeout coverage.
			t.Fatal(err)
		}

		startedAt := time.Now()
		_, err := commandOutputWithTimeout(20*time.Millisecond, scriptPath)
		if err == nil {
			t.Fatal("expected timeout error from hung command")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected classified deadline error, got: %v", err)
		}
		if elapsed := time.Since(startedAt); elapsed > time.Second {
			t.Fatalf("command timeout took too long: %s", elapsed)
		}
	})

	t.Run("orphaned output pipe", func(t *testing.T) {
		scriptPath := filepath.Join(t.TempDir(), "pipe.sh")
		script := "#!/bin/sh\necho started\nsleep 5 &\n"
		if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(scriptPath, 0o700); err != nil { // #nosec G302 -- test needs an executable shell stub for subprocess timeout coverage.
			t.Fatal(err)
		}

		startedAt := time.Now()
		_, err := commandOutputWithTimeout(200*time.Millisecond, scriptPath)
		if err == nil {
			t.Fatal("expected timeout error from inherited output pipe")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected classified deadline error, got: %v", err)
		}
		if elapsed := time.Since(startedAt); elapsed > time.Second {
			t.Fatalf("command wait delay took too long: %s", elapsed)
		}
	})
}

func TestCheckAntigravityBinary(t *testing.T) {
	originalLookPath := lookPathFunc
	originalCommandOutput := commandOutputFunc
	t.Cleanup(func() {
		lookPathFunc = originalLookPath
		commandOutputFunc = originalCommandOutput
	})

	t.Run("missing", func(t *testing.T) {
		lookPathFunc = func(file string) (string, error) { return "", os.ErrNotExist }
		results := CheckAntigravityBinary()
		if len(results) != 1 || results[0].Status != StatusFail || results[0].Message != messages.DoctorAntigravityNotFound {
			t.Fatalf("unexpected result: %#v", results)
		}
	})

	t.Run("too old", func(t *testing.T) {
		lookPathFunc = func(file string) (string, error) { return "/test/bin/agy", nil }
		commandOutputFunc = func(name string, args ...string) ([]byte, error) { return []byte("agy 0.9.9\n"), nil }
		results := CheckAntigravityBinary()
		if len(results) != 1 || results[0].Status != StatusFail || !strings.Contains(results[0].Message, "below required") {
			t.Fatalf("unexpected result: %#v", results)
		}
	})

	t.Run("ok", func(t *testing.T) {
		lookPathFunc = func(file string) (string, error) { return "/test/bin/agy", nil }
		commandOutputFunc = func(name string, args ...string) ([]byte, error) { return []byte("agy 1.0.1\n"), nil }
		results := CheckAntigravityBinary()
		if len(results) != 1 || results[0].Status != StatusOK || results[0].Message != "Antigravity version OK: 1.0.1" {
			t.Fatalf("unexpected result: %#v", results)
		}
	})

	t.Run("ok at boundary 1.0.0", func(t *testing.T) {
		lookPathFunc = func(file string) (string, error) { return "/test/bin/agy", nil }
		commandOutputFunc = func(name string, args ...string) ([]byte, error) { return []byte("agy 1.0.0\n"), nil }
		results := CheckAntigravityBinary()
		if len(results) != 1 || results[0].Status != StatusOK {
			t.Fatalf("expected OK at boundary, got: %#v", results)
		}
	})

	t.Run("ok at large major", func(t *testing.T) {
		lookPathFunc = func(file string) (string, error) { return "/test/bin/agy", nil }
		commandOutputFunc = func(name string, args ...string) ([]byte, error) { return []byte("agy 12.0.0\n"), nil }
		results := CheckAntigravityBinary()
		if len(results) != 1 || results[0].Status != StatusOK {
			t.Fatalf("expected OK at large major, got: %#v", results)
		}
	})

	t.Run("version command failed", func(t *testing.T) {
		lookPathFunc = func(file string) (string, error) { return "/test/bin/agy", nil }
		commandOutputFunc = func(name string, args ...string) ([]byte, error) {
			return []byte("agy"), fmt.Errorf("boom")
		}
		results := CheckAntigravityBinary()
		if len(results) != 1 || results[0].Status != StatusFail {
			t.Fatalf("expected FAIL when --version errors, got: %#v", results)
		}
		if !strings.Contains(results[0].Message, "Failed to read Antigravity version") {
			t.Fatalf("expected version-failed marker in message, got: %q", results[0].Message)
		}
		if !strings.Contains(results[0].Recommendation, "agy") {
			t.Fatalf("expected recommendation to mention agy, got: %q", results[0].Recommendation)
		}
	})

	t.Run("unparseable version output", func(t *testing.T) {
		lookPathFunc = func(file string) (string, error) { return "/test/bin/agy", nil }
		commandOutputFunc = func(name string, args ...string) ([]byte, error) {
			return []byte("agy custom-build\n"), nil
		}
		results := CheckAntigravityBinary()
		if len(results) != 1 || results[0].Status != StatusFail {
			t.Fatalf("expected FAIL on unparseable output, got: %#v", results)
		}
		if !strings.Contains(results[0].Message, "Could not parse Antigravity version") {
			t.Fatalf("expected unparseable marker, got: %q", results[0].Message)
		}
	})

	t.Run("calendar-version after agy keyword is accepted (documented behavior)", func(t *testing.T) {
		// Round 3 F-3-6: the widened agyVersionRE accepts `agy 2026.05.21`
		// because the capture-group is just `\d+\.\d+\.\d+`. We lock the
		// current behavior in a test: such a value is treated as a real
		// semver, compared against 1.0.0, and reports OK (2026 > 1). If
		// upstream ever ships calendar versions, the check still functions;
		// if a future tightening makes calendar versions invalid, this
		// test will fail loudly so the maintainer makes a conscious choice.
		lookPathFunc = func(file string) (string, error) { return "/test/bin/agy", nil }
		commandOutputFunc = func(name string, args ...string) ([]byte, error) {
			return []byte("agy 2026.05.21\n"), nil
		}
		results := CheckAntigravityBinary()
		if len(results) != 1 || results[0].Status != StatusOK {
			t.Fatalf("expected OK for `agy 2026.05.21`, got: %#v", results)
		}
	})

	t.Run("rejects dotted-numeric build noise without version keyword", func(t *testing.T) {
		// agyVersionRE only matches `agy <version>` or `agy version <version>`.
		// `agy build 2026.05.21` has `build` between `agy` and the digits,
		// so the regex returns no match — the result is "Could not parse"
		// (NOT "below required"), pinning F-A-4 / F-B-12: a future regex
		// relaxation that re-introduced silent wrong-version detection from
		// build timestamps would flip the message to "below required" and
		// break this assertion.
		lookPathFunc = func(file string) (string, error) { return "/test/bin/agy", nil }
		commandOutputFunc = func(name string, args ...string) ([]byte, error) {
			return []byte("agy build 2026.05.21\n"), nil
		}
		results := CheckAntigravityBinary()
		if len(results) != 1 || results[0].Status != StatusFail {
			t.Fatalf("expected FAIL when version line is missing, got: %#v", results)
		}
		if !strings.Contains(results[0].Message, "Could not parse Antigravity version") {
			t.Fatalf("expected 'Could not parse' message, got: %q", results[0].Message)
		}
	})
}

// TestCheckAgents_DisabledAntigravitySkipsBinaryCheck asserts F-A-17: when
// Antigravity is disabled (or unset), CheckAgents must NOT invoke the binary
// check, so the user is not failed for missing `agy` on a Claude-only repo.
func TestCheckAgents_DisabledAntigravitySkipsBinaryCheck(t *testing.T) {
	originalLookPath := lookPathFunc
	originalCommandOutput := commandOutputFunc
	t.Cleanup(func() {
		lookPathFunc = originalLookPath
		commandOutputFunc = originalCommandOutput
	})
	binaryCheckCalled := false
	lookPathFunc = func(file string) (string, error) {
		binaryCheckCalled = true
		return "", os.ErrNotExist
	}
	commandOutputFunc = func(name string, args ...string) ([]byte, error) {
		binaryCheckCalled = true
		return nil, fmt.Errorf("should not be invoked")
	}

	falseVal := false
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Antigravity: config.EnableOnlyConfig{Enabled: &falseVal},
			},
		},
	}
	results := CheckAgents(cfg)
	if binaryCheckCalled {
		t.Fatal("CheckAgents must skip binary check when Antigravity is disabled")
	}
	if len(results) == 0 {
		t.Fatal("expected at least the disabled-agent informational result")
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
	if err := os.MkdirAll(filepath.Join(skillsDir, "alpha"), 0o700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	skillPath := filepath.Join(skillsDir, "alpha", "SKILL.md")
	content := `---
name: alpha
description: test
---
Body.
`
	if err := os.WriteFile(skillPath, []byte(content), 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Join(skillsDir, "alpha"), 0o700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	skillPath := filepath.Join(skillsDir, "alpha", "SKILL.md")
	content := `---
description: test
foo: bar
---
Body.
`
	if err := os.WriteFile(skillPath, []byte(content), 0o600); err != nil {
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
			{Name: "missing", SourcePath: filepath.Join(root, ".agent-layer", "skills", "missing", "SKILL.md")},
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
