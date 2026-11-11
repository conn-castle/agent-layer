package musepolicy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Match the canonical roots SyncCommands records, including macOS temp paths.
func policyTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func readRules(t *testing.T, dir string) []json.RawMessage {
	t.Helper()
	doc, err := readDocument(filepath.Join(dir, "approval-policy.json"))
	require.NoError(t, err)
	return doc.Rules
}

func receiptFixture(t *testing.T, dir, root string) {
	t.Helper()
	data, err := json.Marshal(Receipt{GeneratedBy: "agent-layer", Directory: dir, Root: root})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".muse"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".muse/agent-layer-policy.json"), data, 0o600))
}

func TestCommandPolicyUnchangedAfterOtherWorkspaceSync(t *testing.T) {
	dir := filepath.Join(policyTempDir(t), "muse")
	root, other := policyTempDir(t), policyTempDir(t)
	receiptFixture(t, dir, root)
	receiptFixture(t, dir, other)
	require.NoError(t, SyncEnabledCommands(dir, root, []string{"git status", "git log"}))
	require.NoError(t, SyncEnabledCommands(dir, other, []string{"make"}))
	path := filepath.Join(dir, "approval-policy.json")
	// A known old mtime detects even an immediate unnecessary atomic rewrite.
	require.NoError(t, os.Chtimes(path, time.Unix(42, 0), time.Unix(42, 0)))
	before, err := os.ReadFile(path) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, SyncEnabledCommands(dir, root, []string{"git log", "git status"}))
	after, err := os.ReadFile(path) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	current, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, info.ModTime(), current.ModTime())
	require.True(t, os.SameFile(info, current))
}

func TestCommandPolicyPreservesRawRulesAndAppendsOnlyNew(t *testing.T) {
	dir, root, other := policyTempDir(t), policyTempDir(t), policyTempDir(t)
	require.NoError(t, SyncCommands(dir, root, []string{"git status", "go"}))
	require.NoError(t, SyncCommands(dir, other, []string{"make"}))
	rules := readRules(t, dir)
	// Native formatting and future Muse metadata must survive a changed merge.
	rules[0] = append([]byte(`{ "muse_metadata" : {"future": true}, `), rules[0][1:]...)
	rules = append([]json.RawMessage{json.RawMessage(`{ "reason" : "user", "future" : [1, 2] }`)}, rules...)
	path := filepath.Join(dir, "approval-policy.json")
	data := []byte(`{"schema_version":2,"rules":[`)
	for i, raw := range rules {
		if i > 0 {
			data = append(data, ',')
		}
		data = append(data, raw...)
	}
	data = append(data, ']', '}')
	require.NoError(t, os.WriteFile(path, data, 0o600))
	require.NoError(t, SyncCommands(dir, root, []string{"git status", "python", "python", "git 'status'"}))
	actual := readRules(t, dir)
	require.Len(t, actual, 4)
	require.Equal(t, rules[0], actual[0]) // Native user rule.
	require.Equal(t, rules[1], actual[1]) // Kept at its index, extra field intact.
	require.Equal(t, rules[3], actual[2]) // Other root keeps its bytes and order.
	appended, owned := ownedCommandRule(actual[3])
	require.True(t, owned)
	require.Equal(t, []string{"python"}, appended.Rule.Argv)
}

func TestCommandPolicyCompactNativeFormattingHasNoChurn(t *testing.T) {
	dir, root := policyTempDir(t), policyTempDir(t)
	require.NoError(t, SyncCommands(dir, root, []string{"git status", "git 'status'"}))
	rules := readRules(t, dir)
	require.Len(t, rules, 1)
	var rule map[string]any
	require.NoError(t, json.Unmarshal(rules[0], &rule))
	rule["muse_added"] = "keep me"
	data, err := json.Marshal(map[string]any{"schema_version": 2, "rules": []any{rule}})
	require.NoError(t, err)
	path := filepath.Join(dir, "approval-policy.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	require.NoError(t, os.Chtimes(path, time.Unix(42, 0), time.Unix(42, 0)))
	require.NoError(t, SyncCommands(dir, root, []string{"git status", "git status"}))
	actual, err := os.ReadFile(path) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	require.Equal(t, data, actual)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, time.Unix(42, 0), info.ModTime())
}

