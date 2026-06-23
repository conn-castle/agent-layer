package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
)

// mcpServerByID unmarshals rendered config.toml content and returns the server
// with the given id. It fails the test when the server is absent so that
// keep/disable assertions reflect the actual parsed state, not substring luck.
func mcpServerByID(t *testing.T, content string, id string) config.MCPServer {
	t.Helper()
	var doc struct {
		MCP struct {
			Servers []config.MCPServer `toml:"servers"`
		} `toml:"mcp"`
	}
	require.NoError(t, toml.Unmarshal([]byte(content), &doc))
	for _, srv := range doc.MCP.Servers {
		if srv.ID == id {
			return srv
		}
	}
	t.Fatalf("server %q not found in rendered config", id)
	return config.MCPServer{}
}

// mcpServerEnabled reports the parsed enabled state of the server with the given
// id in rendered config.toml content. It fails the test when the server is absent.
func mcpServerEnabled(t *testing.T, content string, id string) bool {
	t.Helper()
	return config.IsAgentEnabled(mcpServerByID(t, content, id).Enabled)
}

// TestPromptDefaultMCPServers_ReEnableClearsDisabledFlag guards the
// back-navigation case where a server is disabled for a missing secret in the
// secrets step (which sets both EnabledMCPServers[id]=false and
// DisabledMCPServers[id]=true) and is then re-selected in the MCP defaults step.
// Re-enabling must clear the stale disabled flag; otherwise the same server is
// listed under BOTH the "Enabled MCP Servers" and "Disabled MCP Servers (missing
// secrets)" sections of the review summary.
func TestPromptDefaultMCPServers_ReEnableClearsDisabledFlag(t *testing.T) {
	choices := NewChoices()
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "tavily", RequiredEnv: []string{"AL_TAVILY_API_KEY"}}}
	// Simulate the secrets step having disabled the server for a blank secret.
	choices.EnabledMCPServers["tavily"] = false
	choices.DisabledMCPServers["tavily"] = true

	ui := &MockUI{
		MultiSelectFunc: func(_ string, _ []string, selected *[]string) error {
			*selected = []string{"tavily"} // user re-selects it
			return nil
		},
	}
	require.NoError(t, promptDefaultMCPServers(ui, choices))

	assert.True(t, choices.EnabledMCPServers["tavily"], "re-selected server must be enabled")
	assert.False(t, choices.DisabledMCPServers["tavily"], "stale disabled flag must be cleared on re-enable")

	// The review summary must list the server only under Enabled, never under both.
	summary := buildSummary(choices)
	enabledIdx := strings.Index(summary, messages.WizardSummaryEnabledMCPServersHeader)
	disabledIdx := strings.Index(summary, messages.WizardSummaryDisabledMCPServersHeader)
	require.GreaterOrEqual(t, enabledIdx, 0)
	require.GreaterOrEqual(t, disabledIdx, 0)
	enabledSection := summary[enabledIdx:disabledIdx]
	disabledSection := summary[disabledIdx:]
	assert.Contains(t, enabledSection, "tavily", "re-enabled server must appear under Enabled")
	assert.NotContains(t, disabledSection, "tavily", "re-enabled server must NOT appear under Disabled")
}

func TestCustomMCPServers(t *testing.T) {
	defaults := []DefaultMCPServer{{ID: "context7"}, {ID: "tavily"}}
	servers := []config.MCPServer{
		{ID: "context7"},  // catalog default -> excluded
		{ID: ""},          // empty id -> skipped
		{ID: "my-server"}, // custom -> included
		{ID: "tavily"},    // catalog default -> excluded
		{ID: "another"},   // custom -> included
	}

	custom := customMCPServers(defaults, servers)

	ids := make([]string, 0, len(custom))
	for _, srv := range custom {
		ids = append(ids, srv.ID)
	}
	assert.Equal(t, []string{"my-server", "another"}, ids,
		"custom detection must exclude catalog defaults and empty ids while preserving config order")
}

