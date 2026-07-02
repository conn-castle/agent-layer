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
	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
)

func TestBuildSummaryIncludesDisabledMCPServers(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll
	choices.DisabledMCPServers["github"] = true
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "github"}}

	summary := buildSummary(choices)

	assert.Contains(t, summary, "Disabled MCP Servers (missing secrets):")
	assert.Contains(t, summary, "- github")
}

func TestBuildSummaryIncludesAntigravityModel(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll
	choices.EnabledAgents[AgentAntigravity] = true
	choices.AntigravityModel = "Gemini 3.5 Flash (High)"

	summary := buildSummary(choices)

	assert.Contains(t, summary, "- antigravity: Gemini 3.5 Flash (High)")
}

func TestBuildSummaryIncludesCodexLocalConfigDir(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll
	choices.EnabledAgents[AgentCodex] = true
	choices.CodexLocalConfigDir = true
	choices.CodexLocalConfigDirTouched = true

	summary := buildSummary(choices)

	assert.Contains(t, summary, "Codex local home: enabled")
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
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configDir+"/config.toml", []byte("# broken config"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Stub loadProjectConfigFunc to fail with a validation error (simulating a config
	// with missing required fields). Must wrap ErrConfigValidation so the narrowed
	// lenient fallback triggers.
	origLoad := loadProjectConfigFunc
	loadProjectConfigFunc = func(root string) (*config.ProjectConfig, error) {
		return nil, fmt.Errorf("%w: agents.claude_vscode.enabled is required", config.ErrConfigValidation)
	}
	t.Cleanup(func() { loadProjectConfigFunc = origLoad })

	// Stub loadConfigLenientFunc to succeed with a partial config.
	origLenient := loadConfigLenientFunc
	trueVal := true
	loadConfigLenientFunc = func(path string) (*config.Config, error) {
		return &config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				Antigravity: config.AntigravityConfig{Enabled: &trueVal},
				Claude:      config.ClaudeConfig{Enabled: &trueVal},
				Codex:       config.CodexConfig{Enabled: &trueVal},
				VSCode:      config.EnableOnlyConfig{Enabled: &trueVal},
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

func TestRunWithWriter_RedirectsWhenConfigNeedsUpgrade(t *testing.T) {
	root := t.TempDir()

	// A real legacy config on disk so the redirect path operates on a plausible file.
	configDir := root + "/.agent-layer"
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configDir+"/config.toml", []byte("[agents.gemini]\nenabled = true\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Stub the loader to fail with a needs-upgrade validation error, matching
	// what ParseConfig produces for a legacy [agents.gemini] table.
	origLoad := loadProjectConfigFunc
	loadProjectConfigFunc = func(string) (*config.ProjectConfig, error) {
		return nil, fmt.Errorf("%w: %w: test: agents.gemini is no longer supported; run 'al upgrade' to migrate",
			config.ErrConfigValidation, config.ErrConfigNeedsUpgrade)
	}
	t.Cleanup(func() { loadProjectConfigFunc = origLoad })

	// runSync must never be called: a needs-upgrade config has to redirect before
	// reaching the apply/sync dead-end that motivated this fix.
	syncCalled := false
	runSync := func(string) (*alsync.Result, error) {
		syncCalled = true
		return &alsync.Result{}, nil
	}

	var out bytes.Buffer
	// errUI errors on any prompt, so reaching the wizard flow would surface a
	// non-nil error — proving the redirect short-circuits before the flow.
	err := RunWithWriter(root, &errUI{err: errors.New("stub UI error")}, runSync, "0.0.0", &out)

	if err != nil {
		t.Fatalf("expected clean exit (nil) for needs-upgrade config, got: %v", err)
	}
	if syncCalled {
		t.Fatal("runSync must not be called when the config needs an upgrade")
	}
	if strings.Contains(out.String(), "will help you fix") {
		t.Fatalf("must not promise a wizard fix for a needs-upgrade config, got: %q", out.String())
	}
	assert.Contains(t, out.String(), "al upgrade")
}

func TestRunWithWriter_RedirectsLegacyAntigravityAgentSpecificModel(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := root + "/.agent-layer"
	configData := basicAgentConfig() + `
[agents.antigravity.agent_specific]
model = "Gemini 3.5 Flash (High)"
`
	if err := os.WriteFile(configDir+"/config.toml", []byte(configData), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(configDir+"/.env", []byte(""), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	syncCalled := false
	runSync := func(string) (*alsync.Result, error) {
		syncCalled = true
		return &alsync.Result{}, nil
	}

	var out bytes.Buffer
	err := RunWithWriter(root, &errUI{err: errors.New("stub UI error")}, runSync, "0.0.0", &out)
	if err != nil {
		t.Fatalf("expected clean exit (nil) for legacy Antigravity model config, got: %v", err)
	}
	if syncCalled {
		t.Fatal("runSync must not be called when legacy Antigravity model config needs an upgrade")
	}
	if strings.Contains(out.String(), "will help you fix") {
		t.Fatalf("must not promise a wizard fix for legacy Antigravity model config, got: %q", out.String())
	}
	assert.Contains(t, out.String(), "al upgrade")
	assert.Contains(t, out.String(), "agents.antigravity.agent_specific.model")
}

func TestRunWithWriter_LenientFallbackOnUnknownKeys(t *testing.T) {
	root := t.TempDir()

	// Create the config file on disk so ensureWizardConfig doesn't try to install.
	configDir := root + "/.agent-layer"
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configDir+"/config.toml", []byte("# unknown keys config"), 0o600); err != nil {
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
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				Antigravity: config.AntigravityConfig{Enabled: &trueVal},
				Claude:      config.ClaudeConfig{Enabled: &trueVal},
				Codex:       config.CodexConfig{Enabled: &trueVal},
				VSCode:      config.EnableOnlyConfig{Enabled: &trueVal},
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
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configDir+"/config.toml", []byte("# placeholder"), 0o600); err != nil {
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
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(configDir+"/config.toml", []byte("# placeholder"), 0o600); err != nil {
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

// TestPromptModels_SetsDisableToggles drives both per-agent feature multi-selects
// through promptModels with no labels checked (the all-disabled edge: unchecking
// every box disables every feature) and asserts each disable-sense choice plus its
// touched flag is set. CodexApps is covered by the coverage/round-trip tests, not
// here.
func TestPromptModels_SetsDisableToggles(t *testing.T) {
	choices := NewChoices()
	choices.EnabledAgents[AgentClaude] = true
	choices.EnabledAgents[AgentCodex] = true

	featureTitles := map[string]bool{
		messages.WizardClaudeFeaturesTitle: true,
		messages.WizardCodexFeaturesTitle:  true,
	}

	ui := &MockUI{
		SelectFunc:  func(string, []string, *string) error { return nil },
		ConfirmFunc: func(string, *bool) error { return nil },
		MultiSelectFunc: func(title string, _ []string, selected *[]string) error {
			if featureTitles[title] {
				*selected = []string{} // check nothing = disable every feature
			}
			return nil
		},
	}

	if err := promptModels(ui, choices); err != nil {
		t.Fatalf("promptModels error: %v", err)
	}

	assert.True(t, choices.ClaudeDisableIDEReading)
	assert.True(t, choices.ClaudeDisableIDEReadingTouched)
	assert.True(t, choices.ClaudeDisableMemory)
	assert.True(t, choices.ClaudeDisableMemoryTouched)
	assert.True(t, choices.ClaudeDisableConnectors)
	assert.True(t, choices.ClaudeDisableConnectorsTouched)
	assert.True(t, choices.ClaudeDisableQuestionTool)
	assert.True(t, choices.ClaudeDisableQuestionToolTouched)
	assert.True(t, choices.CodexDisableBrowser)
	assert.True(t, choices.CodexDisableBrowserTouched)
}

func TestPromptModels_AntigravityModelOptions(t *testing.T) {
	choices := NewChoices()
	choices.EnabledAgents[AgentAntigravity] = true
	choices.AntigravityModel = "Gemini 3.5 Flash (High)"

	var sawAntigravityModel bool
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error {
			if title != messages.WizardAntigravityModelTitle {
				t.Fatalf("unexpected select title %q", title)
			}
			sawAntigravityModel = true
			wantOptions := append([]string{messages.WizardLeaveBlankOption}, config.FieldOptionValues(config.AntigravityModelFieldKey)...)
			wantOptions = append(wantOptions, messages.WizardCustomOption)
			assert.Equal(t, wantOptions, options)
			assert.Equal(t, "Gemini 3.5 Flash (High)", *current)
			*current = "Gemini 3.1 Pro (High)"
			return nil
		},
	}

	if err := promptModels(ui, choices); err != nil {
		t.Fatalf("promptModels error: %v", err)
	}
	assert.True(t, sawAntigravityModel)
	assert.True(t, choices.AntigravityModelTouched)
	assert.Equal(t, "Gemini 3.1 Pro (High)", choices.AntigravityModel)
}

// TestPromptEnabledAgents_ResetsDisableToggles asserts that deselecting the
// Claude agents and Codex clears their per-feature disable toggles so a stale
// "disable" choice cannot survive into an unrelated config.
func TestPromptEnabledAgents_ResetsDisableToggles(t *testing.T) {
	choices := NewChoices()
	choices.AntigravityModel = "Gemini 3.5 Flash (High)"
	choices.AntigravityModelTouched = true
	choices.ClaudeDisableIDEReading = true
	choices.ClaudeDisableIDEReadingTouched = true
	choices.ClaudeDisableMemory = true
	choices.ClaudeDisableMemoryTouched = true
	choices.ClaudeDisableConnectors = true
	choices.ClaudeDisableConnectorsTouched = true
	choices.ClaudeDisableQuestionTool = true
	choices.ClaudeDisableQuestionToolTouched = true
	choices.CodexDisableBrowser = true
	choices.CodexDisableBrowserTouched = true
	choices.CodexLocalConfigDir = true
	choices.CodexLocalConfigDirTouched = true

	ui := &MockUI{
		MultiSelectFunc: func(_ string, _ []string, selected *[]string) error {
			*selected = []string{AgentVSCode} // none of Claude/ClaudeVSCode/Codex
			return nil
		},
	}

	if err := promptEnabledAgents(ui, choices); err != nil {
		t.Fatalf("promptEnabledAgents error: %v", err)
	}

	assert.False(t, choices.ClaudeDisableIDEReading)
	assert.False(t, choices.ClaudeDisableIDEReadingTouched)
	assert.False(t, choices.ClaudeDisableMemory)
	assert.False(t, choices.ClaudeDisableMemoryTouched)
	assert.False(t, choices.ClaudeDisableConnectors)
	assert.False(t, choices.ClaudeDisableConnectorsTouched)
	assert.False(t, choices.ClaudeDisableQuestionTool)
	assert.False(t, choices.ClaudeDisableQuestionToolTouched)
	assert.Empty(t, choices.AntigravityModel)
	assert.False(t, choices.AntigravityModelTouched)
	assert.False(t, choices.CodexDisableBrowser)
	assert.False(t, choices.CodexDisableBrowserTouched)
	assert.False(t, choices.CodexLocalConfigDir)
	assert.False(t, choices.CodexLocalConfigDirTouched)
}

// TestPatchConfig_EndToEndDisableToggles confirms the prompt choices flow
// through to concrete config keys for every toggle at once.
func TestPatchConfig_EndToEndDisableToggles(t *testing.T) {
	content := `
[agents.claude]
enabled = true

[agents.codex]
enabled = true
`
	choices := NewChoices()
	choices.ClaudeDisableIDEReadingTouched = true
	choices.ClaudeDisableIDEReading = true
	choices.ClaudeDisableMemoryTouched = true
	choices.ClaudeDisableMemory = true
	choices.ClaudeDisableConnectorsTouched = true
	choices.ClaudeDisableConnectors = true
	choices.ClaudeDisableQuestionToolTouched = true
	choices.ClaudeDisableQuestionTool = true
	choices.CodexDisableBrowserTouched = true
	choices.CodexDisableBrowser = true

	out, err := PatchConfig(content, choices)
	if err != nil {
		t.Fatalf("PatchConfig error: %v", err)
	}

	for _, want := range []string{
		`agent_specific.env.CLAUDE_CODE_AUTO_CONNECT_IDE = "false"`,
		`agent_specific.env.ENABLE_CLAUDEAI_MCP_SERVERS = "false"`,
		"agent_specific.autoMemoryEnabled = false",
		"disable_question_tool = true",
		"browser_use = false",
	} {
		assert.Contains(t, out, want)
	}
	// The AskUserQuestion toggle is a typed scalar now; it must not touch the
	// user's agent_specific permissions/hooks arrays.
	assert.NotContains(t, out, "agent_specific.permissions.deny")
	assert.NotContains(t, out, "agent_specific.hooks.PreToolUse")
}

// TestReadClaudeDisableToggles covers the read-back helpers' detected-disabled
// paths so re-running the wizard against a disabled config defaults the
// matching prompt to Yes.
func TestReadClaudeDisableToggles(t *testing.T) {
	t.Run("env false detected", func(t *testing.T) {
		as := map[string]any{"env": map[string]any{"CLAUDE_CODE_AUTO_CONNECT_IDE": "false"}}
		assert.True(t, readClaudeEnvFalse(as, "CLAUDE_CODE_AUTO_CONNECT_IDE"))
		assert.False(t, readClaudeEnvFalse(as, "ENABLE_CLAUDEAI_MCP_SERVERS"))
	})
	t.Run("env true or absent not detected", func(t *testing.T) {
		assert.False(t, readClaudeEnvFalse(map[string]any{"env": map[string]any{"CLAUDE_CODE_AUTO_CONNECT_IDE": "true"}}, "CLAUDE_CODE_AUTO_CONNECT_IDE"))
		assert.False(t, readClaudeEnvFalse(map[string]any{}, "CLAUDE_CODE_AUTO_CONNECT_IDE"))
	})
	t.Run("auto memory disabled detected", func(t *testing.T) {
		assert.True(t, readClaudeAutoMemoryDisabled(map[string]any{"autoMemoryEnabled": false}))
		assert.False(t, readClaudeAutoMemoryDisabled(map[string]any{"autoMemoryEnabled": true}))
		assert.False(t, readClaudeAutoMemoryDisabled(map[string]any{}))
	})
	t.Run("legacy question-tool block detected via deny", func(t *testing.T) {
		as := map[string]any{"permissions": map[string]any{"deny": []any{"Bash", "AskUserQuestion"}}}
		assert.True(t, readClaudeQuestionToolDisabledLegacy(as))
	})
	t.Run("legacy question-tool block detected via PreToolUse hook", func(t *testing.T) {
		as := map[string]any{"hooks": map[string]any{"PreToolUse": []any{"skip", map[string]any{"matcher": "AskUserQuestion"}}}}
		assert.True(t, readClaudeQuestionToolDisabledLegacy(as))
	})
	t.Run("legacy question-tool block absent", func(t *testing.T) {
		assert.False(t, readClaudeQuestionToolDisabledLegacy(map[string]any{"permissions": map[string]any{"deny": []any{"Bash"}}}))
		assert.False(t, readClaudeQuestionToolDisabledLegacy(map[string]any{"hooks": map[string]any{"PreToolUse": []any{map[string]any{"matcher": "Other"}}}}))
		assert.False(t, readClaudeQuestionToolDisabledLegacy(map[string]any{}))
	})
}

// TestInitializeChoices_QuestionToolReadBackPrecedence verifies the typed
// disable_question_tool flag wins over a legacy agent_specific block, and that
// the legacy block is the fallback when the flag is unset.
func TestInitializeChoices_QuestionToolReadBackPrecedence(t *testing.T) {
	legacyDeny := map[string]any{"permissions": map[string]any{"deny": []any{"AskUserQuestion"}}}
	disabledFalse := false

	t.Run("legacy block, flag unset -> reads back disabled", func(t *testing.T) {
		cfg := &config.ProjectConfig{
			Config: config.Config{
				Agents: config.AgentsConfig{Claude: config.ClaudeConfig{AgentSpecific: legacyDeny}},
			},
			Root: t.TempDir(),
		}
		choices, err := initializeChoices(cfg)
		if err != nil {
			t.Fatalf("initializeChoices: %v", err)
		}
		if !choices.ClaudeDisableQuestionTool {
			t.Fatal("expected legacy agent_specific deny to read back as disabled")
		}
	})

	t.Run("typed flag false overrides legacy block", func(t *testing.T) {
		cfg := &config.ProjectConfig{
			Config: config.Config{
				Agents: config.AgentsConfig{Claude: config.ClaudeConfig{
					AgentSpecific:       legacyDeny,
					DisableQuestionTool: &disabledFalse,
				}},
			},
			Root: t.TempDir(),
		}
		choices, err := initializeChoices(cfg)
		if err != nil {
			t.Fatalf("initializeChoices: %v", err)
		}
		if choices.ClaudeDisableQuestionTool {
			t.Fatal("expected typed flag=false to take precedence over legacy block")
		}
	})
}

func TestInitializeChoices_StatuslineWizardDefaults(t *testing.T) {
	falseValue := false
	trueValue := true

	tests := []struct {
		name        string
		claudeValue *bool
		codexValue  *bool
		wantClaude  bool
		wantCodex   bool
	}{
		{
			name:       "absent config defaults enabled in wizard",
			wantClaude: true,
			wantCodex:  true,
		},
		{
			name:        "explicit false remains disabled",
			claudeValue: &falseValue,
			codexValue:  &falseValue,
		},
		{
			name:        "explicit true remains enabled",
			claudeValue: &trueValue,
			codexValue:  &trueValue,
			wantClaude:  true,
			wantCodex:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.ProjectConfig{
				Config: config.Config{
					Agents: config.AgentsConfig{
						Claude: config.ClaudeConfig{Statusline: tt.claudeValue},
						Codex:  config.CodexConfig{Statusline: tt.codexValue},
					},
				},
				Root: t.TempDir(),
			}

			choices, err := initializeChoices(cfg)
			if err != nil {
				t.Fatalf("initializeChoices: %v", err)
			}
			if choices.ClaudeStatusline != tt.wantClaude {
				t.Fatalf("ClaudeStatusline = %v, want %v", choices.ClaudeStatusline, tt.wantClaude)
			}
			if choices.CodexStatusline != tt.wantCodex {
				t.Fatalf("CodexStatusline = %v, want %v", choices.CodexStatusline, tt.wantCodex)
			}
		})
	}
}

func TestInitializeChoices_AntigravityModelReadBack(t *testing.T) {
	cfg := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Antigravity: config.AntigravityConfig{Model: "Gemini 3.5 Flash (High)"},
			},
		},
		Root: t.TempDir(),
	}

	choices, err := initializeChoices(cfg)
	if err != nil {
		t.Fatalf("initializeChoices: %v", err)
	}
	assert.Equal(t, "Gemini 3.5 Flash (High)", choices.AntigravityModel)
}

func TestApplyFreshSetupDefaults_DefaultsAntigravityModelToHigh(t *testing.T) {
	choices := NewChoices()

	applyFreshSetupDefaults(choices)

	assert.True(t, choices.EnabledAgents[AgentAntigravity])
	assert.Equal(t, defaultAntigravityModel, choices.AntigravityModel)
}

// TestReadCodexBrowserDisabled covers detecting the Codex browser-disable state.
func TestReadCodexBrowserDisabled(t *testing.T) {
	assert.True(t, readCodexBrowserDisabled(map[string]any{"features": map[string]any{"browser_use": false}}))
	assert.False(t, readCodexBrowserDisabled(map[string]any{"features": map[string]any{"browser_use": true}}))
	assert.False(t, readCodexBrowserDisabled(map[string]any{}))
}
