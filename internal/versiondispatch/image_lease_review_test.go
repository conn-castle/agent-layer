package versiondispatch

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestDirectImageStartupWaitsForCreationLock(t *testing.T) {
	for _, mode := range []string{"startup-wait", "startup-timeout"} {
		t.Run(mode, func(t *testing.T) {
			cache := t.TempDir()
			path := copyTestImage(t, cache)
			require.NoError(t, os.WriteFile(path+".lease", nil, 0o600))
			oldImage(t, filepath.Dir(path))
			lock, err := openImageFile(path+".lock", true)
			require.NoError(t, err)
			defer func() { _ = lock.Close() }()
			require.NoError(t, unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB))
			cmd := exec.Command(path)
			cmd.Env = append(os.Environ(), "AL_TEST_IMAGE_HELPER="+mode, EnvCacheDir+"="+cache, "HOME="+t.TempDir())
			var stderr strings.Builder
			cmd.Stderr = &stderr
			input, err := cmd.StdinPipe()
			require.NoError(t, err)
			output, err := cmd.StdoutPipe()
			require.NoError(t, err)
			require.NoError(t, cmd.Start())
			t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = input.Close() })
			lines := make(chan string, 2)
			go func() {
				scanner := bufio.NewScanner(output)
				for scanner.Scan() {
					lines <- scanner.Text()
				}
			}()
			waitLine := func(want string) {
				select {
				case line := <-lines:
					require.Equal(t, want, line)
				case <-time.After(imageStartupLeaseWait + 10*time.Second):
					t.Fatalf("helper did not report %s", want)
				}
			}
			waitLine("STARTING")
			if mode == "startup-wait" {
				// The holder is this test process; the server has an independent
				// open description. Release only after its startup has begun.
				released := make(chan error, 1)
				timer := time.AfterFunc(250*time.Millisecond, func() { released <- lock.Close() })
				defer timer.Stop()
				waitLine("READY")
				require.NoError(t, <-released)
				oldImage(t, filepath.Dir(path))
				pruneImages(filepath.Dir(filepath.Dir(path)), "")
				require.FileExists(t, path, "server must retain its acquired lease")
			} else {
				waitLine("READY") // Startup timed out; admission retries also fail.
			}
			require.NoError(t, input.Close())
			require.NoError(t, cmd.Wait())
			warnings := strings.Count(stderr.String(), "could not be leased")
			if mode == "startup-timeout" {
				require.Equal(t, 1, warnings)
			} else {
				require.Zero(t, warnings)
			}
		})
	}
}

func TestLeaseTimestampUsesOpenDescriptor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "al.lease")
	f, err := openImageFile(path, true)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	require.NoError(t, unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB))
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(path, old, old))
	held := filepath.Join(dir, "held.lease")
	require.NoError(t, os.Rename(path, held))
	outside := filepath.Join(t.TempDir(), "sentinel")
	require.NoError(t, os.WriteFile(outside, []byte("keep"), 0o600))
	require.NoError(t, os.Chtimes(outside, old, old))
	before, err := os.Stat(outside)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(outside, path))
	start := time.Now()
	require.NoError(t, touchImageLease(f))
	after, err := os.Stat(outside)
	require.NoError(t, err)
	require.Equal(t, before.ModTime(), after.ModTime(), "a swapped lease path must not touch its target")
	i, err := f.Stat()
	require.NoError(t, err)
	require.False(t, i.ModTime().Before(start.Truncate(time.Second)))
	require.True(t, samePath(f, held))
}

func TestImageHandoffPreparesInvocationBeforeForkLock(t *testing.T) {
	f := newImageHandoffFixture(t, "1.2.3")
	sys := f.system()
	lookedUp := false
	sys.LookPathFunc = func(string) (string, error) {
		locked := syscall.ForkLock.TryLock()
		if locked {
			syscall.ForkLock.Unlock()
		}
		require.True(t, locked, "entry discovery must permit concurrent forks")
		lookedUp = true
		return "/install/bin/al", nil
	}
	f.execCheck = func(path string, env []string) {
		locked := syscall.ForkLock.TryLock()
		if locked {
			syscall.ForkLock.Unlock()
		}
		require.False(t, locked, "only the immediate exec must exclude concurrent forks")
		assertHandoffLease(t, path, env)
	}
	err := maybeExecWithOptions(context.Background(), sys, []string{"al", "dispatch", "mcp-server"}, "1.2.3", f.root, execOptions{
		runningImage: func() (string, string, error) { return f.source, f.source, nil },
	})
	require.ErrorIs(t, err, ErrDispatched)
	require.True(t, lookedUp)
}
