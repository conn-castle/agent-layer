package install

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type readFailOnSecondReadSystem struct {
	base      System
	target    string
	readCount int
}

func (s *readFailOnSecondReadSystem) Stat(name string) (os.FileInfo, error) {
	return s.base.Stat(name)
}

func (s *readFailOnSecondReadSystem) Lstat(name string) (os.FileInfo, error) {
	return s.base.Lstat(name)
}

func (s *readFailOnSecondReadSystem) ReadFile(name string) ([]byte, error) {
	if normalizePath(name) == normalizePath(s.target) {
		s.readCount++
		if s.readCount >= 2 {
			return nil, errors.New("read failed after listing")
		}
	}
	return s.base.ReadFile(name)
}

func (s *readFailOnSecondReadSystem) Readlink(name string) (string, error) {
	return s.base.Readlink(name)
}

func (s *readFailOnSecondReadSystem) LookupEnv(key string) (string, bool) {
	return s.base.LookupEnv(key)
}

func (s *readFailOnSecondReadSystem) MkdirAll(path string, perm os.FileMode) error {
	return s.base.MkdirAll(path, perm)
}

func (s *readFailOnSecondReadSystem) RemoveAll(path string) error {
	return s.base.RemoveAll(path)
}

func (s *readFailOnSecondReadSystem) Rename(oldpath string, newpath string) error {
	return s.base.Rename(oldpath, newpath)
}

func (s *readFailOnSecondReadSystem) Symlink(oldname string, newname string) error {
	return s.base.Symlink(oldname, newname)
}

func (s *readFailOnSecondReadSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return s.base.WalkDir(root, fn)
}

func (s *readFailOnSecondReadSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	return s.base.WriteFileAtomic(filename, data, perm)
}

type modeFileInfo struct {
	name string
	mode os.FileMode
}

func (i modeFileInfo) Name() string       { return i.name }
func (i modeFileInfo) Size() int64        { return 0 }
func (i modeFileInfo) Mode() os.FileMode  { return i.mode }
func (i modeFileInfo) ModTime() time.Time { return time.Time{} }
func (i modeFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i modeFileInfo) Sys() any           { return nil }

type customLstatSystem struct {
	base   System
	target string
	mode   os.FileMode
}

func (s *customLstatSystem) Stat(name string) (os.FileInfo, error) {
	return s.base.Stat(name)
}

func (s *customLstatSystem) Lstat(name string) (os.FileInfo, error) {
	if normalizePath(name) == normalizePath(s.target) {
		return modeFileInfo{name: filepath.Base(name), mode: s.mode}, nil
	}
	return s.base.Lstat(name)
}

func (s *customLstatSystem) ReadFile(name string) ([]byte, error) {
	return s.base.ReadFile(name)
}

func (s *customLstatSystem) Readlink(name string) (string, error) {
	return s.base.Readlink(name)
}

func (s *customLstatSystem) LookupEnv(key string) (string, bool) {
	return s.base.LookupEnv(key)
}

func (s *customLstatSystem) MkdirAll(path string, perm os.FileMode) error {
	return s.base.MkdirAll(path, perm)
}

func (s *customLstatSystem) RemoveAll(path string) error {
	return s.base.RemoveAll(path)
}

func (s *customLstatSystem) Rename(oldpath string, newpath string) error {
	return s.base.Rename(oldpath, newpath)
}

func (s *customLstatSystem) Symlink(oldname string, newname string) error {
	return s.base.Symlink(oldname, newname)
}

func (s *customLstatSystem) WalkDir(root string, fn fs.WalkDirFunc) error {
	return s.base.WalkDir(root, fn)
}

func (s *customLstatSystem) WriteFileAtomic(filename string, data []byte, perm os.FileMode) error {
	return s.base.WriteFileAtomic(filename, data, perm)
}

func TestListUpgradeSnapshots_AdditionalCoverageBranches(t *testing.T) {
	t.Run("requires root", func(t *testing.T) {
		_, err := ListUpgradeSnapshots("", RealSystem{})
		if err == nil || !strings.Contains(err.Error(), "root path is required") {
			t.Fatalf("expected root-required error, got %v", err)
		}
	})

	t.Run("requires system", func(t *testing.T) {
		_, err := ListUpgradeSnapshots(t.TempDir(), nil)
		if err == nil || !strings.Contains(err.Error(), "system is required") {
			t.Fatalf("expected system-required error, got %v", err)
		}
	})

	t.Run("list error from snapshot file scan", func(t *testing.T) {
		root := t.TempDir()
		snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}
		fsys := newFaultSystem(RealSystem{})
		fsys.walkErrs[normalizePath(snapshotDir)] = errors.New("walk boom")

		_, err := ListUpgradeSnapshots(root, fsys)
		if err == nil || !strings.Contains(err.Error(), "walk boom") {
			t.Fatalf("expected list error, got %v", err)
		}
	})

	t.Run("skip unreadable snapshot after listing", func(t *testing.T) {
		root := t.TempDir()
		snapshotDir := filepath.Join(root, filepath.FromSlash(upgradeSnapshotDirRelPath))
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}
		path := filepath.Join(snapshotDir, "snapshot-1.json")
		data := `{"schema_version":1,"snapshot_id":"snapshot-1","created_at_utc":"2026-01-01T00:00:00Z","status":"applied","entries":[]}`
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatalf("write snapshot file: %v", err)
		}

		sys := &readFailOnSecondReadSystem{base: RealSystem{}, target: path}
		snapshots, err := ListUpgradeSnapshots(root, sys)
		if err != nil {
			t.Fatalf("ListUpgradeSnapshots: %v", err)
		}
		if len(snapshots) != 0 {
			t.Fatalf("expected unreadable snapshot to be skipped, got %d entries", len(snapshots))
		}
	})
}

