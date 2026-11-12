package versiondispatch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestImagePruneSamplesBeyondRetainedPrefix(t *testing.T) {
	images := filepath.Join(t.TempDir(), imagesDirName)
	const count = imagePruneSampleSize * 16
	for i := range count {
		dir := filepath.Join(images, fmt.Sprintf("%064x", i))
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, imageName), []byte("legacy"), 0o600))
	}
	readNames := func() []string {
		f, err := os.Open(images) // #nosec G304 -- disposable cache directory.
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		names, err := f.Readdirnames(-1)
		require.NoError(t, err)
		return names
	}
	names := readNames()
	require.Len(t, names, count)
	// Select the filesystem's actual last entry; creating files inside it does
	// not change the parent directory's enumeration order.
	last := names[len(names)-1]
	eligible := filepath.Join(images, last)
	for _, name := range []string{"al.lock", "al.lease"} {
		require.NoError(t, os.WriteFile(filepath.Join(eligible, name), nil, 0o600))
	}
	oldImage(t, eligible)
	require.Equal(t, names, readNames())
	// A first independent draw keeps the retained prefix. A later draw selects
	// the last entry into the first candidate slot, without timing assertions.
	pruneImagesWithRandom(images, "", func(n int) int { return n - 1 })
	require.DirExists(t, eligible)
	pruneImagesWithRandom(images, "", func(int) int { return 0 })
	require.NoDirExists(t, eligible)
	require.Len(t, readNames(), count-1, "legacy prefix must remain intact")
}

func TestImageWriteFailureClosesEarlyLease(t *testing.T) {
	images := filepath.Join(t.TempDir(), imagesDirName)
	source := filepath.Join(t.TempDir(), "source")
	require.NoError(t, os.WriteFile(source, []byte("image bytes"), 0o600))
	sum, err := fileSHA256(source)
	require.NoError(t, err)
	dir := filepath.Join(images, sum)
	sys := &testSystem{CreateTempFunc: func(dir, _ string) (*os.File, error) {
		probe, err := openImageFile(filepath.Join(dir, "al.lease"), false)
		require.NoError(t, err, "lease must exist before writeImage creates a temporary file")
		defer func() { _ = probe.Close() }()
		require.ErrorIs(t, unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB), unix.EWOULDBLOCK)
		return nil, errors.New("injected write failure")
	}}
	_, lease, err := ensureLeasedImage(context.Background(), sys, images, source)
	require.ErrorContains(t, err, "injected write failure")
	require.Nil(t, lease)
	probe, err := openImageFile(filepath.Join(dir, "al.lease"), false)
	require.NoError(t, err)
	require.NoError(t, unix.Flock(int(probe.Fd()), unix.LOCK_EX|unix.LOCK_NB), "error path must close its lease")
	require.NoError(t, probe.Close())
	old := time.Now().Add(-imagePruneGrace - time.Hour)
	for _, path := range []string{filepath.Join(dir, "al.lock"), filepath.Join(dir, "al.lease"), dir} {
		require.NoError(t, os.Chtimes(path, old, old))
	}
	pruneImages(images, "")
	require.NoDirExists(t, dir)
}

func TestImageWriterKilledAfterPublicationRemainsPrunable(t *testing.T) {
	cache := t.TempDir()
	source := filepath.Join(t.TempDir(), "source")
	require.NoError(t, os.WriteFile(source, []byte("published image bytes"), 0o600))
	sum, err := fileSHA256(source)
	require.NoError(t, err)
	dir := filepath.Join(cache, imagesDirName, sum)
	self, err := os.Executable()
	require.NoError(t, err)
	helper, _ := startImageHelper(t, self, "crash-write", nil, nil, EnvCacheDir+"="+cache, "HOME="+t.TempDir(), "AL_TEST_IMAGE_SOURCE="+source)
	require.NoError(t, verifyChecksum(filepath.Join(dir, imageName), sum))
	require.FileExists(t, filepath.Join(dir, "al.lease"), "publication must never precede the lease marker")
	oldImage(t, dir)
	pruneImages(filepath.Dir(dir), "")
	require.DirExists(t, dir, "writer still holds its creation lock and lease")
	require.NoError(t, helper.Process.Kill())
	require.Error(t, helper.Wait())
	pruneImages(filepath.Dir(dir), "")
	require.NoDirExists(t, dir, "a killed lease-aware writer must not leave an immortal legacy entry")
}

func TestRunningImageRootSpellings(t *testing.T) {
	for _, mode := range []string{"aliased cache root", "aliased executable root", "relative cache root"} {
		t.Run(mode, func(t *testing.T) {
			base := t.TempDir()
			cache := filepath.Join(base, "cache")
			path := copyTestImage(t, cache)
			physicalCache, err := filepath.EvalSymlinks(cache)
			require.NoError(t, err)
			physicalPath, err := filepath.EvalSymlinks(path)
			require.NoError(t, err)
			alias := filepath.Join(base, "alias")
			require.NoError(t, os.Symlink(physicalCache, alias))
			executable, configured, helperMode := physicalPath, physicalCache, "direct"
			switch mode {
			case "aliased cache root":
				configured = alias
			case "aliased executable root":
				executable = filepath.Join(alias, imagesDirName, filepath.Base(filepath.Dir(path)), imageName)
				helperMode = "direct-spelling"
			case "relative cache root":
				configured = "cache"
			}
			helper, _ := startImageHelperInDir(t, executable, helperMode, nil, nil, base,
				EnvCacheDir+"="+configured, "HOME="+t.TempDir())
			oldImage(t, filepath.Dir(path))
			pruneImages(filepath.Join(cache, imagesDirName), "")
			require.FileExists(t, path, "a legitimate root spelling must retain its image")
			require.NoError(t, helper.Process.Kill())
			require.Error(t, helper.Wait())
			pruneImages(filepath.Join(cache, imagesDirName), "")
			require.NoDirExists(t, filepath.Dir(path))
		})
	}
}

