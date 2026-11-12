package versiondispatch

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/conn-castle/agent-layer/internal/imagelease"
)

// imageHelper runs before testing's flag parser, including for the real worker
// argv spelling that triggers fd 4 adoption in imagelease.init.
func imageHelper() {
	mode := os.Getenv("AL_TEST_IMAGE_HELPER")
	if mode == "" {
		return
	}
	fail := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	var held *os.File
	switch mode {
	case "hold":
		f, err := os.OpenFile(os.Getenv("AL_TEST_LEASE"), os.O_RDWR, 0) // #nosec G304,G703 -- disposable test cache.
		fail(err)
		fail(unix.Flock(int(f.Fd()), unix.LOCK_SH))
		held = f
	case "crash-write":
		sys := &testSystem{RenameFunc: func(from, to string) error {
			if err := os.Rename(from, to); err != nil { // #nosec G703 -- disposable image write under al.lock.
				return err
			}
			// Stop immediately after publication, before writeImage returns.
			fmt.Println("READY")
			_, _ = io.Copy(io.Discard, os.Stdin)
			os.Exit(0)
			return nil
		}}
		_, _, err := ensureLeasedImage(context.Background(), sys, filepath.Join(os.Getenv(EnvCacheDir), imagesDirName), os.Getenv("AL_TEST_IMAGE_SOURCE"))
		fail(err)
		fail(fmt.Errorf("image writer did not pause after publication"))
	case "exec":
		self, err := os.Executable()
		fail(err)
		path, lease, err := ensureLeasedImage(context.Background(), RealSystem{}, filepath.Join(os.Getenv(EnvCacheDir), "images"), self)
		fail(err)
		env, _, err := imagelease.ExecEnv(lease, append(withoutEnv(os.Environ(), "AL_TEST_IMAGE_HELPER"), "AL_TEST_IMAGE_HELPER=server"))
		fail(err)
		fail(unix.Exec(path, []string{path}, env))
	case "direct", "direct-wait":
		if mode == "direct-wait" {
			fmt.Println("MAPPED")
			var token [1]byte
			_, err := io.ReadFull(os.Stdin, token[:])
			fail(err)
		}
		// Use the real MCP entry point, including selections that do not enter
		// handOffToImage (dev/current and already-active shim markers).
		fail(MaybeExecMCPServer(context.Background(), []string{os.Args[0], "dispatch", "mcp-server"}, "dev", os.Getenv(EnvCacheDir), os.Stderr))
		LeaseRunningImage() // Repeated startup calls must not repeat diagnostics.
		if (imagelease.Current() != nil) != (mode == "direct") {
			fail(fmt.Errorf("direct startup lease = %v", imagelease.Current()))
		}
	case "direct-spelling", "direct-unsafe":
		// Linux resolves executable spelling in os.Executable; supply the actual
		// invocation spelling to exercise the same path logic Darwin receives.
		err := leaseRunningImage(os.Args[0])
		if mode == "direct-spelling" {
			fail(err)
			if imagelease.Current() == nil {
				fail(fmt.Errorf("aliased image did not acquire a lease"))
			}
		} else if err == nil || imagelease.Current() != nil {
			fail(fmt.Errorf("unsafe layout was accepted"))
		}
	case "direct-unrelated":
		LeaseRunningImage()
		LeaseRunningImage()
		if imagelease.Current() != nil {
			fail(fmt.Errorf("unrelated image acquired a lease"))
		}
	case "server", "worker", "absent":
		lease := imagelease.Current()
		if (lease != nil) != (mode != "absent") {
			fail(fmt.Errorf("mode %s: lease = %v", mode, lease))
		}
		if os.Getenv(imagelease.EnvFD) != "" {
			fail(fmt.Errorf("handoff environment was not consumed"))
		}
		if lease != nil {
			flags, err := unix.FcntlInt(lease.Fd(), unix.F_GETFD, 0)
			fail(err)
			if flags&unix.FD_CLOEXEC == 0 {
				fail(fmt.Errorf("lease is not CLOEXEC"))
			}
		}
		if mode == "server" {
			self, err := os.Executable()
			fail(err)
			cmd := exec.Command(self)
			cmd.Env = append(withoutEnv(os.Environ(), "AL_TEST_IMAGE_HELPER"), "AL_TEST_IMAGE_HELPER=grandchild")
			cmd.Env = append(cmd.Env, "AL_TEST_PARENT_LEASE_FD="+strconv.Itoa(int(lease.Fd())))
			out, err := cmd.CombinedOutput()
			if err != nil {
				fail(fmt.Errorf("grandchild: %s: %w", out, err))
			}
		}
	case "grandchild":
		if imagelease.Current() != nil || os.Getenv(imagelease.EnvFD) != "" {
			fail(fmt.Errorf("lease state leaked to grandchild"))
		}
		self, err := os.Executable()
		fail(err)
		want, err := os.Stat(self + ".lease")
		fail(err)
		fd, err := strconv.Atoi(os.Getenv("AL_TEST_PARENT_LEASE_FD"))
		fail(err)
		var got syscall.Stat_t
		if syscall.Fstat(fd, &got) == nil {
			st := want.Sys().(*syscall.Stat_t)
			if got.Dev == st.Dev && got.Ino == st.Ino {
				fail(fmt.Errorf("lease descriptor %d leaked", fd))
			}
		}
		os.Exit(0)
	default:
		fail(fmt.Errorf("unknown image helper %s", mode))
	}
	fmt.Println("READY")
	_, _ = io.Copy(io.Discard, os.Stdin)
	runtime.KeepAlive(held)
	os.Exit(0)
}

