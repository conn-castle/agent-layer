package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestRunWithProjectMusePreflightFailsBeforeGeneratedOrNativeWrites(t *testing.T) {
	enabled := true
	tests := []struct {
		name        string
		mode        string
		commands    []string
		servers     []config.MCPServer
		errorDetail string
	}{
		{
			name:        "shell command prefix",
			mode:        config.ApprovalModeCommands,
			commands:    []string{"git status && git diff"},
			errorDetail: "commands.allow",
		},
		{
			name:        "assignment command prefix",
			mode:        config.ApprovalModeCommands,
			commands:    []string{"CI=1 go test ./..."},
			errorDetail: "commands.allow",
		},
		{
			name: "ambiguous MCP namespace",
			mode: config.ApprovalModeMCP,
			servers: []config.MCPServer{
				{ID: "team-tools", Enabled: &enabled, Clients: []string{"muse"}},
				{ID: "team_tools", Enabled: &enabled, Clients: []string{"muse"}},
			},
			errorDetail: "MCP namespaces",
		},
		{
			name:        "invalid MCP namespace",
			mode:        config.ApprovalModeMCP,
			servers:     []config.MCPServer{{ID: "_private", Enabled: &enabled, Clients: []string{"muse"}}},
			errorDetail: "MCP namespaces",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			agentLayerDir := filepath.Join(root, ".agent-layer")
			require.NoError(t, os.MkdirAll(agentLayerDir, 0o700))
			generatedPath := filepath.Join(root, ".gitignore")
			require.NoError(t, os.WriteFile(generatedPath, []byte("generated sentinel\n"), 0o600))

			nativeBase := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", nativeBase)
			nativePath := filepath.Join(nativeBase, "muse", "approval-policy.json")
			require.NoError(t, os.MkdirAll(filepath.Dir(nativePath), 0o700))
			require.NoError(t, os.WriteFile(nativePath, []byte("native sentinel\n"), 0o600))

			hookPath := filepath.Join(root, ".muse", "hooks.json")
			require.NoError(t, os.MkdirAll(filepath.Dir(hookPath), 0o700))
			require.NoError(t, os.WriteFile(hookPath, []byte(`{"hooks":{"Stop":[]}}`), 0o600))

			generatedInfo, generatedData := snapshotFile(t, generatedPath)
			nativeInfo, nativeData := snapshotFile(t, nativePath)
			hookInfo, hookData := snapshotFile(t, hookPath)
			project := &config.ProjectConfig{
				Root:          root,
				CommandsAllow: test.commands,
				Config: config.Config{
					Approvals: config.ApprovalsConfig{Mode: test.mode},
					Agents:    config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}},
					MCP:       config.MCPConfig{Servers: test.servers},
				},
			}

			_, err := RunWithProject(RealSystem{}, root, project)
			require.ErrorContains(t, err, test.errorDetail)
			assertSameFileAndData(t, generatedPath, generatedInfo, generatedData)
			assertSameFileAndData(t, nativePath, nativeInfo, nativeData)
			assertSameFileAndData(t, hookPath, hookInfo, hookData)
		})
	}
}

func TestMusePreflightAppliesOnlyToEnabledGrantedFeatures(t *testing.T) {
	enabled, disabled := true, false
	ambiguous := []config.MCPServer{
		{ID: "team-tools", Enabled: &enabled, Clients: []string{"muse"}},
		{ID: "team_tools", Enabled: &enabled, Clients: []string{"muse"}},
	}
	tests := []struct {
		name    string
		project *config.ProjectConfig
	}{
		{
			name: "Muse disabled",
			project: &config.ProjectConfig{CommandsAllow: []string{"CI=1 go test"}, Config: config.Config{
				Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
				Agents:    config.AgentsConfig{Muse: config.AgentConfig{Enabled: &disabled}},
				MCP:       config.MCPConfig{Servers: ambiguous},
			}},
		},
		{
			name: "no grants",
			project: &config.ProjectConfig{CommandsAllow: []string{"CI=1 go test"}, Config: config.Config{
				Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
				Agents:    config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}},
				MCP:       config.MCPConfig{Servers: ambiguous},
			}},
		},
		{
			name: "commands only ignores MCP namespace",
			project: &config.ProjectConfig{CommandsAllow: []string{"go test"}, Config: config.Config{
				Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
				Agents:    config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}},
				MCP:       config.MCPConfig{Servers: ambiguous},
			}},
		},
		{
			name: "MCP only ignores commands",
			project: &config.ProjectConfig{CommandsAllow: []string{"CI=1 go test"}, Config: config.Config{
				Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeMCP},
				Agents:    config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}},
				MCP: config.MCPConfig{Servers: []config.MCPServer{
					{ID: "team-tools", Enabled: &enabled, Clients: []string{"muse"}},
				}},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, preflightMuseApprovals(test.project))
		})
	}
}

func snapshotFile(t *testing.T, path string) (os.FileInfo, []byte) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	require.NoError(t, err)
	return info, data
}

func assertSameFileAndData(t *testing.T, path string, beforeInfo os.FileInfo, beforeData []byte) {
	t.Helper()
	afterInfo, afterData := snapshotFile(t, path)
	require.True(t, os.SameFile(beforeInfo, afterInfo), "%s was replaced", path)
	require.Equal(t, beforeData, afterData)
}
