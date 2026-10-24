//go:build darwin || linux

package launchers

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGeneratedLaunchersPreservePhysicalRoot(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"folder 'quoted' $cash\twith\nnewline", "folder\n\n"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), name)
			if err := WriteVSCodeLaunchers(testSystem{}, root); err != nil {
				t.Fatal(err)
			}
			// A trimmed sibling makes incorrect directory selection succeed.
			if err := os.MkdirAll(strings.TrimRight(root, "\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			physicalRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			for command, script := range map[string]string{"al": "#!/bin/sh\nprintf '%s' \"$PWD\"\n", "code": "#!/bin/sh\nexit 0\n"} {
				if err := os.WriteFile(filepath.Join(bin, command), []byte(script), 0o700); err != nil {
					t.Fatal(err)
				} // #nosec G306 -- owned executable fixtures.
			}
			paths := VSCodePaths(root)
			// Exercise the app's production shell prelude up to the macOS-only
			// osascript boundary, at the same bundle depth, then run its zsh
			// command directly when zsh is available. Native UI is separate.
			app, err := os.ReadFile(paths.AppExec) // #nosec G304 -- owned generated launcher.
			if err != nil {
				t.Fatal(err)
			}
			prelude, _, ok := strings.Cut(string(app), "exec /usr/bin/osascript")
			if !ok {
				t.Fatal("app has no osascript boundary")
			}
			handoff := "printf '%s' \"$PARENT_ROOT\"\n"
			if _, err := exec.LookPath("zsh"); err == nil {
				handoff = "exec zsh -f -c \"$ZSH_CMD\"\n"
			}
			probe := filepath.Join(paths.AppMacOS, "root-probe")
			if err := os.WriteFile(probe, []byte(prelude+handoff), 0o600); err != nil { // #nosec G703 -- probe is constructed under this test's owned app bundle.
				t.Fatal(err)
			}
			for _, path := range []string{paths.Shell, paths.Command, probe} {
				t.Run(filepath.Base(path), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, "bash", path) // #nosec G204 -- owned generated script.
					cmd.Dir = t.TempDir()
					cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
					out, err := cmd.CombinedOutput()
					if err != nil || string(out) != physicalRoot {
						t.Fatalf("root = %q, want %q; error=%v", out, physicalRoot, err)
					}
				})
			}
		})
	}
}
