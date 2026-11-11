//go:build unix

package sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/testutil"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

// developmentBoundarySystem invalidates a previously valid source only after acquiring the sync lock.
type developmentBoundarySystem struct {
	RealSystem
	source      string
	invalidated bool
	writes      int
}

func (s *developmentBoundarySystem) Flock(fd, how int) error {
	if err := s.RealSystem.Flock(fd, how); err != nil {
		return err
	}
	if how&unix.LOCK_EX != 0 && !s.invalidated {
		s.invalidated = true
		return os.Remove(s.source)
	}
	return nil
}

func (s *developmentBoundarySystem) WriteFileAtomic(string, []byte, os.FileMode) error {
	s.writes++
	return errors.New("unexpected projection write")
}

func TestSyncRechecksDevelopmentAfterAcquiringLock(t *testing.T) {
	project := builtInProject(t)
	root := project.Root
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "gitignore.block"), []byte("/.agent-layer/tmp/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "al")
	if err := os.WriteFile(source, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	} // #nosec G306 -- executable development fixture.
	t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "1")
	t.Setenv("AL_DEV_SOURCE_ROOT", root)
	t.Setenv("AL_DEV_EXECUTABLE", source)
	if err := versiondispatch.ValidateDevelopmentExecutable(root); err != nil {
		t.Fatal(err)
	}
	sys := &developmentBoundarySystem{source: source}
	_, err := RunWithProject(sys, root, project)
	if !errors.Is(err, os.ErrNotExist) || !sys.invalidated || sys.writes != 0 {
		t.Fatalf("projection boundary: error=%v invalidated=%v writes=%d", err, sys.invalidated, sys.writes)
	}
}

func TestDevelopmentProjectionRejectsUnusableSource(t *testing.T) {
	for _, kind := range []string{"missing", "directory", "non-executable", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			project := builtInProject(t)
			root := project.Root
			if err := os.MkdirAll(filepath.Join(root, ".agent-layer"), 0o700); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(t.TempDir(), "al")
			switch kind {
			case "directory":
				if err := os.Mkdir(source, 0o700); err != nil {
					t.Fatal(err)
				}
			case "non-executable":
				if err := os.WriteFile(source, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				testutil.MakeFIFO(t, source, false)
				if err := os.Chmod(source, 0o700); err != nil {
					t.Fatal(err)
				} // #nosec G302 -- FIFO must fail kind validation despite execute bits.
			}
			t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "1")
			t.Setenv("AL_DEV_SOURCE_ROOT", root)
			t.Setenv("AL_DEV_EXECUTABLE", source)
			path := filepath.Join(root, ".codex", "config.toml")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			const marker = "# operator-restored configuration\n"
			if err := os.WriteFile(path, []byte(marker), 0o600); err != nil {
				t.Fatal(err)
			}
			check := func() error {
				if _, err := RunWithProject(RealSystem{}, root, project); err == nil || !strings.Contains(err.Error(), "selected development executable") {
					return errors.New("sync accepted unusable dev source")
				}
				if err := writeCodexConfigWithCLISettings(RealSystem{}, root, project, true); err == nil || !strings.Contains(err.Error(), "selected development executable") {
					return errors.New("direct projection accepted unusable dev source")
				}
				if command, err := herdrCommand("codex", root); err == nil {
					return fmt.Errorf("HerdR silently selected %q", command)
				}
				data, err := fsutil.ReadRegularFile(path)
				if err != nil {
					return err
				}
				if string(data) != marker {
					return errors.New("operator configuration was changed")
				}
				return nil
			}
			var err error
			if kind == "fifo" {
				err = testutil.BoundedFIFO(t, source, check)
			} else {
				err = check()
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHerdRSelectionDoesNotLeakToAnotherRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "1")
	t.Setenv("AL_DEV_SOURCE_ROOT", t.TempDir())
	t.Setenv("AL_DEV_EXECUTABLE", filepath.Join(t.TempDir(), "missing-al"))
	command, err := herdrCommand("muse", root)
	if err != nil || !strings.HasPrefix(command, "exec al hook herdr muse ") {
		t.Fatalf("cross-root source selection: %q %v", command, err)
	}
}
