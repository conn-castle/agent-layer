package versiondispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skillmigration"
	"github.com/conn-castle/agent-layer/internal/version"
)

// EnvCacheDir, EnvNoNetwork, EnvVersionOverride, EnvShimActive, and
// EnvDevelopmentBypassVersionDispatch define dispatch environment keys.
const (
	EnvCacheDir                         = "AL_CACHE_DIR"
	EnvNoNetwork                        = "AL_NO_NETWORK"
	EnvVersionOverride                  = "AL_VERSION"
	EnvShimActive                       = "AL_SHIM_ACTIVE"
	EnvDevelopmentBypassVersionDispatch = "AL_DEV_BYPASS_VERSION_DISPATCH" //nolint:gosec // Environment key, not a credential.
	// EnvDevelopmentExecutable carries the invoking binary through native MCP
	// clients whose server launcher otherwise resolves a different al on PATH.
	EnvDevelopmentExecutable = "AL_DEV_EXECUTABLE"
	// EnvDevelopmentSourceRoot scopes persistent source selection to one configuration root.
	EnvDevelopmentSourceRoot = "AL_DEV_SOURCE_ROOT"
	// EnvInstalledExecutable carries the installed al entry a generated release
	// MCP launcher executed, or the entry a pinned re-exec was invoked through,
	// so regeneration in that process records it again.
	EnvInstalledExecutable = "AL_INSTALLED_EXECUTABLE"

	// sourceCurrent and sourcePin label the origin of the resolved version.
	sourceCurrent = "current"
	sourcePin     = "pin"

	quietFlagMinVersion = "0.8.7"

	// installedEntryName is the only executable name recorded as an installed
	// entry; generated launchers and nested calls invoke al by this name.
	installedEntryName = "al"
)

// ErrDispatched signals that execution has been handed off to another binary.
var ErrDispatched = errors.New(messages.DispatchErrDispatched)

// MaybeExec checks for a pinned version and dispatches to it when needed.
// It returns ErrDispatched if execution was handed off. Canceling ctx stops a
// pending download or cache-lock wait and prevents the hand-off.
func MaybeExec(ctx context.Context, args []string, currentVersion string, cwd string, stderr io.Writer) error {
	return maybeExec(ctx, RealSystem{StderrWriter: stderr}, args, currentVersion, cwd)
}

// maybeExec checks for a pinned version and dispatches to it when needed.
// It returns ErrDispatched if execution was handed off. Canceling ctx stops a
// pending download or cache-lock wait and prevents the hand-off.
func maybeExec(ctx context.Context, sys System, args []string, currentVersion string, cwd string) error {
	return maybeExecWithOptions(ctx, sys, args, currentVersion, cwd, execOptions{})
}

// maybeExecWithOptions is MaybeExec; a non-nil opts.runningImage also enables
// the MCP server image handoff when the running executable is selected.
func maybeExecWithOptions(ctx context.Context, sys System, args []string, currentVersion string, cwd string, opts execOptions) error {
	if len(args) == 0 {
		return fmt.Errorf(messages.DispatchMissingArgv0)
	}
	if cwd == "" {
		return fmt.Errorf(messages.DispatchWorkingDirRequired)
	}

	current, err := normalizeCurrentVersion(currentVersion)
	if err != nil {
		return err
	}
	// Development launchers build and place a source snapshot first on PATH so
	// child agents can invoke that same binary. Its explicit marker must bypass
	// version pin resolution; otherwise a nested `al dispatch` could re-exec a
	// cached release instead of the source snapshot.
	// Preserve the explicit legacy go-run bypass, but rooted Make sessions
	// may bypass only their own project.
	development := strings.TrimSpace(sys.Getenv(EnvDevelopmentBypassVersionDispatch)) != ""
	if development && sys.Getenv(EnvDevelopmentSourceRoot) == "" {
		return nil
	}

	rootDir, found, err := sys.FindAgentLayerRoot(cwd)
	if err != nil {
		return err
	}

	if development && sameDevelopmentRoot(rootDir, sys.Getenv(EnvDevelopmentSourceRoot)) {
		devExe := sys.Getenv(EnvDevelopmentExecutable)
		if devExe != "" && opts.runningImage != nil && opts.sameAs != nil && opts.sameAs(devExe) == nil {
			return handOffToImage(ctx, sys, opts, args, current, true)
		}
		return nil
	}

	requested, source, warning, overridePinned, hasOverridePinned, err := resolveRequestedVersion(sys, rootDir, found, current)
	if err != nil {
		return err
	}
	if requested == current {
		// The image process prints the diagnostics below after its handoff.
		if wantsImageHandoff(sys, opts, source, current) {
			if err := handOffToImage(ctx, sys, opts, args, current, false); err != nil {
				return err
			}
		}
		// No dispatch needed — this is the binary that will run the command.
		// Print version-source diagnostics here so they appear exactly once,
		// even when an older cached binary was dispatched to reach this point.
		if warning != "" {
			_, _ = fmt.Fprint(sys.Stderr(), warning)
		}
		if source != sourceCurrent {
			_, _ = fmt.Fprintf(sys.Stderr(), messages.DispatchVersionSourceFmt, requested, source)
		}
		if source == EnvVersionOverride && found && hasOverridePinned {
			_, _ = fmt.Fprintf(sys.Stderr(), messages.DispatchVersionOverrideWarningFmt, EnvVersionOverride, overridePinned)
		}
		return nil
	}
	if sys.Getenv(EnvShimActive) != "" {
		return fmt.Errorf(messages.DispatchAlreadyActiveFmt, current, requested)
	}
	if version.IsDev(requested) {
		return fmt.Errorf(messages.DispatchDevVersionNotAllowedFmt, EnvVersionOverride)
	}

	if found {
		if err := skillmigration.CheckVersion(ctx, rootDir, requested); err != nil {
			return err
		}
	}
	cacheRoot, err := cacheRootDir(sys)
	if err != nil {
		return err
	}
	path, err := ensureCachedBinaryWithSystem(ctx, sys, cacheRoot, requested, sys.Stderr())
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := execHandoff(sys, path, argsForRequestedVersion(args, requested), current, cacheRoot); err != nil {
		return err
	}
	return ErrDispatched
}

