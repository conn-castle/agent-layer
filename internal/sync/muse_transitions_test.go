package sync

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

func TestMuseTransitionsPreserveExistingClientOutputs(t *testing.T) {
	root, project := loadSyncFixtureProject(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	enabled, disabled := true, false
	project.Config.Agents.Grok.AgentSpecific = map[string]any{"plugins": map[string]any{"enabled": []string{"example"}}}
	project.Config.Agents.Claude.AgentSpecific = map[string]any{"disabledMcpjsonServers": []string{"user-disabled"}, "permissions": map[string]any{"deny": []string{"Bash(private:*)"}}}
	project.Config.MCP.Servers = append(project.Config.MCP.Servers,
		config.MCPServer{ID: "grok-selected", Enabled: &enabled, Transport: "stdio", Command: "fixture", Args: []string{"selected"}, Clients: []string{"grok"}},
		config.MCPServer{ID: "muse-only", Enabled: &enabled, Transport: "stdio", Command: "fixture", Clients: []string{"muse"}},
		config.MCPServer{ID: "shared", Enabled: &enabled, Transport: "stdio", Command: "fixture", Clients: []string{"muse", "claude"}})
	require.NoError(t, runMuseTransition(root, project))
	// Capture complete existing generated trees, including skills and permissions.
	baseline := map[string][]byte{}
	for _, path := range []string{".agents", ".claude", ".codex", ".grok", ".copilot", ".vscode", ".agy", "AGENTS.md", ".github/copilot-instructions.md", ".mcp.json"} {
		require.NoError(t, filepath.WalkDir(filepath.Join(root, path), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path) // #nosec G304 G122 -- path is contained in this test-owned temporary fixture.
			if err != nil {
				return err
			}
			baseline[rel] = data
			return nil
		}))
	}
	project.Config.Agents.Muse.Enabled = &enabled
	require.NoError(t, runMuseTransition(root, project))
	for rel, want := range baseline {
		if rel == ".mcp.json" || rel == ".claude/settings.json" {
			continue
		}
		got, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- path is contained in this test-owned temporary fixture.
		require.NoError(t, err)
		if rel == ".grok/config.toml" {
			var before, after map[string]any
			require.NoError(t, toml.Unmarshal(want, &before))
			require.NoError(t, toml.Unmarshal(got, &after))
			servers := after["mcp_servers"].(map[string]any)
			// Only these fixture IDs are added as disabled root-import masks.
			// Keep permissions, plugins, selected servers and every other field.
			for _, id := range []string{"example", "muse-only", "shared"} {
				entry, ok := servers[id].(map[string]any)
				require.True(t, ok, id)
				require.Equal(t, false, entry["enabled"], id)
				delete(servers, id)
			}
			require.NotEmpty(t, before["permission"])
			require.NotEmpty(t, before["plugins"])
			require.Equal(t, before, after, rel)
			continue
		}
		require.Equal(t, want, got, rel)
	}
	for _, rel := range []string{".agents/skills", ".claude/skills"} {
		info, err := os.Lstat(filepath.Join(root, rel))
		require.NoError(t, err)
		require.True(t, info.IsDir())
		require.Zero(t, info.Mode()&os.ModeSymlink)
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude/settings.json")) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal(data, &settings))
	require.ElementsMatch(t, []any{"user-disabled", "muse-only"}, settings["disabledMcpjsonServers"])
	require.Contains(t, string(data), "Bash(private:*)")
	project.Config.Agents.Muse.Enabled = &disabled
	require.NoError(t, runMuseTransition(root, project))
	for rel, want := range baseline {
		got, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- path is contained in this test-owned temporary fixture.
		require.NoError(t, err)
		require.Equal(t, want, got, rel)
	}
	require.NoFileExists(t, filepath.Join(root, ".muse/agent-layer-policy.json"))
}

func runMuseTransition(root string, project *config.ProjectConfig) error {
	_, err := RunWithProject(RealSystem{}, root, project)
	return err
}