func TestCustomMCPServers_DeduplicatesIDs(t *testing.T) {
	defaults := []DefaultMCPServer{{ID: "context7"}}
	servers := []config.MCPServer{
		{ID: "my-server"},
		{ID: "my-server"}, // duplicate id (possible under lenient load) -> collapsed
		{ID: "another"},
	}

	custom := customMCPServers(defaults, servers)

	ids := make([]string, 0, len(custom))
	for _, srv := range custom {
		ids = append(ids, srv.ID)
	}
	assert.Equal(t, []string{"my-server", "another"}, ids,
		"duplicate custom ids must collapse to first-seen so the id-keyed prompt has no indistinguishable options")
}

// customServerConfig is a config.toml with one catalog default and one custom
// (non-catalog) server, used by the patch-level keep/disable tests.
func customServerConfig(customEnabled bool) string {
	enabled := "true"
	if !customEnabled {
		enabled = "false"
	}
	return `[mcp]

[[mcp.servers]]
id = "context7"
enabled = false
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@2.1.1"]

[[mcp.servers]]
id = "my-custom-server"
enabled = ` + enabled + `
transport = "stdio"
command = "my-custom-cli"
args = ["--something"]
`
}

// TestPatchConfig_CustomMCP_DisableSetsEnabledFalseKeepsBlock covers the core
// decision: disabling a custom server sets enabled = false but preserves the
// entry (there is no catalog template to restore it from).
func TestPatchConfig_CustomMCP_DisableSetsEnabledFalseKeepsBlock(t *testing.T) {
	choices := NewChoices()
	choices.DefaultMCPServers = catalogTestDefaults(t)
	choices.CustomMCPServers = []string{"my-custom-server"}
	choices.CustomMCPServersTouched = true
	choices.CustomMCPServersEnabled = map[string]bool{"my-custom-server": false}

	out, err := PatchConfig(customServerConfig(true), choices)
	require.NoError(t, err)

	assert.Contains(t, out, `id = "my-custom-server"`, "disabling a custom server must not delete its entry")
	assert.Contains(t, out, `command = "my-custom-cli"`, "the server definition must be preserved")
	server := mcpServerByID(t, out, "my-custom-server")
	assert.False(t, config.IsAgentEnabled(server.Enabled), "disabled custom server must have enabled = false")
}

// TestPatchConfig_CustomMCP_KeepLeavesEnabled confirms that keeping a custom
// server leaves it enabled.
func TestPatchConfig_CustomMCP_KeepLeavesEnabled(t *testing.T) {
	choices := NewChoices()
	choices.DefaultMCPServers = catalogTestDefaults(t)
	choices.CustomMCPServers = []string{"my-custom-server"}
	choices.CustomMCPServersTouched = true
	choices.CustomMCPServersEnabled = map[string]bool{"my-custom-server": true}

	out, err := PatchConfig(customServerConfig(true), choices)
	require.NoError(t, err)

	server := mcpServerByID(t, out, "my-custom-server")
	assert.True(t, config.IsAgentEnabled(server.Enabled), "kept custom server must stay enabled")
}

// TestPatchConfig_CustomMCP_ReEnablePreviouslyDisabled confirms the step can also
// re-enable a custom server that was disabled in config.toml.
func TestPatchConfig_CustomMCP_ReEnablePreviouslyDisabled(t *testing.T) {
	choices := NewChoices()
	choices.DefaultMCPServers = catalogTestDefaults(t)
	choices.CustomMCPServers = []string{"my-custom-server"}
	choices.CustomMCPServersTouched = true
	choices.CustomMCPServersEnabled = map[string]bool{"my-custom-server": true}

	out, err := PatchConfig(customServerConfig(false), choices)
	require.NoError(t, err)

	server := mcpServerByID(t, out, "my-custom-server")
	assert.True(t, config.IsAgentEnabled(server.Enabled), "selecting a previously disabled custom server must re-enable it")
}

