//go:build unix

package sync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestNativeConfigReadersRefuseFIFO(t *testing.T) {
	for _, relative := range []string{".codex/config.toml", ".grok-config/trusted_folders.toml", ".vscode/settings.json"} {
		for _, linked := range []bool{false, true} {
			name := relative + "/direct"
			if linked {
				name = relative + "/linked"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, relative)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				fifo := testutil.MakeFIFO(t, path, linked)
				err := testutil.BoundedFIFO(t, fifo, func() error {
					switch relative {
					case ".codex/config.toml":
						return writeCodexConfig(RealSystem{}, root, &config.ProjectConfig{})
					case ".grok-config/trusted_folders.toml":
						return writeGrokTrustedFolders(RealSystem{}, root)
					default:
						return writeVSCodeSettings(RealSystem{}, root, &config.ProjectConfig{})
					}
				})
				if !errors.Is(err, fsutil.ErrNotRegularFile) || !strings.Contains(err.Error(), path) {
					t.Fatalf("error = %v, want non-regular path %s", err, path)
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
					t.Fatalf("FIFO replaced: %v, %v", info, err)
				}
				if linked {
					info, err := os.Lstat(path)
					if err != nil || info.Mode()&os.ModeSymlink == 0 {
						t.Fatalf("link replaced: %v, %v", info, err)
					}
				}
			})
		}
	}
}

func TestNativeConfigReadersMergeRegularSymlinks(t *testing.T) {
	for _, relative := range []string{".codex/config.toml", ".grok-config/trusted_folders.toml", ".vscode/settings.json"} {
		t.Run(relative, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, relative)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			content := "# user setting\nuser_setting = true\n"
			if relative == ".vscode/settings.json" {
				content = "{\"user_setting\": true}\n"
			}
			if err := os.WriteFile(path+".target", []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(path+".target", path); err != nil {
				t.Fatal(err)
			}
			var err error
			switch relative {
			case ".codex/config.toml":
				err = writeCodexConfig(RealSystem{}, root, &config.ProjectConfig{})
			case ".grok-config/trusted_folders.toml":
				err = writeGrokTrustedFolders(RealSystem{}, root)
			default:
				err = writeVSCodeSettings(RealSystem{}, root, &config.ProjectConfig{})
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path) // #nosec G304 -- path is a test-controlled native settings fixture.
			if err != nil || !strings.Contains(string(data), "user_setting") {
				t.Fatalf("user setting lost: %s, %v", data, err)
			}
		})
	}
}
