//go:build unix

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestDocumentReadersRefuseFIFO(t *testing.T) {
	for _, reader := range []string{"config-fs", "env-fs", "commands-fs", "instruction-lock-fs", "instruction-file-fs", "env", "lenient"} {
		for _, linked := range []bool{false, true} {
			name := reader + "/direct"
			if linked {
				name = reader + "/linked"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				paths := DefaultPaths(root)
				if err := os.MkdirAll(filepath.Dir(paths.ConfigPath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(paths.SkillsDir, 0o700); err != nil {
					t.Fatal(err)
				}
				data, err := templates.Read("config.toml")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths.ConfigPath, data, 0o600); err != nil {
					t.Fatal(err)
				}
				path := paths.EnvPath
				var read func() error
				switch reader {
				case "config-fs":
					path = paths.ConfigPath
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					read = func() error { _, err := LoadProjectConfigFS(os.DirFS(root), root); return err }
				case "env-fs":
					read = func() error { _, err := LoadProjectConfigFS(os.DirFS(root), root); return err }
				case "commands-fs":
					path = paths.CommandsAllow
					read = func() error { _, err := LoadProjectConfigFS(os.DirFS(root), root); return err }
				case "instruction-lock-fs":
					path = paths.InstructionsLockPath
					read = func() error {
						_, err := LoadOrderedInstructionsFS(os.DirFS(root), root, InstructionsConfig{})
						return err
					}
				case "instruction-file-fs":
					path = filepath.Join(paths.InstructionsDir, "rules.md")
					if err := os.MkdirAll(paths.InstructionsDir, 0o700); err != nil {
						t.Fatal(err)
					}
					read = func() error { _, err := readFileFS(os.DirFS(root), root, path); return err }
				case "env":
					read = func() error { _, err := LoadEnv(path); return err }
				case "lenient":
					path = paths.ConfigPath
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					read = func() error { _, err := LoadConfigLenient(path); return err }
				}
				fifo := testutil.MakeFIFO(t, path, linked)
				err = testutil.BoundedFIFO(t, fifo, read)
				if !errors.Is(err, fsutil.ErrNotRegularFile) || !strings.Contains(err.Error(), path) {
					t.Fatalf("error = %v, want non-regular path %s", err, path)
				}
			})
		}
	}
}

func TestEnvReadersFollowRegularSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	path := filepath.Join(root, ".env")
	if err := os.WriteFile(target, []byte("AL_TEST=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	for _, read := range []func() (map[string]string, error){func() (map[string]string, error) { return LoadEnv(path) }, func() (map[string]string, error) { return LoadEnvFS(os.DirFS(root), root, path) }} {
		env, err := read()
		if err != nil || env["AL_TEST"] != "value" {
			t.Fatalf("env = %v, %v", env, err)
		}
	}
}