func TestWriteAndPruneUpgradeSnapshot_ErrorCoverage(t *testing.T) {
	t.Run("writeUpgradeSnapshot prune error", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: newFaultSystem(RealSystem{})}
		fsys := inst.sys.(*faultSystem)
		fsys.statErrs[normalizePath(inst.upgradeSnapshotDirPath())] = errors.New("stat boom")
		snapshot := upgradeSnapshot{
			SchemaVersion: upgradeSnapshotSchemaVersion,
			SnapshotID:    "snapshot-1",
			CreatedAtUTC:  "2026-01-01T00:00:00Z",
			Status:        upgradeSnapshotStatusCreated,
			Entries:       []upgradeSnapshotEntry{},
		}

		err := inst.writeUpgradeSnapshot(snapshot, true)
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected prune stat error, got %v", err)
		}
	})

	t.Run("prune list error", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: newFaultSystem(RealSystem{})}
		snapshotDir := inst.upgradeSnapshotDirPath()
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}
		fsys := inst.sys.(*faultSystem)
		fsys.walkErrs[normalizePath(snapshotDir)] = errors.New("walk boom")

		err := inst.pruneUpgradeSnapshots(0)
		if err == nil || !strings.Contains(err.Error(), "walk boom") {
			t.Fatalf("expected prune list error, got %v", err)
		}
	})
}

func TestValidateUpgradeSnapshot_EntryErrorCoverage(t *testing.T) {
	snapshot := upgradeSnapshot{
		SchemaVersion: upgradeSnapshotSchemaVersion,
		SnapshotID:    "snapshot-1",
		CreatedAtUTC:  "2026-01-01T00:00:00Z",
		Status:        upgradeSnapshotStatusCreated,
		Entries: []upgradeSnapshotEntry{
			{
				Path:       "docs/agent-layer",
				Kind:       upgradeSnapshotEntryKindDir,
				LinkTarget: "invalid",
			},
		},
	}
	if err := validateUpgradeSnapshot(snapshot); err == nil {
		t.Fatal("expected invalid snapshot entry error")
	}

	dirEntryErr := validateUpgradeSnapshotEntry(upgradeSnapshotEntry{
		Path:       "docs/agent-layer",
		Kind:       upgradeSnapshotEntryKindDir,
		LinkTarget: "invalid",
	})
	if dirEntryErr == nil || !strings.Contains(dirEntryErr.Error(), "must not set link_target") {
		t.Fatalf("expected dir link_target error, got %v", dirEntryErr)
	}

	absentEntryErr := validateUpgradeSnapshotEntry(upgradeSnapshotEntry{
		Path:       "docs/agent-layer/old.md",
		Kind:       upgradeSnapshotEntryKindAbsent,
		LinkTarget: "invalid",
	})
	if absentEntryErr == nil || !strings.Contains(absentEntryErr.Error(), "must not set link_target") {
		t.Fatalf("expected absent link_target error, got %v", absentEntryErr)
	}
}

func TestListUpgradeSnapshotFiles_StatAndWalkErrorCoverage(t *testing.T) {
	root := t.TempDir()
	inst := &installer{root: root, sys: newFaultSystem(RealSystem{})}
	snapshotDir := inst.upgradeSnapshotDirPath()

	t.Run("stat error", func(t *testing.T) {
		fsys := newFaultSystem(RealSystem{})
		fsys.statErrs[normalizePath(snapshotDir)] = errors.New("stat boom")
		localInst := &installer{root: root, sys: fsys}
		_, err := localInst.listUpgradeSnapshotFiles()
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected stat error, got %v", err)
		}
	})

	t.Run("walk callback error", func(t *testing.T) {
		if err := os.MkdirAll(snapshotDir, 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}
		localInst := &installer{root: root, sys: walkCallbackErrSystem{base: RealSystem{}}}
		_, err := localInst.listUpgradeSnapshotFiles()
		if err == nil || !strings.Contains(err.Error(), "walk callback boom") {
			t.Fatalf("expected walk callback error, got %v", err)
		}
	})
}

func TestCaptureUpgradeSnapshot_UnsupportedAndRelativePathBranches(t *testing.T) {
	t.Run("capture target unsupported file type", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "special")
		sys := &customLstatSystem{
			base:   RealSystem{},
			target: target,
			mode:   os.ModeNamedPipe,
		}
		inst := &installer{root: root, sys: sys}
		err := inst.captureUpgradeSnapshotTarget(target, map[string]upgradeSnapshotEntry{})
		if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
			t.Fatalf("expected unsupported file type error, got %v", err)
		}
	})

	t.Run("capture symlink repo-relative path error", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: RealSystem{}}
		outsidePath := filepath.Join(t.TempDir(), "link")
		err := inst.captureUpgradeSnapshotSymlink(outsidePath, map[string]upgradeSnapshotEntry{})
		if err == nil {
			t.Fatal("expected repo-relative path error")
		}
	})
}
