package testutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// IsolateGit prepares a test process before fixtures are created. Same-binary
// re-execs keep the parent's test-owned configuration and temporary directory.
func IsolateGit() {
	binary, err := os.Executable()
	if err == nil && os.Getenv("AL_TEST_GIT_BINARY") == binary {
		return
	}
	if err == nil {
		err = prepareTestGit(binary)
	}
	if err != nil {
		panic(fmt.Errorf("prepare test Git environment: %w", err))
	}
}

func prepareTestGit(binary string) error {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "GIT_") {
			if err := os.Unsetenv(name); err != nil {
				return err
			}
		}
	}
	parent, err := testTempParent()
	if err != nil {
		return err
	}
	for name, value := range map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1", "GIT_OPTIONAL_LOCKS": "0",
		"TMPDIR": parent, "AL_TEST_GIT_BINARY": binary,
	} {
		if err := os.Setenv(name, value); err != nil {
			return err
		}
	}
	return nil
}

func testTempParent() (string, error) {
candidates:
	for _, candidate := range []string{os.TempDir(), "/tmp", "/var/tmp"} {
		parent, err := filepath.Abs(candidate)
		if err != nil {
			return "", err
		}
		parent, err = filepath.EvalSymlinks(parent)
		if err != nil {
			continue
		}
		for dir := parent; ; dir = filepath.Dir(dir) {
			for _, marker := range []string{".git", ".agent-layer", "go.mod"} {
				if _, err := os.Lstat(filepath.Join(dir, marker)); err == nil {
					continue candidates
				} else if !errors.Is(err, os.ErrNotExist) {
					return "", err
				}
			}
			_, head := os.Stat(filepath.Join(dir, "HEAD"))
			_, objects := os.Stat(filepath.Join(dir, "objects"))
			_, refs := os.Stat(filepath.Join(dir, "refs"))
			if head == nil && objects == nil && refs == nil {
				continue candidates
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
		probe, err := os.CreateTemp(parent, "al-test-parent-")
		if err != nil {
			continue
		}
		if err := errors.Join(probe.Close(), os.Remove(probe.Name())); err != nil {
			return "", err
		}
		return parent, nil
	}
	return "", errors.New("no writable temporary location outside a Git or Agent Layer repository")
}
