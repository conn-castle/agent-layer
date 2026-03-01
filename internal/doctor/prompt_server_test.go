package doctor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/sync"
)

func TestCheckPromptServer_ConfigMissing(t *testing.T) {
	results := CheckPromptServer(t.TempDir(), nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if results[0].Message != messages.DoctorPromptServerConfigMissing {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func promptServerConfigWithClaudeEnabled() *config.ProjectConfig {
	tBool := true
	return &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Enabled: &tBool},
			},
		},
	}
}

func TestCheckPromptServer_NoClients(t *testing.T) {
	results := CheckPromptServer(t.TempDir(), &config.ProjectConfig{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusOK {
		t.Fatalf("expected OK status, got %s", results[0].Status)
	}
	if results[0].Message != messages.DoctorPromptServerNotRequired {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServer_ResolveCommandError(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "", nil, errors.New("boom")
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{}, nil
	}

	results := CheckPromptServer(t.TempDir(), promptServerConfigWithClaudeEnabled())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "boom") {
		t.Fatalf("expected error detail in message, got %s", results[0].Message)
	}
}

func TestCheckPromptServer_ResolveCommandEmpty(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return " ", nil, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	results := CheckPromptServer(t.TempDir(), promptServerConfigWithClaudeEnabled())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "resolved empty command") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServer_ResolveEnvError(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return nil, errors.New("env")
	}

	results := CheckPromptServer(t.TempDir(), promptServerConfigWithClaudeEnabled())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "env") {
		t.Fatalf("expected error detail in message, got %s", results[0].Message)
	}
}

