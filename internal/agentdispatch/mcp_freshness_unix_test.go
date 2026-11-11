//go:build unix

package agentdispatch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/executableimage"
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

// TestMCPProviderWorkRefusesFIFO keeps the post-startup projection guard nonblocking.
func TestMCPProviderWorkRefusesFIFO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "al")
	freshnessWrite(t, path, "startup source")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	testutil.MakeFIFO(t, path, false)
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	} // #nosec G302 -- executable FIFO exercises the regular-file guard.
	t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "1")
	t.Setenv("AL_DEV_SOURCE_ROOT", filepath.Dir(path))
	t.Setenv("AL_DEV_EXECUTABLE", path)
	server := &dispatchToolServer{
		root: filepath.Dir(path), version: "dev",
		development: developmentSelection{path: path, info: info, snapshot: true},
	}
	err = testutil.BoundedFIFO(t, path, server.checkProviderWork)
	if err == nil || !strings.Contains(err.Error(), "missing or unusable") {
		t.Fatalf("provider work with FIFO source: %v", err)
	}
}

// TestDevelopmentModeChangeKeepsFileIdentity isolates the mode portion of options status.
func TestDevelopmentModeChangeKeepsFileIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "al")
	freshnessWrite(t, path, "fixture")
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	} // #nosec G302 -- executable development fixture.
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	d := developmentSelection{path: path, info: original}
	if d.changed() {
		t.Fatal("unchanged executable reported replaced")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executableimage.SameFile(original, path); err != nil {
		t.Fatalf("chmod changed identity: %v", err)
	}
	if !d.changed() {
		t.Fatal("chmod-only change was not reported")
	}
}

func TestProviderWorkPreservesMissingSourceCause(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "missing-al")
	t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "1")
	t.Setenv("AL_DEV_SOURCE_ROOT", root)
	t.Setenv("AL_DEV_EXECUTABLE", path)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(self)
	if err != nil {
		t.Fatal(err)
	}
	server := &dispatchToolServer{root: root, version: "dev", development: developmentSelection{path: path, info: info, snapshot: true}}
	if err := server.checkProviderWork(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lost source cause: %v", err)
	}
}
