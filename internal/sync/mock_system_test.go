package sync

import (
	"errors"
	"os"
)

type MockSystem struct {
	Fallback            System
	LookPathFunc        func(file string) (string, error)
	StatFunc            func(name string) (os.FileInfo, error)
	MkdirAllFunc        func(path string, perm os.FileMode) error
	WriteFileAtomicFunc func(filename string, data []byte, perm os.FileMode) error
	MarshalIndentFunc   func(v any, prefix, indent string) ([]byte, error)
	ReadFileFunc        func(name string) ([]byte, error)
	ReadDirFunc         func(name string) ([]os.DirEntry, error)
	RemoveFunc          func(name string) error
	RemoveAllFunc       func(path string) error
}

func (m *MockSystem) LookPath(file string) (string, error) {
	if m.LookPathFunc != nil {
		return m.LookPathFunc(file)
	}
	if m.Fallback != nil {
		return m.Fallback.LookPath(file)
	}
	return "", os.ErrNotExist
}

func (m *MockSystem) Stat(name string) (os.FileInfo, error) {
	if m.StatFunc != nil {
		return m.StatFunc(name)
	}
	if m.Fallback != nil {
		return m.Fallback.Stat(name)
	}
	return nil, os.ErrNotExist
}

func (m *MockSystem) MkdirAll(path string, perm os.FileMode) error {
	if m.MkdirAllFunc != nil {
		return m.MkdirAllFunc(path, perm)
	}
	if m.Fallback != nil {
		return m.Fallback.MkdirAll(path, perm)
	}
	return errors.New("mock system MkdirAll not implemented")
}

func (m *MockSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	if m.WriteFileAtomicFunc != nil {
		return m.WriteFileAtomicFunc(filename, data, perm)
	}
	if m.Fallback != nil {
		return m.Fallback.WriteFileAtomic(filename, data, perm)
	}
	return errors.New("mock system WriteFileAtomic not implemented")
}

func (m *MockSystem) MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	if m.MarshalIndentFunc != nil {
		return m.MarshalIndentFunc(v, prefix, indent)
	}
	if m.Fallback != nil {
		return m.Fallback.MarshalIndent(v, prefix, indent)
	}
	return nil, errors.New("mock system MarshalIndent not implemented")
}

func (m *MockSystem) ReadFile(name string) ([]byte, error) {
	if m.ReadFileFunc != nil {
		return m.ReadFileFunc(name)
	}
	if m.Fallback != nil {
		return m.Fallback.ReadFile(name)
	}
	return nil, os.ErrNotExist
}

func (m *MockSystem) ReadDir(name string) ([]os.DirEntry, error) {
	if m.ReadDirFunc != nil {
		return m.ReadDirFunc(name)
	}
	if m.Fallback != nil {
		return m.Fallback.ReadDir(name)
	}
	return nil, os.ErrNotExist
}

func (m *MockSystem) Remove(name string) error {
	if m.RemoveFunc != nil {
		return m.RemoveFunc(name)
	}
	if m.Fallback != nil {
		return m.Fallback.Remove(name)
	}
	return os.ErrNotExist
}

func (m *MockSystem) RemoveAll(path string) error {
	if m.RemoveAllFunc != nil {
		return m.RemoveAllFunc(path)
	}
	if m.Fallback != nil {
		return m.Fallback.RemoveAll(path)
	}
	return os.ErrNotExist
}
