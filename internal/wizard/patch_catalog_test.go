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

// TestPatchConfig_Catalog_PruneOnDisable covers the prune-on-disable case:
// existing enabled=false block + EnabledMCPServersTouched + EnabledMCPServers[id] = false
// should remove the block entirely.
func TestPatchConfig_Catalog_PruneOnDisable(t *testing.T) {
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
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{id: false}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)
	assert.NotContains(t, out, fmt.Sprintf(`id = "%s"`, id))
}

// TestPatchConfig_Catalog_PruneCustomizedDefault locks in the user-direction
// "full prune" behavior: even hand-customized default-catalog blocks are
// removed when the user disables them in the wizard.
func TestPatchConfig_Catalog_PruneCustomizedDefault(t *testing.T) {
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
	assert.NotContains(t, out, fmt.Sprintf(`id = "%s"`, id))
	assert.NotContains(t, out, "/opt/custom/context7")
	assert.NotContains(t, out, "--enterprise")
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
		assert.NotContains(t, out, fmt.Sprintf(`id = "%s"`, contextID))
		assert.Contains(t, out, `id = "my-custom-server"`)
		assert.Contains(t, out, `command = "my-custom-cli"`)
	})
}

// TestPatchConfig_Catalog_RestoreMissingReadsCatalog covers the restore-missing
// flow under the new catalog source: the inserted block must come from
// mcp-catalog.toml, not the install seed (which has no entries).
func TestPatchConfig_Catalog_RestoreMissingReadsCatalog(t *testing.T) {
	defaults := catalogTestDefaults(t)
	id := defaults[0].ID

	content := `[mcp]
`
	choices := NewChoices()
	choices.DefaultMCPServers = defaults
	choices.RestoreMissingMCPServers = true
	choices.MissingDefaultMCPServers = []string{id}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)
	assert.Contains(t, out, fmt.Sprintf(`id = "%s"`, id))
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
// id must round-trip through prune cleanly.
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

	t.Run("disable removes all duplicates", func(t *testing.T) {
		choices := NewChoices()
		choices.DefaultMCPServers = defaults
		choices.EnabledMCPServersTouched = true
		choices.EnabledMCPServers = map[string]bool{id: false}

		out, err := PatchConfig(content, choices)
		require.NoError(t, err)
		assert.NotContains(t, out, fmt.Sprintf(`id = "%s"`, id))
		assert.NotContains(t, out, "context7-alt")
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

// TestPatchConfig_Catalog_MalformedDefaultBlockPrunesCleanly covers a default
// block missing the transport field. Prune-on-disable must still remove it
// without errors.
func TestPatchConfig_Catalog_MalformedDefaultBlockPrunesCleanly(t *testing.T) {
	defaults := catalogTestDefaults(t)
	id := "context7"

	content := fmt.Sprintf(`[mcp]

[[mcp.servers]]
id = "%s"
enabled = false
command = "npx"
`, id)
	choices := NewChoices()
	choices.DefaultMCPServers = defaults
	choices.EnabledMCPServersTouched = true
	choices.EnabledMCPServers = map[string]bool{id: false}

	out, err := PatchConfig(content, choices)
	require.NoError(t, err)
	assert.NotContains(t, out, fmt.Sprintf(`id = "%s"`, id))
}

// TestRun_Catalog_PrunesDisabledDefaultEndToEnd is the interactive-flow
// regression test: drive the full Run flow with a MockUI that toggles one
// default-catalog server off, then assert the rendered config has zero
// [[mcp.servers]] entries. This is the only path that exercises prune-on-disable
// end-to-end (profile-mode bypasses it).
func TestRun_Catalog_PrunesDisabledDefaultEndToEnd(t *testing.T) {
	root := t.TempDir()
	setupRepo(t, root)
	configDir := filepath.Join(root, ".agent-layer")

	// Start from a config with context7 enabled — the wizard's MultiSelect mock
	// will then leave it unchecked, which triggers prune-on-disable.
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
	assert.NotContains(t, string(data), "[[mcp.servers]]",
		"interactive wizard run that disables a default-catalog server should prune the block")
	assert.NotContains(t, string(data), `id = "context7"`)
}
