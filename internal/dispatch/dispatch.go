package dispatch

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
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
var ErrDispatched = errors.New(messages.DispatchErrDispatched)

// MaybeExec checks for a pinned version and dispatches to it when needed.
// It returns ErrDispatched if execution was handed off.
func MaybeExec(args []string, currentVersion string, cwd string, exit func(int)) error {
	return MaybeExecWithSystem(RealSystem{}, args, currentVersion, cwd, exit)
}

// MaybeExecWithSystem checks for a pinned version and dispatches to it when needed.
// It returns ErrDispatched if execution was handed off.
func MaybeExecWithSystem(sys System, args []string, currentVersion string, cwd string, exit func(int)) error {
	if sys == nil {
		return fmt.Errorf(messages.DispatchSystemRequired)
	}
	if len(args) == 0 {
		return fmt.Errorf(messages.DispatchMissingArgv0)
	}
	if cwd == "" {
		return fmt.Errorf(messages.DispatchWorkingDirRequired)
	}
	if exit == nil {
		return fmt.Errorf(messages.DispatchExitHandlerRequired)
	}

	current, err := normalizeCurrentVersion(currentVersion)
	if err != nil {
		return err
	}

	rootDir, found, err := sys.FindAgentLayerRoot(cwd)
	if err != nil {
		return err
	}

	requested, _, err := resolveRequestedVersion(sys, rootDir, found, current)
	if err != nil {
		return err
	}
	if requested == current {
		return nil
	}
	if sys.Getenv(EnvShimActive) != "" {
		return fmt.Errorf(messages.DispatchAlreadyActiveFmt, current, requested)
	}
	if version.IsDev(requested) {
		return fmt.Errorf(messages.DispatchDevVersionNotAllowedFmt, EnvVersionOverride)
	}

	cacheRoot, err := cacheRootDir(sys)
	if err != nil {
		return err
	}
	path, err := ensureCachedBinary(cacheRoot, requested)
	if err != nil {
		return err
	}

	env := append(sys.Environ(), fmt.Sprintf("%s=1", EnvShimActive))
	execArgs := append([]string{path}, args[1:]...)
	if err := sys.ExecBinary(path, execArgs, env, exit); err != nil {
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
		return "", fmt.Errorf(messages.DispatchInvalidBuildVersionFmt, raw, err)
	}
	return normalized, nil
}

// resolveRequestedVersion determines the target version and its source (env override, pin, or current).
func resolveRequestedVersion(sys System, rootDir string, hasRoot bool, current string) (string, string, error) {
	override := strings.TrimSpace(sys.Getenv(EnvVersionOverride))
	if override != "" {
		normalized, err := version.Normalize(override)
		if err != nil {
			return "", "", fmt.Errorf(messages.DispatchInvalidEnvVersionFmt, EnvVersionOverride, err)
		}
		return normalized, EnvVersionOverride, nil
	}

	if hasRoot {
		pinned, ok, err := readPinnedVersion(sys, rootDir)
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
func cacheRootDir(sys System) (string, error) {
	if override := strings.TrimSpace(sys.Getenv(EnvCacheDir)); override != "" {
		return override, nil
	}
	base, err := sys.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf(messages.DispatchResolveUserCacheDirFmt, err)
	}
	return filepath.Join(base, "agent-layer"), nil
}
