package musepolicy

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
)

func hookConfig(t *testing.T, root, mode string, enabled bool, clients string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700))
	state := "false"
	if enabled {
		state = "true"
	}
	content := `[approvals]
mode = "` + mode + `"
[agents.muse]
enabled = ` + state + `
[[mcp.servers]]
id = "selected"
enabled = true
transport = "stdio"
command = "fixture"
clients = ["` + clients + `"]
`
	for _, agent := range []string{"antigravity", "claude", "claude_vscode", "codex", "copilot_cli", "vscode", "grok"} {
		content += "\n[agents." + agent + "]\nenabled = false\n"
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer/config.toml"), []byte(content), 0o600))
}

func TestMCPHookRereadsModeEnablementAndClients(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		mode    string
		enabled bool
		client  string
		allowed bool
	}{
		{"all", true, "muse", true}, {"none", true, "muse", false},
		{"commands", true, "muse", false}, {"mcp", true, "muse", true},
		{"all", false, "muse", false}, {"all", true, "claude", false},
		{"all", true, "muse", true},
	} {
		hookConfig(t, root, tc.mode, tc.enabled, tc.client)
		for _, tool := range []string{"mcp__selected__write", "mcp__selected_extra__write", "mcp__excluded__write", "bash", "mcp__selected___write", "mcp__selected__other__write"} {
			data, err := json.Marshal(map[string]string{"hook_event_name": "PermissionRequest", "cwd": root, "tool_name": tool})
			require.NoError(t, err)
			var out bytes.Buffer
			require.NoError(t, HandleMCP(root, bytes.NewReader(data), &out))
			require.Equal(t, tc.allowed && tool == "mcp__selected__write", strings.Contains(out.String(), `"allow"`), tool)
		}
	}
	var out bytes.Buffer
	require.Error(t, HandleMCP(root, strings.NewReader(`{} {}`), &out))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".agent-layer/config.toml"), []byte("malformed"), 0o600))
	data, err := json.Marshal(map[string]string{"hook_event_name": "PermissionRequest", "cwd": root, "tool_name": "mcp__selected__write"})
	require.NoError(t, err)
	require.Error(t, HandleMCP(root, bytes.NewReader(data), &out))
	require.NotContains(t, out.String(), `"allow"`)
}

func TestMCPNamespacesRejectAmbiguousGrants(t *testing.T) {
	enabled := true
	for _, ids := range [][]string{{"a-b", "a_b"}, {"agent_layer"}, {"a__b"}, {"a.b"}, {"a_"}, {"_a"}, {"a-"}, {"-a"}} {
		cfg := config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}}}
		for _, id := range ids {
			cfg.MCP.Servers = append(cfg.MCP.Servers, config.MCPServer{ID: id, Enabled: &enabled})
		}
		_, err := MCPPrefixes(cfg)
		require.Error(t, err, ids)
	}
}

func TestMCPHookWorkspaceContainment(t *testing.T) {
	root := t.TempDir()
	hookConfig(t, root, "all", true, "muse")
	nested, outside := filepath.Join(root, "nested"), t.TempDir()
	require.NoError(t, os.Mkdir(nested, 0o700))
	escape, alias := filepath.Join(root, "escape"), filepath.Join(root, "alias")
	require.NoError(t, os.Symlink(outside, escape))
	require.NoError(t, os.Symlink(nested, alias))
	for _, tc := range []struct {
		cwd     string
		allowed bool
	}{{root, true}, {nested, true}, {alias, true}, {outside, false}, {escape, false}} {
		data, err := json.Marshal(map[string]any{"hook_event_name": "PermissionRequest", "cwd": tc.cwd, "tool_name": "mcp__selected__write", "tool_input": strings.Repeat("x", 2*1024*1024)})
		require.NoError(t, err)
		var out bytes.Buffer
		require.NoError(t, HandleMCP(root, bytes.NewReader(data), &out))
		require.Equal(t, tc.allowed, strings.Contains(out.String(), `"allow"`), tc.cwd)
	}
}