func TestEnabledCommandPolicyPrunesOnlyProvableOrphans(t *testing.T) {
	for _, test := range []struct {
		name  string
		prune bool
	}{
		{"deleted root", true}, {"missing muse directory", true}, {"missing receipt", true},
		{"another native directory", true}, {"missing native directory", true}, {"another root", true}, {"valid receipt", false},
		{"native directory stat denied", false}, {"native directory stat error", false},
		{"malformed receipt", false}, {"invalid marker", false}, {"relative directory", false},
		{"wrong directory name", false}, {"relative receipt root", false},
		{"symlink muse directory", false}, {"symlink receipt", false},
		{"nonregular receipt", false}, {"non-directory muse", false}, {"symlink root", false}, {"non-directory root", false},
		{"permission denied", false}, {"receipt read denied", false}, {"unclean receipt paths", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, root, current := filepath.Join(policyTempDir(t), "muse"), policyTempDir(t), policyTempDir(t)
			receiptFixture(t, dir, root)
			receiptFixture(t, dir, current)
			require.NoError(t, SyncCommands(dir, root, []string{"make", "go"}))
			local := filepath.Join(root, ".muse")
			receiptPath := filepath.Join(local, "agent-layer-policy.json")
			switch test.name {
			case "deleted root":
				require.NoError(t, os.RemoveAll(root))
			case "missing muse directory":
				require.NoError(t, os.RemoveAll(local))
			case "missing receipt":
				require.NoError(t, os.Remove(receiptPath))
			case "another native directory", "missing native directory":
				otherDir := filepath.Join(policyTempDir(t), "muse")
				if test.name == "another native directory" {
					require.NoError(t, os.Mkdir(otherDir, 0o700))
				}
				receiptFixture(t, otherDir, root)
			case "native directory stat denied":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses permission checks")
				}
				parent := policyTempDir(t)
				receiptFixture(t, filepath.Join(parent, "muse"), root)
				require.NoError(t, os.Chmod(parent, 0))
				t.Cleanup(func() { require.NoError(t, os.Chmod(parent, 0o700)) }) // #nosec G302 -- restore traversal of a test-owned directory.
			case "native directory stat error":
				parent := filepath.Join(policyTempDir(t), "file")
				require.NoError(t, os.WriteFile(parent, nil, 0o600))
				receiptFixture(t, filepath.Join(parent, "muse"), root)
			case "another root":
				data, err := json.Marshal(Receipt{GeneratedBy: "agent-layer", Directory: dir, Root: current})
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(receiptPath, data, 0o600))
			case "malformed receipt", "invalid marker", "relative directory", "wrong directory name", "relative receipt root":
				data := []byte(`{"generated_by":"agent-layer",`)
				if test.name != "malformed receipt" {
					receipt := Receipt{GeneratedBy: "agent-layer", Directory: dir, Root: root}
					switch test.name {
					case "invalid marker":
						receipt.GeneratedBy = "user"
					case "relative directory":
						receipt.Directory = "muse"
					case "wrong directory name":
						receipt.Directory = filepath.Dir(dir)
					case "relative receipt root":
						receipt.Root = "relative"
					}
					var err error
					data, err = json.Marshal(receipt)
					require.NoError(t, err)
				}
				require.NoError(t, os.WriteFile(receiptPath, data, 0o600))
			case "symlink muse directory":
				require.NoError(t, os.RemoveAll(local))
				require.NoError(t, os.Symlink(policyTempDir(t), local))
			case "symlink receipt":
				require.NoError(t, os.Remove(receiptPath))
				// Even a dangling symlink is uncertain, rather than a missing receipt.
				require.NoError(t, os.Symlink(filepath.Join(policyTempDir(t), "missing"), receiptPath))
			case "nonregular receipt":
				require.NoError(t, os.Remove(receiptPath))
				require.NoError(t, os.Mkdir(receiptPath, 0o700))
			case "non-directory muse":
				require.NoError(t, os.RemoveAll(local))
				require.NoError(t, os.WriteFile(local, nil, 0o600))
			case "symlink root":
				require.NoError(t, os.RemoveAll(root))
				require.NoError(t, os.Symlink(policyTempDir(t), root))
			case "non-directory root":
				require.NoError(t, os.RemoveAll(root))
				require.NoError(t, os.WriteFile(root, nil, 0o600))
			case "permission denied", "receipt read denied":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses permission checks")
				}
				denied := root
				if test.name == "receipt read denied" {
					denied = receiptPath
				}
				info, err := os.Stat(denied)
				require.NoError(t, err)
				require.NoError(t, os.Chmod(denied, 0))
				t.Cleanup(func() { require.NoError(t, os.Chmod(denied, info.Mode().Perm())) })
			case "unclean receipt paths":
				data, err := json.Marshal(Receipt{GeneratedBy: "agent-layer", Directory: dir + "/../muse", Root: root + "/."})
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(receiptPath, data, 0o600))
			}
			require.Equal(t, test.prune, IsOrphan(dir, root))
			inspection, err := Inspect(context.Background(), dir, "")
			require.NoError(t, err)
			require.Equal(t, 2, inspection.Total)
			if test.prune {
				require.Equal(t, []RootRules{{Root: root, Count: 2}}, inspection.Orphans)
			} else {
				require.Empty(t, inspection.Orphans)
				if test.name != "valid receipt" {
					require.Equal(t, []RootRules{{Root: root, Count: 2}}, inspection.Unknown)
				}
			}
			// The enabled empty-list path must still lock and prune other roots.
			require.NoError(t, SyncEnabledCommands(dir, current, nil))
			if test.prune {
				require.Empty(t, readRules(t, dir))
			} else {
				require.Len(t, readRules(t, dir), 2)
			}
		})
	}
}