func copyTestImage(t *testing.T, cache string) string {
	t.Helper()
	self, err := os.Executable()
	require.NoError(t, err)
	sum, err := fileSHA256(self)
	require.NoError(t, err)
	dir := filepath.Join(cache, imagesDirName, sum)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	data, err := os.ReadFile(self) // #nosec G304 -- own test executable.
	require.NoError(t, err)
	path := filepath.Join(dir, imageName)
	require.NoError(t, os.WriteFile(path, data, 0o700)) // #nosec G306,G703 -- disposable test executable.
	return path
}

func TestDirectImageStartupLeasesExistingEntry(t *testing.T) {
	for _, mode := range []string{"no inherited lease", "rejected inherited lease"} {
		t.Run(mode, func(t *testing.T) {
			cache := t.TempDir()
			path := copyTestImage(t, cache)
			dir := filepath.Dir(path)
			require.NoFileExists(t, path+".lease")
			var files []*os.File
			env := []string{EnvCacheDir + "=" + cache, "HOME=" + t.TempDir(), EnvShimActive + "=1"}
			if mode == "rejected inherited lease" {
				foreign, err := os.CreateTemp(t.TempDir(), "foreign")
				require.NoError(t, err)
				defer func() { _ = foreign.Close() }()
				files = []*os.File{foreign}
				env = append(env, imagelease.EnvFD+"=3")
			}
			helper, _ := startImageHelper(t, path, "direct", nil, files, env...)
			oldImage(t, dir)
			pruneImages(filepath.Dir(dir), "")
			require.DirExists(t, dir, "direct startup must pin its existing image")
			require.NoError(t, helper.Process.Kill())
			require.Error(t, helper.Wait())
			pruneImages(filepath.Dir(dir), "")
			require.NoDirExists(t, dir)
		})
	}
}

