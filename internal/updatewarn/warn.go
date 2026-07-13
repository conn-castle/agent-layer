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

// CheckForUpdate is a seam for tests.
var CheckForUpdate = update.Check

// WarnIfOutdated emits update warnings to stderr when a newer release is available.
// It is a best-effort warning and never returns an error.
func WarnIfOutdated(ctx context.Context, currentVersion string, stderr io.Writer) {
	if strings.TrimSpace(os.Getenv(versiondispatch.EnvVersionOverride)) != "" {
		return
	}
	if strings.TrimSpace(os.Getenv(versiondispatch.EnvNoNetwork)) != "" {
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
	if result.CurrentIsDev {
		_, _ = warnColor.Fprintf(stderr, messages.InitWarnDevBuildFmt, result.Latest)
		return
	}
	if result.Outdated {
		_, _ = warnColor.Fprintf(stderr, messages.InitWarnUpdateAvailableFmt, result.Latest, result.Current)
	}
}