func TestCommandPolicyPruningPreservesUnexpectedAndUnownedRules(t *testing.T) {
	dir, root := policyTempDir(t), policyTempDir(t)
	require.NoError(t, SyncCommands(dir, root, []string{"go"}))
	original := readRules(t, dir)[0]
	var rules []json.RawMessage
	for _, test := range []struct {
		key   string
		value any
	}{
		{"reason", "user"}, {"reason", "agent-layer:" + policyTempDir(t)}, {"effect", "deny"},
		{"durability", "session"}, {"kind", "tool_action"}, {"workspace_root", "relative"},
		{"workspace_root", root + "/."}, {"argv_prefix", map[string]any{"future": true}},
		{"argv_prefix", []any{"go", nil}}, {"argv_prefix", nil}, {"argv_prefix", []any{}},
	} {
		var entry map[string]any
		require.NoError(t, json.Unmarshal(original, &entry))
		if test.key == "kind" || test.key == "workspace_root" || test.key == "argv_prefix" {
			entry["rule"].(map[string]any)[test.key] = test.value
		} else {
			entry[test.key] = test.value
		}
		raw, err := json.Marshal(entry)
		require.NoError(t, err)
		rules = append(rules, raw)
	}
	var unclean map[string]any
	require.NoError(t, json.Unmarshal(original, &unclean))
	unclean["reason"] = "agent-layer:" + root + "/."
	unclean["rule"].(map[string]any)["workspace_root"] = root + "/."
	raw, err := json.Marshal(unclean)
	require.NoError(t, err)
	rules = append(rules, raw)
	rules = append(rules, json.RawMessage(`{"reason":123,"rule":"future"}`))
	data, err := json.Marshal(policyDocument{Version: 2, Rules: rules})
	require.NoError(t, err)
	path := filepath.Join(dir, "approval-policy.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	require.NoError(t, os.RemoveAll(root))
	inspection, err := Inspect(context.Background(), dir, "")
	require.NoError(t, err)
	require.Zero(t, inspection.Total)
	require.Empty(t, inspection.Roots)
	require.NoError(t, SyncEnabledCommands(dir, policyTempDir(t), nil))
	actual, err := os.ReadFile(path) // #nosec G304 -- path is contained in this test-owned temporary fixture.
	require.NoError(t, err)
	require.Equal(t, data, actual)
}

func TestCommandRetirementDoesNotPruneOtherRoots(t *testing.T) {
	dir, root, other := policyTempDir(t), policyTempDir(t), policyTempDir(t)
	require.NoError(t, SyncCommands(dir, root, []string{"go"}))
	require.NoError(t, SyncCommands(dir, other, []string{"make"}))
	require.NoError(t, os.RemoveAll(other))
	require.NoError(t, SyncCommands(dir, root, nil))
	rules := readRules(t, dir)
	require.Len(t, rules, 1)
	rule, owned := ownedCommandRule(rules[0])
	require.True(t, owned)
	require.Equal(t, other, rule.Rule.Root)
}

func TestConcurrentEnabledCommandsKeepFreshReceipts(t *testing.T) {
	dir := filepath.Join(policyTempDir(t), "muse")
	roots := []string{policyTempDir(t), policyTempDir(t), policyTempDir(t), policyTempDir(t)}
	var wg sync.WaitGroup
	errs := make(chan error, len(roots))
	for _, root := range roots {
		receiptFixture(t, dir, root)
		wg.Go(func() { errs <- SyncEnabledCommands(dir, root, []string{"git status"}) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Len(t, readRules(t, dir), len(roots))
}

func TestEnabledCommandPolicyKeepsNativeDirectoryAliases(t *testing.T) {
	for _, receiptUsesAlias := range []bool{true, false} {
		name := "sync uses alias"
		if receiptUsesAlias {
			name = "receipt uses alias"
		}
		t.Run(name, func(t *testing.T) {
			base := policyTempDir(t)
			alias := filepath.Join(policyTempDir(t), "config-alias")
			require.NoError(t, os.Symlink(base, alias))
			realDir, aliasDir := filepath.Join(base, "muse"), filepath.Join(alias, "muse")
			recorded, selected := realDir, aliasDir
			if receiptUsesAlias {
				recorded, selected = aliasDir, realDir
			}
			root, current := policyTempDir(t), policyTempDir(t)
			receiptFixture(t, recorded, root)
			receiptFixture(t, selected, current)
			require.NoError(t, SyncCommands(realDir, root, []string{"go", "make"}))
			before := readRules(t, realDir)
			require.False(t, IsOrphan(selected, root))
			inspection, err := Inspect(context.Background(), selected, "")
			require.NoError(t, err)
			require.Equal(t, 2, inspection.Total)
			require.Empty(t, inspection.Orphans)
			require.NoError(t, SyncEnabledCommands(selected, current, nil))
			require.Equal(t, before, readRules(t, realDir))
		})
	}
}

func TestEnabledCommandPolicyEmptyMissingStoreCreatesNothing(t *testing.T) {
	for _, existingDir := range []bool{true, false} {
		name := "missing directory"
		if existingDir {
			name = "existing directory"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(policyTempDir(t), "muse")
			if existingDir {
				require.NoError(t, os.Mkdir(dir, 0o700))
			}
			require.NoError(t, SyncEnabledCommands(dir, policyTempDir(t), nil))
			require.NoFileExists(t, filepath.Join(dir, "approval-policy.json"))
			require.NoFileExists(t, filepath.Join(dir, "approval-policy.lock"))
			if !existingDir {
				require.NoDirExists(t, dir)
			}
		})
	}
}

func TestEnabledCommandPolicyKeepsWorkspaceRootAliases(t *testing.T) {
	for _, ruleUsesAlias := range []bool{true, false} {
		name := "receipt uses alias"
		if ruleUsesAlias {
			name = "rule uses alias"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(policyTempDir(t), "muse")
			parent := policyTempDir(t)
			realRoot := filepath.Join(parent, "repo")
			require.NoError(t, os.Mkdir(realRoot, 0o700))
			alias := filepath.Join(policyTempDir(t), "parent-alias")
			require.NoError(t, os.Symlink(parent, alias))
			aliasRoot := filepath.Join(alias, "repo")
			ruleRoot, receiptRoot := realRoot, aliasRoot
			if ruleUsesAlias {
				ruleRoot, receiptRoot = aliasRoot, realRoot
			}
			receiptFixture(t, dir, receiptRoot)
			require.NoError(t, SyncCommands(dir, realRoot, []string{"make"}))
			// Sync canonicalizes symlink spellings. Model a saved rule from
			// before the parent alias was introduced, without merging roots.
			rules := readRules(t, dir)
			var rule commandRule
			require.NoError(t, json.Unmarshal(rules[0], &rule))
			rule.Reason = "agent-layer:" + ruleRoot
			rule.Rule.Root = ruleRoot
			raw, err := json.Marshal(rule)
			require.NoError(t, err)
			data, err := json.Marshal(policyDocument{Version: 2, Rules: []json.RawMessage{raw}})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "approval-policy.json"), data, 0o600))
			require.False(t, IsOrphan(dir, ruleRoot))
			inspection, err := Inspect(context.Background(), dir, "")
			require.NoError(t, err)
			require.Empty(t, inspection.Orphans)
			current := policyTempDir(t)
			receiptFixture(t, dir, current)
			require.NoError(t, SyncEnabledCommands(dir, current, []string{"go"}))
			require.Equal(t, json.RawMessage(raw), readRules(t, dir)[0])
			require.Len(t, readRules(t, dir), 2)
		})
	}
}

func TestMusePolicyInspectionCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inspection, err := Inspect(ctx, policyTempDir(t), "")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, Inspection{}, inspection)
}

// Cancel during root inspection without timing or filesystem race dependencies.
type cancelInspectionContext struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (ctx *cancelInspectionContext) Err() error {
	ctx.checks++
	if ctx.checks == 8 {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func TestMusePolicyInspectionDropsPartialResultsOnCancellation(t *testing.T) {
	dir := filepath.Join(policyTempDir(t), "muse")
	for range 3 {
		root := policyTempDir(t)
		receiptFixture(t, dir, root)
		require.NoError(t, SyncCommands(dir, root, []string{"go"}))
	}
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &cancelInspectionContext{Context: base, cancel: cancel}
	inspection, err := Inspect(ctx, dir, "")
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, Inspection{}, inspection)
}

func TestCommandHousekeepingReadOnlyNativeDirectoryIsBestEffort(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	for _, existingLock := range []bool{false, true} {
		name := "lock creation fails"
		if existingLock {
			name = "atomic write fails"
		}
		t.Run(name, func(t *testing.T) {
			dir, orphan, current := policyTempDir(t), policyTempDir(t), policyTempDir(t)
			// This existing root has no receipt, so its rule is an orphan.
			require.NoError(t, SyncCommands(dir, orphan, []string{"make"}))
			path := filepath.Join(dir, "approval-policy.json")
			before, err := os.ReadFile(path) // #nosec G304 -- isolated temporary policy fixture.
			require.NoError(t, err)
			if !existingLock {
				require.NoError(t, os.Remove(filepath.Join(dir, "approval-policy.lock")))
			}
			require.NoError(t, os.Chmod(dir, 0o500))                       // #nosec G302 -- a read-only test directory must retain traversal permission.
			t.Cleanup(func() { require.NoError(t, os.Chmod(dir, 0o700)) }) // #nosec G302 -- restore traversal of a test-owned directory.
			require.NoError(t, SyncEnabledCommands(dir, current, nil))
			after, err := os.ReadFile(path) // #nosec G304 -- isolated temporary policy fixture.
			require.NoError(t, err)
			require.Equal(t, before, after)
			// Adding grants and retiring our own rules remain mandatory writes.
			require.Error(t, SyncEnabledCommands(dir, current, []string{"go"}))
			require.Error(t, SyncEnabledCommands(dir, orphan, nil))
		})
	}
}

func TestCommandHousekeepingSkipsBusyNativeLock(t *testing.T) {
	dir, orphan, current := policyTempDir(t), policyTempDir(t), policyTempDir(t)
	require.NoError(t, SyncCommands(dir, orphan, []string{"make"}))
	path := filepath.Join(dir, "approval-policy.json")
	before, err := os.ReadFile(path) // #nosec G304 -- isolated temporary policy fixture.
	require.NoError(t, err)
	lock, err := os.OpenFile(filepath.Join(dir, "approval-policy.lock"), os.O_RDWR, 0o600) // #nosec G304 -- isolated temporary lock fixture.
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, lock.Close()) })
	require.NoError(t, unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB))
	t.Cleanup(func() { require.NoError(t, unix.Flock(int(lock.Fd()), unix.LOCK_UN)) })
	done := make(chan error, 1)
	go func() { done <- SyncEnabledCommands(dir, current, nil) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		// Release before failing so a regressed blocking writer can finish.
		require.NoError(t, unix.Flock(int(lock.Fd()), unix.LOCK_UN))
		<-done
		t.Fatal("optional pruning waited on the busy native policy lock")
	}
	after, err := os.ReadFile(path) // #nosec G304 -- isolated temporary policy fixture.
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestCommandPolicyUncleanReceiptsAreUnknown(t *testing.T) {
	for _, field := range []string{"workspace_root", "directory"} {
		t.Run(field, func(t *testing.T) {
			base, elsewhere := policyTempDir(t), policyTempDir(t)
			root, dir := filepath.Join(base, "repo"), filepath.Join(base, "muse")
			require.NoError(t, os.Mkdir(root, 0o700))
			subdir := filepath.Join(base, "subdir")
			require.NoError(t, os.Mkdir(subdir, 0o700))
			link := filepath.Join(elsewhere, "link")
			require.NoError(t, os.Symlink(subdir, link))
			receiptFixture(t, dir, root)
			require.NoError(t, SyncCommands(dir, root, []string{"make"}))
			receipt := Receipt{GeneratedBy: "agent-layer", Directory: dir, Root: root}
			// Construct without filepath.Join, which would erase symlink/..
			// before the filesystem gets to resolve the original spelling.
			if field == "workspace_root" {
				receipt.Root = link + "/../repo"
			} else {
				receipt.Directory = link + "/../muse"
			}
			data, err := json.Marshal(receipt)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(root, ".muse/agent-layer-policy.json"), data, 0o600))
			// Shared receipt parsing keeps its existing validation contract.
			parsed, err := ParseReceipt(data)
			require.NoError(t, err)
			require.Equal(t, receipt, *parsed)
			inspection, err := Inspect(context.Background(), dir, root)
			require.NoError(t, err)
			require.True(t, inspection.CurrentReceiptUnknown)
			require.False(t, inspection.CurrentReceiptOrphan)
			require.Empty(t, inspection.Orphans)
			path := filepath.Join(dir, "approval-policy.json")
			before, err := os.ReadFile(path) // #nosec G304 -- isolated temporary native policy.
			require.NoError(t, err)
			require.NoError(t, SyncEnabledCommands(dir, policyTempDir(t), nil))
			after, err := os.ReadFile(path) // #nosec G304 -- isolated temporary native policy.
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestCommandPolicySymlinkEmptyPreflightPreservesOwnershipBoundaries(t *testing.T) {
	for _, owner := range []string{"user", "other workspace", "current workspace"} {
		t.Run(owner, func(t *testing.T) {
			dir, targetDir, root := policyTempDir(t), policyTempDir(t), policyTempDir(t)
			target := filepath.Join(targetDir, "approval-policy.json")
			if owner == "user" {
				require.NoError(t, os.WriteFile(target, []byte(`{"schema_version":2,"rules":[{"reason":"user"}]}`), 0o600))
			} else {
				ownedRoot := root
				if owner == "other workspace" {
					ownedRoot = policyTempDir(t)
				}
				require.NoError(t, SyncCommands(targetDir, ownedRoot, []string{"make"}))
			}
			path := filepath.Join(dir, "approval-policy.json")
			require.NoError(t, os.Symlink(target, path))
			before, err := os.ReadFile(target) // #nosec G304 -- isolated temporary native policy.
			require.NoError(t, err)
			info, err := os.Stat(target)
			require.NoError(t, err)
			err = SyncEnabledCommands(dir, root, nil)
			if owner == "current workspace" {
				require.ErrorContains(t, err, "regular file")
			} else {
				require.NoError(t, err)
			}
			// Disabled/retirement reads remain strict even for unrelated rules.
			require.ErrorContains(t, SyncCommands(dir, root, nil), "regular file")
			after, err := os.ReadFile(target) // #nosec G304 -- isolated temporary native policy.
			require.NoError(t, err)
			current, err := os.Stat(target)
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Equal(t, info.ModTime(), current.ModTime())
			require.True(t, os.SameFile(info, current))
			require.NoFileExists(t, filepath.Join(dir, "approval-policy.lock"))
			linkInfo, err := os.Lstat(path)
			require.NoError(t, err)
			require.NotZero(t, linkInfo.Mode()&os.ModeSymlink)
		})
	}
}

func TestCommandPolicySymlinkPreflightRejectsUnsafeTargets(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "FIFO", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			dir := policyTempDir(t)
			target := filepath.Join(policyTempDir(t), "target")
			switch kind {
			case "directory":
				require.NoError(t, os.Mkdir(target, 0o700))
			case "FIFO":
				require.NoError(t, unix.Mkfifo(target, 0o600))
			case "unreadable":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses permission checks")
				}
				require.NoError(t, os.WriteFile(target, []byte(`{"schema_version":2,"rules":[]}`), 0o600))
				require.NoError(t, os.Chmod(target, 0))
				t.Cleanup(func() { require.NoError(t, os.Chmod(target, 0o600)) })
			}
			require.NoError(t, os.Symlink(target, filepath.Join(dir, "approval-policy.json")))
			require.Error(t, SyncEnabledCommands(dir, policyTempDir(t), nil))
			require.NoFileExists(t, filepath.Join(dir, "approval-policy.lock"))
		})
	}
}

