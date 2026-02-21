package wizard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildSummaryIncludesDisabledMCPServers(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = ApprovalAll
	choices.DisabledMCPServers["github"] = true
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "github"}}

	summary := buildSummary(choices)

	assert.Contains(t, summary, "Disabled MCP Servers (missing secrets):")
	assert.Contains(t, summary, "- github")
}

func TestBuildSummaryIncludesRestoredMCPServers(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = ApprovalAll
	choices.MissingDefaultMCPServers = []string{"context7"}
	choices.RestoreMissingMCPServers = true
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "context7"}}

	summary := buildSummary(choices)

	assert.Contains(t, summary, "Restored Default MCP Servers:")
	assert.Contains(t, summary, "- context7")
}

func TestApprovalModeLabelForValue_NotFound(t *testing.T) {
	label, ok := approvalModeLabelForValue("unknown")
	assert.False(t, ok)
	assert.Equal(t, "", label)
}

// errUI is a minimal UI that returns an error for every prompt.
type errUI struct{ err error }

func (u *errUI) Select(string, []string, *string) error        { return u.err }
func (u *errUI) MultiSelect(string, []string, *[]string) error { return u.err }
func (u *errUI) Confirm(string, *bool) error                   { return u.err }
func (u *errUI) Input(string, *string) error                   { return u.err }
func (u *errUI) SecretInput(string, *string) error             { return u.err }
func (u *errUI) Note(string, string) error                     { return u.err }

func TestRunWithWriter_LenientFallbackOnBrokenConfig(t *testing.T) {
	root := t.TempDir()

	// Create the config file on disk so ensureWizardConfig doesn't try to install.
	configDir := root + "/.agent-layer"
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configDir+"/config.toml", []byte("# broken config"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Stub loadProjectConfigFunc to fail with a validation error (simulating a config
	// with missing required fields). Must wrap ErrConfigValidation so the narrowed
	// lenient fallback triggers.
	origLoad := loadProjectConfigFunc
	loadProjectConfigFunc = func(root string) (*config.ProjectConfig, error) {
		return nil, fmt.Errorf("%w: agents.claude-vscode.enabled is required", config.ErrConfigValidation)
	}
	t.Cleanup(func() { loadProjectConfigFunc = origLoad })

	// Stub loadConfigLenientFunc to succeed with a partial config.
	origLenient := loadConfigLenientFunc
	trueVal := true
	loadConfigLenientFunc = func(path string) (*config.Config, error) {
		return &config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Enabled: &trueVal},
				Claude: config.AgentConfig{Enabled: &trueVal},
				Codex:  config.CodexConfig{Enabled: &trueVal},
				VSCode: config.EnableOnlyConfig{Enabled: &trueVal},
			},
		}, nil
	}
	t.Cleanup(func() { loadConfigLenientFunc = origLenient })

	// Stub default MCP servers and warning defaults to return empty values.
	origMCP := loadDefaultMCPServersFunc
	loadDefaultMCPServersFunc = func() ([]DefaultMCPServer, error) {
		return nil, nil
	}
	t.Cleanup(func() { loadDefaultMCPServersFunc = origMCP })

	origWarnings := loadWarningDefaultsFunc
	loadWarningDefaultsFunc = func() (WarningDefaults, error) {
		return WarningDefaults{
			InstructionTokenThreshold:      100000,
			MCPServerThreshold:             10,
			MCPToolsTotalThreshold:         200,
			MCPServerToolsThreshold:        50,
			MCPSchemaTokensTotalThreshold:  500000,
			MCPSchemaTokensServerThreshold: 100000,
		}, nil
	}
	t.Cleanup(func() { loadWarningDefaultsFunc = origWarnings })

	var out bytes.Buffer
	stubErr := errors.New("stub UI error")

	// The wizard should proceed past config loading (lenient fallback).
	// It will fail later in the prompt flow (errUI returns an error), but the key
	// assertion is that it reaches initializeChoices, not that it completes.
	err := RunWithWriter(root, &errUI{err: stubErr}, nil, "0.8.1", &out)

	// We expect an error from the stub UI, not from config loading.
	if err == nil {
		t.Fatal("expected error from stub UI")
	}
	if strings.Contains(err.Error(), "failed to load config") {
		t.Fatalf("wizard should not fail with config load error after lenient fallback, got: %v", err)
	}

	// Verify that the lenient-loading info message was printed.
	assert.Contains(t, out.String(), "validation errors")
	assert.Contains(t, out.String(), "the wizard")
}