// TestPatchConfig_CustomMCP_TouchedFalsePreservesEnabledState locks the no-op
// branch: when the custom step is untouched (profile/--yes/programmatic callers),
// the original enabled state passes through unchanged.
func TestPatchConfig_CustomMCP_TouchedFalsePreservesEnabledState(t *testing.T) {
	choices := NewChoices()
	choices.DefaultMCPServers = catalogTestDefaults(t)
	// Populate the maps as initializeChoices would, but leave Touched false.
	choices.CustomMCPServers = []string{"my-custom-server"}
	choices.CustomMCPServersEnabled = map[string]bool{"my-custom-server": false}

	out, err := PatchConfig(customServerConfig(true), choices)
	require.NoError(t, err)

	server := mcpServerByID(t, out, "my-custom-server")
	assert.True(t, config.IsAgentEnabled(server.Enabled),
		"untouched custom step must preserve the original enabled = true state")
}

func TestBuildSummary_DisabledCustomMCPServers(t *testing.T) {
	t.Run("lists disabled custom servers when touched", func(t *testing.T) {
		c := NewChoices()
		c.ApprovalMode = "all"
		c.CustomMCPServers = []string{"keep-me", "drop-me"}
		c.CustomMCPServersTouched = true
		c.CustomMCPServersEnabled = map[string]bool{"keep-me": true, "drop-me": false}

		summary := buildSummary(c)
		assert.Contains(t, summary, "Disabled Custom MCP Servers")
		assert.Contains(t, summary, "- drop-me")
		assert.NotContains(t, summary, "- keep-me", "kept custom servers are not listed as disabled")
	})

	t.Run("omits section when custom step untouched", func(t *testing.T) {
		c := NewChoices()
		c.ApprovalMode = "all"
		c.CustomMCPServers = []string{"drop-me"}
		c.CustomMCPServersEnabled = map[string]bool{"drop-me": false}

		summary := buildSummary(c)
		assert.NotContains(t, summary, "Disabled Custom MCP Servers")
	})
}

// TestPromptCustomMCPServers_PreselectsEnabledAndAppliesSelection confirms that
// only currently-enabled custom servers are pre-selected and that the resulting
// selection drives the keep/disable decision (including re-enabling one that was
// disabled in config and disabling one that was enabled).
func TestPromptCustomMCPServers_PreselectsEnabledAndAppliesSelection(t *testing.T) {
	choices := NewChoices()
	choices.CustomMCPServers = []string{"enabled-one", "disabled-one"}
	choices.CustomMCPServersEnabled = map[string]bool{"enabled-one": true, "disabled-one": false}

	var gotOptions, gotPreselected []string
	ui := &MockUI{
		MultiSelectFunc: func(_ string, options []string, selected *[]string) error {
			gotOptions = append([]string{}, options...)
			gotPreselected = append([]string{}, (*selected)...)
			*selected = []string{"disabled-one"} // flip both
			return nil
		},
	}

	require.NoError(t, promptCustomMCPServers(ui, choices))
	assert.Equal(t, []string{"enabled-one", "disabled-one"}, gotOptions)
	assert.Equal(t, []string{"enabled-one"}, gotPreselected, "only enabled custom servers are pre-selected")
	assert.True(t, choices.CustomMCPServersTouched)
	assert.False(t, choices.CustomMCPServersEnabled["enabled-one"], "deselected server becomes disabled")
	assert.True(t, choices.CustomMCPServersEnabled["disabled-one"], "selected server becomes enabled")
}

// TestPromptCustomMCPServers_MultiSelectError confirms the multiselect error is
// propagated and the step is not marked touched when it fails before completing.
func TestPromptCustomMCPServers_MultiSelectError(t *testing.T) {
	choices := NewChoices()
	choices.CustomMCPServers = []string{"my-custom-server"}
	choices.CustomMCPServersEnabled = map[string]bool{"my-custom-server": true}

	wantErr := errors.New("multiselect boom")
	ui := &MockUI{
		MultiSelectFunc: func(string, []string, *[]string) error { return wantErr },
	}

	err := promptCustomMCPServers(ui, choices)
	require.ErrorIs(t, err, wantErr)
	require.False(t, choices.CustomMCPServersTouched, "an aborted custom step must not be marked touched")
}

