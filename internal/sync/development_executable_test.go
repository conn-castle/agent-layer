//go:build unix

package sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
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
			writeGitignoreBlockForSkillsDedupTest(t, root)
			source := filepath.Join(t.TempDir(), "al")
			if err := os.WriteFile(source, []byte("fixture"), 0o700); err != nil { // #nosec G306 -- executable development fixture.
				t.Fatal(err)
			}
			t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "1")
			t.Setenv("AL_DEV_SOURCE_ROOT", root)
			t.Setenv("AL_DEV_EXECUTABLE", source)
			paths := []string{filepath.Join(root, ".gitignore"), filepath.Join(root, ".codex", "config.toml")}
			const marker = "# operator-restored configuration\n"
			writeMarkers := func() {
				for _, path := range paths {
					if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(marker), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			// Prove the complete fixture can sync and change both operator files.
			writeMarkers()
			if _, err := RunWithProject(RealSystem{}, root, project); err != nil {
				t.Fatalf("valid-source control sync: %v", err)
			}
			for _, path := range paths {
				data, err := fsutil.ReadRegularFile(path)
				if err != nil || string(data) == marker {
					t.Fatalf("control sync did not change %s: %v", path, err)
				}
			}
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
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
			writeMarkers()
			check := func() error {
				_, syncErr := RunWithProject(RealSystem{}, root, project)
				// Check writes even when sync fails at a later projection boundary.
				for _, path := range paths {
					data, err := fsutil.ReadRegularFile(path)
					if err != nil {
						return err
					}
					if string(data) != marker {
						return fmt.Errorf("operator configuration was changed: %s", path)
					}
				}
				if syncErr == nil || !strings.Contains(syncErr.Error(), "selected development executable") {
					return fmt.Errorf("sync did not reject unusable dev source: %v", syncErr)
				}
				if err := writeCodexConfigWithCLISettings(RealSystem{}, root, project, true); err == nil || !strings.Contains(err.Error(), "selected development executable") {
					return errors.New("direct projection accepted unusable dev source")
				}
				if command, err := herdrCommand("codex", root); err == nil {
					return fmt.Errorf("HerdR silently selected %q", command)
				}
				data, err := fsutil.ReadRegularFile(paths[1])
				if err != nil {
					return err
				}
				if string(data) != marker {
					return errors.New("operator configuration was changed by direct projection")
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

func TestLegacyBypassDoesNotSelectDevelopmentProjections(t *testing.T) {
	for _, kind := range []string{"missing", "relative", "valid"} {
		for _, client := range []string{"none", "codex"} {
			t.Run(kind+"/"+client, func(t *testing.T) {
				project := builtInProject(t)
				root := project.Root
				project.Config.Agents = agentsForSkillsTest(client)
				disabled := false
				project.Config.Agents.Muse.Enabled = &disabled
				writeGitignoreBlockForSkillsDedupTest(t, root)
				source := filepath.Join(t.TempDir(), "al")
				switch kind {
				case "relative":
					source = "./al"
				case "valid":
					if err := os.WriteFile(source, []byte("fixture"), 0o700); err != nil { // #nosec G306 -- executable development fixture.
						t.Fatal(err)
					}
				}
				t.Setenv("AL_DEV_BYPASS_VERSION_DISPATCH", "1")
				t.Setenv("AL_DEV_SOURCE_ROOT", "")
				t.Setenv("AL_DEV_EXECUTABLE", source)
				if _, err := RunWithProject(RealSystem{}, root, project); err != nil {
					t.Fatalf("legacy bypass sync: %v", err)
				}
				command := mustHerdRCommand(t, "codex", root)
				if !strings.HasPrefix(command, "exec al hook herdr codex ") {
					t.Fatalf("legacy bypass persisted source selection: %q", command)
				}
				if client == "codex" {
					data, err := fsutil.ReadRegularFile(filepath.Join(root, ".codex", "config.toml"))
					if err != nil {
						t.Fatal(err)
					}
					var projected struct {
						Hooks map[string][]struct {
							Hooks []struct{ Command string }
						}
						MCPServers map[string]struct {
							Command string
							Args    []string
						} `toml:"mcp_servers"`
					}
					if err := toml.Unmarshal(data, &projected); err != nil {
						t.Fatal(err)
					}
					for _, event := range []string{"SessionStart", "UserPromptSubmit"} {
						groups := projected.Hooks[event]
						if len(groups) != 1 || len(groups[0].Hooks) != 1 || groups[0].Hooks[0].Command != command {
							t.Fatalf("legacy bypass %s hook projection: %#v", event, groups)
						}
					}
					server := projected.MCPServers["agent-layer"]
					wantBuiltInDispatchInvocation(t, server.Command, server.Args, root)
				}
			})
		}
	}
}
