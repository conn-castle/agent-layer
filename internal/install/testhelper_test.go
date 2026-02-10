package install

import (
	"io/fs"
	"os"
	"path/filepath"
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
	writeErrs  map[string]error
}

func newFaultSystem(base System) *faultSystem {
	return &faultSystem{
		base:       base,
		statErrs:   map[string]error{},
		readErrs:   map[string]error{},
		walkErrs:   map[string]error{},
		mkdirErrs:  map[string]error{},
		removeErrs: map[string]error{},
		writeErrs:  map[string]error{},
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
