package sync

import (
	"os"
	"path/filepath"
	"testing"
)

func writePromptServerBinary(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, "al")
	if err := os.WriteFile(path, []byte("stub"), 0o644); err != nil {
		t.Fatalf("write prompt server binary: %v", err)
	}
}
