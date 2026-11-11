package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/musepolicy"
)

func doctorPolicyTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func museDoctorConfig(t *testing.T, enabled bool) *config.ProjectConfig {
	t.Helper()
	return &config.ProjectConfig{Root: doctorPolicyTempDir(t), Config: config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}}}}
}

func doctorMuseReceipt(t *testing.T, dir, root string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".muse"), 0o700))
	data, err := json.Marshal(musepolicy.Receipt{GeneratedBy: "agent-layer", Directory: dir, Root: root})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".muse/agent-layer-policy.json"), data, 0o600))
}

func TestCheckMusePolicyCountsOrphansAndNestedRetirement(t *testing.T) {
	base := doctorPolicyTempDir(t)
	t.Setenv("XDG_CONFIG_HOME", base)
	dir := filepath.Join(base, "muse")
	parent := doctorPolicyTempDir(t)
	nested := filepath.Join(parent, ".agent-layer/tmp/copy")
	orphan := doctorPolicyTempDir(t)
	uncertain := filepath.Join(parent, ".codex/worktrees/uncertain")
	doctorMuseReceipt(t, dir, parent)
	doctorMuseReceipt(t, dir, nested)
	doctorMuseReceipt(t, dir, uncertain)
	require.NoError(t, os.WriteFile(filepath.Join(uncertain, ".muse/agent-layer-policy.json"), []byte(`{`), 0o600))
	require.NoError(t, musepolicy.SyncCommands(dir, parent, []string{"go", "make"}))
	require.NoError(t, musepolicy.SyncCommands(dir, nested, []string{"go"}))
	require.NoError(t, musepolicy.SyncCommands(dir, orphan, []string{"python", "make"}))
	require.NoError(t, musepolicy.SyncCommands(dir, uncertain, []string{"git"}))
	require.NoError(t, os.RemoveAll(orphan))
	path := filepath.Join(dir, "approval-policy.json")
	require.NoError(t, os.Remove(filepath.Join(dir, "approval-policy.lock")))
	require.NoError(t, os.Chtimes(path, time.Unix(42, 0), time.Unix(42, 0)))
	before, err := os.ReadFile(path) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	results := CheckMusePolicy(context.Background(), museDoctorConfig(t, true))
	require.Len(t, results, 4)
	require.Equal(t, Result{Status: StatusOK, CheckName: messages.DoctorCheckNameMusePolicy,
		Message: "6 Agent Layer-owned Muse command rules across 4 workspace roots"}, results[0])
	require.Equal(t, StatusWarn, results[1].Status)
	require.Contains(t, results[1].Message, orphan+" (2 rules)")
	require.Contains(t, results[1].Recommendation, "next Muse-enabled `al sync` in any workspace")
	require.Equal(t, StatusWarn, results[2].Status)
	require.Contains(t, results[2].Message, uncertain+" (1 rules)")
	require.Contains(t, results[2].Message, "unverifiable ownership (grants kept)")
	require.Contains(t, results[2].Recommendation, "Repair or remove that workspace's")
	require.Equal(t, StatusWarn, results[3].Status)
	require.Contains(t, results[3].Message, nested+" (1 rules)")
	require.NotContains(t, results[3].Message, uncertain)
	require.Contains(t, results[3].Recommendation, "delete that copy then run `al sync`")
	require.Contains(t, results[3].Recommendation, "`enabled = false` under `[agents.muse]`")
	require.Contains(t, results[3].Recommendation, "`.agent-layer/config.toml`")
	require.Contains(t, results[3].Recommendation, "run `al sync` there")
	after, err := os.ReadFile(path) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	require.Equal(t, before, after)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, time.Unix(42, 0), info.ModTime())
	require.NoFileExists(t, filepath.Join(dir, "approval-policy.lock"))
}