func TestRunWithWriter_LenientFallbackOnUnknownKeys(t *testing.T) {
	root := t.TempDir()

	// Create the config file on disk so ensureWizardConfig doesn't try to install.
	configDir := root + "/.agent-layer"
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configDir+"/config.toml", []byte("# unknown keys config"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Stub loadProjectConfigFunc to fail with an ErrConfigValidation-wrapped
	// unknown-key error (matching what ParseConfig now produces).
	origLoad := loadProjectConfigFunc
	loadProjectConfigFunc = func(root string) (*config.ProjectConfig, error) {
		return nil, fmt.Errorf("%w: test: unrecognized config keys: model", config.ErrConfigValidation)
	}
	t.Cleanup(func() { loadProjectConfigFunc = origLoad })

	// Stub loadConfigLenientFunc to succeed with a partial config.
	origLenient := loadConfigLenientFunc
	trueVal := true
	loadConfigLenientFunc = func(path string) (*config.Config, error) {
		return &config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Enabled: &trueVal},
				Claude: config.AgentConfig{Enabled: &trueVal},
				Codex:  config.CodexConfig{Enabled: &trueVal},
				VSCode: config.EnableOnlyConfig{Enabled: &trueVal},
			},
		}, nil
	}
	t.Cleanup(func() { loadConfigLenientFunc = origLenient })

	// Stub default MCP servers and warning defaults to return empty values.
	origMCP := loadDefaultMCPServersFunc
	loadDefaultMCPServersFunc = func() ([]DefaultMCPServer, error) {
		return nil, nil
	}
	t.Cleanup(func() { loadDefaultMCPServersFunc = origMCP })

	origWarnings := loadWarningDefaultsFunc
	loadWarningDefaultsFunc = func() (WarningDefaults, error) {
		return WarningDefaults{
			InstructionTokenThreshold:      100000,
			MCPServerThreshold:             10,
			MCPToolsTotalThreshold:         200,
			MCPServerToolsThreshold:        50,
			MCPSchemaTokensTotalThreshold:  500000,
			MCPSchemaTokensServerThreshold: 100000,
		}, nil
	}
	t.Cleanup(func() { loadWarningDefaultsFunc = origWarnings })

	var out bytes.Buffer
	stubErr := errors.New("stub UI error")

	// The wizard should proceed past config loading (lenient fallback).
	err := RunWithWriter(root, &errUI{err: stubErr}, nil, "0.8.1", &out)

	// We expect an error from the stub UI, not from config loading.
	if err == nil {
		t.Fatal("expected error from stub UI")
	}
	if strings.Contains(err.Error(), "failed to load config") {
		t.Fatalf("wizard should not fail with config load error after lenient fallback, got: %v", err)
	}

	// Verify that the lenient-loading info message was printed.
	assert.Contains(t, out.String(), "validation errors")
	assert.Contains(t, out.String(), "the wizard")
}

func TestRunWithWriter_NonValidationErrorPropagates(t *testing.T) {
	root := t.TempDir()

	// Create the config file on disk so ensureWizardConfig doesn't try to install.
	configDir := root + "/.agent-layer"
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configDir+"/config.toml", []byte("# placeholder"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Stub loadProjectConfigFunc to fail with a non-validation error (e.g., missing
	// env file). The wizard should propagate this immediately without attempting
	// lenient fallback.
	origLoad := loadProjectConfigFunc
	loadProjectConfigFunc = func(root string) (*config.ProjectConfig, error) {
		return nil, errors.New("missing env file")
	}
	t.Cleanup(func() { loadProjectConfigFunc = origLoad })

	var out bytes.Buffer

	err := RunWithWriter(root, nil, nil, "0.8.1", &out)

	if err == nil {
		t.Fatal("expected error")
	}
	assert.Contains(t, err.Error(), "failed to load config")
}

func TestRunWithWriter_ValidationErrorLenientAlsoFails(t *testing.T) {
	root := t.TempDir()

	// Create the config file on disk so ensureWizardConfig doesn't try to install.
	configDir := root + "/.agent-layer"
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configDir+"/config.toml", []byte("# placeholder"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Stub loadProjectConfigFunc to fail with a validation error.
	origLoad := loadProjectConfigFunc
	loadProjectConfigFunc = func(root string) (*config.ProjectConfig, error) {
		return nil, fmt.Errorf("%w: missing required fields", config.ErrConfigValidation)
	}
	t.Cleanup(func() { loadProjectConfigFunc = origLoad })

	// Stub loadConfigLenientFunc to also fail (TOML syntax error).
	origLenient := loadConfigLenientFunc
	loadConfigLenientFunc = func(path string) (*config.Config, error) {
		return nil, errors.New("toml syntax error")
	}
	t.Cleanup(func() { loadConfigLenientFunc = origLenient })

	var out bytes.Buffer

	err := RunWithWriter(root, nil, nil, "0.8.1", &out)

	if err == nil {
		t.Fatal("expected error")
	}
	assert.Contains(t, err.Error(), "failed to load config")
}
