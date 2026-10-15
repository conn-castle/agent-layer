// Package executableimage keeps executable selection tied to an executing image.
package executableimage

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Capture at process initialization, before a development build can replace the
// path. In particular, Darwin cannot recover the old identity from that path later.
var initialPath, initialInfo, initialErr = capture()

func capture() (string, os.FileInfo, error) {
	p, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	image := p
	if runtime.GOOS == "linux" {
		image = "/proc/self/exe"
	}
	info, err := os.Stat(image)
	// Keep the invocation spelling too: a package-manager symlink can be
	// repointed while its old executable target remains installed.
	if invoked, lookupErr := exec.LookPath(os.Args[0]); lookupErr == nil {
		if absolute, absErr := filepath.Abs(invoked); absErr == nil {
			p = absolute
		}
	}
	return p, info, err
}

// SameAs reports whether path still names the executing image. Atomic replacement
// counts as a change even for identical bytes; no content hashing is necessary.
func SameAs(path string) error {
	if initialErr != nil {
		return initialErr
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(initialInfo, info) || initialInfo.Size() != info.Size() || !initialInfo.ModTime().Equal(info.ModTime()) {
		return errors.New("running executable was replaced")
	}
	return nil
}

// Unchanged checks the original executable pathname, including unpinned installs.
func Unchanged() error { return SameAs(initialPath) }

// Self returns a stable reference to this process's image. On platforms without
// procfs, a replaced pathname is a clean error before worker publication.
func Self() (string, error) {
	if runtime.GOOS == "linux" {
		return "/proc/self/exe", nil
	}
	if err := Unchanged(); err != nil {
		return "", err
	}
	return initialPath, nil
}