func TestDirectImageStartupNeverRecreatesPrunedEntry(t *testing.T) {
	for _, mode := range []string{"removed", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			cache := t.TempDir()
			path := copyTestImage(t, cache)
			dir := filepath.Dir(path)
			cmd := exec.Command(path)
			cmd.Env = append(os.Environ(), "AL_TEST_IMAGE_HELPER=direct-wait", EnvCacheDir+"="+cache, "HOME="+t.TempDir())
			input, err := cmd.StdinPipe()
			require.NoError(t, err)
			output, err := cmd.StdoutPipe()
			require.NoError(t, err)
			var stderr strings.Builder
			cmd.Stderr = &stderr
			require.NoError(t, cmd.Start())
			t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = input.Close() })
			reader := bufio.NewReader(output)
			line, err := reader.ReadString('\n')
			require.NoError(t, err)
			require.Equal(t, "MAPPED\n", line)
			trash := filepath.Join(filepath.Dir(dir), ".prune-"+filepath.Base(dir)+"-0123456789abcdef")
			require.NoError(t, os.Rename(dir, trash))
			require.NoError(t, os.RemoveAll(trash))
			if mode == "replaced" {
				require.NoError(t, os.Mkdir(dir, 0o700))
				require.NoError(t, os.WriteFile(path, []byte("replacement"), 0o700)) // #nosec G306 -- synthetic replacement.
			}
			_, err = input.Write([]byte{1})
			require.NoError(t, err)
			line, err = reader.ReadString('\n')
			require.NoError(t, err)
			require.Equal(t, "READY\n", line, "process must continue without a lease")
			if mode == "removed" {
				require.NoDirExists(t, dir)
			} else {
				data, err := os.ReadFile(path) // #nosec G304 -- disposable replacement.
				require.NoError(t, err)
				require.Equal(t, "replacement", string(data))
				require.NoFileExists(t, path+".lock")
				require.NoFileExists(t, path+".lease")
			}
			require.NoError(t, input.Close())
			require.NoError(t, cmd.Wait())
			require.LessOrEqual(t, strings.Count(stderr.String(), "could not be leased"), 1)
		})
	}
}

func syntheticImage(t *testing.T, images, key string) string {
	t.Helper()
	dir := filepath.Join(images, strings.Repeat(key, 64))
	require.NoError(t, os.MkdirAll(dir, 0o700))
	for _, name := range []string{"al", "al.lock", "al.lease"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o600))
	}
	oldImage(t, dir)
	return dir
}

func oldImage(t *testing.T, dir string) {
	t.Helper()
	old := time.Now().Add(-imagePruneGrace - time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "al.lease"), old, old))
}

func startImageHelper(t *testing.T, executable, mode string, args []string, files []*os.File, env ...string) (*exec.Cmd, io.WriteCloser) {
	return startImageHelperInDir(t, executable, mode, args, files, "", env...)
}

func startImageHelperInDir(t *testing.T, executable, mode string, args []string, files []*os.File, dir string, env ...string) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	cmd := exec.Command(executable, args...)
	cmd.Dir = dir
	cmd.Env = append(withoutEnv(os.Environ(), "AL_TEST_IMAGE_HELPER"), "AL_TEST_IMAGE_HELPER="+mode)
	cmd.Env = append(cmd.Env, env...)
	cmd.ExtraFiles = files
	cmd.Stderr = os.Stderr
	input, err := cmd.StdinPipe()
	require.NoError(t, err)
	output, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = input.Close() })
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		require.Equal(t, "READY\n", line)
	case <-time.After(15 * time.Second):
		t.Fatal("image helper did not become ready")
	}
	return cmd, input
}

func TestImagePruneProcessLeasesAndSIGKILL(t *testing.T) {
	images := filepath.Join(t.TempDir(), "images")
	dir := syntheticImage(t, images, "a")
	self, err := os.Executable()
	require.NoError(t, err)
	a, _ := startImageHelper(t, self, "hold", nil, nil, "AL_TEST_LEASE="+filepath.Join(dir, "al.lease"))
	b, _ := startImageHelper(t, self, "hold", nil, nil, "AL_TEST_LEASE="+filepath.Join(dir, "al.lease"))
	pruneImages(images, "")
	require.DirExists(t, dir)
	require.NoError(t, a.Process.Kill())
	require.Error(t, a.Wait())
	pruneImages(images, "")
	require.DirExists(t, dir, "second server must keep the entry pinned")
	require.NoError(t, b.Process.Kill())
	require.Error(t, b.Wait())
	pruneImages(images, "")
	require.NoDirExists(t, dir, "kernel must release leases on SIGKILL")
}

