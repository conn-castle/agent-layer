package testutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/conn-castle/agent-layer/internal/gitenv"
)

// RunIsolated runs a test suite without the invoking hook's Git context or a
// repository above its temporary fixtures. Call it from TestMain in suites that
// invoke Git, including through product APIs. Test-owned config overrides added
// during run remain effective; production Git environments are not changed.
func RunIsolated(run func() int) int {
	if err := isolateGitEnvironment(); err != nil {
		fmt.Fprintln(os.Stderr, "isolate test Git environment:", err)
		return 1
	}
	root, err := isolatedTempRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "isolate test temporary directory:", err)
		return 1
	}
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		if err := os.Setenv(name, root); err != nil {
			fmt.Fprintln(os.Stderr, "set test temporary directory:", err)
			_ = os.RemoveAll(root)
			return 1
		}
	}
	code := run()
	if err := os.RemoveAll(root); err != nil { // #nosec G703 -- root is the private directory allocated above for this test process.
		fmt.Fprintln(os.Stderr, "remove test temporary directory:", err)
		return 1
	}
	return code
}

func isolateGitEnvironment() error {
	// Keep the production discovery filter's policy in one place. The remaining
	// names are Git's other hook-local variables (git rev-parse --local-env-vars),
	// plus namespace/template selection and numbered command-config entries.
	kept := gitenv.WithoutDiscovery()
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if slices.Contains(kept, entry) && !slices.Contains([]string{
			"GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
			"GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE", "GIT_SHALLOW_FILE",
			"GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE", "GIT_NAMESPACE",
			"GIT_TEMPLATE_DIR",
		}, name) && !strings.HasPrefix(name, "GIT_CONFIG_KEY_") && !strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			return err
		}
	}
	// Host hooks, signing, filters and identity are not fixture configuration.
	// Individual tests can still install their own global config or URL rewrites.
	for _, name := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		if err := os.Setenv(name, os.DevNull); err != nil {
			return err
		}
	}
	if err := os.Setenv("GIT_CONFIG_NOSYSTEM", "1"); err != nil {
		return err
	}
	// Read-only Git probes of the source checkout must not refresh its index.
	// Required fixture writes (add, read-tree, commit) still take their locks.
	return os.Setenv("GIT_OPTIONAL_LOCKS", "0")
}

func isolatedTempRoot() (string, error) {
	candidates := []string{os.TempDir()}
	if os.PathSeparator == '/' {
		candidates = append(candidates, "/tmp", "/var/tmp")
	}
	if cache, err := os.UserCacheDir(); err == nil {
		candidates = append(candidates, cache)
	}
	for _, parent := range candidates {
		dir, err := os.MkdirTemp(parent, "agent-layer-tests-")
		if err != nil {
			continue
		}
		// Publish the physical absolute path: product root discovery also walks
		// lexical ancestors, which may contain a project above a temp symlink.
		absolute, err := filepath.Abs(dir)
		if err != nil {
			_ = os.RemoveAll(dir) // #nosec G703 -- dir was just allocated above for this test process.
			return "", err
		}
		physical, err := filepath.EvalSymlinks(absolute)
		if err == nil {
			var unsafe bool
			unsafe, err = repositoryAncestor(physical)
			if err == nil && !unsafe {
				return physical, nil
			}
		}
		if removeErr := os.RemoveAll(dir); removeErr != nil { // #nosec G703 -- dir was just allocated by this function.
			return "", removeErr
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("no writable temporary location outside a Git or Agent Layer repository")
}

func repositoryAncestor(physical string) (bool, error) {
	for {
		for _, marker := range []string{".git", ".agent-layer", "go.mod"} {
			if _, err := os.Lstat(filepath.Join(physical, marker)); err == nil {
				return true, nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		}
		// Git also discovers bare repositories, which have no .git marker.
		bare := true
		for _, marker := range []string{"HEAD", "objects", "refs"} {
			if _, err := os.Stat(filepath.Join(physical, marker)); errors.Is(err, os.ErrNotExist) {
				bare = false
				break
			} else if err != nil {
				return false, err
			}
		}
		if bare {
			return true, nil
		}
		parent := filepath.Dir(physical)
		if parent == physical {
			return false, nil
		}
		physical = parent
	}
}
