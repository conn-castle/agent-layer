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

	// sourceCurrent and sourcePin label the origin of the resolved version.
	sourceCurrent = "current"
	sourcePin     = "pin"
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

	requested, source, warning, overridePinned, hasOverridePinned, err := resolveRequestedVersion(sys, rootDir, found, current)
	if err != nil {
		return err
	}
	if warning != "" {
		_, _ = fmt.Fprint(sys.Stderr(), warning)
	}
	if source != sourceCurrent {
		_, _ = fmt.Fprintf(sys.Stderr(), messages.DispatchVersionSourceFmt, requested, source)
	}
	if source == EnvVersionOverride && found && hasOverridePinned {
		_, _ = fmt.Fprintf(sys.Stderr(), messages.DispatchVersionOverrideWarningFmt, EnvVersionOverride, overridePinned)
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
	path, err := ensureCachedBinaryWithSystem(sys, cacheRoot, requested, sys.Stderr())
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
// The warning return value is non-empty when a pin file exists but is empty or corrupt.
func resolveRequestedVersion(sys System, rootDir string, hasRoot bool, current string) (string, string, string, string, bool, error) {
	override := strings.TrimSpace(sys.Getenv(EnvVersionOverride))
	if override != "" {
		normalized, err := version.Normalize(override)
		if err != nil {
			return "", "", "", "", false, fmt.Errorf(messages.DispatchInvalidEnvVersionFmt, EnvVersionOverride, err)
		}
		if !hasRoot {
			return normalized, EnvVersionOverride, "", "", false, nil
		}
		pinned, ok, warning, err := readPinnedVersion(sys, rootDir)
		if err != nil {
			return "", "", "", "", false, err
		}
		return normalized, EnvVersionOverride, warning, pinned, ok, nil
	}

	if hasRoot {
		pinned, ok, warning, err := readPinnedVersion(sys, rootDir)
		if err != nil {
			return "", "", "", "", false, err
		}
		if ok {
			return pinned, sourcePin, "", "", false, nil
		}
		if warning != "" {
			return current, sourceCurrent, warning, "", false, nil
		}
	}

	return current, sourceCurrent, "", "", false, nil
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