func TestCheckMusePolicyNestedListIsBoundedAndSorted(t *testing.T) {
	base := doctorPolicyTempDir(t)
	t.Setenv("XDG_CONFIG_HOME", base)
	dir, parent := filepath.Join(base, "muse"), doctorPolicyTempDir(t)
	doctorMuseReceipt(t, dir, parent)
	require.NoError(t, musepolicy.SyncCommands(dir, parent, []string{"go"}))
	for i := 11; i >= 0; i-- {
		root := filepath.Join(parent, ".codex/worktrees", fmt.Sprintf("copy-%02d", i))
		doctorMuseReceipt(t, dir, root)
		require.NoError(t, musepolicy.SyncCommands(dir, root, []string{"go"}))
	}
	// A path with a common textual prefix is not nested.
	sibling := parent + "-sibling"
	require.NoError(t, os.Mkdir(sibling, 0o700))
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(sibling)) })
	doctorMuseReceipt(t, dir, sibling)
	require.NoError(t, musepolicy.SyncCommands(dir, sibling, []string{"go"}))
	results := CheckMusePolicy(context.Background(), museDoctorConfig(t, true))
	require.Len(t, results, 2)
	expected := "12 nested or possible throwaway Muse policy roots: "
	for i := 0; i < 10; i++ {
		if i > 0 {
			expected += "; "
		}
		expected += filepath.Join(parent, ".codex/worktrees", fmt.Sprintf("copy-%02d", i)) + " (1 rules)"
	}
	expected += "; ... and 2 more roots"
	require.Equal(t, expected, results[1].Message)
}

func TestCheckMusePolicyMissingInvalidAndDisabled(t *testing.T) {
	base := doctorPolicyTempDir(t)
	t.Setenv("XDG_CONFIG_HOME", base)
	results := CheckMusePolicy(context.Background(), museDoctorConfig(t, true))
	require.Len(t, results, 1)
	require.Equal(t, StatusOK, results[0].Status)
	require.Contains(t, results[0].Message, "0 Agent Layer-owned Muse command rules across 0 workspace roots")
	dir := filepath.Join(base, "muse")
	require.NoDirExists(t, dir)
	require.NoError(t, os.Mkdir(dir, 0o700))
	path := filepath.Join(dir, "approval-policy.json")
	for _, data := range []string{`{`, `{"schema_version":999,"rules":[]}`, `{"schema_version":2,"rules":[],"future":true}`} {
		require.NoError(t, os.WriteFile(path, []byte(data), 0o600))
		results = CheckMusePolicy(context.Background(), museDoctorConfig(t, true))
		require.Len(t, results, 1)
		require.Equal(t, StatusWarn, results[0].Status)
		require.Contains(t, results[0].Message, "Could not inspect Muse approval policy")
		actual, err := os.ReadFile(path) // #nosec G304 -- path is contained in this test-owned temporary fixture.
		require.NoError(t, err)
		require.Equal(t, data, string(actual))
	}
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Mkdir(path, 0o700))
	require.Equal(t, StatusWarn, CheckMusePolicy(context.Background(), museDoctorConfig(t, true))[0].Status)
	require.Empty(t, CheckMusePolicy(context.Background(), museDoctorConfig(t, false)))
	// Even resolving native storage must be skipped when Muse is disabled.
	t.Setenv("XDG_CONFIG_HOME", "relative-invalid-config")
	require.Empty(t, CheckMusePolicy(context.Background(), museDoctorConfig(t, false)))
	require.Empty(t, CheckMusePolicy(context.Background(), nil))
}

func TestCheckAgentsIncludesMusePolicyOnlyWhenEnabled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", doctorPolicyTempDir(t))
	oldLookPath, oldCommandOutput := lookPathFunc, commandOutputFunc
	t.Cleanup(func() { lookPathFunc, commandOutputFunc = oldLookPath, oldCommandOutput })
	lookPathFunc = func(name string) (string, error) {
		require.Equal(t, "muse", name)
		return "/stub/muse", nil
	}
	commandOutputFunc = func(_ context.Context, name string, args ...string) ([]byte, error) {
		require.Equal(t, "/stub/muse", name)
		require.Equal(t, []string{"--version"}, args)
		return []byte("Muse Code 1.4.15"), nil
	}
	for _, enabled := range []bool{true, false} {
		found := false
		for _, result := range CheckAgents(context.Background(), museDoctorConfig(t, enabled)) {
			if result.CheckName == messages.DoctorCheckNameMusePolicy {
				found = true
			}
		}
		require.Equal(t, enabled, found)
	}
}

