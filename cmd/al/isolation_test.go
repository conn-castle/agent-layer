package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestShellFixtureRootIsolation(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "al")
	out, err := exec.Command("go", "build", "-buildvcs=false", "-o", binary, ".").CombinedOutput() //nolint:gosec // Builds the source CLI into a test-owned path.
	require.NoError(t, err, "%s", out)
	bootstrap, err := filepath.Abs(filepath.Join("..", "..", "scripts", "test-git-env.sh"))
	require.NoError(t, err)
	for _, mode := range []string{"project-parent", "external-symlink"} {
		t.Run(mode, func(t *testing.T) {
			parent := t.TempDir()
			writeTestRepo(t, parent)
			config := filepath.Join(parent, ".agent-layer", "config.toml")
			data, err := os.ReadFile(config) //nolint:gosec // Test-owned fixture.
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(config, []byte(strings.ReplaceAll(string(data), "enabled = true", "enabled = false")), 0o600)) //nolint:gosec // The config belongs to this t.TempDir fixture.
			cmd := exec.Command("git", "init", "--quiet", parent)                                                                          //nolint:gosec // Disposable sentinel repository.
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
			temp := parent
			if mode == "external-symlink" {
				temp = filepath.Join(parent, "temp-link")
				require.NoError(t, os.Symlink(t.TempDir(), temp))
			}
			before := testutil.SnapshotEvidence(t, parent)
			cmd = exec.Command("bash", "-c", `set -euo pipefail
source "$1"
isolate_test_git
fixture="$(mktemp -d "$TMPDIR/al-isolation.XXXXXX")"
trap 'rm -rf "$fixture"' EXIT
git -C "$fixture" init --quiet
cd "$fixture"
"$2" sync
`, "fixture-probe", bootstrap, binary) //nolint:gosec // Fixed probe with disposable fixtures and the source CLI.
			cmd.Dir = parent
			cmd.Env = append(os.Environ(), "TMPDIR="+temp, "GIT_DIR="+filepath.Join(parent, ".git"), "GIT_WORK_TREE="+parent, "AL_DEV_BYPASS_VERSION_DISPATCH=1", "AL_NO_NETWORK=1")
			out, err = cmd.CombinedOutput()
			require.Equal(t, before, testutil.SnapshotEvidence(t, parent), "shell/CLI changed parent fixture")
			require.Error(t, err)
			require.Contains(t, string(out), "isn't initialized")
		})
	}
}
