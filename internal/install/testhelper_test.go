package install

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

// faultSystem is a test helper that allows deterministic error injection for the
// installer System interface without chmod-based permission tricks.
type faultSystem struct {
	base         System
	lstatErrs    map[string]error
	statErrs     map[string]error
	readErrs     map[string]error
	readlinkErrs map[string]error
	walkErrs     map[string]error
	mkdirErrs    map[string]error
	removeErrs   map[string]error
	renameErrs   map[string]error
	symlinkErrs  map[string]error
	writeErrs    map[string]error
	lookupEnvs   map[string]*string
}

func newFaultSystem(base System) *faultSystem {
	return &faultSystem{
		base:         base,
		lstatErrs:    map[string]error{},
		statErrs:     map[string]error{},
		readErrs:     map[string]error{},
		readlinkErrs: map[string]error{},
		walkErrs:     map[string]error{},
		mkdirErrs:    map[string]error{},
		removeErrs:   map[string]error{},
		renameErrs:   map[string]error{},
		symlinkErrs:  map[string]error{},
		writeErrs:    map[string]error{},
		lookupEnvs:   map[string]*string{},
	}
}

func normalizePath(path string) string {
	return filepath.Clean(path)
}

func (f *faultSystem) Lstat(name string) (os.FileInfo, error) {
	if err, ok := f.lstatErrs[normalizePath(name)]; ok {
		return nil, err
	}
	return f.base.Lstat(name)
}

func (f *faultSystem) Stat(name string) (os.FileInfo, error) {
	if err, ok := f.statErrs[normalizePath(name)]; ok {
		return nil, err
	}
	return f.base.Stat(name)
}

func (f *faultSystem) ReadFile(name string) ([]byte, error) {
	if err, ok := f.readErrs[normalizePath(name)]; ok {
		return nil, err
	}
	return f.base.ReadFile(name)
}

func (f *faultSystem) Readlink(name string) (string, error) {
	if err, ok := f.readlinkErrs[normalizePath(name)]; ok {
		return "", err
	}
	return f.base.Readlink(name)
}

func (f *faultSystem) LookupEnv(key string) (string, bool) {
	if value, ok := f.lookupEnvs[key]; ok {
		if value == nil {
			return "", false
		}
		return *value, true
	}
	return f.base.LookupEnv(key)
}

func (f *faultSystem) MkdirAll(path string, perm os.FileMode) error {
	if err, ok := f.mkdirErrs[normalizePath(path)]; ok {
		return err
	}
	return f.base.MkdirAll(path, perm)
}

func (f *faultSystem) RemoveAll(path string) error {
	if err, ok := f.removeErrs[normalizePath(path)]; ok {
		return err
	}
	return f.base.RemoveAll(path)
}

func (f *faultSystem) Rename(oldpath string, newpath string) error {
	if err, ok := f.renameErrs[normalizePath(oldpath)]; ok {
		return err
	}
	return f.base.Rename(oldpath, newpath)
}

func (f *faultSystem) Symlink(oldname string, newname string) error {
	if err, ok := f.symlinkErrs[normalizePath(newname)]; ok {
		return err
	}
	return f.base.Symlink(oldname, newname)
}

func (f *faultSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	if err, ok := f.walkErrs[normalizePath(root)]; ok {
		return err
	}
	return f.base.WalkDir(root, fn)
}

func (f *faultSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	if err, ok := f.writeErrs[normalizePath(filename)]; ok {
		return err
	}
	return f.base.WriteFileAtomic(filename, data, perm)
}

func withMigrationManifestOverride(t *testing.T, targetVersion string, manifestJSON string) {
	t.Helper()
	manifestPath := fmt.Sprintf("migrations/%s.json", targetVersion)
	original := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		if name == manifestPath {
			return []byte(manifestJSON), nil
		}
		return original(name)
	}
	t.Cleanup(func() { templates.ReadFunc = original })
}

// fakeDirEntry implements fs.DirEntry for test walk overrides.
type fakeDirEntry struct {
	name  string
	isDir bool
}

func (f fakeDirEntry) Name() string               { return f.name }
func (f fakeDirEntry) IsDir() bool                { return f.isDir }
func (f fakeDirEntry) Type() fs.FileMode          { return 0 }
func (f fakeDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

// withMigrationManifestChainOverride intercepts both templates.ReadFunc and
// templates.WalkFunc so that listMigrationManifestVersions discovers exactly
// the provided versions and loadUpgradeMigrationManifestByVersion returns the
// provided JSON for each. The map keys are version strings (e.g. "0.6.1"),
// and values are the full manifest JSON.
func withMigrationManifestChainOverride(t *testing.T, manifests map[string]string) {
	t.Helper()

	// Build path→JSON lookup.
	pathMap := make(map[string]string, len(manifests))
	for ver, jsonStr := range manifests {
		pathMap[fmt.Sprintf("migrations/%s.json", ver)] = jsonStr
	}

	origRead := templates.ReadFunc
	templates.ReadFunc = func(name string) ([]byte, error) {
		if jsonStr, ok := pathMap[name]; ok {
			return []byte(jsonStr), nil
		}
		// For migration paths not in the override map, return not-found
		// so the override fully controls the migrations namespace.
		if strings.HasPrefix(name, "migrations/") && strings.HasSuffix(name, ".json") {
			return nil, fs.ErrNotExist
		}
		return origRead(name)
	}

	origWalk := templates.WalkFunc
	templates.WalkFunc = func(root string, fn fs.WalkDirFunc) error {
		if root != "migrations" {
			return origWalk(root, fn)
		}
		// Emit the directory entry first.
		if err := fn("migrations", fakeDirEntry{name: "migrations", isDir: true}, nil); err != nil {
			return err
		}
		// Emit one entry per overridden version.
		for ver := range manifests {
			filename := ver + ".json"
			entryPath := "migrations/" + filename
			if err := fn(entryPath, fakeDirEntry{name: filename, isDir: false}, nil); err != nil {
				return err
			}
		}
		return nil
	}

	t.Cleanup(func() {
		templates.ReadFunc = origRead
		templates.WalkFunc = origWalk
	})
}