func TestCheckPromptServer_MissingRepoRoot(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{}, nil
	}

	results := CheckPromptServer(t.TempDir(), promptServerConfigWithClaudeEnabled())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if results[0].Message != messages.DoctorPromptServerMissingRepoRoot {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServer_Resolved(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	results := CheckPromptServer("/repo", promptServerConfigWithClaudeEnabled())
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusOK {
		t.Fatalf("expected OK status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "al mcp-prompts") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_NoClients(t *testing.T) {
	results := CheckPromptServerConfig(t.TempDir(), &config.ProjectConfig{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusOK {
		t.Fatalf("expected OK status, got %s", results[0].Status)
	}
	if results[0].Message != messages.DoctorPromptServerConfigNotRequired {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_MCPConfigMatches(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	root := t.TempDir()
	mcpPath := filepath.Join(root, ".mcp.json")
	mcpPayload := map[string]any{
		"mcpServers": map[string]any{
			"agent-layer": map[string]any{
				"type":    "stdio",
				"command": "al",
				"args":    []string{"mcp-prompts"},
				"env": map[string]string{
					"AL_REPO_ROOT": root,
				},
			},
		},
	}
	mcpData, err := json.MarshalIndent(mcpPayload, "", "  ")
	if err != nil {
		t.Fatalf("marshal mcp.json: %v", err)
	}
	if err := os.WriteFile(mcpPath, append(mcpData, '\n'), 0o644); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(root, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusOK {
		t.Fatalf("expected OK status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, ".mcp.json") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_ClaudeVSCodeConfigMatches(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	root := t.TempDir()
	mcpPath := filepath.Join(root, ".mcp.json")
	mcpPayload := map[string]any{
		"mcpServers": map[string]any{
			"agent-layer": map[string]any{
				"type":    "stdio",
				"command": "al",
				"args":    []string{"mcp-prompts"},
				"env": map[string]string{
					"AL_REPO_ROOT": root,
				},
			},
		},
	}
	mcpData, err := json.MarshalIndent(mcpPayload, "", "  ")
	if err != nil {
		t.Fatalf("marshal mcp.json: %v", err)
	}
	if err := os.WriteFile(mcpPath, append(mcpData, '\n'), 0o644); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(root, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusOK {
		t.Fatalf("expected OK status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, ".mcp.json") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_MCPMissingFile(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(t.TempDir(), cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, ".mcp.json") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_MCPMissingEntry(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	root := t.TempDir()
	mcpPath := filepath.Join(root, ".mcp.json")
	mcpPayload := map[string]any{
		"mcpServers": map[string]any{
			"other": map[string]any{
				"type":    "stdio",
				"command": "al",
				"args":    []string{"mcp-prompts"},
				"env": map[string]string{
					"AL_REPO_ROOT": root,
				},
			},
		},
	}
	mcpData, err := json.MarshalIndent(mcpPayload, "", "  ")
	if err != nil {
		t.Fatalf("marshal mcp.json: %v", err)
	}
	if err := os.WriteFile(mcpPath, append(mcpData, '\n'), 0o644); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(root, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "missing the internal prompt server entry") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_MCPInvalidJSON(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	root := t.TempDir()
	mcpPath := filepath.Join(root, ".mcp.json")
	if err := os.WriteFile(mcpPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(root, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "Invalid") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_ResolveCommandEmpty(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return " ", nil, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(t.TempDir(), cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "resolved empty command") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_ResolveEnvError(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return nil, errors.New("env")
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(t.TempDir(), cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "env") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_ResolveCommandError(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "", nil, errors.New("boom")
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(t.TempDir(), cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "boom") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_GeminiMismatch(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	root := t.TempDir()
	geminiDir := filepath.Join(root, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatalf("mkdir gemini: %v", err)
	}
	geminiPath := filepath.Join(geminiDir, "settings.json")
	geminiPayload := map[string]any{
		"mcpServers": map[string]any{
			"agent-layer": map[string]any{
				"command": "wrong",
				"args":    []string{"mcp-prompts"},
				"trust":   true,
				"env": map[string]string{
					"AL_REPO_ROOT": root,
				},
			},
		},
	}
	geminiData, err := json.MarshalIndent(geminiPayload, "", "  ")
	if err != nil {
		t.Fatalf("marshal gemini settings: %v", err)
	}
	if err := os.WriteFile(geminiPath, append(geminiData, '\n'), 0o644); err != nil {
		t.Fatalf("write gemini settings: %v", err)
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(root, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "does not match") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_InvalidGeminiJSON(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	root := t.TempDir()
	geminiDir := filepath.Join(root, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatalf("mkdir gemini: %v", err)
	}
	geminiPath := filepath.Join(geminiDir, "settings.json")
	if err := os.WriteFile(geminiPath, []byte("{"), 0o644); err != nil {
		t.Fatalf("write gemini settings: %v", err)
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(root, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "Invalid") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_MissingGeminiFile(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	root := t.TempDir()
	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(root, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, ".gemini/settings.json") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_GeminiConfigMatches(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	root := t.TempDir()
	geminiDir := filepath.Join(root, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatalf("mkdir gemini: %v", err)
	}
	geminiPath := filepath.Join(geminiDir, "settings.json")
	geminiPayload := map[string]any{
		"mcpServers": map[string]any{
			"agent-layer": map[string]any{
				"command": "al",
				"args":    []string{"mcp-prompts"},
				"trust":   true,
				"env": map[string]string{
					"AL_REPO_ROOT": root,
				},
			},
		},
	}
	geminiData, err := json.MarshalIndent(geminiPayload, "", "  ")
	if err != nil {
		t.Fatalf("marshal gemini settings: %v", err)
	}
	if err := os.WriteFile(geminiPath, append(geminiData, '\n'), 0o644); err != nil {
		t.Fatalf("write gemini settings: %v", err)
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(root, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusOK {
		t.Fatalf("expected OK status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, ".gemini/settings.json") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_GeminiConfigMatchesWithoutMCPApprovals(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	root := t.TempDir()
	geminiDir := filepath.Join(root, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatalf("mkdir gemini: %v", err)
	}
	geminiPath := filepath.Join(geminiDir, "settings.json")
	geminiPayload := map[string]any{
		"mcpServers": map[string]any{
			"agent-layer": map[string]any{
				"command": "al",
				"args":    []string{"mcp-prompts"},
				"trust":   false,
				"env": map[string]string{
					"AL_REPO_ROOT": root,
				},
			},
		},
	}
	geminiData, err := json.MarshalIndent(geminiPayload, "", "  ")
	if err != nil {
		t.Fatalf("marshal gemini settings: %v", err)
	}
	if err := os.WriteFile(geminiPath, append(geminiData, '\n'), 0o644); err != nil {
		t.Fatalf("write gemini settings: %v", err)
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "commands"},
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(root, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusOK {
		t.Fatalf("expected OK status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, ".gemini/settings.json") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckPromptServerConfig_BothClientsReturnsTwoResults(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	root := t.TempDir()
	mcpPath := filepath.Join(root, ".mcp.json")
	mcpPayload := map[string]any{
		"mcpServers": map[string]any{
			"agent-layer": map[string]any{
				"type":    "stdio",
				"command": "al",
				"args":    []string{"mcp-prompts"},
				"env": map[string]string{
					"AL_REPO_ROOT": root,
				},
			},
		},
	}
	mcpData, err := json.MarshalIndent(mcpPayload, "", "  ")
	if err != nil {
		t.Fatalf("marshal mcp.json: %v", err)
	}
	if err := os.WriteFile(mcpPath, append(mcpData, '\n'), 0o644); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}

	geminiDir := filepath.Join(root, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatalf("mkdir gemini: %v", err)
	}
	geminiPath := filepath.Join(geminiDir, "settings.json")
	geminiPayload := map[string]any{
		"mcpServers": map[string]any{
			"agent-layer": map[string]any{
				"command": "al",
				"args":    []string{"mcp-prompts"},
				"trust":   true,
				"env": map[string]string{
					"AL_REPO_ROOT": root,
				},
			},
		},
	}
	geminiData, err := json.MarshalIndent(geminiPayload, "", "  ")
	if err != nil {
		t.Fatalf("marshal gemini settings: %v", err)
	}
	if err := os.WriteFile(geminiPath, append(geminiData, '\n'), 0o644); err != nil {
		t.Fatalf("write gemini settings: %v", err)
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents: config.AgentsConfig{
				Claude: config.ClaudeConfig{Enabled: &tBool},
				Gemini: config.AgentConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(root, cfg)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(results), results)
	}
	for _, result := range results {
		if result.Status != StatusOK {
			t.Fatalf("expected OK status, got %s (%s)", result.Status, result.Message)
		}
	}
}

func TestCheckPromptServerConfig_GeminiTrustMismatch(t *testing.T) {
	origCommand := resolvePromptServerCommandFunc
	origEnv := resolvePromptServerEnvFunc
	t.Cleanup(func() {
		resolvePromptServerCommandFunc = origCommand
		resolvePromptServerEnvFunc = origEnv
	})

	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return "al", []string{"mcp-prompts"}, nil
	}
	resolvePromptServerEnvFunc = func(root string) (sync.OrderedMap[string], error) {
		return sync.OrderedMap[string]{config.BuiltinRepoRootEnvVar: root}, nil
	}

	root := t.TempDir()
	geminiDir := filepath.Join(root, ".gemini")
	if err := os.MkdirAll(geminiDir, 0o755); err != nil {
		t.Fatalf("mkdir gemini: %v", err)
	}
	geminiPath := filepath.Join(geminiDir, "settings.json")
	geminiPayload := map[string]any{
		"mcpServers": map[string]any{
			"agent-layer": map[string]any{
				"command": "al",
				"args":    []string{"mcp-prompts"},
				"trust":   false,
				"env": map[string]string{
					"AL_REPO_ROOT": root,
				},
			},
		},
	}
	geminiData, err := json.MarshalIndent(geminiPayload, "", "  ")
	if err != nil {
		t.Fatalf("marshal gemini settings: %v", err)
	}
	if err := os.WriteFile(geminiPath, append(geminiData, '\n'), 0o644); err != nil {
		t.Fatalf("write gemini settings: %v", err)
	}

	tBool := true
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Enabled: &tBool},
			},
		},
	}

	results := CheckPromptServerConfig(root, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(results), results)
	}
	if results[0].Status != StatusFail {
		t.Fatalf("expected FAIL status, got %s", results[0].Status)
	}
	if !strings.Contains(results[0].Message, "trust expected true") {
		t.Fatalf("unexpected message: %s", results[0].Message)
	}
}

func TestComparePromptServerSpec_EnvMismatchRedactsValues(t *testing.T) {
	actual := promptServerSpec{
		Command: "al",
		Env: map[string]string{
			"SECRET": "value-a",
		},
	}
	expected := promptServerSpec{
		Command: "al",
		Env: map[string]string{
			"SECRET": "value-b",
		},
	}

	mismatch := comparePromptServerSpec(actual, expected)
	if !strings.Contains(mismatch, "different values for SECRET") {
		t.Fatalf("unexpected mismatch: %s", mismatch)
	}
	if strings.Contains(mismatch, "value-a") || strings.Contains(mismatch, "value-b") {
		t.Fatalf("mismatch should redact values, got: %s", mismatch)
	}
}

func TestComparePromptServerSpec_RepoRootCanonicalized(t *testing.T) {
	root := t.TempDir()
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "repo-link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	actual := promptServerSpec{
		Command: "al",
		Env: map[string]string{
			config.BuiltinRepoRootEnvVar: link,
		},
	}
	expected := promptServerSpec{
		Command: "al",
		Env: map[string]string{
			config.BuiltinRepoRootEnvVar: root,
		},
	}

	mismatch := comparePromptServerSpec(actual, expected)
	if mismatch != "" {
		t.Fatalf("expected no mismatch, got: %s", mismatch)
	}
}
