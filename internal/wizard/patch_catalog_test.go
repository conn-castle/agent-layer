package wizard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/messages"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// catalogTestDefaults returns a stable set of default catalog ids parsed from the embedded catalog.
func catalogTestDefaults(t *testing.T) []DefaultMCPServer {
	t.Helper()
	defaults, err := loadDefaultMCPServers()
	require.NoError(t, err)
	require.NotEmpty(t, defaults)
	return defaults
}

// TestPatchConfig_Catalog_EnableInsertsBlockFromCatalog covers the
// "catalog-only enable" case: no current block, EnabledMCPServers[id] = true,
// EnabledMCPServersTouched. The catalog block should be inserted from
// mcp-catalog.toml.
func TestPatchConfig_Catalog_EnableInsertsBlockFromCatalog(t *testing.T) {
	defaults := catalogTestDefaults(t)
	id := defaults[0].ID

	content := `[mcp]
`
	choices := NewChoices()
	choices.DefaultMCPServers = defaults
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{id: true}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)
	assert.Contains(t, out, fmt.Sprintf(`id = "%s"`, id))
	assert.Contains(t, out, "enabled = true")
}

// TestPatchConfig_Catalog_DisableInPlace covers the disable-in-place case:
// existing block + EnabledMCPServersTouched + EnabledMCPServers[id] = false keeps
// the block with enabled = false rather than deleting it.
func TestPatchConfig_Catalog_DisableInPlace(t *testing.T) {
	defaults := catalogTestDefaults(t)
	id := defaults[0].ID

	content := fmt.Sprintf(`[mcp]

[[mcp.servers]]
id = "%s"
enabled = true
transport = "stdio"
command = "npx"
`, id)
	choices := NewChoices()
	choices.DefaultMCPServers = defaults
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{id: false}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)
	assert.Contains(t, out, fmt.Sprintf(`id = "%s"`, id))
	assert.False(t, mcpServerEnabled(t, out, id), "disabled default must be kept with enabled = false")
}

// TestPatchConfig_Catalog_DisableKeepsCustomizedDefault locks in disable-in-place
// for hand-customized default-catalog blocks: disabling preserves the block and
// its customization, only flipping enabled to false.
func TestPatchConfig_Catalog_DisableKeepsCustomizedDefault(t *testing.T) {
	defaults := catalogTestDefaults(t)
	id := "context7"

	content := fmt.Sprintf(`[mcp]

[[mcp.servers]]
id = "%s"
enabled = true
transport = "stdio"
command = "/opt/custom/context7"
args = ["--my-flag", "--enterprise"]
env = { CONTEXT7_API_KEY = "${AL_CONTEXT7_API_KEY}" }
`, id)
	choices := NewChoices()
	choices.DefaultMCPServers = defaults
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{id: false}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)
	assert.Contains(t, out, fmt.Sprintf(`id = "%s"`, id))
	assert.Contains(t, out, "/opt/custom/context7")
	assert.Contains(t, out, "--enterprise")
	assert.False(t, mcpServerEnabled(t, out, id), "disabled customized default must keep enabled = false")
}

// TestPatchConfig_Catalog_PreservesCustomServerThroughEnableDisable confirms
// that user-defined non-catalog server blocks survive enable and disable
// wizard passes (their id is outside the catalog, so prune does not apply).
func TestPatchConfig_Catalog_PreservesCustomServerThroughEnableDisable(t *testing.T) {
	defaults := catalogTestDefaults(t)
	contextID := "context7"

	content := fmt.Sprintf(`[mcp]

[[mcp.servers]]
id = "%s"
enabled = false
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@2.1.1"]

[[mcp.servers]]
id = "my-custom-server"
enabled = true
transport = "stdio"
command = "my-custom-cli"
args = ["--something"]
`, contextID)

	t.Run("custom block survives when default is enabled", func(t *testing.T) {
		choices := NewChoices()
		choices.DefaultMCPServers = defaults
		choices.EnabledMCPServersTouched = true
		choices.EnabledMCPServers = map[string]bool{contextID: true}

		out, err := PatchConfig(content, choices)
		require.NoError(t, err)
		assert.Contains(t, out, `id = "my-custom-server"`)
		assert.Contains(t, out, `command = "my-custom-cli"`)
	})

	t.Run("custom block survives when default is disabled", func(t *testing.T) {
		choices := NewChoices()
		choices.DefaultMCPServers = defaults
		choices.EnabledMCPServersTouched = true
		choices.EnabledMCPServers = map[string]bool{contextID: false}

		out, err := PatchConfig(content, choices)
		require.NoError(t, err)
		// Disabling a default keeps its block (enabled = false), not deletes it.
		assert.Contains(t, out, fmt.Sprintf(`id = "%s"`, contextID))
		assert.False(t, mcpServerEnabled(t, out, contextID))
		assert.Contains(t, out, `id = "my-custom-server"`)
		assert.Contains(t, out, `command = "my-custom-cli"`)
	})
}

// TestPatchConfig_Catalog_TouchedFalsePreservesAll covers the no-op branch:
// when EnabledMCPServersTouched = false, every existing block is preserved
// regardless of EnabledMCPServers map state. This protects programmatic
// callers that don't run the MCP step.
func TestPatchConfig_Catalog_TouchedFalsePreservesAll(t *testing.T) {
	defaults := catalogTestDefaults(t)
	id := defaults[0].ID

	content := fmt.Sprintf(`[mcp]

[[mcp.servers]]
id = "%s"
enabled = false
transport = "stdio"
command = "npx"
`, id)
	choices := NewChoices()
	choices.DefaultMCPServers = defaults
	choices.EnabledMCPServersTouched = false
	choices.EnabledMCPServers = map[string]bool{id: false}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)
	assert.Contains(t, out, fmt.Sprintf(`id = "%s"`, id))
}

