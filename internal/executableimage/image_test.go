package executableimage

import (
	"os"
	"path/filepath"
	"testing"
)

// captureWithArgv0 recaptures this test binary's identity as if invoked as argv0.
func captureWithArgv0(t *testing.T, argv0 string) {
	t.Helper()
	savedArg, savedPath, savedInfo, savedErr := os.Args[0], initialPath, initialInfo, initialErr
	t.Cleanup(func() { os.Args[0], initialPath, initialInfo, initialErr = savedArg, savedPath, savedInfo, savedErr })
	os.Args[0] = argv0
	initialPath, initialInfo, initialErr = capture()
	if initialErr != nil {
		t.Fatal(initialErr)
	}
}

func TestCaptureInvocationSpelling(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	decoy := filepath.Join(dir, "decoy")
	if err := os.WriteFile(decoy, []byte("#!/bin/sh\n"), 0o700); err != nil { // #nosec G306 -- synthetic executable.
		t.Fatal(err)
	}

	t.Run("shadowing alias is ignored", func(t *testing.T) {
		captureWithArgv0(t, decoy)
		if initialPath == decoy {
			t.Fatalf("adopted alias of another file: %s", initialPath)
		}
		if err := Unchanged(); err != nil {
			t.Fatalf("fresh image reported stale: %v", err)
		}
	})

	t.Run("symlink alias detects retarget", func(t *testing.T) {
		link := filepath.Join(dir, "al")
		if err := os.Symlink(self, link); err != nil {
			t.Fatal(err)
		}
		captureWithArgv0(t, link)
		if initialPath != link {
			t.Fatalf("invocation spelling = %s, want %s", initialPath, link)
		}
		if err := Unchanged(); err != nil {
			t.Fatalf("fresh image reported stale: %v", err)
		}
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(decoy, link); err != nil {
			t.Fatal(err)
		}
		if err := Unchanged(); err == nil {
			t.Fatal("retargeted symlink was not detected")
		}
	})
}
