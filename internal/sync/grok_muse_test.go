package sync

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestGrokMuseSharedMCPSelection(t *testing.T) {
	for _, claude := range []bool{false, true} {
		for _, muse := range []bool{false, true} {
			for _, transport := range []string{"stdio", "http"} {
				t.Run(transport+"/claude="+strconv.FormatBool(claude)+"/muse="+strconv.FormatBool(muse), func(t *testing.T) {
					enabled := true
					project := &config.ProjectConfig{Config: config.Config{Agents: config.AgentsConfig{
						Grok: config.GrokConfig{Enabled: &enabled}, Claude: config.ClaudeConfig{Enabled: &claude}, Muse: config.AgentConfig{Enabled: &muse},
					}}, Env: map[string]string{"AL_MASK_SECRET": "synthetic-private-mask-token"}}
					sets := map[string][]string{
						"claude": {"claude"}, "muse": {"muse"}, "grok": {"grok"},
						"claude-muse": {"claude", "muse"}, "muse-grok": {"muse", "grok"},
						"claude-grok": {"claude", "grok"}, "all": {"claude", "muse", "grok"}, "other": {"codex"},
					}
					for id, clients := range sets {
						server := config.MCPServer{ID: id, Enabled: &enabled, Clients: clients, Transport: transport}
						if transport == "stdio" {
							server.Command = "fixture"
							server.Args = []string{"--id", id}
							server.Env = map[string]string{"FIXTURE": "${AL_MASK_SECRET}"}
						} else {
							server.URL = "http://127.0.0.1:19998/" + id
							server.HTTPTransport = "streamable"
							server.Headers = map[string]string{"Authorization": "Bearer ${AL_MASK_SECRET}"}
						}
						project.Config.MCP.Servers = append(project.Config.MCP.Servers, server)
					}
					root := t.TempDir()
					require.NoError(t, writeGrokConfig(RealSystem{}, root, project))
					data, err := os.ReadFile(filepath.Join(root, ".grok/config.toml")) // #nosec G304 -- generated file inside a test-owned temporary directory.
					require.NoError(t, err)
					require.NotContains(t, string(data), "synthetic-private-mask-token")
					var got struct {
						Servers map[string]struct {
							Enabled *bool
							Command string
							Args    []string
							Env     map[string]string
							URL     string
							Headers map[string]string
						} `toml:"mcp_servers"`
					}
					require.NoError(t, toml.Unmarshal(data, &got))
					selected := []string{"agent-layer", "grok", "muse-grok", "claude-grok", "all"}
					var actual []string
					for id, entry := range got.Servers {
						if entry.Enabled == nil || *entry.Enabled {
							actual = append(actual, id)
						}
					}
					require.ElementsMatch(t, selected, actual)
					masks := []string{}
					if muse {
						masks = append(masks, "muse", "claude-muse")
						if claude {
							masks = append(masks, "claude")
						}
					}
					require.Len(t, got.Servers, len(selected)+len(masks))
					for _, id := range masks {
						entry, ok := got.Servers[id]
						require.True(t, ok)
						require.NotNil(t, entry.Enabled)
						require.False(t, *entry.Enabled)
						if transport == "stdio" {
							require.Equal(t, "fixture", entry.Command)
							require.Equal(t, []string{"--id", id}, entry.Args)
							require.Equal(t, "${AL_MASK_SECRET}", entry.Env["FIXTURE"])
						} else {
							require.Equal(t, "http://127.0.0.1:19998/"+id, entry.URL)
							require.Equal(t, "Bearer ${AL_MASK_SECRET}", entry.Headers["Authorization"])
						}
					}
				})
			}
		}
	}
}
