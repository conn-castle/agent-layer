package testutil_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/gitenv"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestGitIsolation(t *testing.T) {
	if parent := os.Getenv("AL_TEST_GIT_PROBE"); parent != "" {
		outside := t.TempDir()
		status := exec.Command("git", "status", "--porcelain") // #nosec G204 -- disposable parent metadata probe.
		status.Dir, status.Env = parent, gitenv.WithoutDiscovery()
		out, err := status.CombinedOutput()
		require.NoError(t, err, "%s", out)
		cmd := exec.Command("git", "read-tree", "--empty") // #nosec G204 -- deliberately tests nonrepository behavior in a disposable directory.
		cmd.Dir = outside
		out, err = cmd.CombinedOutput()
		require.Error(t, err, "read-tree must not reach an ancestor or inherited index")
		require.Contains(t, string(out), "not a git repository")
		gitFixture(t, outside, "init", "--quiet")
		gitFixture(t, outside, "config", "user.name", "Fixture")
		require.NoError(t, os.WriteFile(filepath.Join(outside, "file"), []byte("fixture"), 0o600))
		gitFixture(t, outside, "add", "file")
		gitFixture(t, outside, "-c", "maintenance.auto=false", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "fixture")
		return
	}
	parent := t.TempDir()
	gitFixture(t, parent, "init", "--quiet")
	file := filepath.Join(parent, "sentinel")
	require.NoError(t, os.WriteFile(file, []byte("preserve"), 0o600))
	gitFixture(t, parent, "add", "sentinel")
	gitFixture(t, parent, "-c", "maintenance.auto=false", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--quiet", "-m", "seed")
	stale := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(file, stale, stale))
	hooks := filepath.Join(parent, "hooks")
	require.NoError(t, os.Mkdir(hooks, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\necho fixture-global-hook >&2\nexit 19\n"), 0o700))
	home := filepath.Join(parent, "home")
	require.NoError(t, os.Mkdir(home, 0o700))
	gitFixture(t, parent, "config", "--file", filepath.Join(home, ".gitconfig"), "core.hooksPath", hooks)
	before := testutil.SnapshotEvidence(t, parent)
	cmd := exec.Command(os.Args[0], "-test.run=^TestGitIsolation$", "-test.count=1") // #nosec G204 G702 -- re-exec the regression under a disposable hook context.
	cmd.Dir = parent
	cmd.Env = append(os.Environ(), "AL_TEST_GIT_BINARY=", "AL_TEST_GIT_PROBE="+parent,
		"TMPDIR="+parent, "GIT_DIR="+filepath.Join(parent, ".git"), "GIT_WORK_TREE="+parent,
		"GIT_COMMON_DIR="+filepath.Join(parent, ".git"), "GIT_INDEX_FILE="+filepath.Join(parent, ".git/index"),
		"GIT_CONFIG="+filepath.Join(parent, ".git/config"), "GIT_OPTIONAL_LOCKS=1", "LC_ALL=C",
		"HOME="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.hooksPath", "GIT_CONFIG_VALUE_0="+hooks)
	out, err := cmd.CombinedOutput()
	require.Equal(t, before, testutil.SnapshotEvidence(t, parent), "child changed parent Git metadata")
	require.NoError(t, err, "%s", out)
}

func gitFixture(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- explicit Git fixture commands in test-owned directories.
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
}