func TestRunningImageUnrelatedRootStaysQuiet(t *testing.T) {
	path := copyTestImage(t, t.TempDir())
	for _, configured := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		cmd := exec.Command(path)
		cmd.Env = append(os.Environ(), "AL_TEST_IMAGE_HELPER=direct-unrelated", EnvCacheDir+"="+configured, "HOME="+t.TempDir())
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", out)
		require.Equal(t, "READY\n", string(out), "unrelated shaped executables must not warn")
		require.NoFileExists(t, path+".lease")
	}
}

func TestRunningImageRejectsSymlinkedLayout(t *testing.T) {
	for _, component := range []string{"images", "entry"} {
		t.Run(component, func(t *testing.T) {
			path := copyTestImage(t, t.TempDir())
			cache := t.TempDir()
			images := filepath.Join(cache, imagesDirName)
			if component == "images" {
				require.NoError(t, os.Symlink(filepath.Dir(filepath.Dir(path)), images))
			} else {
				require.NoError(t, os.Mkdir(images, 0o700))
				require.NoError(t, os.Symlink(filepath.Dir(path), filepath.Join(images, filepath.Base(filepath.Dir(path)))))
			}
			invoked := filepath.Join(images, filepath.Base(filepath.Dir(path)), imageName)
			cmd := exec.Command(invoked)
			cmd.Env = append(os.Environ(), "AL_TEST_IMAGE_HELPER=direct-unsafe", EnvCacheDir+"="+cache, "HOME="+t.TempDir())
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
			require.NoFileExists(t, path+".lease")
			require.NoFileExists(t, path+".lock", "rejected aliases must not create files through symlinks")
		})
	}
}

func TestImagePruneIncompleteEntries(t *testing.T) {
	images := filepath.Join(t.TempDir(), imagesDirName)
	old := time.Now().Add(-imagePruneGrace - time.Hour)
	makeEntry := func(key string) string {
		dir := filepath.Join(images, strings.Repeat(key, 64))
		require.NoError(t, os.MkdirAll(dir, 0o700))
		for _, name := range []string{"al.lock", "al.tmp-abandoned"} {
			path := filepath.Join(dir, name)
			require.NoError(t, os.WriteFile(path, []byte("unfinished"), 0o600))
			require.NoError(t, os.Chtimes(path, old, old))
		}
		require.NoError(t, os.Chtimes(dir, old, old))
		return dir
	}
	stale := makeEntry("a")
	busy := makeEntry("b")
	lock, err := openImageFile(filepath.Join(busy, "al.lock"), false)
	require.NoError(t, err)
	defer func() { _ = lock.Close() }()
	require.NoError(t, unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB))
	recentTemp := makeEntry("c")
	require.NoError(t, os.Chtimes(filepath.Join(recentTemp, "al.tmp-abandoned"), time.Now(), time.Now()))
	recentDir := makeEntry("d")
	require.NoError(t, os.Chtimes(recentDir, time.Now(), time.Now()))
	recentLock := makeEntry("e")
	require.NoError(t, os.Chtimes(filepath.Join(recentLock, "al.lock"), time.Now(), time.Now()))
	extra := makeEntry("f")
	require.NoError(t, os.WriteFile(filepath.Join(extra, "user-file"), nil, 0o600))
	require.NoError(t, os.Chtimes(filepath.Join(extra, "user-file"), old, old))
	require.NoError(t, os.Chtimes(extra, old, old))
	symlink := makeEntry("0")
	outside := filepath.Join(t.TempDir(), "outside")
	require.NoError(t, os.WriteFile(outside, []byte("keep"), 0o600))
	require.NoError(t, os.Chtimes(outside, old, old))
	require.NoError(t, os.Symlink(outside, filepath.Join(symlink, "al.tmp-link")))
	require.NoError(t, os.Chtimes(symlink, old, old))
	legacy := syntheticImage(t, images, "1")
	require.NoError(t, os.Remove(filepath.Join(legacy, "al.lease")))
	for _, p := range []string{filepath.Join(legacy, "al"), filepath.Join(legacy, "al.lock"), legacy} {
		require.NoError(t, os.Chtimes(p, old, old))
	}
	pruneImages(images, "")
	require.NoDirExists(t, stale)
	for _, kept := range []string{busy, recentTemp, recentDir, recentLock, extra, symlink, legacy} {
		require.DirExists(t, kept)
	}
	data, err := os.ReadFile(outside) // #nosec G304 -- disposable sentinel outside images/.
	require.NoError(t, err)
	require.Equal(t, "keep", string(data))
	require.NoError(t, lock.Close())
	pruneImages(images, "")
	require.NoDirExists(t, busy, "abandoned writer becomes collectable after lock release")
}