func TestImagePruneSafetyAndTrash(t *testing.T) {
	images := filepath.Join(t.TempDir(), "images")
	own := syntheticImage(t, images, "a")
	legacy := syntheticImage(t, images, "b")
	require.NoError(t, os.Remove(filepath.Join(legacy, "al.lease")))
	recent := syntheticImage(t, images, "c")
	require.NoError(t, os.Chtimes(filepath.Join(recent, "al.lease"), time.Now(), time.Now()))
	extra := syntheticImage(t, images, "d")
	require.NoError(t, os.WriteFile(filepath.Join(extra, "user-data"), []byte("keep"), 0o600))
	outside := syntheticImage(t, filepath.Join(t.TempDir(), "outside"), "e")
	link := filepath.Join(images, strings.Repeat("e", 64))
	require.NoError(t, os.Symlink(outside, link))
	nonhex := filepath.Join(images, "not-a-sha")
	require.NoError(t, os.Mkdir(nonhex, 0o700))
	uppercase := syntheticImage(t, images, "A")
	symlinkFile := syntheticImage(t, images, "f")
	require.NoError(t, os.Symlink(filepath.Join(outside, "al"), filepath.Join(symlinkFile, "al.tmp-link")))
	trash := syntheticImage(t, images, "0")
	trashName := filepath.Join(images, ".prune-"+filepath.Base(trash)+"-0123456789abcdef")
	require.NoError(t, os.Rename(trash, trashName))
	partial := syntheticImage(t, images, "4")
	partialName := filepath.Join(images, ".prune-"+filepath.Base(partial)+"-0123456789abcdef")
	require.NoError(t, os.Rename(partial, partialName))
	require.NoError(t, os.Remove(filepath.Join(partialName, "al.lock")))
	require.NoError(t, os.Remove(filepath.Join(partialName, "al.lease")))
	eligible := syntheticImage(t, images, "1")
	busy := syntheticImage(t, images, "2")
	lock, err := openImageFile(filepath.Join(busy, "al.lock"), false)
	require.NoError(t, err)
	defer func() { _ = lock.Close() }()
	require.NoError(t, unix.Flock(int(lock.Fd()), unix.LOCK_EX))
	pruneImages(images, own)
	for _, dir := range []string{own, legacy, recent, extra, outside, nonhex, uppercase, symlinkFile, busy} {
		require.DirExists(t, dir)
	}
	target, err := os.Readlink(link)
	require.NoError(t, err)
	require.Equal(t, outside, target)
	require.NoDirExists(t, trashName)
	require.NoDirExists(t, partialName, "recover a crash midway through trash removal")
	require.NoDirExists(t, eligible)
	// A symlinked images directory must never authorize deletion of its target.
	alias := filepath.Join(t.TempDir(), "images")
	require.NoError(t, os.Symlink(images, alias))
	pruneImages(alias, "")
	require.DirExists(t, own)
	if os.Geteuid() == 0 {
		foreign := syntheticImage(t, images, "3")
		require.NoError(t, os.Chown(foreign, 12345, -1))
		pruneImages(images, own)
		require.DirExists(t, foreign)
	}
}

func TestImageHandoffPrunesAfterSecuringLease(t *testing.T) {
	f := newImageHandoffFixture(t, "1.2.3")
	images := filepath.Join(f.cache, "images")
	stale := syntheticImage(t, images, "a")
	sys := f.system()
	sys.ExecBinaryFunc = func(path string, _ []string, _ []string) error {
		require.NoDirExists(t, stale, "handoff must trigger maintenance")
		probe, err := openImageFile(path+".lease", false)
		require.NoError(t, err)
		defer func() { _ = probe.Close() }()
		require.ErrorIs(t, unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB), unix.EWOULDBLOCK)
		info, err := probe.Stat()
		require.NoError(t, err)
		require.WithinDuration(t, time.Now(), info.ModTime(), time.Second)
		return nil
	}
	err := handOffToImage(context.Background(), sys, execOptions{
		runningImage: func() (string, string, error) { return f.source, f.source, nil },
	}, []string{f.source, "dispatch", "mcp-server"}, "1.2.3", false)
	require.ErrorIs(t, err, ErrDispatched)
}