func TestCheckMusePolicyCurrentReceiptNeedsRepair(t *testing.T) {
	for _, test := range []struct {
		name    string
		unknown bool
	}{
		{"missing receipt", false}, {"missing local directory", false},
		{"another root", false}, {"another native directory", false},
		{"malformed receipt", true}, {"symlink receipt", true},
		{"nonregular receipt", true}, {"symlink local directory", true},
		{"non-directory local path", true}, {"unreadable receipt", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := doctorPolicyTempDir(t)
			t.Setenv("XDG_CONFIG_HOME", base)
			dir, root := filepath.Join(base, "muse"), doctorPolicyTempDir(t)
			doctorMuseReceipt(t, dir, root)
			require.NoError(t, musepolicy.SyncCommands(dir, root, []string{"go"}))
			local := filepath.Join(root, ".muse")
			receipt := filepath.Join(local, "agent-layer-policy.json")
			switch test.name {
			case "missing receipt":
				require.NoError(t, os.Remove(receipt))
			case "missing local directory":
				require.NoError(t, os.RemoveAll(local))
			case "another root", "another native directory":
				recordedRoot, recordedDir := root, dir
				if test.name == "another root" {
					recordedRoot = doctorPolicyTempDir(t)
				} else {
					recordedDir = filepath.Join(doctorPolicyTempDir(t), "muse")
					require.NoError(t, os.Mkdir(recordedDir, 0o700))
				}
				data, err := json.Marshal(musepolicy.Receipt{GeneratedBy: "agent-layer", Directory: recordedDir, Root: recordedRoot})
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(receipt, data, 0o600))
			case "malformed receipt":
				require.NoError(t, os.WriteFile(receipt, []byte(`{`), 0o600))
			case "symlink receipt":
				target := filepath.Join(doctorPolicyTempDir(t), "receipt")
				require.NoError(t, os.Rename(receipt, target))
				require.NoError(t, os.Symlink(target, receipt))
			case "nonregular receipt":
				require.NoError(t, os.Remove(receipt))
				require.NoError(t, os.Mkdir(receipt, 0o700))
			case "symlink local directory", "non-directory local path":
				require.NoError(t, os.RemoveAll(local))
				if test.name == "symlink local directory" {
					require.NoError(t, os.Symlink(doctorPolicyTempDir(t), local))
				} else {
					require.NoError(t, os.WriteFile(local, nil, 0o600))
				}
			case "unreadable receipt":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses permission checks")
				}
				require.NoError(t, os.Chmod(receipt, 0))
				t.Cleanup(func() { require.NoError(t, os.Chmod(receipt, 0o600)) })
			}
			alias := filepath.Join(doctorPolicyTempDir(t), "project-alias")
			require.NoError(t, os.Symlink(filepath.Dir(root), alias))
			cfg := museDoctorConfig(t, true)
			cfg.Root = filepath.Join(alias, filepath.Base(root))
			path := filepath.Join(dir, "approval-policy.json")
			before, err := os.ReadFile(path) // #nosec G304 -- isolated temporary policy fixture.
			require.NoError(t, err)
			results := CheckMusePolicy(context.Background(), cfg)
			require.Len(t, results, 2)
			require.Equal(t, StatusOK, results[0].Status)
			require.Equal(t, StatusWarn, results[1].Status)
			if test.unknown {
				require.Contains(t, results[1].Message, "ownership could not be verified")
				require.Contains(t, results[1].Message, "grants are kept, but cleanup cannot be verified")
				require.NotContains(t, results[1].Message, "may remove")
				require.Contains(t, results[1].Recommendation, "`.muse` a real directory")
				require.Contains(t, results[1].Recommendation, "`.muse/agent-layer-policy.json` a regular file")
				require.Contains(t, results[1].Recommendation, "remove the bad receipt")
				require.Contains(t, results[1].Recommendation, "run `al sync` here")
			} else {
				require.Contains(t, results[1].Message, "receipt is missing or names another workspace root or native policy directory")
				require.Contains(t, results[1].Message, "other workspaces' Muse-enabled syncs may remove its grants")
				require.Contains(t, results[1].Recommendation, "Run `al sync` here to rewrite")
			}
			require.NotContains(t, results[1].Message, "orphaned")
			after, err := os.ReadFile(path) // #nosec G304 -- isolated temporary policy fixture.
			require.NoError(t, err)
			require.Equal(t, before, after)
			if test.name == "missing receipt" {
				require.NoFileExists(t, receipt)
			}
		})
	}
}

