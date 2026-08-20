//go:build unix

package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestEnsurePrivateDirReportsChmodFailure(t *testing.T) {
	t.Cleanup(func() { privateDirFchmod = unix.Fchmod })
	path := filepath.Join(t.TempDir(), "home")
	require.NoError(t, os.Mkdir(path, 0o700))
	require.NoError(t, os.Chmod(path, 0o755)) // #nosec G302 -- fixture starts too open so the chmod failure path is reachable.

	injected := errors.New("injected")
	privateDirFchmod = func(int, uint32) error { return injected }
	err := EnsurePrivateDir(path)
	require.ErrorIs(t, err, injected)
	require.Contains(t, err.Error(), "restrict")
}
