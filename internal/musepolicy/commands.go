// Package musepolicy projects Agent Layer grants into Muse's native policy.
package musepolicy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/shlex"
	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/fsutil"
)

const (
	policyLocalPersistent  = "local_persistent"
	shellCommandArgvPrefix = "shell_command_argv_prefix"
)

// ConfigDir follows Muse's XDG convention on both Linux and macOS.
func ConfigDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(base) {
		return "", fmt.Errorf("muse configuration directory must be absolute: %s", base)
	}
	return filepath.Join(base, "muse"), nil
}

// ParseCommandPrefixes validates commands.allow entries as literal argv
// prefixes and returns their native Muse representation.
func ParseCommandPrefixes(commands []string) ([][]string, error) {
	prefixes := make([][]string, 0, len(commands))
	for _, command := range commands {
		// Prefixes describe literal argv, not shell programs. Silently turning
		// operators into argv tokens would create rules that never match.
		if strings.ContainsAny(command, ";&|<>`$()\n\r") {
			return nil, fmt.Errorf("muse command prefix %q contains shell syntax; use one literal argv prefix per line", command)
		}
		argv, err := shlex.Split(command)
		if err != nil || len(argv) == 0 || argv[0] == "" || strings.Contains(argv[0], "=") {
			return nil, fmt.Errorf("invalid Muse command prefix %q: expected a nonempty quoted argv prefix", command)
		}
		// Split drops an unquoted word-initial # and the rest of the line as a
		// comment, which would approve a shorter, broader prefix. The tokenizer
		// still returns that comment, so any extra token reveals one.
		if commandTokenCount(command) != len(argv) {
			return nil, fmt.Errorf("muse command prefix %q contains a # comment; quote a literal # argument", command)
		}
		prefixes = append(prefixes, argv)
	}
	return prefixes, nil
}

// commandTokenCount counts shlex words and comments in a command that Split
// already accepted.
func commandTokenCount(command string) int {
	tokenizer := shlex.NewTokenizer(strings.NewReader(command))
	count := 0
	for {
		if _, err := tokenizer.Next(); err != nil {
			return count
		}
		count++
	}
}

// SyncCommands merges only this workspace's Agent Layer-owned allow rules.
// Native Muse evaluates argv prefixes, compound stages, and deny precedence.
// It uses this same lock file when persisting interactive approval decisions.
func SyncCommands(dir, root string, commands []string) error {
	return syncCommands(dir, root, commands, false)
}

// SyncEnabledCommands merges this workspace's grants and prunes provable orphans
// in this native directory. The caller must persist its receipt before calling.
func SyncEnabledCommands(dir, root string, commands []string) error {
	return syncCommands(dir, root, commands, true)
}

