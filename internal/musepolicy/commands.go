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
		prefixes = append(prefixes, argv)
	}
	return prefixes, nil
}

// SyncCommands replaces only this workspace's Agent Layer-owned allow rules.
// Native Muse evaluates argv prefixes, compound stages, and deny precedence.
// It uses this same lock file when persisting interactive approval decisions.
func SyncCommands(dir, root string, commands []string) (err error) {
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
	for _, argv := range prefixes {
		rule := map[string]any{"effect": policyAllow, "durability": "local_persistent", "reason": owner,
			"rule": map[string]any{"kind": "shell_command_argv_prefix", "argv_prefix": argv, "workspace_root": root}}
		data, marshalErr := json.Marshal(rule)
		if marshalErr != nil {
			return marshalErr
		}
		desired = append(desired, data)
	}
	path := filepath.Join(dir, "approval-policy.json")
	if len(desired) == 0 {
		// No mutation or lock is needed when this workspace owns no rules.
		// Inspect ownership without requiring the schema used for writes.
		data, readErr := readRegular(path)
		if readErr != nil {
			return readErr
		}
		if data == nil {
			return nil
		}
		var existing struct {
			Rules []struct {
				Reason string `json:"reason"`
			} `json:"rules"`
		}
		if readErr = json.Unmarshal(data, &existing); readErr != nil {
			return fmt.Errorf("inspect Muse approval policy %s: %w", path, readErr)
		}
		owned := false
		for _, rule := range existing.Rules {
			if rule.Reason == owner {
				owned = true
				break
			}
		}
		if !owned {
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
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out locking Muse approval policy: %w", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	defer func() { err = errors.Join(err, unix.Flock(fd, unix.LOCK_UN)) }()
	var document struct {
		Version int               `json:"schema_version"`
		Rules   []json.RawMessage `json:"rules"`
	}
	data, err := readRegular(path)
	if err != nil {
		return err
	}
	if data != nil {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&document); err != nil {
			return fmt.Errorf("read Muse approval policy %s: %w", path, err)
		}
		if !json.Valid(data) || document.Version != 2 {
			return fmt.Errorf("muse approval policy %s must use schema_version 2; open it with supported Muse before syncing", path)
		}
	}
	if data == nil {
		document.Version = 2
	}
	kept := make([]json.RawMessage, 0, len(document.Rules)+len(desired))
	for _, raw := range document.Rules {
		var rule struct {
			Reason     string `json:"reason"`
			Effect     string `json:"effect"`
			Durability string `json:"durability"`
			Rule       struct {
				Kind string `json:"kind"`
				Root string `json:"workspace_root"`
			} `json:"rule"`
		}
		if err = json.Unmarshal(raw, &rule); err != nil {
			return fmt.Errorf("read Muse approval rule: %w", err)
		}
		if rule.Reason == owner && rule.Effect == policyAllow && rule.Durability == "local_persistent" && rule.Rule.Kind == "shell_command_argv_prefix" && rule.Rule.Root == root {
			continue
		}
		kept = append(kept, raw)
	}
	// Avoid rewriting native state when this repo owns no rules.
	if len(desired) == 0 && len(kept) == len(document.Rules) {
		return nil
	}
	kept = append(kept, desired...)
	document.Rules = kept
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomicIfChanged(path, append(encoded, '\n'), 0o600)
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