// execHandoff replaces this process with the cached binary at path.
func execHandoff(sys System, path string, args []string, current string, cacheRoot string) error {
	return execHandoffEnv(sys, path, args, current, cacheRoot, sys.Environ())
}

func execHandoffEnv(sys System, path string, args []string, current string, cacheRoot string, inherited []string) error {
	env := append([]string(nil), inherited...)
	env = append(env, fmt.Sprintf("%s=1", EnvShimActive))
	// The cached binary's argv[0] is its cache path, so pass the stable entry
	// that generated MCP launchers record (overriding an inherited one). Source
	// builds such as go run and cached binaries are not installations.
	if current != "dev" {
		if entry := invokingEntry(sys, args[0], cacheRoot); entry != "" {
			env = append(withoutEnv(env, EnvInstalledExecutable), EnvInstalledExecutable+"="+entry)
		}
	}
	execArgs := append([]string{path}, args[1:]...)
	return sys.ExecBinary(path, execArgs, env)
}

// invokingEntry returns the absolute al entry this process was invoked
// through, or "" when it cannot be determined, lies in the version cache, or is
// not named al. The name rule matches the entries generated launchers record:
// a wrapper's target (such as a benchmark gate's al-real) or a renamed binary
// must not replace the al entry that wraps it.
func invokingEntry(sys System, argv0 string, cacheRoot string) string {
	if filepath.Base(argv0) != installedEntryName {
		return ""
	}
	entry := ""
	if strings.ContainsRune(argv0, filepath.Separator) {
		if abs, err := filepath.Abs(argv0); err == nil {
			entry = abs
		}
	} else if found, err := sys.LookPath(argv0); err == nil && filepath.IsAbs(found) {
		entry = found
	}
	if entry == "" || withinDir(cacheRoot, entry) {
		return ""
	}
	return entry
}

// withinDir reports whether path lies inside dir. It compares absolute paths
// and, when both exist, their symlink-resolved forms, so a relative or
// symlinked cache directory still matches. Only the entry's parent directory
// is resolved: an entry that is itself a symlink is judged by its own path.
func withinDir(dir string, path string) bool {
	if dir == "" {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	if pathWithin(absDir, path) {
		return true
	}
	resolvedDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return false
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return false
	}
	return pathWithin(resolvedDir, filepath.Join(resolvedParent, filepath.Base(path)))
}

// pathWithin reports whether the absolute path lies inside the absolute dir.
func pathWithin(dir string, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// withoutEnv removes every key=value entry for key, reusing env's backing array.
func withoutEnv(env []string, key string) []string {
	return slices.DeleteFunc(env, func(entry string) bool { return strings.HasPrefix(entry, key+"=") })
}

func argsForRequestedVersion(args []string, requested string) []string {
	if supportsQuietFlag(requested) {
		return args
	}
	return stripQuietFlags(args)
}

func supportsQuietFlag(rawVersion string) bool {
	normalized, err := version.Normalize(rawVersion)
	if err != nil {
		return false
	}
	return semverAtLeast(normalized, quietFlagMinVersion)
}

func semverAtLeast(versionA string, versionB string) bool {
	comparison, err := version.Compare(versionA, versionB)
	if err != nil {
		return false
	}
	return comparison >= 0
}

func stripQuietFlags(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	stripped := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			stripped = append(stripped, args[i:]...)
			break
		}
		if arg == quietFlag || arg == "-q" || strings.HasPrefix(arg, "--quiet=") {
			continue
		}
		stripped = append(stripped, arg)
	}
	return stripped
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
