package imagelease

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestMain(m *testing.M) {
	if os.Getenv("AL_TEST_BUSY_LEASE") != "" {
		var stat unix.Stat_t
		if Current() != nil || unix.Fstat(4, &stat) != unix.EBADF || os.Getenv(EnvFD) != "" {
			fmt.Fprintln(os.Stderr, "failed adoption did not close the proven lease")
			os.Exit(2)
		}
		var gate [1]byte
		if n, err := unix.Read(3, gate[:]); err != nil || n != 1 || gate[0] != 'g' {
			fmt.Fprintln(os.Stderr, "failed lease adoption damaged gate")
			os.Exit(2)
		}
		os.Exit(0)
	}
	if os.Getenv("AL_TEST_BORROWED_FD") != "" {
		if err := checkBorrowedDescriptors(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestContendedLeaseAdoptionClosesOwnedDescriptor(t *testing.T) {
	self, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(self) // #nosec G304 -- own test binary.
	require.NoError(t, err)
	cache := t.TempDir()
	dir := filepath.Join(cache, "images", fmt.Sprintf("%x", sha256.Sum256(data)))
	require.NoError(t, os.MkdirAll(dir, 0o700)) // #nosec G703 -- disposable cache.
	path := filepath.Join(dir, "al")
	require.NoError(t, os.WriteFile(path, data, 0o700))                     // #nosec G306,G703 -- disposable executable.
	holder, err := os.OpenFile(path+".lease", os.O_CREATE|os.O_RDWR, 0o600) // #nosec G304,G703 -- disposable lease.
	require.NoError(t, err)
	defer func() { _ = holder.Close() }()
	require.NoError(t, unix.Flock(int(holder.Fd()), unix.LOCK_EX|unix.LOCK_NB))
	// A separate open description cannot acquire SH while holder has EX.
	candidate, err := os.OpenFile(path+".lease", os.O_RDWR, 0) // #nosec G304,G703 -- disposable lease.
	require.NoError(t, err)
	defer func() { _ = candidate.Close() }()
	gate, writer, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = gate.Close(); _ = writer.Close() }()
	_, err = writer.Write([]byte{'g'})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "__dispatch-worker")
	cmd.ExtraFiles = []*os.File{gate, candidate}
	cmd.Env = append(os.Environ(), "AL_TEST_BUSY_LEASE=1", EnvFD+"=4", "AL_CACHE_DIR="+cache, "HOME="+t.TempDir())
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

// Use only raw fd operations here too: wrapping an unrelated descriptor in an
// os.File would itself introduce ownership/finalizer behavior into the proof.
func checkBorrowedDescriptors() error {
	if Current() != nil || os.Getenv(EnvFD) != "" {
		return fmt.Errorf("foreign descriptor was adopted or handoff variable survived")
	}
	for _, fd := range []int{3, 4} {
		flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
		if err != nil || flags&unix.FD_CLOEXEC != 0 {
			return fmt.Errorf("descriptor %d flags changed: %d, %v", fd, flags, err)
		}
	}
	offset, err := unix.Seek(4, 0, 1)
	if err != nil || offset != 3 {
		return fmt.Errorf("foreign file offset changed: %d, %v", offset, err)
	}
	var full [11]byte
	n, err := unix.Pread(4, full[:], 0)
	if err != nil || string(full[:n]) != "xxxsentinel" {
		return fmt.Errorf("foreign file contents changed: %q, %v", full[:], err)
	}
	var data [8]byte
	n, err = unix.Read(4, data[:])
	if err != nil || string(data[:n]) != "sentinel" {
		return fmt.Errorf("foreign file contents unavailable: %q, %v", data[:], err)
	}
	var gate [1]byte
	n, err = unix.Read(3, gate[:])
	if err != nil || n != 1 || gate[0] != 'g' {
		return fmt.Errorf("authorization gate changed: %q, %v", gate, err)
	}
	return nil
}

func TestRejectBorrowedDescriptorsWithoutMutation(t *testing.T) {
	self, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(self) // #nosec G304 -- own test binary.
	require.NoError(t, err)
	cache := t.TempDir()
	dir := filepath.Join(cache, "images", fmt.Sprintf("%x", sha256.Sum256(data)))
	require.NoError(t, os.MkdirAll(dir, 0o700)) // #nosec G703 -- disposable test cache.
	path := filepath.Join(dir, "al")
	require.NoError(t, os.WriteFile(path, data, 0o700))         // #nosec G306,G703 -- disposable test executable.
	require.NoError(t, os.WriteFile(path+".lease", nil, 0o600)) // #nosec G703 -- disposable test lease.
	for _, mode := range []string{"forged handoff variable", "foreign worker fd 4"} {
		t.Run(mode, func(t *testing.T) {
			foreign, err := os.CreateTemp(t.TempDir(), "foreign")
			require.NoError(t, err)
			defer func() { _ = foreign.Close() }()
			_, err = foreign.WriteString("xxxsentinel")
			require.NoError(t, err)
			_, err = foreign.Seek(3, 0)
			require.NoError(t, err)
			gate, writer, err := os.Pipe()
			require.NoError(t, err)
			defer func() { _ = gate.Close(); _ = writer.Close() }()
			_, err = writer.Write([]byte{'g'})
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, path)
			cmd.Env = append(os.Environ(), "AL_TEST_BORROWED_FD=1", "AL_CACHE_DIR="+cache, "HOME="+t.TempDir())
			cmd.ExtraFiles = []*os.File{gate, foreign}
			if mode == "forged handoff variable" {
				cmd.Env = append(cmd.Env, EnvFD+"=4")
			} else {
				cmd.Args = append(cmd.Args, "__dispatch-worker")
			}
			output, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", output)
		})
	}
}
