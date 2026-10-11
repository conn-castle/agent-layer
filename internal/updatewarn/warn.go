package updatewarn

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"

	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/update"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

// EnvSuppress disables the best-effort update notice for machine-oriented
// execution contexts whose output must stay focused on the requested work.
const EnvSuppress = "AL_SUPPRESS_UPDATE_WARNING"

// CheckForUpdate is a seam for tests.
var CheckForUpdate = update.Check

// CachedCheckForUpdate is the cached launch check seam for tests.
var CachedCheckForUpdate = update.CachedCheck

// RefreshUpdateCache is the background refresh seam for tests.
var RefreshUpdateCache = update.StartCacheRefresh

// WarnCachedIfOutdated reads local release information and starts a bounded,
// silent refresh for the next launch. Cache misses and corruption are silent.
func WarnCachedIfOutdated(ctx context.Context, currentVersion string, stderr io.Writer) {
	if suppressed() {
		return
	}
	result, err := CachedCheckForUpdate(ctx, currentVersion)
	if err == nil {
		printNotice(result, stderr)
	}
	RefreshUpdateCache()
}

func suppressed() bool {
	return strings.TrimSpace(os.Getenv(EnvSuppress)) != "" ||
		strings.TrimSpace(os.Getenv(versiondispatch.EnvVersionOverride)) != "" ||
		strings.TrimSpace(os.Getenv(versiondispatch.EnvNoNetwork)) != ""
}

// WarnIfOutdated emits update warnings to stderr when a newer release is available.
// It is a best-effort warning and never returns an error.
func WarnIfOutdated(ctx context.Context, currentVersion string, stderr io.Writer) {
	if suppressed() {
		return
	}
	if stderr == nil {
		stderr = io.Discard
	}

	warnColor := color.New(color.FgYellow)
	result, err := CheckForUpdate(ctx, currentVersion)
	if err != nil {
		if update.IsRateLimitError(err) {
			return
		}
		_, _ = warnColor.Fprintf(stderr, messages.InitWarnUpdateCheckFailedFmt, err)
		return
	}
	printNotice(result, stderr)
}

func printNotice(result update.CheckResult, stderr io.Writer) {
	if stderr == nil {
		stderr = io.Discard
	}
	warnColor := color.New(color.FgYellow)
	if result.CurrentIsDev {
		_, _ = warnColor.Fprintf(stderr, messages.InitWarnDevBuildFmt, result.Latest)
		return
	}
	if result.Outdated {
		_, _ = warnColor.Fprintf(stderr, messages.InitWarnUpdateAvailableFmt, result.Latest, result.Current)
	}
}
