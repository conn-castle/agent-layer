package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/musepolicy"
)

func TestMuseApprovalsSyncPreservesUserHooksAndRevokesOwnedRules(t *testing.T) {
	root := t.TempDir()
	native := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", native)
	enabled := true
	project := &config.ProjectConfig{Root: root, CommandsAllow: []string{"git status"}, Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll}, Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}}}}
	path := filepath.Join(root, ".muse", "hooks.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"hooks":{"PermissionRequest":[{"matcher":".*","hooks":[{"type":"command","command":"user-review-hook"}]}],"Stop":[{"hooks":[{"type":"command","command":"user-stop-hook"}]}],"SessionStart":[{"hooks":[{"type":"command","command":"user-session-hook"}]}],"UserPromptSubmit":[{"hooks":[{"type":"command","command":"user-prompt-hook"}]}]}}`), 0o600))
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
	require.Contains(t, string(data), "user-session-hook")
	require.Contains(t, string(data), "user-prompt-hook")
	require.Equal(t, 2, strings.Count(string(data), agentLayerHerdRMarker))
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
	require.NotContains(t, string(data), agentLayerHerdRMarker)
	require.Contains(t, string(data), "user-review-hook")
	require.Contains(t, string(data), "user-stop-hook")
	require.Contains(t, string(data), "user-session-hook")
	require.Contains(t, string(data), "user-prompt-hook")
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

func TestForgedMusePolicyReceiptDoesNotRetireAnotherWorkspace(t *testing.T) {
	victimRoot := t.TempDir()
	native := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", native)
	enabled := true
	victim := &config.ProjectConfig{Root: victimRoot, CommandsAllow: []string{"git status"}, Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands}, Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}}}}
	require.NoError(t, writeMuseApprovals(RealSystem{}, victimRoot, victim))
	policy := filepath.Join(native, "muse", "approval-policy.json")
	require.Equal(t, [][]string{{"git", "status"}}, museOwnedCommandPrefixes(t, policy))

	attackerRoot := t.TempDir()
	victimCanonical, err := filepath.EvalSymlinks(victimRoot)
	require.NoError(t, err)
	victimCanonical, err = filepath.Abs(victimCanonical)
	require.NoError(t, err)
	receiptPath := filepath.Join(attackerRoot, ".muse", "agent-layer-policy.json")
	require.NoError(t, writeMusePolicyReceipt(RealSystem{}, receiptPath, filepath.Join(native, "muse"), victimCanonical))
	disabled := false
	attacker := &config.ProjectConfig{Root: attackerRoot, Config: config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &disabled}}}}
	require.ErrorContains(t, writeMuseApprovals(RealSystem{}, attackerRoot, attacker), "workspace_root does not match the current workspace")
	require.Equal(t, [][]string{{"git", "status"}}, museOwnedCommandPrefixes(t, policy))
	require.FileExists(t, receiptPath)
}

func TestForgedMusePolicyReceiptDoesNotRetireAnotherWorkspaceOnEnable(t *testing.T) {
	victimRoot := t.TempDir()
	native := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", native)
	enabled := true
	victim := &config.ProjectConfig{Root: victimRoot, CommandsAllow: []string{"git status"}, Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands}, Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}}}}
	require.NoError(t, writeMuseApprovals(RealSystem{}, victimRoot, victim))
	policy := filepath.Join(native, "muse", "approval-policy.json")
	require.Equal(t, [][]string{{"git", "status"}}, museOwnedCommandPrefixes(t, policy))

	attackerRoot := t.TempDir()
	victimCanonical, err := filepath.EvalSymlinks(victimRoot)
	require.NoError(t, err)
	victimCanonical, err = filepath.Abs(victimCanonical)
	require.NoError(t, err)
	require.NoError(t, writeMusePolicyReceipt(RealSystem{}, filepath.Join(attackerRoot, ".muse", "agent-layer-policy.json"), filepath.Join(native, "muse"), victimCanonical))
	attacker := &config.ProjectConfig{Root: attackerRoot, CommandsAllow: []string{"git diff"}, Config: config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands}, Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}}}}
	require.NoError(t, writeMuseApprovals(RealSystem{}, attackerRoot, attacker))
	require.ElementsMatch(t, [][]string{{"git", "status"}, {"git", "diff"}}, museOwnedCommandPrefixes(t, policy))
	canonical, err := filepath.EvalSymlinks(attackerRoot)
	require.NoError(t, err)
	canonical, err = filepath.Abs(canonical)
	require.NoError(t, err)
	receipt, err := readMusePolicyReceipt(RealSystem{}, filepath.Join(attackerRoot, ".muse", "agent-layer-policy.json"))
	require.NoError(t, err)
	require.Equal(t, canonical, receipt.Root)
	require.Equal(t, filepath.Join(native, "muse"), receipt.Directory)
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

