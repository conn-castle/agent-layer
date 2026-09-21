package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestMuseApprovalsSyncPreservesUserHooksAndRevokesOwnedRules(t *testing.T) {
	root := t.TempDir()
	native := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", native)
	enabled := true
	project := &config.ProjectConfig{Root: root, CommandsAllow: []string{"git status"}, Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll}, Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}}}}
	path := filepath.Join(root, ".muse", "hooks.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"hooks":{"PermissionRequest":[{"matcher":".*","hooks":[{"type":"command","command":"user-review-hook"}]}],"Stop":[{"hooks":[{"type":"command","command":"user-stop-hook"}]}]}}`), 0o600))
	for range 2 {
		require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Len(t, doc["hooks"].(map[string]any)["PermissionRequest"], 2)
	require.Contains(t, string(data), "user-review-hook")
	require.Contains(t, string(data), "user-stop-hook")
	policy := filepath.Join(native, "muse", "approval-policy.json")
	require.Equal(t, [][]string{{"git", "status"}}, museOwnedCommandPrefixes(t, policy))
	project.Config.Approvals.Mode = config.ApprovalModeNone
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	require.Empty(t, museOwnedCommandPrefixes(t, policy))
	enabled = false
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	data, err = os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	require.NoError(t, err)
	require.NotContains(t, string(data), "al hook muse-mcp")
	require.Contains(t, string(data), "user-review-hook")
	require.Contains(t, string(data), "user-stop-hook")
}

func TestMuseApprovalsRejectsSymlinkAndMalformedHooks(t *testing.T) {
	enabled := true
	project := &config.ProjectConfig{Config: config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}}}}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root := t.TempDir()
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(root, ".muse")))
	require.ErrorContains(t, writeMuseApprovals(RealSystem{}, root, project), "real directory")
	root = t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, ".muse"), 0o700))
	path := filepath.Join(root, ".muse/hooks.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"hooks":{"PermissionRequest":{}}}`), 0o600))
	require.ErrorContains(t, writeMuseApprovals(RealSystem{}, root, project), "array")
}

func TestDisablingMusePrunesCommandsWithoutProjectHook(t *testing.T) {
	root := t.TempDir()
	native := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", native)
	enabled := true
	project := &config.ProjectConfig{Root: root, CommandsAllow: []string{"git status"}, Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands}, Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}}}}
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	require.NoError(t, os.Remove(filepath.Join(root, ".muse/hooks.json")))
	enabled = false
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	require.Empty(t, museOwnedCommandPrefixes(t, filepath.Join(native, "muse/approval-policy.json")))
}

func TestMuseApprovalHookRegistrationIsStableAcrossModeAndClientChanges(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	enabled := true
	project := &config.ProjectConfig{Root: root, Config: config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}}, MCP: config.MCPConfig{Servers: []config.MCPServer{
		{ID: "selected", Enabled: &enabled, Clients: []string{"muse"}},
		{ID: "excluded", Enabled: &enabled, Clients: []string{"claude"}},
	}}}}
	cases := []struct {
		name    string
		mode    string
		clients []string
	}{
		{name: "all with selected server", mode: config.ApprovalModeAll, clients: []string{"muse"}},
		{name: "none with server excluded", mode: config.ApprovalModeNone, clients: []string{"claude"}},
		{name: "MCP with selected server", mode: config.ApprovalModeMCP, clients: []string{"muse"}},
		{name: "commands with server excluded", mode: config.ApprovalModeCommands, clients: []string{"claude"}},
		{name: "YOLO with selected server", mode: config.ApprovalModeYOLO, clients: []string{"muse"}},
	}
	canonical, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	expectedCommand := "al hook muse-mcp " + shellSingleQuote(canonical) + museApprovalHookMarker
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			project.Config.Approvals.Mode = test.mode
			project.Config.MCP.Servers[0].Clients = test.clients
			require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
			data, err := os.ReadFile(filepath.Join(root, ".muse/hooks.json")) // #nosec G304 -- test-controlled temporary path.
			require.NoError(t, err)
			var document struct {
				Hooks map[string][]struct {
					Matcher string `json:"matcher"`
					Hooks   []struct {
						Type    string `json:"type"`
						Command string `json:"command"`
					} `json:"hooks"`
				} `json:"hooks"`
			}
			require.NoError(t, json.Unmarshal(data, &document))
			entries := document.Hooks[musePermissionRequestHook]
			require.Len(t, entries, 1)
			require.Equal(t, "mcp__.*", entries[0].Matcher)
			require.Len(t, entries[0].Hooks, 1)
			require.Equal(t, chimeHandlerCommandType, entries[0].Hooks[0].Type)
			require.Equal(t, expectedCommand, entries[0].Hooks[0].Command)
		})
	}
}

func TestDisabledMuseWithoutNativeHomeNeedsNoPolicyStore(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	root := t.TempDir()
	disabled := false
	project := &config.ProjectConfig{Root: root, Config: config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &disabled}}}}
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	enabled := true
	project.Config.Agents.Muse.Enabled = &enabled
	require.ErrorContains(t, writeMuseApprovals(RealSystem{}, root, project), "resolve Muse approval policy directory")
}

func museOwnedCommandPrefixes(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	require.NoError(t, err)
	var document struct {
		Rules []struct {
			Reason string `json:"reason"`
			Rule   struct {
				Kind       string   `json:"kind"`
				ArgvPrefix []string `json:"argv_prefix"`
			} `json:"rule"`
		} `json:"rules"`
	}
	require.NoError(t, json.Unmarshal(data, &document))
	var prefixes [][]string
	for _, rule := range document.Rules {
		if rule.Rule.Kind == "shell_command_argv_prefix" && strings.HasPrefix(rule.Reason, "agent-layer:") {
			prefixes = append(prefixes, rule.Rule.ArgvPrefix)
		}
	}
	return prefixes
}
