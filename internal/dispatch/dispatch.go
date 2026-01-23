package dispatch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/root"
	"github.com/conn-castle/agent-layer/internal/version"
)

// EnvCacheDir, EnvNoNetwork, EnvVersionOverride, and EnvShimActive define dispatch environment keys.
const (
	EnvCacheDir        = "AL_CACHE_DIR"
	EnvNoNetwork       = "AL_NO_NETWORK"
	EnvVersionOverride = "AL_VERSION"
	EnvShimActive      = "AL_SHIM_ACTIVE"
)

// ErrDispatched signals that execution has been handed off to another binary.
var ErrDispatched = errors.New("dispatch executed")

var (
	execBinaryFunc = execBinary
	userCacheDir   = os.UserCacheDir
)

// MaybeExec checks for a pinned version and dispatches to it when needed.
// It returns ErrDispatched if execution was handed off.
func MaybeExec(args []string, currentVersion string, cwd string, exit func(int)) error {
	if len(args) == 0 {
		return fmt.Errorf("missing argv[0]")
	}
	if cwd == "" {
		return fmt.Errorf("working directory is required")
	}
	if exit == nil {
		return fmt.Errorf("exit handler is required")
	}

	current, err := normalizeCurrentVersion(currentVersion)
	if err != nil {
		return err
	}

	rootDir, found, err := root.FindAgentLayerRoot(cwd)
	if err != nil {
		return err
	}

	requested, _, err := resolveRequestedVersion(rootDir, found, current)
	if err != nil {
		return err
	}
	if requested == current {
		return nil
	}
	if os.Getenv(EnvShimActive) != "" {
		return fmt.Errorf("version dispatch already active (current %s, requested %s)", current, requested)
	}
	if version.IsDev(requested) {
		return fmt.Errorf("cannot dispatch to dev version; set %s to a release version", EnvVersionOverride)
	}

	cacheRoot, err := cacheRootDir()
	if err != nil {
		return err
	}
	path, err := ensureCachedBinary(cacheRoot, requested)
	if err != nil {
		return err
	}

	env := append(os.Environ(), fmt.Sprintf("%s=1", EnvShimActive))
	execArgs := append([]string{path}, args[1:]...)
	if err := execBinaryFunc(path, execArgs, env, exit); err != nil {
		if errors.Is(err, ErrDispatched) {
			return err
		}
		return err
	}
	return ErrDispatched
}

// normalizeCurrentVersion validates the running build version and returns it in X.Y.Z form.
func normalizeCurrentVersion(raw string) (string, error) {
	if version.IsDev(raw) {
		return "dev", nil
	}
	normalized, err := version.Normalize(raw)
	if err != nil {
		return "", fmt.Errorf("invalid build version %q: %w", raw, err)
	}
	return normalized, nil
}

// resolveRequestedVersion determines the target version and its source (env override, pin, or current).
func resolveRequestedVersion(rootDir string, hasRoot bool, current string) (string, string, error) {
	override := strings.TrimSpace(os.Getenv(EnvVersionOverride))
	if override != "" {
		normalized, err := version.Normalize(override)
		if err != nil {
			return "", "", fmt.Errorf("invalid %s: %w", EnvVersionOverride, err)
		}
		return normalized, EnvVersionOverride, nil
	}

	if hasRoot {
		pinned, ok, err := readPinnedVersion(rootDir)
		if err != nil {
			return "", "", err
		}
		if ok {
			return pinned, "pin", nil
		}
	}

	return current, "current", nil
}

// cacheRootDir resolves the cache root directory, honoring AL_CACHE_DIR when set.
func cacheRootDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv(EnvCacheDir)); override != "" {
		return override, nil
	}
	base, err := userCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache dir: %w", err)
	}
	return filepath.Join(base, "agent-layer"), nil
}
