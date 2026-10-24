//go:build unix

package skilllock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestLockReadersRefuseFIFO(t *testing.T) {
	for name, load := range map[string]func(string) (*File, error){"skills": Load, "instructions": LoadInstructions} {
		t.Run(name, func(t *testing.T) {
			for _, linked := range []bool{false, true} {
				name := "direct"
				if linked {
					name = "linked"
				}
				t.Run(name, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "lock.json")
					if _, err := load(path); !errors.Is(err, ErrMissing) {
						t.Fatalf("missing lock = %v", err)
					}
					fifo := testutil.MakeFIFO(t, path, linked)
					err := testutil.BoundedFIFO(t, fifo, func() error { _, err := load(path); return err })
					if !errors.Is(err, fsutil.ErrNotRegularFile) || !strings.Contains(err.Error(), path) {
						t.Fatalf("error = %v, want non-regular path %s", err, path)
					}
				})
			}
			path := filepath.Join(t.TempDir(), "lock.json")
			envelope := "skills"
			if name == "instructions" {
				envelope = "instructions"
			}
			if err := os.WriteFile(path, []byte(`{"version":2,"`+envelope+`":[]}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path, path+".link"); err != nil {
				t.Fatal(err)
			}
			if _, err := load(path + ".link"); err != nil {
				t.Fatalf("regular link: %v", err)
			}
		})
	}
}