func TestImageLockRetriesDetachedInode(t *testing.T) {
	images := filepath.Join(t.TempDir(), "images")
	source := filepath.Join(t.TempDir(), "source")
	require.NoError(t, os.WriteFile(source, []byte("complete image"), 0o700)) // #nosec G306 -- synthetic executable.
	sum, err := fileSHA256(source)
	require.NoError(t, err)
	dir := filepath.Join(images, sum)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	held, err := openImageFile(filepath.Join(dir, "al.lock"), true)
	require.NoError(t, err)
	defer func() { _ = held.Close() }()
	require.NoError(t, unix.Flock(int(held.Fd()), unix.LOCK_EX))
	stale, err := held.Stat()
	require.NoError(t, err)
	var once sync.Once
	opened := make(chan struct{})
	sys := &testSystem{FlockFunc: func(fd, how int) error {
		if how&unix.LOCK_EX != 0 {
			once.Do(func() { close(opened) })
		}
		return unix.Flock(fd, how)
	}}
	type result struct {
		path  string
		lease *os.File
		err   error
	}
	done := make(chan result, 1)
	go func() {
		path, lease, err := ensureLeasedImage(context.Background(), sys, images, source)
		done <- result{path, lease, err}
	}()
	<-opened // The waiter has opened the old lock while our pruner holds it.
	require.NoError(t, os.Rename(dir, filepath.Join(images, ".detached")))
	require.NoError(t, held.Close())
	var r result
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("waiter did not retry")
	}
	require.NoError(t, r.err)
	path, lease := r.path, r.lease
	defer func() { _ = lease.Close() }()

	got, err := os.Lstat(filepath.Join(dir, "al.lock"))
	require.NoError(t, err)
	require.False(t, os.SameFile(stale, got), "must retry using the live path's lock")
	require.NoError(t, verifyChecksum(path, sum))
	require.True(t, samePath(lease, path+".lease"))
}

func TestImageEnsureRacesPrune(t *testing.T) {
	images := filepath.Join(t.TempDir(), "images")
	source := filepath.Join(t.TempDir(), "source")
	require.NoError(t, os.WriteFile(source, []byte("verified image bytes"), 0o700)) // #nosec G306 -- synthetic executable.
	sum, err := fileSHA256(source)
	require.NoError(t, err)
	for range 100 {
		path, lease, err := ensureLeasedImage(context.Background(), RealSystem{}, images, source)
		require.NoError(t, err)
		require.NoError(t, lease.Close())
		oldImage(t, filepath.Dir(path))
		var wg sync.WaitGroup
		wg.Go(func() { pruneImages(images, "") })
		path, lease, err = ensureLeasedImage(context.Background(), RealSystem{}, images, source)
		wg.Wait()
		require.NoError(t, err)
		require.NoError(t, verifyChecksum(path, sum))
		require.True(t, samePath(lease, path+".lease"))
		require.NoError(t, lease.Close())
	}
}

func TestImageLeaseAcrossExecAndWorkerInheritance(t *testing.T) {
	cache := t.TempDir()
	self, err := os.Executable()
	require.NoError(t, err)
	server, _ := startImageHelper(t, self, "exec", nil, nil, EnvCacheDir+"="+cache)
	sum, err := fileSHA256(self)
	require.NoError(t, err)
	images := filepath.Join(cache, "images")
	dir := filepath.Join(images, sum)
	path := filepath.Join(dir, "al")
	oldImage(t, dir)
	pruneImages(images, "")
	require.DirExists(t, dir, "exec must carry the lease without a gap")
	// Independently acquire a lease as a launching server would. The child gets
	// the same OFD in fd 4, then survives both the server and this reference.
	_, parentLease, err := ensureLeasedImage(context.Background(), RealSystem{}, images, self)
	require.NoError(t, err)
	gate, gateWrite, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = gate.Close(); _ = gateWrite.Close() }()
	worker, _ := startImageHelper(t, path, "worker", []string{"__dispatch-worker"}, []*os.File{gate, parentLease})
	require.NoError(t, parentLease.Close())
	require.NoError(t, server.Process.Kill())
	require.Error(t, server.Wait())
	oldImage(t, dir)
	pruneImages(images, "")
	require.DirExists(t, dir, "worker must retain the inherited lease")
	for _, mode := range []string{"missing", "foreign"} {
		files := []*os.File{gate}
		if mode == "foreign" {
			files = append(files, gateWrite)
		}
		child, input := startImageHelper(t, path, "absent", []string{"__dispatch-worker"}, files)
		require.NoError(t, input.Close())
		require.NoError(t, child.Wait())
	}
	require.NoError(t, worker.Process.Kill())
	require.Error(t, worker.Wait())
	pruneImages(images, "")
	require.NoDirExists(t, dir)
}
