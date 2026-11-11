//go:build unix

package agentdispatch

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

// TestDevelopmentStartupRefusesUnusableSources rejects paths that cannot establish executable provenance.
func TestDevelopmentStartupRefusesUnusableSources(t *testing.T) {
	for _, kind := range []string{"directory", "non-executable", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "al")
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "non-executable":
				freshnessWrite(t, path, "fixture")
			case "fifo":
				testutil.MakeFIFO(t, path, false)
				if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- executable FIFO fixture exercises the file-kind guard.
					t.Fatal(err)
				}
			}
			verify := func() error { _, err := verifyDevelopmentExecutable(path, "fixture"); return err }
			var err error
			if kind == "fifo" {
				err = testutil.BoundedFIFO(t, path, verify)
			} else {
				err = verify()
			}
			if err == nil {
				t.Fatal("startup accepted an unusable executable")
			}
		})
	}
}

// TestImageSHA256RefusesFIFO bounds a regressed blocking open, including startup path replacement races.
func TestImageSHA256RefusesFIFO(t *testing.T) {
	path := testutil.MakeFIFO(t, filepath.Join(t.TempDir(), "al"), false)
	err := testutil.BoundedFIFO(t, path, func() error { _, err := imageSHA256(path); return err })
	if !errors.Is(err, fsutil.ErrNotRegularFile) {
		t.Fatalf("hashing FIFO: %v", err)
	}
}
