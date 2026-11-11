package musepolicy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCommandPolicyOrphanOnlyWriteFailureIsBestEffort(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	for _, test := range []struct {
		name      string
		commands  []string
		extra     string
		writable  bool
		wantError bool
	}{
		{name: "unchanged read-only", commands: []string{"git status"}},
		{name: "unchanged writable", commands: []string{"git status"}, writable: true},
		{name: "add command", commands: []string{"git status", "go"}, wantError: true},
		{name: "revoke nonempty", commands: []string{"git status"}, extra: "revoke", wantError: true},
		{name: "revoke empty", wantError: true},
		{name: "duplicate", commands: []string{"git status"}, extra: "duplicate", wantError: true},
		{name: "malformed", commands: []string{"git status"}, extra: "malformed", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			base, root, orphan := policyTempDir(t), policyTempDir(t), policyTempDir(t)
			t.Setenv("XDG_CONFIG_HOME", base)
			dir := filepath.Join(base, "muse")
			receiptFixture(t, dir, root)
			require.NoError(t, SyncCommands(dir, orphan, []string{"git diff"}))
			initial := []string{"git status"}
			if test.extra == "revoke" {
				initial = []string{"go", "git status"}
			}
			require.NoError(t, SyncCommands(dir, root, initial))
			rules := readRules(t, dir)
			switch test.extra {
			case "duplicate":
				rules = append(rules, rules[1])
			case "malformed":
				var rule map[string]any
				require.NoError(t, json.Unmarshal(rules[1], &rule))
				rule["rule"].(map[string]any)["argv_prefix"] = []any{nil}
				raw, err := json.Marshal(rule)
				require.NoError(t, err)
				rules = append([]json.RawMessage{rules[0], raw}, rules[1:]...)
			}
			// Match the base writer's formatting: orphan first, valid own rule last.
			data, err := json.MarshalIndent(policyDocument{Version: 2, Rules: rules}, "", "  ")
			require.NoError(t, err)
			path := filepath.Join(dir, "approval-policy.json")
			require.NoError(t, os.WriteFile(path, append(data, '\n'), 0o600))
			rules = readRules(t, dir)
			require.Equal(t, orphan, decodeCommandRule(rules[0]).Rule.Root)
			require.Equal(t, root, decodeCommandRule(rules[len(rules)-1]).Rule.Root)
			require.Equal(t, []string{"git", "status"}, decodeCommandRule(rules[len(rules)-1]).argv)
			require.Equal(t, ownershipValid, ownershipState(dir, root))
			require.True(t, IsOrphan(dir, orphan))
			require.NoError(t, os.Chtimes(path, time.Unix(42, 0), time.Unix(42, 0)))
			before, err := os.ReadFile(path) // #nosec G304 -- isolated temporary native policy.
			require.NoError(t, err)
			info, err := os.Stat(path)
			require.NoError(t, err)
			if !test.writable {
				dirInfo, err := os.Stat(dir)
				require.NoError(t, err)
				require.NoError(t, os.Chmod(dir, 0o555)) // #nosec G302 -- read-only test directory retains traversal permission.
				t.Cleanup(func() { require.NoError(t, os.Chmod(dir, dirInfo.Mode().Perm())) })
				for _, file := range []string{path, filepath.Join(dir, "approval-policy.lock")} {
					fileInfo, err := os.Stat(file)
					require.NoError(t, err)
					require.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm())
					opened, err := os.OpenFile(file, os.O_RDWR, 0o600) // #nosec G304 -- existing files in this temporary native fixture.
					require.NoError(t, err)
					require.NoError(t, opened.Close())
				}
				probe, err := os.CreateTemp(dir, "permission-probe-*")
				if err == nil {
					require.NoError(t, probe.Close())
					require.NoError(t, os.Remove(probe.Name()))
					t.Fatal("read-only native directory allowed creating a temporary file")
				}
				require.ErrorIs(t, err, os.ErrPermission)
			}
			err = SyncEnabledCommands(dir, root, test.commands)
			if test.wantError {
				require.ErrorIs(t, err, os.ErrPermission)
				require.ErrorContains(t, err, "approval-policy.json.tmp-")
			} else {
				require.NoError(t, err)
			}
			if test.writable {
				require.Equal(t, []json.RawMessage{rules[1]}, readRules(t, dir))
			} else {
				after, err := os.ReadFile(path) // #nosec G304 -- isolated temporary native policy.
				require.NoError(t, err)
				current, err := os.Stat(path)
				require.NoError(t, err)
				require.Equal(t, before, after)
				require.Equal(t, info.ModTime(), current.ModTime())
				require.True(t, os.SameFile(info, current))
			}
		})
	}
}
