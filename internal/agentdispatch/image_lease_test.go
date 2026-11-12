package agentdispatch

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/imagelease"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

// detachedLeaseHelper exercises the production launcher from a real image
// process; its worker checks both the gate's fd 3 and the lease's fd 4.
func detachedLeaseHelper() {
	dir := os.Getenv("AL_TEST_DETACHED_LEASE")
	if dir == "" {
		return
	}
	fail := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	if len(os.Args) > 1 && os.Args[1] == workerCommand {
		if imagelease.Current() == nil {
			fail(fmt.Errorf("worker did not inherit image lease"))
		}
		gate := os.NewFile(3, "gate")
		var token [1]byte
		_, err := io.ReadFull(gate, token[:])
		fail(err)
		if token[0] != 1 {
			fail(fmt.Errorf("wrong gate token"))
		}
		fail(os.WriteFile(filepath.Join(dir, "ready"), []byte("ready"), 0o600)) // #nosec G703 -- disposable test control directory.
		for {
			if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil { // #nosec G703 -- disposable test control directory.
				os.Exit(0)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	if os.Getenv("AL_TEST_RETRY_LEASE") != "" {
		versiondispatch.LeaseRunningImage()
		versiondispatch.RetryRunningImageLease()
		if imagelease.Current() != nil {
			fail(fmt.Errorf("busy startup unexpectedly acquired lease"))
		}
		fmt.Println("UNLEASED")
		var token [1]byte
		_, err := io.ReadFull(os.Stdin, token[:])
		fail(err)
	} else if imagelease.Current() == nil {
		fail(fmt.Errorf("server image lease missing"))
	}
	w, err := launchDetachedWorker(dir, "lease-test", filepath.Join(dir, "worker.log"))
	fail(err)
	if imagelease.Current() == nil {
		fail(fmt.Errorf("worker admission did not retry the image lease"))
	}
	_, err = w.gate.Write([]byte{1})
	fail(err)
	fail(w.gate.Close())
	fmt.Println("READY")
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func TestLaunchDetachedWorkerRetainsImageLease(t *testing.T) {
	testDetachedWorkerLease(t, false)
}

func TestLaunchDetachedWorkerRetriesTransientLeaseFailure(t *testing.T) {
	testDetachedWorkerLease(t, true)
}

func testDetachedWorkerLease(t *testing.T, retry bool) {
	t.Helper()
	cache := t.TempDir()
	self, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(self) // #nosec G304 -- own test executable.
	require.NoError(t, err)
	dir := filepath.Join(cache, "images", fmt.Sprintf("%x", sha256.Sum256(data)))
	require.NoError(t, os.MkdirAll(dir, 0o700)) // #nosec G703 -- disposable test cache.
	path := filepath.Join(dir, "al")
	require.NoError(t, os.WriteFile(path, data, 0o700))                    // #nosec G306,G703 -- disposable test executable.
	lease, err := os.OpenFile(path+".lease", os.O_RDWR|os.O_CREATE, 0o600) // #nosec G304,G703 -- disposable image lease.
	require.NoError(t, err)
	defer func() { _ = lease.Close() }()
	control := t.TempDir()
	parent := exec.Command(path)
	parent.Env = append(os.Environ(), "AL_TEST_DETACHED_LEASE="+control, "AL_CACHE_DIR="+cache, "HOME="+t.TempDir())
	var startupLock *os.File
	if retry {
		startupLock, err = os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304,G703 -- disposable cache lock.
		require.NoError(t, err)
		defer func() { _ = startupLock.Close() }()
		require.NoError(t, unix.Flock(int(startupLock.Fd()), unix.LOCK_EX|unix.LOCK_NB))
		parent.Env = append(parent.Env, "AL_TEST_RETRY_LEASE=1")
	} else {
		require.NoError(t, unix.Flock(int(lease.Fd()), unix.LOCK_SH))
		parent.Env = append(parent.Env, imagelease.EnvFD+"=3")
		parent.ExtraFiles = []*os.File{lease}
	}
	var stderr strings.Builder
	parent.Stderr = &stderr
	input, err := parent.StdinPipe()
	require.NoError(t, err)
	output, err := parent.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, parent.Start())
	t.Cleanup(func() {
		_ = parent.Process.Kill()
		_ = parent.Wait()
		_ = input.Close()
		_ = os.WriteFile(filepath.Join(control, "stop"), nil, 0o600)
	})
	ready := make(chan string, 2)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			ready <- scanner.Text()
		}
	}()
	waitLine := func(want string) {
		select {
		case line := <-ready:
			require.Equal(t, want, line)
		case <-time.After(15 * time.Second):
			t.Fatalf("server did not report %s", want)
		}
	}
	if retry {
		waitLine("UNLEASED")
		require.NoError(t, startupLock.Close())
		_, err = input.Write([]byte{1})
		require.NoError(t, err)
	}
	waitLine("READY")
	require.Eventually(t, func() bool { _, err := os.Stat(filepath.Join(control, "ready")); return err == nil }, 15*time.Second, 10*time.Millisecond)
	require.NoError(t, lease.Close())
	require.NoError(t, input.Close())
	require.NoError(t, parent.Wait())
	if retry {
		require.Equal(t, 1, strings.Count(stderr.String(), "could not be leased"), "repeat failures must diagnose only once")
	}
	probe, err := os.OpenFile(path+".lease", os.O_RDWR, 0) // #nosec G304,G703 -- disposable image lease.
	require.NoError(t, err)
	defer func() { _ = probe.Close() }()
	require.ErrorIs(t, unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB), unix.EWOULDBLOCK, "worker must pin image after server exit")
	require.NoError(t, os.WriteFile(filepath.Join(control, "stop"), nil, 0o600))
	require.Eventually(t, func() bool { return unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB) == nil }, 15*time.Second, 10*time.Millisecond)
}

