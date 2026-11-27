package musepolicy

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func isolateSessionMCPCacheDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	return home
}

func TestMCPHookAllowsSessionMCPServersAcrossApprovalsModes(t *testing.T) {
	isolateSessionMCPCacheDir(t)
	root := t.TempDir()
	sessionA := "11111111-1111-4111-8111-111111111111"
	sessionB := "22222222-2222-4222-8222-222222222222"
	otherSession := "33333333-3333-4333-8333-333333333333"

	require.NoError(t, WriteSessionMCPGrant(root, sessionA, []string{"workflow-a"}))
	require.NoError(t, WriteSessionMCPGrant(root, sessionB, []string{"workflow-b"}))

	for _, mode := range []string{"none", "commands", "mcp", "all", "yolo"} {
		hookConfig(t, root, mode, true, "muse")
		projectAllowed := mode == "mcp" || mode == "all" || mode == "yolo"

		cases := []struct {
			name      string
			sessionID string
			tool      string
			wantAllow bool
		}{
			{"sessionA own tool", sessionA, "mcp__workflow_a__submit", true},
			{"sessionA cannot use sessionB tool", sessionA, "mcp__workflow_b__submit", false},
			{"sessionB own tool", sessionB, "mcp__workflow_b__check", true},
			{"other session denied sessionA tool", otherSession, "mcp__workflow_a__submit", false},
			{"interactive (empty session_id) denied sessionA tool", "", "mcp__workflow_a__submit", false},
			{"sessionA does not escalate project server", sessionA, "mcp__selected__write", projectAllowed},
			{"sessionA rejects malformed double underscore tool", sessionA, "mcp__workflow_a__nested__tool", false},
			{"sessionA rejects leading underscore tool", sessionA, "mcp__workflow_a___hidden", false},
		}
		for _, tc := range cases {
			payload := map[string]string{
				"hook_event_name": "PermissionRequest",
				"cwd":             root,
				"tool_name":       tc.tool,
			}
			if tc.sessionID != "" {
				payload["session_id"] = tc.sessionID
			}
			data, err := json.Marshal(payload)
			require.NoError(t, err)
			var out bytes.Buffer
			require.NoError(t, HandleMCP(root, bytes.NewReader(data), &out))
			require.Equal(t, tc.wantAllow, strings.Contains(out.String(), `"allow"`), "mode=%s case=%s out=%s", mode, tc.name, out.String())
		}
	}

	// Disabled Muse denies session MCP servers too.
	hookConfig(t, root, "all", false, "muse")
	data, err := json.Marshal(map[string]string{
		"hook_event_name": "PermissionRequest",
		"cwd":             root,
		"tool_name":       "mcp__workflow_a__submit",
		"session_id":      sessionA,
	})
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, HandleMCP(root, bytes.NewReader(data), &out))
	require.NotContains(t, out.String(), `"allow"`)
}

func TestSessionMCPGrantPruningAndCleanup(t *testing.T) {
	home := isolateSessionMCPCacheDir(t)
	root := t.TempDir()
	keepID := "11111111-1111-4111-8111-111111111111"
	staleID := "22222222-2222-4222-8222-222222222222"

	require.NoError(t, WriteSessionMCPGrant(root, keepID, []string{"wf"}))
	require.NoError(t, WriteSessionMCPGrant(root, staleID, []string{"wf"}))

	// Confirm grants are stored inside the isolated test home rather than the user's real cache dir.
	resolvedHome, err := resolvePathThroughExistingAncestors(home)
	require.NoError(t, err)
	initialKeepPath, err := sessionMCPGrantPath(root, keepID)
	require.NoError(t, err)
	require.True(t, grantPathWithinRoot(initialKeepPath, resolvedHome), "expected grant path %q inside isolated home %q", initialKeepPath, resolvedHome)

	// A freshly written grant after notAfter is preserved against concurrent start races.
	require.NoError(t, PruneSessionMCPGrants(root, map[string]bool{keepID: true}, time.Now().Add(-time.Minute), nil))
	stalePath, err := sessionMCPGrantPath(root, staleID)
	require.NoError(t, err)
	_, err = os.Stat(stalePath)
	require.NoError(t, err, "recently written grant should not be pruned before cutoff")

	// Once older than notAfter (and not active on recheck), stale grant is pruned.
	require.NoError(t, PruneSessionMCPGrants(root, map[string]bool{keepID: true}, time.Now().Add(time.Minute), nil))
	keepPath, err := sessionMCPGrantPath(root, keepID)
	require.NoError(t, err)

	_, err = os.Stat(keepPath)
	require.NoError(t, err, "active session grant should be retained")
	_, err = os.Stat(stalePath)
	require.True(t, os.IsNotExist(err), "stale session grant should be pruned")

	require.NoError(t, RemoveSessionMCPGrant(root, keepID))
	_, err = os.Stat(keepPath)
	require.True(t, os.IsNotExist(err), "removed session grant should be gone")

	// Reject mismatched session_id inside a tampered grant file.
	require.NoError(t, os.MkdirAll(filepath.Dir(keepPath), 0o700))
	require.NoError(t, os.WriteFile(keepPath, []byte(`{"session_id":"`+staleID+`","server_ids":["wf"]}`), 0o600))
	_, _, err = readSessionMCPGrant(root, keepID)
	require.Error(t, err)
}