func TestNeverEnabledMuseDoesNotAccessNativeHome(t *testing.T) {
	root, project := loadSyncFixtureProject(t)
	// Invalid native paths would fail immediately if sync tried to inspect them.
	native := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(native, []byte("untouched"), 0o600))
	t.Setenv("XDG_CONFIG_HOME", native)
	for _, enabled := range []*bool{nil, configBool(false)} {
		project.Config.Agents.Muse.Enabled = enabled
		require.NoError(t, runMuseTransition(root, project))
	}
	data, err := os.ReadFile(native) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	require.Equal(t, "untouched", string(data))
	require.NoDirExists(t, filepath.Join(root, ".muse"))
}

func configBool(value bool) *bool { return &value }

func TestMusePolicyReceiptSurvivesFailureAndRetiresPreviousHome(t *testing.T) {
	root, project := loadSyncFixtureProject(t)
	native := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", native)
	project.Config.Agents.Muse.Enabled = configBool(true)
	project.CommandsAllow = []string{"git status"}
	policy := filepath.Join(native, "muse/approval-policy.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(policy), 0o700))
	require.NoError(t, os.WriteFile(policy, []byte("invalid policy"), 0o600))
	require.Error(t, runMuseTransition(root, project))
	require.FileExists(t, filepath.Join(root, ".muse/agent-layer-policy.json"))
	require.NoError(t, os.WriteFile(policy, []byte(`{"schema_version":2,"rules":[]}`), 0o600))
	require.NoError(t, runMuseTransition(root, project))
	// An unrelated native deny must survive ownership-bounded retirement.
	var document map[string]any
	data, err := os.ReadFile(policy) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &document))
	document["rules"] = append(document["rules"].([]any), map[string]any{"effect": "deny", "reason": "user", "rule": map[string]any{"kind": "tool_action", "tool_name": "private"}})
	data, err = json.Marshal(document)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(policy, data, 0o600))
	require.NoError(t, os.Remove(filepath.Join(root, ".muse/hooks.json")))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "new-home"))
	project.Config.Agents.Muse.Enabled = configBool(false)
	require.NoError(t, runMuseTransition(root, project))
	data, err = os.ReadFile(policy) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	require.NotContains(t, string(data), "agent-layer:")
	require.Contains(t, string(data), `"deny"`)
	require.NoDirExists(t, os.Getenv("XDG_CONFIG_HOME"))
}

func TestDisabledMusePreservesUnownedLocalConfiguration(t *testing.T) {
	for _, enabled := range []*bool{nil, configBool(false)} {
		for _, shape := range []string{"directory-symlink", "file-symlink", "malformed", "schema"} {
			t.Run(shape, func(t *testing.T) {
				root, project := loadSyncFixtureProject(t)
				project.Config.Agents.Muse.Enabled = enabled
				t.Setenv("XDG_CONFIG_HOME", "relative-path-must-not-be-read")
				external := t.TempDir()
				for _, rel := range []string{".muse/hooks.json", ".muse-config/muse/settings.json"} {
					path := filepath.Join(root, rel)
					contents := []byte("broken user configuration")
					if shape == "schema" {
						contents = []byte(`{"hooks":42,"mcpServers":false}`)
					}
					if shape == "directory-symlink" {
						base := ".muse"
						if rel != ".muse/hooks.json" {
							base = ".muse-config"
						}
						target := filepath.Join(external, base)
						require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(external, rel)), 0o700))
						require.NoError(t, os.WriteFile(filepath.Join(external, rel), contents, 0o600))
						require.NoError(t, os.Symlink(target, filepath.Join(root, base)))
					} else {
						require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
						if shape == "file-symlink" {
							target := filepath.Join(external, filepath.Base(path))
							require.NoError(t, os.WriteFile(target, contents, 0o600))
							require.NoError(t, os.Symlink(target, path))
						} else {
							require.NoError(t, os.WriteFile(path, contents, 0o600))
						}
					}
					before, err := os.Lstat(path)
					require.NoError(t, err)
					require.NoError(t, runMuseTransition(root, project))
					after, err := os.Lstat(path)
					require.NoError(t, err)
					require.Equal(t, before.Mode(), after.Mode())
					got, err := os.ReadFile(path) // #nosec G304 -- test-owned temporary fixture.
					require.NoError(t, err)
					require.Equal(t, contents, got)
				}
			})
		}
	}
}

