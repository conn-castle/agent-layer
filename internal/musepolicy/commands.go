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
	desired := make([]json.RawMessage, 0, len(prefixes))
	wanted := make(map[string]bool, len(prefixes))
	for _, argv := range prefixes {
		key := prefixKey(argv)
		if wanted[key] {
			continue
		}
		wanted[key] = true
		rule := map[string]any{"effect": policyAllow, "durability": "local_persistent", "reason": owner,
			"rule": map[string]any{"kind": "shell_command_argv_prefix", "argv_prefix": argv, "workspace_root": root}}
		data, marshalErr := json.Marshal(rule)
		if marshalErr != nil {
			return marshalErr
		}
		desired = append(desired, data)
	}
	path := filepath.Join(dir, "approval-policy.json")
	housekeepingOnly := false
	defer func() {
		// Once ownership inspection proves no required update, every failure
		// (including unlock/close) belongs to optional housekeeping only.
		if housekeepingOnly {
			err = nil
		}
	}()
	if len(desired) == 0 {
		// Read leniently before deciding whether this workspace needs a write.
		// Optional orphan housekeeping must not impose our writer's native
		// directory and schema requirements on otherwise unrelated state.
		data, readErr := readRegular(path)
		if readErr != nil {
			return readErr
		}
		if data == nil {
			return nil
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
		if !owned {
			if !prune || !candidates {
				return nil
			}
			housekeepingOnly = true
			if _, readErr := readDocument(path); readErr != nil {
				return nil // Unsupported native state is reported by doctor.
			}
		}
	}
	if !housekeepingOnly {
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if info, statErr := os.Lstat(dir); statErr != nil {
		if housekeepingOnly {
			return nil
		}
		return statErr
	} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		if housekeepingOnly {
			return nil
		}
		return fmt.Errorf("muse policy directory must be a real directory: %s", dir)
	}
	fd, err := unix.Open(filepath.Join(dir, "approval-policy.lock"), unix.O_CREAT|unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("open Muse approval policy lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), "Muse approval policy lock")
	defer func() { err = errors.Join(err, lock.Close()) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if housekeepingOnly {
			return nil // One non-blocking attempt; never wait for optional pruning.
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
		if housekeepingOnly {
			return nil
		}
		return err
	}
	kept := make([]json.RawMessage, 0, len(document.Rules)+len(desired))
	seen := make(map[string]bool, len(wanted))
	orphans := make(map[string]bool)
	for _, raw := range document.Rules {
		rule, owned := ownedCommandRule(raw)
		if owned && rule.Rule.Root == root {
			key := prefixKey(rule.Rule.Argv)
			if !wanted[key] || seen[key] {
				continue
			}
			seen[key] = true
		} else if prune && owned {
			orphan, checked := orphans[rule.Rule.Root]
			if !checked {
				// Check ownership only under approval-policy.lock, after reading
				// the latest rules. Fresh rules always have a receipt already.
				orphan = IsOrphan(dir, rule.Rule.Root)
				orphans[rule.Rule.Root] = orphan
			}
			if orphan {
				continue
			}
		}
		kept = append(kept, raw)
	}
	for _, raw := range desired {
		rule, _ := ownedCommandRule(raw)
		if !seen[prefixKey(rule.Rule.Argv)] {
			kept = append(kept, raw)
		}
	}
	if sameRules(document.Rules, kept) {
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
