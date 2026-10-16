package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

// Exercise shell startup and the real CLI, rather than duplicating its root
// traversal rules in a shell-only assertion. All discovery targets are fixtures.
func TestShellFixtureRootIsolation(t *testing.T) {
	for _, executable := range []string{"git", "bash"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Skip(executable + " is required")
		}
	}
	binary := filepath.Join(t.TempDir(), "al")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".") //nolint:gosec // Build the source CLI into a test-owned path.
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	bootstrap, err := filepath.Abs(filepath.Join("..", "..", "scripts", "test-git-env.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"project-parent", "external-symlink"} {
		t.Run(mode, func(t *testing.T) {
			parent := t.TempDir()
			git := func(args ...string) {
				t.Helper()
				cmd := exec.Command("git", args...) //nolint:gosec // Explicit commands only target this disposable sentinel.
				cmd.Dir = parent
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("fixture Git: %v\n%s", err, out)
				}
			}
			git("init", "--quiet", "--initial-branch=main")
			if err := os.WriteFile(filepath.Join(parent, "sentinel"), []byte("preserve\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			git("add", "sentinel")
			git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "seed")
			layer := filepath.Join(parent, ".agent-layer")
			for _, dir := range []string{layer, filepath.Join(layer, "instructions"), filepath.Join(layer, "skills")} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			config, err := templates.Read("config.toml")
			if err != nil {
				t.Fatal(err)
			}
			config = bytes.ReplaceAll(config, []byte("enabled = true"), []byte("enabled = false"))
			for name, data := range map[string][]byte{"config.toml": config, ".env": nil, "commands.allow": nil, "gitignore.block": []byte("# disposable sentinel\n")} {
				if err := os.WriteFile(filepath.Join(layer, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			tempParent := parent
			external := ""
			if mode == "external-symlink" {
				external, err = filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				tempParent = filepath.Join(parent, "temp-link")
				if err := os.Symlink(external, tempParent); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := func() map[string]string {
				t.Helper()
				files := map[string]string{}
				err := filepath.WalkDir(parent, func(path string, entry fs.DirEntry, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if !entry.Type().IsRegular() {
						return nil
					}
					data, err := os.ReadFile(path) //nolint:gosec // Only this test's disposable sentinel.
					if err == nil {
						files[path] = string(data)
					}
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				return files
			}
			before := snapshot()
			selected := filepath.Join(t.TempDir(), "selected-parent")
			cmd := exec.Command("bash", "-c", `set -euo pipefail
source "$1"
isolate_test_git
printf '%s' "$TMPDIR" > "$3"
dir="$(mktemp -d "$TMPDIR/al-shell-isolation.XXXXXX")"
trap 'rm -rf "$dir"' EXIT
git -C "$dir" init --quiet
cd "$dir"
"$2" sync
`, "fixture-probe", bootstrap, binary, selected) //nolint:gosec // Fixed shell probe; arguments name only disposable fixtures and source CLI.
			cmd.Dir = parent
			cmd.Env = append(os.Environ(), "TMPDIR="+tempParent, "TMP="+tempParent, "TEMP="+tempParent, "AL_DEV_BYPASS_VERSION_DISPATCH=1", "AL_NO_NETWORK=1", "GIT_OPTIONAL_LOCKS=1")
			out, runErr := cmd.CombinedOutput()
			after := snapshot()
			if len(before) != len(after) {
				t.Fatal("shell CLI changed sentinel files")
			}
			for path, data := range before {
				if after[path] != data {
					t.Fatalf("shell CLI changed sentinel %s", path)
				}
			}
			if runErr == nil || !strings.Contains(string(out), "isn't initialized") {
				t.Fatalf("expected nonproject CLI error: %v\n%s", runErr, out)
			}
			if external != "" {
				selectedPath, err := os.ReadFile(selected) //nolint:gosec // Test-owned probe output.
				if err != nil || string(selectedPath) != external {
					t.Fatalf("safe external temp root not retained: %q, %v", selectedPath, err)
				}
			}
		})
	}
}

func TestCompletionHelperPreservesFixtureTemp(t *testing.T) {
	tempParent := t.TempDir()
	marker := filepath.Join(tempParent, "parent-owned")
	if err := os.WriteFile(marker, []byte("preserve\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--", "zsh") //nolint:gosec // Existing private completion helper protocol.
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "FAKE_FPATH_OUTPUT=fixture-output", "TMPDIR="+tempParent, "TMP="+tempParent, "TEMP="+tempParent)
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "fixture-output" {
		t.Fatalf("completion helper: %v\n%s", err, out)
	}
	entries, err := os.ReadDir(tempParent)
	if err != nil || len(entries) != 1 || entries[0].Name() != "parent-owned" {
		t.Fatalf("completion helper changed its parent's temporary directory: %v, %v", entries, err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "preserve\n" { //nolint:gosec // Test-owned sentinel.
		t.Fatalf("completion helper changed parent fixture: %q, %v", data, err)
	}
}