func TestCheckMusePolicyDisposableSegmentsWithoutOwnedParents(t *testing.T) {
	base, parent := doctorPolicyTempDir(t), doctorPolicyTempDir(t)
	t.Setenv("XDG_CONFIG_HOME", base)
	dir := filepath.Join(base, "muse")
	copies := []string{filepath.Join(parent, ".agent-layer/tmp/copy"), filepath.Join(parent, ".codex/worktrees/copy")}
	for _, root := range copies {
		doctorMuseReceipt(t, dir, root)
		require.NoError(t, musepolicy.SyncCommands(dir, root, []string{"go"}))
	}
	// A real nested project is listed for visibility, with advice to keep it.
	monorepo := doctorPolicyTempDir(t)
	service := filepath.Join(monorepo, "services/api")
	for _, root := range []string{monorepo, service} {
		doctorMuseReceipt(t, dir, root)
		require.NoError(t, musepolicy.SyncCommands(dir, root, []string{"make"}))
	}
	results := CheckMusePolicy(context.Background(), museDoctorConfig(t, true))
	require.Len(t, results, 2)
	require.Contains(t, results[1].Message, "3 nested or possible throwaway Muse policy roots")
	for _, root := range append(copies, service) {
		require.Contains(t, results[1].Message, root)
	}
	require.Contains(t, results[1].Recommendation, "not removed automatically")
	require.Contains(t, results[1].Recommendation, "leave real nested projects alone")
	require.Contains(t, results[1].Recommendation, "If a listed copy is disposable")
}

func TestCheckMusePolicyCancelled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", doctorPolicyTempDir(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Empty(t, CheckMusePolicy(ctx, museDoctorConfig(t, true)))
}

func TestCheckMusePolicyWarnsOnSymlinkedNativeDirectory(t *testing.T) {
	base, target := doctorPolicyTempDir(t), doctorPolicyTempDir(t)
	t.Setenv("XDG_CONFIG_HOME", base)
	require.NoError(t, os.Symlink(target, filepath.Join(base, "muse")))
	require.NoError(t, os.WriteFile(filepath.Join(target, "approval-policy.json"), []byte(`{"schema_version":2,"rules":[]}`), 0o600))
	results := CheckMusePolicy(context.Background(), museDoctorConfig(t, true))
	require.Len(t, results, 1)
	require.Equal(t, StatusWarn, results[0].Status)
	require.Contains(t, results[0].Message, "real directory")
}

func TestCheckMusePolicyUnknownRootsAreBoundedAndSorted(t *testing.T) {
	base, parent := doctorPolicyTempDir(t), doctorPolicyTempDir(t)
	t.Setenv("XDG_CONFIG_HOME", base)
	dir := filepath.Join(base, "muse")
	for i := 11; i >= 0; i-- {
		root := filepath.Join(parent, fmt.Sprintf("copy-%02d", i))
		doctorMuseReceipt(t, dir, root)
		require.NoError(t, musepolicy.SyncCommands(dir, root, []string{"make"}))
		require.NoError(t, os.WriteFile(filepath.Join(root, ".muse/agent-layer-policy.json"), []byte(`{`), 0o600))
	}
	results := CheckMusePolicy(context.Background(), museDoctorConfig(t, true))
	require.Len(t, results, 2)
	require.Equal(t, StatusWarn, results[1].Status)
	expected := "12 Muse policy roots with unverifiable ownership (grants kept): "
	for i := 0; i < 10; i++ {
		if i > 0 {
			expected += "; "
		}
		expected += filepath.Join(parent, fmt.Sprintf("copy-%02d", i)) + " (1 rules)"
	}
	expected += "; ... and 2 more roots"
	require.Equal(t, expected, results[1].Message)
	require.Contains(t, results[1].Recommendation, "Grants are kept because ownership cannot be verified")
	require.Contains(t, results[1].Recommendation, "unreadable, malformed or unclean")
	require.Contains(t, results[1].Recommendation, "`.muse/agent-layer-policy.json`")
	require.Contains(t, results[1].Recommendation, "run `al sync` there")
	require.Contains(t, results[1].Recommendation, "delete the directory if it is disposable")
}