func selfLeaseWorkerHelper() {
	if os.Getenv("AL_TEST_SELF_LEASE_WORKER") == "" {
		return
	}
	gate := os.NewFile(3, "gate")
	err := RunWorker(os.Getenv("AL_TEST_SELF_LEASE_WORKER"), "lease-test", gate)
	// There is deliberately no prepared request: observe the retained lease
	// after authorization and before any provider can be admitted.
	if err == nil {
		fmt.Fprintln(os.Stderr, "unprepared worker unexpectedly succeeded")
		os.Exit(2)
	}
	if os.Getenv("AL_TEST_UNAUTHORIZED_WORKER") != "" {
		if imagelease.Current() != nil || !strings.Contains(err.Error(), "not authorized") {
			fmt.Fprintln(os.Stderr, "unauthorized worker acquired a lease")
			os.Exit(2)
		}
		fmt.Println("UNAUTHORIZED")
		os.Exit(0)
	}
	if imagelease.Current() == nil {
		fmt.Fprintln(os.Stderr, "authorized worker did not acquire its image lease")
		os.Exit(2)
	}
	fmt.Println("READY")
	_, _ = io.Copy(io.Discard, os.Stdin)
	os.Exit(0)
}

func TestWorkerWithoutInheritedLeaseLeasesOwnImage(t *testing.T) {
	cache := t.TempDir()
	self, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(self) // #nosec G304 -- own test executable.
	require.NoError(t, err)
	dir := filepath.Join(cache, "images", fmt.Sprintf("%x", sha256.Sum256(data)))
	require.NoError(t, os.MkdirAll(dir, 0o700)) // #nosec G703 -- disposable test cache.
	path := filepath.Join(dir, "al")
	require.NoError(t, os.WriteFile(path, data, 0o700)) // #nosec G306,G703 -- disposable executable.
	gate, writer, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = gate.Close(); _ = writer.Close() }()
	cmd := exec.Command(path, workerCommand)
	cmd.ExtraFiles = []*os.File{gate} // fd 4 is deliberately absent.
	cmd.Env = append(os.Environ(), "AL_TEST_SELF_LEASE_WORKER="+t.TempDir(), "AL_CACHE_DIR="+cache, "HOME="+t.TempDir())
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer func() { _ = input.Close() }()
	output, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	_, err = writer.Write([]byte{1})
	require.NoError(t, err)
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		require.Equal(t, "READY\n", line)
	case <-time.After(15 * time.Second):
		t.Fatal("authorized worker did not lease its own image")
	}
	probe, err := os.OpenFile(path+".lease", os.O_RDWR, 0) // #nosec G304,G703 -- disposable lease.
	require.NoError(t, err)
	defer func() { _ = probe.Close() }()
	require.ErrorIs(t, unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB), unix.EWOULDBLOCK)
	require.NoError(t, cmd.Process.Kill())
	require.Error(t, cmd.Wait())
	require.NoError(t, unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB))
}

func TestUnauthorizedWorkerDoesNotAcquireOrRefreshLease(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing lease=%t", existing), func(t *testing.T) {
			cache := t.TempDir()
			self, err := os.Executable()
			require.NoError(t, err)
			data, err := os.ReadFile(self) // #nosec G304 -- own test binary.
			require.NoError(t, err)
			dir := filepath.Join(cache, "images", fmt.Sprintf("%x", sha256.Sum256(data)))
			require.NoError(t, os.MkdirAll(dir, 0o700)) // #nosec G703 -- disposable image cache.
			path := filepath.Join(dir, "al")
			require.NoError(t, os.WriteFile(path, data, 0o700)) // #nosec G306,G703 -- disposable executable.
			var before time.Time
			if existing {
				require.NoError(t, os.WriteFile(path+".lease", nil, 0o600)) // #nosec G703 -- disposable image-cache fixture.
				old := time.Now().Add(-48 * time.Hour)
				require.NoError(t, os.Chtimes(path+".lease", old, old))
				i, err := os.Stat(path + ".lease") // #nosec G703 -- disposable image-cache fixture.
				require.NoError(t, err)
				before = i.ModTime()
			}
			gate, writer, err := os.Pipe()
			require.NoError(t, err)
			defer func() { _ = gate.Close(); _ = writer.Close() }()
			_, err = writer.Write([]byte{0})
			require.NoError(t, err)
			cmd := exec.Command(path, workerCommand)
			cmd.ExtraFiles = []*os.File{gate}
			cmd.Env = append(os.Environ(), "AL_TEST_SELF_LEASE_WORKER="+t.TempDir(), "AL_TEST_UNAUTHORIZED_WORKER=1", "AL_CACHE_DIR="+cache, "HOME="+t.TempDir())
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
			require.Equal(t, "UNAUTHORIZED\n", string(out))
			require.NoFileExists(t, path+".lock")
			if existing {
				i, err := os.Stat(path + ".lease") // #nosec G703 -- disposable image-cache fixture.
				require.NoError(t, err)
				require.Equal(t, before, i.ModTime())
			} else {
				require.NoFileExists(t, path+".lease")
			}
		})
	}
}
