package agentdispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestCapabilityCacheReprobesDelegatedBinaryAfterMaxAge verifies that an unchanged
// launcher can retain a delegated binary's old version only until the cache expires.
func TestCapabilityCacheReprobesDelegatedBinaryAfterMaxAge(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	launcher := filepath.Join(binDir, "muse")
	writeExecutable(t, launcher, "#!/bin/sh\nexec \"$(dirname \"$0\")/muse-bin\" \"$@\"\n")
	writeDelegate := func(version string) {
		writeExecutable(t, filepath.Join(binDir, "muse-bin"), fmt.Sprintf("#!/bin/sh\necho 'Muse Code %s'\n", version))
	}
	target, ok := lookupTarget(AgentMuse)
	if !ok {
		t.Fatal("muse target is not registered")
	}
	resolve := func() string {
		t.Helper()
		_, version, err := compatibleTargetVersionCached(root, launcher, target, nil)
		if err != nil {
			t.Fatalf("resolve version: %v", err)
		}
		return version
	}

	writeDelegate("1.4.0")
	if got := resolve(); got != "1.4.0" {
		t.Fatalf("first version = %q, want 1.4.0", got)
	}
	writeDelegate("1.4.3")
	if got := resolve(); got != "1.4.0" {
		t.Fatalf("version within max age = %q, want cached 1.4.0", got)
	}

	var cache capabilityCache
	if err := readJSON(capabilityCachePath(root), &cache); err != nil {
		t.Fatalf("read cache: %v", err)
	}
	entry := cache.Entries[AgentMuse]
	entry.CheckedAt = time.Now().Add(-capabilityCacheMaxAge)
	cache.Entries[AgentMuse] = entry
	if err := writeJSONAtomic(capabilityCachePath(root), cache); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	if got := resolve(); got != "1.4.3" {
		t.Fatalf("version after max age = %q, want re-probed 1.4.3", got)
	}
}

// writeExecutable creates or replaces an executable test stub, failing the test
// if the file cannot be written.
func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil { // #nosec G306 -- test stub must be executable.
		t.Fatalf("write %s: %v", path, err)
	}
}