func TestMusePolicyReceiptDiagnostic(t *testing.T) {
	for _, contents := range []string{"broken", "", "{}", `{"generated_by":42}`} {
		t.Run(contents, func(t *testing.T) {
			root, project := loadSyncFixtureProject(t)
			require.NoError(t, os.MkdirAll(filepath.Join(root, ".muse"), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(root, ".muse/agent-layer-policy.json"), []byte(contents), 0o600))
			err := runMuseTransition(root, project)
			require.ErrorContains(t, err, "policy ownership receipt")
			require.ErrorContains(t, err, "agent-layer-policy.json")
		})
	}
}

func TestMuseMCPRuntimeEnvironmentDefaults(t *testing.T) {
	root, project := loadSyncFixtureProject(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	project.Config.Agents.Muse.Enabled = configBool(true)
	project.Config.MCP.Servers = []config.MCPServer{
		{ID: "shared-env", Enabled: configBool(true), Transport: "stdio", Command: "fixture", Clients: []string{"muse", "claude"}, Env: map[string]string{ghConfigDirEnv: "/explicit/gh"}},
		{ID: "claude-env", Enabled: configBool(true), Transport: "stdio", Command: "fixture", Clients: []string{"claude"}},
	}
	require.NoError(t, runMuseTransition(root, project))
	data, err := os.ReadFile(filepath.Join(root, ".mcp.json")) // #nosec G304 -- test-owned temporary fixture.
	require.NoError(t, err)
	var output struct {
		Servers map[string]struct {
			Env map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(data, &output))
	require.Equal(t, map[string]string{ghConfigDirEnv: "/explicit/gh", xdgConfigHomeEnv: "${XDG_CONFIG_HOME:-}", xdgDataHomeEnv: "${XDG_DATA_HOME:-}"}, output.Servers["shared-env"].Env)
	require.Empty(t, output.Servers["claude-env"].Env)
	require.Equal(t, map[string]string{ghConfigDirEnv: "${GH_CONFIG_DIR:-}", xdgConfigHomeEnv: "${XDG_CONFIG_HOME:-}", xdgDataHomeEnv: "${XDG_DATA_HOME:-}", versiondispatch.EnvDevelopmentBypassVersionDispatch: "${AL_DEV_BYPASS_VERSION_DISPATCH:-}", "AL_DEV_EXECUTABLE": "${AL_DEV_EXECUTABLE:-}", "AL_DISPATCH_ACTIVE": "${AL_DISPATCH_ACTIVE:-0}", "AL_RUN_ID": "${AL_RUN_ID:-}", "AL_RUN_DIR": "${AL_RUN_DIR:-}"}, output.Servers["agent-layer"].Env)
	require.NotContains(t, string(data), os.Getenv("XDG_CONFIG_HOME"))

	project.Config.Agents.Muse.Enabled = configBool(false)
	require.NoError(t, runMuseTransition(root, project))
	data, err = os.ReadFile(filepath.Join(root, ".mcp.json")) // #nosec G304 -- test-owned temporary fixture.
	require.NoError(t, err)
	require.NotContains(t, string(data), "${AL_DEV_BYPASS_VERSION_DISPATCH:-}")
}

func TestDisabledMuseOwnedCleanupFailsSafely(t *testing.T) {
	for _, damage := range []string{"hooks", "policy"} {
		t.Run(damage, func(t *testing.T) {
			root, project := loadSyncFixtureProject(t)
			native := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", native)
			project.Config.Agents.Muse.Enabled = configBool(true)
			project.CommandsAllow = []string{"git status"}
			require.NoError(t, runMuseTransition(root, project))
			path := filepath.Join(root, ".muse/hooks.json")
			if damage == "policy" {
				path = filepath.Join(native, "muse/approval-policy.json")
			}
			require.NoError(t, os.WriteFile(path, []byte("corrupted owned state"), 0o600))
			project.Config.Agents.Muse.Enabled = configBool(false)
			require.Error(t, runMuseTransition(root, project))
			require.FileExists(t, filepath.Join(root, ".muse/agent-layer-policy.json"))
			contents, err := os.ReadFile(path) // #nosec G304 -- test-owned temporary fixture.
			require.NoError(t, err)
			require.Equal(t, "corrupted owned state", string(contents))
		})
	}
}