func TestMuseEnabledEmptyCommandsPruneButDisabledSyncStaysNarrow(t *testing.T) {
	base, root, orphan := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	project := &config.ProjectConfig{CommandsAllow: []string{"go"}, Config: config.Config{
		Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
		Agents:    config.AgentsConfig{Muse: config.AgentConfig{Enabled: configBool(true)}},
	}}
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	require.NoError(t, writeMuseApprovals(RealSystem{}, orphan, project))
	require.NoError(t, os.RemoveAll(orphan))
	project.Config.Agents.Muse.Enabled = configBool(false)
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	policy := filepath.Join(base, "muse/approval-policy.json")
	require.Equal(t, [][]string{{"go"}}, museOwnedCommandPrefixes(t, policy))
	require.NoFileExists(t, filepath.Join(root, ".muse/agent-layer-policy.json"))
	project.Config.Agents.Muse.Enabled = configBool(true)
	project.CommandsAllow = nil
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	require.Empty(t, museOwnedCommandPrefixes(t, policy))
	require.FileExists(t, filepath.Join(root, ".muse/agent-layer-policy.json"))
}

func TestMuseRetirementOfPreviousDirectoryDoesNotPruneOtherRoots(t *testing.T) {
	base, root, orphan := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	project := &config.ProjectConfig{CommandsAllow: []string{"make"}, Config: config.Config{
		Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
		Agents:    config.AgentsConfig{Muse: config.AgentConfig{Enabled: configBool(true)}},
	}}
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	require.NoError(t, writeMuseApprovals(RealSystem{}, orphan, project))
	require.NoError(t, os.RemoveAll(orphan))
	newBase := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", newBase)
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	require.Equal(t, [][]string{{"make"}}, museOwnedCommandPrefixes(t, filepath.Join(base, "muse/approval-policy.json")))
	require.Equal(t, [][]string{{"make"}}, museOwnedCommandPrefixes(t, filepath.Join(newBase, "muse/approval-policy.json")))
}

func TestMuseEmptyCommandsPreserveUnrelatedUnsupportedNativeState(t *testing.T) {
	for _, state := range []string{"symlink directory", "future schema", "unknown fields"} {
		for _, owner := range []string{"none", "other", "current"} {
			t.Run(state+"/"+owner, func(t *testing.T) {
				base, root, other := t.TempDir(), t.TempDir(), t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", base)
				native := filepath.Join(base, "muse")
				realDir := native
				if state == "symlink directory" {
					realDir = t.TempDir()
					require.NoError(t, os.Symlink(realDir, native))
				}
				path := filepath.Join(realDir, "approval-policy.json")
				if owner == "none" {
					require.NoError(t, os.MkdirAll(realDir, 0o700))
					require.NoError(t, os.WriteFile(path, []byte(`{"schema_version":2,"rules":[{"reason":"user"}]}`), 0o600))
				} else {
					ownedRoot := other
					if owner == "current" {
						ownedRoot = root
					}
					require.NoError(t, musepolicy.SyncCommands(realDir, ownedRoot, []string{"make"}))
					require.NoError(t, os.Remove(filepath.Join(realDir, "approval-policy.lock")))
				}
				before, err := os.ReadFile(path) // #nosec G304 -- isolated temporary native fixture.
				require.NoError(t, err)
				switch state {
				case "future schema":
					before = []byte(strings.ReplaceAll(strings.ReplaceAll(string(before), `"schema_version": 2`, `"schema_version": 999`), `"schema_version":2`, `"schema_version":999`))
				case "unknown fields":
					before = append([]byte(`{"future":true,`), before[1:]...)
				}
				require.NoError(t, os.WriteFile(path, before, 0o600)) // #nosec G703 -- policy path and source bytes belong to this temporary fixture.
				project := &config.ProjectConfig{Root: root, Config: config.Config{
					Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
					Agents:    config.AgentsConfig{Muse: config.AgentConfig{Enabled: configBool(true)}},
				}}
				err = writeMuseApprovals(RealSystem{}, root, project)
				if owner == "current" {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				after, err := os.ReadFile(path) // #nosec G304 -- isolated temporary native fixture.
				require.NoError(t, err)
				require.Equal(t, before, after)
				if owner == "none" || state == "symlink directory" {
					require.NoFileExists(t, filepath.Join(realDir, "approval-policy.lock"))
				}
				// With other owned candidates, optional pruning now validates
				// the schema once under its lock; the policy stays unchanged.
			})
		}
	}
}

func TestMuseNativeDirectoryAliasDoesNotRetireAndReappend(t *testing.T) {
	base, root, other := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	project := &config.ProjectConfig{CommandsAllow: []string{"go"}, Config: config.Config{
		Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
		Agents:    config.AgentsConfig{Muse: config.AgentConfig{Enabled: configBool(true)}},
	}}
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	require.NoError(t, writeMuseApprovals(RealSystem{}, other, project))
	path := filepath.Join(base, "muse/approval-policy.json")
	require.NoError(t, os.Chtimes(path, time.Unix(42, 0), time.Unix(42, 0)))
	before, err := os.ReadFile(path) // #nosec G304 -- isolated temporary native fixture.
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	alias := filepath.Join(t.TempDir(), "config-alias")
	require.NoError(t, os.Symlink(base, alias))
	t.Setenv("XDG_CONFIG_HOME", alias)
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	after, err := os.ReadFile(path) // #nosec G304 -- isolated temporary native fixture.
	require.NoError(t, err)
	current, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, info.ModTime(), current.ModTime())
	require.True(t, os.SameFile(info, current))
}

