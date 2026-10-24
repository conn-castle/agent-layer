//go:build unix

package skillimport

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestLoadStateRefusesFIFOConfig(t *testing.T) {
	for _, linked := range []bool{false, true} {
		name := "direct"
		if linked {
			name = "linked"
		}
		t.Run(name, func(t *testing.T) {
			p := newProject(t)
			path := p.paths.ConfigPath
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			fifo := testutil.MakeFIFO(t, path, linked)
			for _, instructions := range []bool{false, true} {
				err := testutil.BoundedFIFO(t, fifo, func() error { _, err := loadState(p.root, instructions); return err })
				if !errors.Is(err, fsutil.ErrNotRegularFile) || !strings.Contains(err.Error(), path) {
					t.Fatalf("error = %v, want non-regular path %s", err, path)
				}
			}
		})
	}
}

func TestLoadStatePreservesLinkedConfigBytes(t *testing.T) {
	p := newProject(t)
	path := p.paths.ConfigPath
	target := path + ".target"
	raw := "# Preserve this comment and whitespace.\n\n" + baseConfigTOML + "\n"
	writeProjectFile(t, target, raw)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(p.root, false)
	if err != nil {
		t.Fatal(err)
	}
	if state.configRaw != raw {
		t.Fatalf("config bytes changed: %q", state.configRaw)
	}
}
