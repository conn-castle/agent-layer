package updatewarn

import (
	"context"
	"io"
	"os"
	"strings"

	"github.com/fatih/color"

	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/update"
)

// CheckForUpdate is a seam for tests.
var CheckForUpdate = update.Check

// WarnIfOutdated emits update warnings to stderr when a newer release is available.
// It is a best-effort warning and never returns an error.
func WarnIfOutdated(ctx context.Context, currentVersion string, stderr io.Writer) {
	if strings.TrimSpace(os.Getenv(dispatch.EnvVersionOverride)) != "" {
		return
	}
	if strings.TrimSpace(os.Getenv(dispatch.EnvNoNetwork)) != "" {
		return
	}
	if stderr == nil {
		stderr = io.Discard
	}

	warnColor := color.New(color.FgYellow)
	result, err := CheckForUpdate(ctx, currentVersion)
	if err != nil {
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
