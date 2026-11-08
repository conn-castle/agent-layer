//go:build unix

package wizard

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestWizardInputReadersRefuseFIFO(t *testing.T) {
	for _, reader := range []string{"profile", "profile-current-config", "answers", "answers-current-config"} {
		for _, linked := range []bool{false, true} {
			name := reader + "/direct"
			if linked {
				name = reader + "/linked"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				setupRepo(t, root)
				configPath := filepath.Join(root, ".agent-layer", "config.toml")
				profilePath := filepath.Join(root, "profile.toml")
				require.NoError(t, os.WriteFile(profilePath, []byte(basicAgentConfig()), 0o600))

				var path string
				var read func() error
				switch reader {
				case "profile":
					path = filepath.Join(root, "fifo-profile.toml")
					read = func() error { return RunProfile(context.Background(), root, nil, "", path, false, nil) }
				case "profile-current-config":
					path = configPath
					read = func() error { return RunProfile(context.Background(), root, nil, "", profilePath, false, nil) }
				case "answers":
					path = filepath.Join(root, "answers.json")
					read = func() error { _, err := NewScriptedUIFromFile(path); return err }
				case "answers-current-config":
					path = configPath
					read = func() error {
						return RunWithWriter(context.Background(), root, &ScriptedUI{used: map[string]struct{}{}}, nil, "", io.Discard)
					}
				}
				fifo := testutil.MakeFIFO(t, path, linked)
				err := testutil.BoundedFIFO(t, fifo, read)
				if !errors.Is(err, fsutil.ErrNotRegularFile) || !strings.Contains(err.Error(), path) {
					t.Fatalf("error = %v, want non-regular path %s", err, path)
				}
			})
		}
	}
}