func TestMuseNonemptyCommandsPruneOrphans(t *testing.T) {
	base, root, orphan := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	project := &config.ProjectConfig{CommandsAllow: []string{"make"}, Config: config.Config{
		Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
		Agents:    config.AgentsConfig{Muse: config.AgentConfig{Enabled: configBool(true)}},
	}}
	require.NoError(t, writeMuseApprovals(RealSystem{}, orphan, project))
	require.NoError(t, os.RemoveAll(orphan))
	project.CommandsAllow = []string{"go"}
	require.NoError(t, writeMuseApprovals(RealSystem{}, root, project))
	require.Equal(t, [][]string{{"go"}}, museOwnedCommandPrefixes(t, filepath.Join(base, "muse/approval-policy.json")))
}

func TestMuseReceiptRetirementKeepsExistingUncleanPathValidation(t *testing.T) {
	base, root := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	dir := filepath.Join(base, "muse") + "/../muse"
	receiptPath := filepath.Join(root, ".muse/agent-layer-policy.json")
	require.NoError(t, writeMusePolicyReceipt(RealSystem{}, receiptPath, dir, root+"/."))
	receipt, err := readMusePolicyReceipt(RealSystem{}, receiptPath)
	require.NoError(t, err)
	require.Equal(t, root+"/.", receipt.Root)
	require.Equal(t, dir, receipt.Directory)
	target, err := musePolicyRetirementDirectory(receipt, root)
	require.NoError(t, err)
	require.Equal(t, dir, target)
}

func TestMuseReceiptErrorIdentifiesReceiptAndPathOnce(t *testing.T) {
	for _, kind := range []string{"invalid marker", "malformed JSON", "empty document", "wrong root", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			path := filepath.Join(root, ".muse/agent-layer-policy.json")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			data := []byte(`{"generated_by":"user","directory":"/native/muse","workspace_root":"/project"}`)
			switch kind {
			case "malformed JSON":
				data = []byte(`{`)
			case "empty document":
				data = []byte(`{}`)
			case "wrong root":
				var err error
				data, err = json.Marshal(musePolicyReceipt{GeneratedBy: "agent-layer", Directory: filepath.Join(t.TempDir(), "muse"), Root: t.TempDir()})
				require.NoError(t, err)
			}
			require.NoError(t, os.WriteFile(path, data, 0o600))
			if kind == "unreadable" {
				if os.Geteuid() == 0 {
					t.Skip("root bypasses permission checks")
				}
				require.NoError(t, os.Chmod(path, 0))
				t.Cleanup(func() { require.NoError(t, os.Chmod(path, 0o600)) })
			}
			project := &config.ProjectConfig{Config: config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: configBool(false)}}}}
			err := writeMuseApprovals(RealSystem{}, root, project)
			require.Error(t, err)
			require.Equal(t, 1, strings.Count(err.Error(), "invalid Agent Layer Muse policy ownership receipt"))
			require.Equal(t, 1, strings.Count(err.Error(), path))
		})
	}
}
