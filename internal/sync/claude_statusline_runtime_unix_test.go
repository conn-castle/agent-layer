//go:build unix

package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestClaudeStatuslineScript_ReadsOnlyRegularUntrackedFiles(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatalf("write untracked fixture: %v", err)
	}
	outside := t.TempDir()
	external := filepath.Join(outside, "external.txt")
	if err := os.WriteFile(external, []byte("a\nb\nc\nd\ne\n"), 0o600); err != nil {
		t.Fatalf("write external fixture: %v", err)
	}
	if err := os.Symlink(external, filepath.Join(root, "linked-external")); err != nil {
		t.Fatalf("link external fixture: %v", err)
	}
	fifo := testutil.MakeFIFO(t, filepath.Join(outside, "ipc.pipe"), false)
	if err := os.Symlink(fifo, filepath.Join(root, "linked-ipc")); err != nil {
		t.Fatalf("link FIFO fixture: %v", err)
	}

	input := `{"model": {"display_name": "Opus 4.8"}, "workspace": {"current_dir": "` + root + `"}}`
	cmd, stdout, stderr := claudeStatuslineCommand(t, root, input)
	if err := testutil.BoundedFIFOWithin(t, fifo, 10*time.Second, cmd.Run); err != nil {
		t.Fatalf("run statusline script: %v\nstderr: %s", err, stderr.String())
	}
	out := ansiEscape.ReplaceAllString(stdout.String(), "")

	// All three untracked entries count as changed; only notes.txt is read.
	if !strings.Contains(out, "+2/-0 Δ3") {
		t.Errorf("expected +2/-0 Δ3 from the regular untracked file only, got: %q", out)
	}
}