func syncCommands(dir, root string, commands []string, prune bool) (err error) {
	prefixes, err := ParseCommandPrefixes(commands)
	if err != nil {
		return err
	}
	originalRoot := root
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		// A receipt may name a moved/deleted workspace. Only retirement may use it.
		if !os.IsNotExist(err) || len(commands) != 0 || !filepath.IsAbs(originalRoot) {
			return err
		}
		root = filepath.Clean(originalRoot)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	owner := "agent-layer:" + root
	type desiredRule struct {
		key  string
		data json.RawMessage
	}
	desired := make([]desiredRule, 0, len(prefixes))
	wanted := make(map[string]bool, len(prefixes))
	for _, argv := range prefixes {
		key := prefixKey(argv)
		if wanted[key] {
			continue
		}
		wanted[key] = true
		rule := map[string]any{"effect": policyAllow, "durability": policyLocalPersistent, "reason": owner,
			"rule": map[string]any{"kind": shellCommandArgvPrefix, "argv_prefix": argv, "workspace_root": root}}
		data, marshalErr := json.Marshal(rule)
		if marshalErr != nil {
			return marshalErr
		}
		desired = append(desired, desiredRule{key: key, data: data})
	}
	path := filepath.Join(dir, "approval-policy.json")
	if len(desired) == 0 {
		// Read leniently before deciding whether this workspace needs a write.
		// Optional orphan housekeeping must not impose our writer's native
		// directory and schema requirements on otherwise unrelated state.
		info, statErr := os.Lstat(path)
		if os.IsNotExist(statErr) {
			return nil
		}
		if statErr != nil {
			return statErr
		}
		symlinkPolicy := info.Mode()&os.ModeSymlink != 0
		if !info.Mode().IsRegular() && !symlinkPolicy {
			return fmt.Errorf("muse approval policy must be a regular file: %s", path)
		}
		// A single read-only open supplies the ownership snapshot. The helper
		// opens nonblocking and verifies the opened fd is regular, so a link
		// target changed to a FIFO cannot block or supply ownership evidence.
		data, readErr := fsutil.ReadRegularFile(path)
		if readErr != nil {
			return readErr
		}
		var existing struct {
			Rules []json.RawMessage `json:"rules"`
		}
		if readErr = json.Unmarshal(data, &existing); readErr != nil {
			return fmt.Errorf("inspect Muse approval policy %s: %w", path, readErr)
		}
		owned, candidates := false, false
		for _, raw := range existing.Rules {
			var rule struct {
				Reason string `json:"reason"`
			}
			if json.Unmarshal(raw, &rule) == nil && rule.Reason == owner {
				owned = true
			}
			if _, candidate := ownedCommandRule(raw); candidate {
				candidates = true
			}
		}
		if symlinkPolicy {
			if owned {
				return fmt.Errorf("muse approval policy must be a regular file: %s", path)
			}
			return nil // Skip pruning without writing through the policy-file symlink.
		}
		if !owned {
			if prune && candidates {
				pruneOrphans(dir, root)
			}
			return nil
		}
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if info, statErr := os.Lstat(dir); statErr != nil {
		return statErr
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("muse policy directory must be a real directory: %s", dir)
	}
	lock, err := openPolicyLock(dir)
	if err != nil {
		return err
	}
	fd := int(lock.Fd())
	defer func() { err = errors.Join(err, lock.Close()) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out locking Muse approval policy: %w", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	defer func() { err = errors.Join(err, unix.Flock(fd, unix.LOCK_UN)) }()
	document, err := readDocument(path)
	if err != nil {
		return err
	}
	kept := make([]json.RawMessage, 0, len(document.Rules)+len(desired))
	seen := make(map[string]bool, len(wanted))
	orphans := make(map[string]bool)
	for _, raw := range document.Rules {
		if workspaceCommandRule(raw, root) {
			// Current-root cleanup preserves the original ownership breadth:
			// malformed argv is ours to retire, not foreign state to preserve.
			rule, valid := ownedCommandRule(raw)
			key := prefixKey(rule.Rule.Argv)
			if !valid || !wanted[key] || seen[key] {
				continue
			}
			seen[key] = true
		} else if prune {
			if rule, owned := ownedCommandRule(raw); owned && rule.Rule.Root != root && orphanRoot(dir, rule.Rule.Root, orphans) {
				continue
			}
		}
		kept = append(kept, raw)
	}
	for _, rule := range desired {
		if !seen[rule.key] {
			kept = append(kept, rule.data)
		}
	}
	return writeRulesIfChanged(path, document.Rules, kept)
}

// workspaceCommandRule deliberately ignores argv shape. Current-root cleanup
// may retire any command rule with our exact ownership metadata, as before.
func workspaceCommandRule(raw json.RawMessage, root string) bool {
	var rule struct {
		Reason     string `json:"reason"`
		Effect     string `json:"effect"`
		Durability string `json:"durability"`
		Rule       struct {
			Kind string `json:"kind"`
			Root string `json:"workspace_root"`
		} `json:"rule"`
	}
	return json.Unmarshal(raw, &rule) == nil && rule.Reason == "agent-layer:"+root &&
		rule.Effect == policyAllow && rule.Durability == policyLocalPersistent &&
		rule.Rule.Kind == shellCommandArgvPrefix && rule.Rule.Root == root
}

func openPolicyLock(dir string) (*os.File, error) {
	fd, err := unix.Open(filepath.Join(dir, "approval-policy.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open Muse approval policy lock: %w", err)
	}
	return os.NewFile(uintptr(fd), "Muse approval policy lock"), nil
}

// pruneOrphans is optional housekeeping, independent of required rule updates.
// It creates no directories, tries the lock once, and ignores all failures.
func pruneOrphans(dir, root string) {
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	path := filepath.Join(dir, "approval-policy.json")
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		return
	}
	lock, err := openPolicyLock(dir)
	if err != nil {
		return
	}
	defer func() { _ = lock.Close() }()
	fd := int(lock.Fd())
	if unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return
	}
	defer func() { _ = unix.Flock(fd, unix.LOCK_UN) }()
	document, err := readDocument(path)
	if err != nil {
		return
	}
	kept := make([]json.RawMessage, 0, len(document.Rules))
	orphans := make(map[string]bool)
	for _, raw := range document.Rules {
		if rule, owned := ownedCommandRule(raw); owned && rule.Rule.Root != root && orphanRoot(dir, rule.Rule.Root, orphans) {
			continue
		}
		kept = append(kept, raw)
	}
	_ = writeRulesIfChanged(path, document.Rules, kept)
}

// Call only under approval-policy.lock, after reading the latest rule list.
func orphanRoot(dir, root string, cache map[string]bool) bool {
	if orphan, checked := cache[root]; checked {
		return orphan
	}
	orphan := IsOrphan(dir, root)
	cache[root] = orphan
	return orphan
}

func writeRulesIfChanged(path string, original, kept []json.RawMessage) error {
	if sameRules(original, kept) {
		// Compare the rule list before encoding: Muse may use different
		// whitespace, key ordering or extra rule fields. No write means no
		// formatting or mtime churn, even after another workspace synced.
		return nil
	}
	// Do not pass RawMessages through MarshalIndent: it reformats their
	// contents. Preserve every retained rule's bytes, including Muse metadata.
	var encoded bytes.Buffer
	encoded.WriteString("{\n  \"schema_version\": 2,\n  \"rules\": [")
	for i, raw := range kept {
		if i != 0 {
			encoded.WriteByte(',')
		}
		encoded.WriteString("\n    ")
		encoded.Write(raw)
	}
	encoded.WriteString("\n  ]\n}\n")
	return fsutil.WriteFileAtomicIfChanged(path, encoded.Bytes(), 0o600)
}

func prefixKey(argv []string) string {
	data, _ := json.Marshal(argv) // []string always marshals successfully.
	return string(data)
}

func sameRules(a, b []json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("muse approval policy must be a regular file: %s", path)
	}
	return os.ReadFile(path) // #nosec G304 -- native policy path, checked regular above.
}