// TestLoadCatalogDocument_MissingFileErrors covers the "catalog file missing"
// case: loadCatalogDocument must error loudly, never silently fall back.
func TestLoadCatalogDocument_MissingFileErrors(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == "mcp-catalog.toml" {
			return nil, errors.New("simulated missing file")
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadCatalogDocument()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "simulated missing file")
}

// TestLoadDefaultMCPServers_UnparseableCatalogErrors covers the "catalog file
// unparseable" case: invalid TOML should bubble up as an error.
func TestLoadDefaultMCPServers_UnparseableCatalogErrors(t *testing.T) {
	original := templates.ReadFunc
	templates.ReadFunc = func(path string) ([]byte, error) {
		if path == "mcp-catalog.toml" {
			return []byte("not = valid = toml = ====="), nil
		}
		return original(path)
	}
	t.Cleanup(func() { templates.ReadFunc = original })

	_, err := loadDefaultMCPServers()
	require.Error(t, err)
}

// TestPatchConfig_Catalog_DuplicateIDLegacyState covers lenient legacy state:
// a config that contains two [[mcp.servers]] blocks with the same default-catalog
// id must collapse to exactly one block on both enable and disable.
func TestPatchConfig_Catalog_DuplicateIDLegacyState(t *testing.T) {
	defaults := catalogTestDefaults(t)
	id := "context7"

	content := fmt.Sprintf(`[mcp]

[[mcp.servers]]
id = "%[1]s"
enabled = false
transport = "stdio"
command = "npx"

[[mcp.servers]]
id = "%[1]s"
enabled = true
transport = "stdio"
command = "/usr/local/bin/context7-alt"
`, id)

	t.Run("disable collapses duplicates to one disabled block", func(t *testing.T) {
		choices := NewChoices()
		choices.DefaultMCPServers = defaults
		choices.EnabledMCPServersTouched = true
		choices.EnabledMCPServers = map[string]bool{id: false}

		out, err := PatchConfig(content, choices)
		require.NoError(t, err)
		count := strings.Count(out, fmt.Sprintf(`id = "%s"`, id))
		assert.Equal(t, 1, count, "duplicates must collapse to exactly one block")
		assert.False(t, mcpServerEnabled(t, out, id), "kept block must be disabled")
	})

	t.Run("enable keeps exactly one block", func(t *testing.T) {
		choices := NewChoices()
		choices.DefaultMCPServers = defaults
		choices.EnabledMCPServersTouched = true
		choices.EnabledMCPServers = map[string]bool{id: true}

		out, err := PatchConfig(content, choices)
		require.NoError(t, err)
		count := strings.Count(out, fmt.Sprintf(`id = "%s"`, id))
		assert.Equal(t, 1, count, "expected exactly one block for default id")
	})
}

// TestPatchConfig_Catalog_MalformedDefaultBlockDisablesCleanly covers a default
// block missing the transport field. Disable-in-place must keep it with
// enabled = false without errors.
func TestPatchConfig_Catalog_MalformedDefaultBlockDisablesCleanly(t *testing.T) {
	defaults := catalogTestDefaults(t)
	id := "context7"

	content := fmt.Sprintf(`[mcp]

[[mcp.servers]]
id = "%s"
enabled = true
command = "npx"
`, id)
	choices := NewChoices()
	choices.DefaultMCPServers = defaults
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{id: false}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)
	assert.Contains(t, out, fmt.Sprintf(`id = "%s"`, id))
	assert.False(t, mcpServerEnabled(t, out, id))
}

// TestRun_Catalog_DisablesDefaultEndToEnd is the interactive-flow regression
// test: drive the full Run flow with a MockUI that toggles one default-catalog
// server off, then assert the rendered config keeps the [[mcp.servers]] entry
// with enabled = false. This is the only path that exercises disable-in-place
// end-to-end (profile-mode bypasses the MCP step).
func TestRun_Catalog_DisablesDefaultEndToEnd(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	// Start from a config with context7 enabled — the wizard's MultiSelect mock
	// will then leave it unchecked, which disables it in place.
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
id = "context7"
enabled = true
transport = "stdio"
command = "npx"
args = ["-y", "@upstash/context7-mcp@2.1.1"]
env = { CONTEXT7_API_KEY = "${AL_CONTEXT7_API_KEY}" }
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(initialConfig), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, ".env"), []byte("AL_CONTEXT7_API_KEY=set\n"), 0o600))

	ui := &MockUI{
		ConfirmFunc: func(title string, value *bool) error {
			*value = true
			return nil
		},
		SelectFunc: func(title string, options []string, current *string) error { return nil },
		MultiSelectFunc: func(title string, options []string, selected *[]string) error {
			// Leave nothing selected — context7 was on, user toggles it off.
			if title == messages.WizardEnableDefaultMCPServersTitle {
				*selected = []string{}
			}
			return nil
		},
		NoteFunc: func(title, body string) error { return nil },
	}

	mockSync := func(string) (*alsync.Result, error) { return &alsync.Result{}, nil }

	err := Run(root, ui, mockSync, "")
	require.NoError(t, err)

	data, readErr := os.ReadFile(filepath.Join(configDir, "config.toml"))
	require.NoError(t, readErr)
	assert.Contains(t, string(data), `id = "context7"`,
		"interactive wizard run that disables a default-catalog server should keep the block")
	assert.False(t, mcpServerEnabled(t, string(data), "context7"),
		"disabled default-catalog server should be kept with enabled = false")
}
