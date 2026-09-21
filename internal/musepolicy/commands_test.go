package musepolicy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/stretchr/testify/require"
)

func TestCommandPolicyPreservesNativeAndOtherWorkspaces(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	other := t.TempDir()
	path := filepath.Join(dir, "approval-policy.json")
	native := `{"schema_version":2,"rules":[{"effect":"deny","durability":"local_persistent","reason":null,"rule":{"kind":"tool_action","tool_name":"mcp__private__write"}}]}`
	require.NoError(t, os.WriteFile(path, []byte(native), 0o600))
	require.NoError(t, SyncCommands(dir, root, []string{`git status`, `printf 'quoted argument'`}))
	require.NoError(t, SyncCommands(dir, other, []string{"git log"}))
	require.NoError(t, SyncCommands(dir, root, []string{"git diff"}))
	var doc struct {
		Rules []map[string]any `json:"rules"`
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Len(t, doc.Rules, 3)
	require.Equal(t, "deny", doc.Rules[0]["effect"])
	require.Contains(t, string(data), "git")
	require.NotContains(t, string(data), "status")
	require.NoError(t, SyncCommands(dir, root, nil))
	data, err = os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Len(t, doc.Rules, 2)
	require.Equal(t, "deny", doc.Rules[0]["effect"])
	require.NoError(t, SyncCommands(dir, other, nil))
	data, err = os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Len(t, doc.Rules, 1)
}

func TestCommandPolicyRejectsInvalidStateWithoutOverwrite(t *testing.T) {
	for _, content := range []string{``, `{}`, `null`, `{`, `{"schema_version":1,"rules":[]}`, `{"schema_version":2,"rules":[]} {}`, `{"schema_version":2,"unknown":1,"rules":[]}`} {
		t.Run(content, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "approval-policy.json")
			require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
			require.Error(t, SyncCommands(dir, t.TempDir(), []string{"git status"}))
			data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
			require.NoError(t, err)
			require.Equal(t, content, string(data))
		})
	}
	dir := t.TempDir()
	dest := filepath.Join(t.TempDir(), "policy")
	require.NoError(t, os.WriteFile(dest, []byte("original"), 0o600))
	require.NoError(t, os.Symlink(dest, filepath.Join(dir, "approval-policy.json")))
	require.ErrorContains(t, SyncCommands(dir, t.TempDir(), []string{"git"}), "regular file")
	data, err := os.ReadFile(dest) // #nosec G304 -- test-controlled temporary path.
	require.NoError(t, err)
	require.Equal(t, "original", string(data))
	require.Error(t, SyncCommands(t.TempDir(), t.TempDir(), []string{`'unterminated`}))
}

func TestConcurrentCommandPolicyUpdates(t *testing.T) {
	dir := t.TempDir()
	roots := []string{t.TempDir(), t.TempDir()}
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, root := range roots {
		wg.Go(func() { errs <- SyncCommands(dir, root, []string{"git status"}) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "approval-policy.json")) // #nosec G304 -- test-controlled temporary path.
	require.NoError(t, err)
	var doc struct {
		Rules []json.RawMessage `json:"rules"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Len(t, doc.Rules, 2)
}

func TestConfigDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	dir, err := ConfigDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(os.Getenv("HOME"), ".config", "muse"), dir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir, err = ConfigDir()
	require.NoError(t, err)
	require.Equal(t, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "muse"), dir)
	t.Setenv("XDG_CONFIG_HOME", "relative")
	_, err = ConfigDir()
	require.Error(t, err)
}

func TestCommandPolicyRejectsShellPrograms(t *testing.T) {
	for _, command := range []string{"cd frontend && npm test", "git status; echo done", "git log|cat", "git status>file", "echo $(date)", "FOO=1 git status"} {
		require.Error(t, SyncCommands(t.TempDir(), t.TempDir(), []string{command}), command)
	}
}

func TestCommandCleanupIgnoresUnownedNativeSchema(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	path := filepath.Join(dir, "approval-policy.json")
	data := []byte(`{"schema_version":999,"future":true,"rules":[{"reason":"user"}]}`)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	// An unusable lock proves cleanup does not acquire it for unrelated state.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "approval-policy.lock"), 0o700))
	require.NoError(t, SyncCommands(dir, root, nil))
	actual, err := os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	require.NoError(t, err)
	require.Equal(t, data, actual)
}

func TestCommandPolicyMergesLatestStateAfterConcurrentNativeWriter(t *testing.T) {
	dir, root := t.TempDir(), t.TempDir()
	path := filepath.Join(dir, "approval-policy.json")
	lock, err := os.OpenFile(filepath.Join(dir, "approval-policy.lock"), os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- lock path belongs to this isolated temporary fixture.
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Close()) })
	require.NoError(t, unix.Flock(int(lock.Fd()), unix.LOCK_EX))
	unlocked := false
	t.Cleanup(func() {
		if !unlocked {
			_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		}
	})
	done := make(chan error, 1)
	go func() { done <- SyncCommands(dir, root, []string{"git status"}) }()
	select {
	case err := <-done:
		t.Fatalf("policy update ignored native writer's lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	// Simulate the native writer publishing an explicit denial while holding its
	// policy lock. The waiting sync must merge this latest version after unlock.
	require.NoError(t, os.WriteFile(path, []byte(`{"schema_version":2,"rules":[{"effect":"deny","reason":"concurrent user","durability":"local_persistent","rule":{"kind":"shell_command_argv_prefix","argv_prefix":["git"]}}]}`), 0o600))
	require.NoError(t, unix.Flock(int(lock.Fd()), unix.LOCK_UN))
	unlocked = true
	require.NoError(t, <-done)
	data, err := os.ReadFile(path) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	require.Contains(t, string(data), "concurrent user")
	require.Contains(t, string(data), "agent-layer:")
	require.NoError(t, SyncCommands(dir, root, nil))
	data, err = os.ReadFile(path) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	require.Contains(t, string(data), "concurrent user")
	require.NotContains(t, string(data), "agent-layer:")
}

func TestCommandPolicyRetiresMovedWorkspaceOnly(t *testing.T) {
	dir, root := t.TempDir(), t.TempDir()
	require.NoError(t, SyncCommands(dir, root, []string{"git status"}))
	canonical, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	moved := filepath.Join(t.TempDir(), "moved")
	require.NoError(t, os.Rename(root, moved))
	require.NoError(t, SyncCommands(dir, canonical, nil))
	data, err := os.ReadFile(filepath.Join(dir, "approval-policy.json")) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	require.NotContains(t, string(data), "agent-layer:")
}
