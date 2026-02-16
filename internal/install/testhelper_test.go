package install

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

// faultSystem is a test helper that allows deterministic error injection for the
// installer System interface without chmod-based permission tricks.
type faultSystem struct {
	base       System
	statErrs   map[string]error
	readErrs   map[string]error
	walkErrs   map[string]error
	mkdirErrs  map[string]error
	removeErrs map[string]error
	renameErrs map[string]error
	writeErrs  map[string]error
	lookupEnvs map[string]*string
}

func newFaultSystem(base System) *faultSystem {
	return &faultSystem{
		base:       base,
		statErrs:   map[string]error{},
		readErrs:   map[string]error{},
		walkErrs:   map[string]error{},
		mkdirErrs:  map[string]error{},
		removeErrs: map[string]error{},
		renameErrs: map[string]error{},
		writeErrs:  map[string]error{},
		lookupEnvs: map[string]*string{},
	}
}

func normalizePath(path string) string {
	return filepath.Clean(path)
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