func TestCommandPolicyRetiresMalformedCurrentRulesOnly(t *testing.T) {
	for _, mode := range []string{"enabled desired", "enabled empty", "retirement"} {
		t.Run(mode, func(t *testing.T) {
			dir, root, other := policyTempDir(t), policyTempDir(t), policyTempDir(t)
			require.NoError(t, SyncCommands(dir, root, []string{"git status"}))
			valid := readRules(t, dir)[0]
			var malformed []json.RawMessage
			for _, argv := range []any{[]string{""}, []any{nil}, []string{}, nil, []any{"git", nil}, []any{1}, map[string]any{"future": true}} {
				var entry map[string]any
				require.NoError(t, json.Unmarshal(valid, &entry))
				entry["rule"].(map[string]any)["argv_prefix"] = argv
				raw, err := json.Marshal(entry)
				require.NoError(t, err)
				malformed = append(malformed, raw)
			}
			var foreign map[string]any
			require.NoError(t, json.Unmarshal(malformed[0], &foreign))
			foreign["reason"] = "agent-layer:" + other
			foreign["rule"].(map[string]any)["workspace_root"] = other
			foreignRaw, err := json.Marshal(foreign)
			require.NoError(t, err)
			var unrelated map[string]any
			require.NoError(t, json.Unmarshal(malformed[0], &unrelated))
			unrelated["rule"].(map[string]any)["kind"] = "tool_action"
			unrelatedRaw, err := json.Marshal(unrelated)
			require.NoError(t, err)
			user := json.RawMessage(`{"reason":"user"}`)
			rules := append([]json.RawMessage{user, valid}, malformed...)
			rules = append(rules, valid, foreignRaw, unrelatedRaw)
			data, err := json.Marshal(policyDocument{Version: 2, Rules: rules})
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "approval-policy.json"), data, 0o600))
			inspection, err := Inspect(context.Background(), dir, root)
			require.NoError(t, err)
			require.Equal(t, 2, inspection.Total) // Only the two well-formed copies count.
			switch mode {
			case "enabled desired":
				require.NoError(t, SyncEnabledCommands(dir, root, []string{"git status", "go"}))
			case "enabled empty":
				require.NoError(t, SyncEnabledCommands(dir, root, nil))
			case "retirement":
				require.NoError(t, SyncCommands(dir, root, nil))
			}
			actual := readRules(t, dir)
			if mode == "enabled desired" {
				require.Len(t, actual, 5)
				require.Equal(t, []json.RawMessage{user, valid, foreignRaw, unrelatedRaw}, actual[:4])
				added, owned := ownedCommandRule(actual[4])
				require.True(t, owned)
				require.Equal(t, []string{"go"}, added.Rule.Argv)
			} else {
				require.Equal(t, []json.RawMessage{user, foreignRaw, unrelatedRaw}, actual)
			}
		})
	}
}
