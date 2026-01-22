package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicholasjconn/agent-layer/internal/config"
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

	cfg := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:      "demo",
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
		"Missing secret: URL_TOKEN":                 StatusFail,
		"Missing secret: CMD_TOKEN":                 StatusFail,
		"Missing secret: ENV_TOKEN":                 StatusFail,
		"Secret found in .env: ARG_TOKEN":           StatusOK,
		"Secret found in environment: HEADER_TOKEN": StatusOK,
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
	if err := os.Mkdir(filepath.Join(configDir, "slash-commands"), 0755); err != nil {
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
	if results[0].Message != "No required secrets detected in configuration." {
		t.Errorf("unexpected message: %s", results[0].Message)
	}
}

func TestCheckAgents(t *testing.T) {
	tBool := true
	fBool := false
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Gemini:      config.AgentConfig{Enabled: &tBool},
				Claude:      config.AgentConfig{Enabled: &fBool},
				Codex:       config.CodexConfig{Enabled: nil},
				VSCode:      config.AgentConfig{Enabled: &tBool},
				Antigravity: config.AgentConfig{Enabled: &fBool},
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