// TestPromptWizardFlow_BackFromSecretsSkipsEmptyCustomStep verifies that when
// config.toml has no custom servers, pressing back from the secrets step skips
// the (empty) custom-MCP step and returns to the MCP defaults step rather than
// trapping the user on a no-op screen.
func TestPromptWizardFlow_BackFromSecretsSkipsEmptyCustomStep(t *testing.T) {
	choices := NewChoices()
	choices.ApprovalMode = config.ApprovalModeAll
	// A default server with a required secret so the secrets step prompts.
	choices.DefaultMCPServers = []DefaultMCPServer{{ID: "context7", RequiredEnv: []string{"AL_CONTEXT7_API_KEY"}}}
	// Force the manual-entry path: an inherited value would let promptSecrets
	// resolve the secret from the environment and never prompt (so no back).
	t.Setenv("AL_CONTEXT7_API_KEY", "")
	// No custom servers -> the custom step must be skipped in both directions.

	var mcpDefaultsCalls, secretInputCalls int
	ui := &MockUI{
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		ConfirmFunc: func(title string, value *bool) error {
			*value = title != messages.WizardEnableWarningsPrompt // warnings off, everything else on
			return nil
		},
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			switch title {
			case messages.WizardEnableDefaultMCPServersTitle:
				mcpDefaultsCalls++
				*selected = []string{"context7"}
			case messages.WizardKeepCustomMCPServersTitle:
				t.Fatalf("custom MCP step must be skipped when there are no custom servers")
			}
			return nil
		},
		SecretInputFunc: func(title string, value *string) error {
			secretInputCalls++
			if secretInputCalls == 1 {
				return errWizardBack // back from secrets
			}
			*value = "secret-value"
			return nil
		},
		NoteFunc: func(title, body string) error { return nil },
	}

	require.NoError(t, promptWizardFlow(t.TempDir(), ui, choices))
	require.Equal(t, 2, mcpDefaultsCalls, "back from secrets should return to the MCP defaults step")
	require.Equal(t, 2, secretInputCalls)
	require.Equal(t, "secret-value", choices.Secrets["AL_CONTEXT7_API_KEY"])
}

// TestRun_CustomMCP_DisableKeepsEntryEndToEnd drives the full interactive flow:
// a config with one custom server, the user unchecks it in the custom-server
// step, and the rendered config keeps the entry with enabled = false.
func TestRun_CustomMCP_DisableKeepsEntryEndToEnd(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	initialConfig := `[approvals]
mode = "none"
[agents.antigravity]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.copilot_cli]
enabled = false
[mcp]

[[mcp.servers]]
id = "my-custom-server"
enabled = true
transport = "stdio"
command = "my-custom-cli"
args = ["--something"]
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte(""), 0o600))

	sawCustomStep := false
	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error { *value = true; return nil },
		SelectFunc:  func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			if title == messages.WizardKeepCustomMCPServersTitle {
				sawCustomStep = true
				assert.Equal(t, []string{"my-custom-server"}, options)
				assert.Equal(t, []string{"my-custom-server"}, *selected, "enabled custom server is pre-selected")
				*selected = []string{} // user unchecks it -> disable
			}
			return nil
		},
		NoteFunc: func(title, body string) error { return nil },
	}

	mockSync := func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	require.NoError(t, Run(root, ui, mockSync, ""))
	assert.True(t, sawCustomStep, "wizard must present the custom MCP server step when a custom server exists")

	data, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), `id = "my-custom-server"`, "disabling must keep the entry, not delete it")
	server := mcpServerByID(t, string(data), "my-custom-server")
	assert.False(t, config.IsAgentEnabled(server.Enabled), "disabled custom server must have enabled = false")
}
