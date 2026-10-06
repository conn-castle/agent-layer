package agentdispatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/clients/muse"
)

func TestCapabilityCacheReprobesBinaryUpdatedBehindUnchangedLauncher(t *testing.T) {
	root := t.TempDir()
	bin := t.TempDir()
	launcher := filepath.Join(bin, "muse")
	if err := os.WriteFile(launcher, []byte("#!/bin/sh\nexec \"$(dirname \"$0\")/muse-bin\" \"$@\"\n"), 0o700); err != nil { // #nosec G306 -- executable provider fixture.
		t.Fatal(err)
	}
	installDelegate := func(version string) {
		t.Helper()
		staged := filepath.Join(bin, "muse-bin.new")
		if err := os.WriteFile(staged, []byte("#!/bin/sh\necho 'Muse Code "+version+"'\n"), 0o700); err != nil { // #nosec G306 -- executable provider fixture.
			t.Fatal(err)
		}
		if err := os.Rename(staged, filepath.Join(bin, "muse-bin")); err != nil {
			t.Fatal(err)
		}
	}
	target, ok := lookupTarget(AgentMuse)
	if !ok {
		t.Fatal("muse target is not registered")
	}
	resolve := func() string {
		t.Helper()
		_, version, err := compatibleTargetVersionCached(root, launcher, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		return version
	}
	launcherInfo := func() os.FileInfo {
		t.Helper()
		info, err := os.Stat(launcher)
		if err != nil {
			t.Fatal(err)
		}
		return info
	}

	installDelegate(muse.SupportedVersion)
	before := launcherInfo()
	if got := resolve(); got != muse.SupportedVersion {
		t.Fatalf("first probe version = %q, want %q", got, muse.SupportedVersion)
	}
	const updated = "99.0.0"
	installDelegate(updated)
	if after := launcherInfo(); !os.SameFile(before, after) || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Fatal("test must update the delegated binary without touching the launcher")
	}
	if got := resolve(); got != muse.SupportedVersion {
		t.Fatalf("version within cache bound = %q, want cached %q", got, muse.SupportedVersion)
	}

	cachePath := capabilityCachePath(root)
	var cache capabilityCache
	if err := readJSON(cachePath, &cache); err != nil {
		t.Fatal(err)
	}
	entry := cache.Entries[AgentMuse]
	entry.CheckedAt = time.Now().UTC().Add(-capabilityCacheTTL)
	cache.Entries[AgentMuse] = entry
	if err := writeJSONAtomic(cachePath, cache); err != nil {
		t.Fatal(err)
	}
	if got := resolve(); got != updated {
		t.Fatalf("version after cache bound = %q, want re-probed %q", got, updated)
	}
}
