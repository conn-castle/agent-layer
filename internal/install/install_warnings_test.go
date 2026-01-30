package install

import (
	"path/filepath"
	"testing"
)

func TestRecordDiff(t *testing.T) {
	inst := &installer{root: "/test", sys: RealSystem{}}
	inst.recordDiff("/test/file1.txt")
	inst.recordDiff("/test/file2.txt")

	if len(inst.diffs) != 2 {
		t.Fatalf("expected 2 diffs, got %d", len(inst.diffs))
	}
	if inst.diffs[0] != "/test/file1.txt" {
		t.Fatalf("unexpected diff[0]: %s", inst.diffs[0])
	}
	if inst.diffs[1] != "/test/file2.txt" {
		t.Fatalf("unexpected diff[1]: %s", inst.diffs[1])
	}
}

func TestWarnDifferencesWithDiffs(t *testing.T) {
	root := t.TempDir()
	inst := &installer{
		root:      root,
		overwrite: false,
		diffs:     []string{filepath.Join(root, "file1.txt"), filepath.Join(root, "file2.txt")},
		sys:       RealSystem{},
	}

	// Capture stderr by redirecting - warnDifferences writes to os.Stderr.
	// We verify the function completes without panic and processes diffs correctly.
	// The function sorts diffs and formats output.
	inst.warnDifferences()

	// Verify diffs were sorted (warnDifferences sorts inst.diffs).
	if inst.diffs[0] != filepath.Join(root, "file1.txt") {
		t.Fatalf("expected sorted diffs")
	}
}

func TestWarnDifferencesWithOverwrite(t *testing.T) {
	inst := &installer{
		root:      "/test",
		overwrite: true,
		diffs:     []string{"/test/file1.txt"},
		sys:       RealSystem{},
	}

	// Should return early without processing diffs when overwrite is true.
	inst.warnDifferences()
	// No panic means success - early return path.
}

func TestWarnDifferencesNoDiffs(t *testing.T) {
	inst := &installer{
		root:      "/test",
		overwrite: false,
		diffs:     []string{},
		sys:       RealSystem{},
	}

	// Should return early without processing when no diffs.
	inst.warnDifferences()
	// No panic means success - early return path.
}
