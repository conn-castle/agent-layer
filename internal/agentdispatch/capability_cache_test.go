package agentdispatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const capabilityCacheNewerMuseVersion = "99.0.0"

// writeMuseLauncher installs a launcher that, like the Muse installer's, runs
// a sibling binary without changing itself when that binary updates.
func writeMuseLauncher(t *testing.T, version string) (string, string) {
	t.Helper()
	binDir := t.TempDir()
	launcher := filepath.Join(binDir, "muse")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec \"$(dirname \"$0\")/muse-bin\" \"$@\"\n"), 0o700); err != nil { // #nosec G306 -- test executable.
		t.Fatalf("write launcher: %v", err)
	}
	delegate := filepath.Join(binDir, "muse-bin")
	writeMuseDelegate(t, delegate, version)
	pinned := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, path := range []string{launcher, delegate, binDir} {
		if err := os.Chtimes(path, pinned, pinned); err != nil {
			t.Fatalf("pin %s times: %v", path, err)
		}
	}
	return launcher, delegate
}

func writeMuseDelegate(t *testing.T, path string, version string) {
	t.Helper()
	content := "#!/bin/sh\necho 'Muse Code " + version + " (" + version + "-R1.1)'\n"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil { // #nosec G306 -- test executable.
		t.Fatalf("write delegate: %v", err)
	}
}

func cachedMuseVersion(t *testing.T, root string, launcher string) string {
	t.Helper()
	target, ok := lookupTarget(AgentMuse)
	if !ok {
		t.Fatal("missing Muse target")
	}
	_, version, err := compatibleTargetVersionCached(root, launcher, target, nil)
	if err != nil {
		t.Fatalf("resolve Muse version: %v", err)
	}
	return version
}

func TestCapabilityCacheFollowsDelegatedBinaryReplacedBehindUnchangedLauncher(t *testing.T) {
	root := t.TempDir()
	launcher, delegate := writeMuseLauncher(t, supportedProviderVersions[AgentMuse])
	if got := cachedMuseVersion(t, root, launcher); got != supportedProviderVersions[AgentMuse] {
		t.Fatalf("initial version = %q, want %q", got, supportedProviderVersions[AgentMuse])
	}
	before, err := os.Stat(launcher)
	if err != nil {
		t.Fatalf("stat launcher: %v", err)
	}

	staged := delegate + ".new"
	writeMuseDelegate(t, staged, capabilityCacheNewerMuseVersion)
	if err := os.Rename(staged, delegate); err != nil {
		t.Fatalf("replace delegate: %v", err)
	}
	after, err := os.Stat(launcher)
	if err != nil {
		t.Fatalf("stat launcher: %v", err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatal("test changed the launcher")
	}

	if got := cachedMuseVersion(t, root, launcher); got != capabilityCacheNewerMuseVersion {
		t.Fatalf("version after delegate replacement = %q, want %q", got, capabilityCacheNewerMuseVersion)
	}
}

func TestCapabilityCacheReprobesInPlaceDelegateUpdateAfterTTL(t *testing.T) {
	root := t.TempDir()
	launcher, delegate := writeMuseLauncher(t, supportedProviderVersions[AgentMuse])
	if got := cachedMuseVersion(t, root, launcher); got != supportedProviderVersions[AgentMuse] {
		t.Fatalf("initial version = %q, want %q", got, supportedProviderVersions[AgentMuse])
	}

	// Overwriting in place changes neither the launcher nor its directory, so
	// only the cache age bounds how long the old version is reported.
	writeMuseDelegate(t, delegate, capabilityCacheNewerMuseVersion)
	if got := cachedMuseVersion(t, root, launcher); got != supportedProviderVersions[AgentMuse] {
		t.Fatalf("version within TTL = %q, want cached %q", got, supportedProviderVersions[AgentMuse])
	}

	var cache capabilityCache
	if err := readJSON(capabilityCachePath(root), &cache); err != nil {
		t.Fatalf("read cache: %v", err)
	}
	entry := cache.Entries[AgentMuse]
	entry.CheckedAt = time.Now().Add(-capabilityCacheTTL - time.Second)
	cache.Entries[AgentMuse] = entry
	if err := writeJSONAtomic(capabilityCachePath(root), cache); err != nil {
		t.Fatalf("age cache: %v", err)
	}

	if got := cachedMuseVersion(t, root, launcher); got != capabilityCacheNewerMuseVersion {
		t.Fatalf("version after TTL = %q, want %q", got, capabilityCacheNewerMuseVersion)
	}
}
