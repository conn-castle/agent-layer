package versiondispatch

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/conn-castle/agent-layer/internal/executableimage"
	"github.com/conn-castle/agent-layer/internal/imagelease"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/version"
)

const (
	// imagesDirName holds immutable content-addressed copies of running
	// images (<cache root>/images/<sha256>/al). Selected development and pinned
	// MCP servers run from them so replacing the source executable keeps the
	// server's startup code available. Entries are never written to versions/<v>,
	// which holds only checksum-verified downloads.
	imagesDirName        = "images"
	imageName            = "al"
	imageLockWaitTimeout = 30 * time.Second
)

// execOptions selects optional dispatch behavior for one command.
type execOptions struct {
	// runningImage enables the MCP server image handoff. It returns a readable
	// reference to the running image and the pathname this process runs from.
	runningImage func() (source string, path string, err error)
	// sameAs verifies that the selected development path names this image.
	sameAs func(string) error
}

// MaybeExecMCPServer is MaybeExec for `al dispatch mcp-server`. A selected
// development executable or pinned release first re-executes from an immutable
// copy of this image. Any failure keeps running in place.
func MaybeExecMCPServer(ctx context.Context, args []string, currentVersion string, cwd string, stderr io.Writer) error {
	LeaseRunningImage()
	return maybeExecWithOptions(ctx, RealSystem{StderrWriter: stderr}, args, currentVersion, cwd, execOptions{runningImage: runningImage, sameAs: executableimage.SameAs})
}

// runningImage returns a readable reference to this process's image (procfs
// on Linux, otherwise the unchanged executable path) and its pathname.
func runningImage() (string, string, error) {
	source, err := executableimage.Self()
	if err != nil {
		return "", "", err
	}
	path, err := os.Executable()
	return source, path, err
}

// RunningFromImageCache reports whether this process runs from the image cache.
func RunningFromImageCache() bool {
	cacheRoot, err := cacheRootDir(RealSystem{})
	if err != nil {
		return false
	}
	path, err := os.Executable()
	return err == nil && withinDir(filepath.Join(cacheRoot, imagesDirName), path)
}

// wantsImageHandoff reports whether a selection equal to the running version
// should move to an immutable image. AL_SHIM_ACTIVE marks a process that a
// handoff already started, so it never re-executes again.
func wantsImageHandoff(sys System, opts execOptions, source string, current string) bool {
	return opts.runningImage != nil && source != sourceCurrent && !version.IsDev(current) && sys.Getenv(EnvShimActive) == ""
}

// handOffToImage re-executes this process from <cache root>/images/<sha256>/al.
// It returns ErrDispatched after a handoff, a context error when canceled, and
// nil to keep running in place (after one diagnostic on failure).
func handOffToImage(ctx context.Context, sys System, opts execOptions, args []string, current string, development bool) error {
	err := func() error {
		cacheRoot, err := cacheRootDir(sys)
		if err != nil {
			return err
		}
		source, running, err := opts.runningImage()
		if err != nil {
			return err
		}
		imagesDir := filepath.Join(cacheRoot, imagesDirName)
		if withinDir(imagesDir, running) {
			LeaseRunningImage()
			return nil
		}
		path, lease, err := ensureLeasedImage(ctx, sys, imagesDir, source)
		if err != nil {
			return err
		}
		defer func() { _ = lease.Close() }()
		pruneImages(imagesDir, filepath.Dir(path))
		// On platforms without procfs the source is a pathname; a replacement
		// during the copy must not hand off to a different release.
		if _, _, err := opts.runningImage(); err != nil {
			return errors.New(messages.DispatchImageChangedFmt)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		env, restore, err := imagelease.ExecEnv(lease, sys.Environ())
		if err != nil {
			return err
		}
		defer restore()
		if development {
			// A dev snapshot is a process-local move, not version dispatch. Keep
			// all inherited environment entries unchanged: a shim/installed-entry
			// marker would affect projections and nested release selections.
			if err := sys.ExecBinary(path, append([]string{path}, args[1:]...), env); err != nil {
				return err
			}
		} else if err := execHandoffEnv(sys, path, args, current, cacheRoot, env); err != nil {
			return err
		}
		return ErrDispatched
	}()
	if err == nil || errors.Is(err, ErrDispatched) || ctx.Err() != nil {
		return err
	}
	diagnostic := messages.DispatchMCPImageFallbackFmt
	if development {
		diagnostic = messages.DispatchMCPDevImageFallbackFmt
	}
	_, _ = fmt.Fprintf(sys.Stderr(), diagnostic, err)
	return nil
}

// ensureLeasedImage verifies and leases the image without releasing al.lock
// between those operations. The caller owns the returned lease until exec.
func ensureLeasedImage(ctx context.Context, sys System, imagesDir string, source string) (string, *os.File, error) {
	sum, err := fileSHA256(source)
	if err != nil {
		return "", nil, err
	}
	dir := filepath.Join(imagesDir, sum)
	path := filepath.Join(dir, imageName)
	lock, err := lockImage(ctx, sys, dir)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = lock.release() }()
	if info, statErr := os.Lstat(path); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 && verifyChecksum(path, sum) == nil { // #nosec G703 -- content-addressed path under the validated image cache.
		// Verified cache hit.
	} else {
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", nil, fmt.Errorf(messages.DispatchCheckCachedBinaryFmt, path, statErr)
		}
		if err := writeImage(sys, source, dir, path, sum); err != nil {
			return "", nil, err
		}
	}
	lease, err := leaseImage(path)
	if err != nil {
		return "", nil, err
	}
	return path, lease, nil
}

// writeImage copies source into place through a synced, verified temporary file
// and an atomic rename, so readers see either nothing or a complete image.
func writeImage(sys System, source string, dir string, path string, sum string) error {
	in, err := os.Open(source) // #nosec G304,G703 -- own executable or content-addressed cache path.
	if err != nil {
		return fmt.Errorf(messages.DispatchOpenFileFmt, source, err)
	}
	defer func() { _ = in.Close() }()
	tmp, err := sys.CreateTemp(dir, imageName+".tmp-*")
	if err != nil {
		return fmt.Errorf(messages.DispatchCreateTempFileFmt, err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName) // #nosec G703 -- temporary file created in the locked cache entry.
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return fmt.Errorf(messages.DispatchReadFailedFmt, source, err)
	}
	if err := sys.FileSync(tmp); err != nil {
		_ = tmp.Close()
		return fmt.Errorf(messages.DispatchSyncTempFileFmt, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf(messages.DispatchCloseTempFileFmt, err)
	}
	if err := sys.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf(messages.DispatchChmodCachedBinaryFmt, err)
	}
	if err := verifyChecksum(tmpName, sum); err != nil {
		return fmt.Errorf(messages.DispatchImageMismatchFmt+": %w", path, err)
	}
	if err := sys.Rename(tmpName, path); err != nil {
		return fmt.Errorf(messages.DispatchMoveCachedBinaryFmt, err)
	}
	committed = true
	return nil
}

// fileSHA256 returns the hex SHA-256 of path's content, the image cache key.
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path) // #nosec G304,G703 -- own executable or content-addressed cache path.
	if err != nil {
		return "", fmt.Errorf(messages.DispatchOpenFileFmt, path, err)
	}
	defer func() { _ = file.Close() }()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf(messages.DispatchHashFileFmt, path, err)
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}
