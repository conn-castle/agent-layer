package agentdispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

type capabilityCache struct {
	Entries map[string]capabilityCacheEntry `json:"entries"`
}

type capabilityCacheEntry struct {
	Identity string `json:"identity"`
	Version  string `json:"version"`
}

func compatibleTargetVersionCached(root string, path string, target targetMeta, lookup func(string, string) (string, error)) (targetMeta, string, error) {
	if lookup != nil {
		return compatibleTargetVersion(path, target, lookup)
	}
	identity, err := providerBinaryIdentity(path, target.Name)
	if err != nil {
		return targetMeta{}, "", wrapExitError(ExitUnavailable, fmt.Sprintf("identify %s binary", target.Name), err)
	}
	var version string
	err = withCapabilityCacheLock(root, func() error {
		cache := capabilityCache{Entries: map[string]capabilityCacheEntry{}}
		cachePath := capabilityCachePath(root)
		if readErr := readJSON(cachePath, &cache); readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return wrapExitError(ExitConfig, "read dispatch capability cache", readErr)
		}
		if cache.Entries == nil {
			cache.Entries = map[string]capabilityCacheEntry{}
		}
		if entry, ok := cache.Entries[target.Name]; ok && entry.Identity == identity {
			version = entry.Version
			return nil
		}
		resolved, resolveErr := requireSupportedVersion(path, target.Name, nil)
		if resolveErr != nil {
			return resolveErr
		}
		version = resolved
		cache.Entries[target.Name] = capabilityCacheEntry{Identity: identity, Version: resolved}
		if err := writeJSONAtomic(cachePath, cache); err != nil {
			return wrapExitError(ExitConfig, "publish dispatch capability cache", err)
		}
		return nil
	})
	if err != nil {
		return targetMeta{}, "", err
	}
	target.Binary = path
	return target, version, nil
}

func providerBinaryIdentity(path string, agent string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	stat, _ := info.Sys().(*syscall.Stat_t)
	device, inode := any(0), any(0)
	if stat != nil {
		device, inode = stat.Dev, stat.Ino
	}
	return fmt.Sprintf("%s|%d|%d|%d|%d|%s", path, device, inode, info.Size(), info.ModTime().UnixNano(), supportedProviderVersions[agent]), nil
}

func capabilityCachePath(root string) string {
	return filepath.Join(root, ".agent-layer", "state", "dispatch-capabilities", "cache.json")
}

func withCapabilityCacheLock(root string, fn func() error) error {
	path := filepath.Join(root, ".agent-layer", "state", "dispatch-capabilities", "cache.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304 -- fixed Agent Layer state path.
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil { //nolint:gosec // supported Unix descriptor.
		return err
	}
	defer func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN) }() //nolint:gosec // supported Unix descriptor.
	return fn()
}
