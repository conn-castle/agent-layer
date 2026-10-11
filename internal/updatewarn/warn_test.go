package updatewarn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/update"
	"github.com/conn-castle/agent-layer/internal/versiondispatch"
)

// TestMain keeps warning tests independent of a dispatched child's
// AL_SUPPRESS_UPDATE_WARNING inheritance. Tests that exercise suppression
// set the variable themselves.
func TestMain(m *testing.M) {
	if err := os.Unsetenv(EnvSuppress); err != nil {
		fmt.Fprintf(os.Stderr, "clear %s: %v\n", EnvSuppress, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestWarnIfOutdated_SkipsWhenVersionOverrideSet(t *testing.T) {
	t.Setenv(versiondispatch.EnvVersionOverride, "1")
	orig := CheckForUpdate
	called := 0
	CheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
		called++
		return update.CheckResult{}, nil
	}
	t.Cleanup(func() { CheckForUpdate = orig })

	var stderr bytes.Buffer
	WarnIfOutdated(context.Background(), "v1.0.0", &stderr)
	if called != 0 {
		t.Fatalf("expected update check to be skipped, got %d calls", called)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no output, got %q", stderr.String())
	}
}

func TestWarnIfOutdated_SkipsWhenSuppressed(t *testing.T) {
	t.Setenv(EnvSuppress, "1")
	orig := CheckForUpdate
	called := 0
	CheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
		called++
		return update.CheckResult{}, nil
	}
	t.Cleanup(func() { CheckForUpdate = orig })

	var stderr bytes.Buffer
	WarnIfOutdated(context.Background(), "v1.0.0", &stderr)
	if called != 0 {
		t.Fatalf("expected update check to be skipped, got %d calls", called)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected no output, got %q", stderr.String())
	}
}

func TestWarnIfOutdated_SkipsWhenNoNetworkSet(t *testing.T) {
	t.Setenv(versiondispatch.EnvNoNetwork, "1")
	orig := CheckForUpdate
	called := 0
	CheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
		called++
		return update.CheckResult{}, nil
	}
	t.Cleanup(func() { CheckForUpdate = orig })

	WarnIfOutdated(context.Background(), "v1.0.0", nil)
	if called != 0 {
		t.Fatalf("expected update check to be skipped, got %d calls", called)
	}
}

func TestWarnIfOutdated_ErrorDevAndOutdated(t *testing.T) {
	cases := []struct {
		name   string
		result update.CheckResult
		err    error
		want   string
	}{
		{name: "error", err: errors.New("boom"), want: "failed to check for updates"},
		{name: "dev", result: update.CheckResult{CurrentIsDev: true, Latest: "2.0.0"}, want: "running dev build"},
		{name: "outdated", result: update.CheckResult{Outdated: true, Latest: "2.0.0", Current: "1.0.0"}, want: "update available"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orig := CheckForUpdate
			CheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
				return tc.result, tc.err
			}
			t.Cleanup(func() { CheckForUpdate = orig })

			var stderr bytes.Buffer
			WarnIfOutdated(context.Background(), "v1.0.0", &stderr)
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("expected %q in output, got %q", tc.want, stderr.String())
			}
		})
	}
}

func TestWarnIfOutdated_OutdatedTellsUserHowToSilence(t *testing.T) {
	orig := CheckForUpdate
	CheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Outdated: true, Current: "1.0.0", Latest: "2.0.0"}, nil
	}
	t.Cleanup(func() { CheckForUpdate = orig })

	var stderr bytes.Buffer
	WarnIfOutdated(context.Background(), "v1.0.0", &stderr)
	out := stderr.String()
	if !strings.Contains(out, "version_update_on_sync = false") {
		t.Fatalf("expected the warning to tell the user how to silence it, got %q", out)
	}
}

func TestWarnIfOutdated_RateLimitProducesNoOutput(t *testing.T) {
	orig := CheckForUpdate
	CheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{}, &update.RateLimitError{StatusCode: 429, Status: "429 Too Many Requests"}
	}
	t.Cleanup(func() { CheckForUpdate = orig })

	var stderr bytes.Buffer
	WarnIfOutdated(context.Background(), "v1.0.0", &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("expected no output, got %q", stderr.String())
	}
}

func TestWarnIfOutdated_NoOutputWhenUpToDate(t *testing.T) {
	orig := CheckForUpdate
	CheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Outdated: false, Current: "1.0.0", Latest: "1.0.0"}, nil
	}
	t.Cleanup(func() { CheckForUpdate = orig })

	var stderr bytes.Buffer
	WarnIfOutdated(context.Background(), "v1.0.0", &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("expected no output, got %q", stderr.String())
	}
}

func TestWarnIfOutdated_NilWriterDoesNotPanic(t *testing.T) {
	orig := CheckForUpdate
	CheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
		return update.CheckResult{Outdated: true, Current: "1.0.0", Latest: "2.0.0"}, nil
	}
	t.Cleanup(func() { CheckForUpdate = orig })

	WarnIfOutdated(context.Background(), "v1.0.0", nil)
}

func TestCachedWarningNeverCallsLiveCheck(t *testing.T) {
	for _, cacheErr := range []error{nil, errors.New("missing or corrupt cache")} {
		origLive, origCached, origRefresh := CheckForUpdate, CachedCheckForUpdate, RefreshUpdateCache
		CheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
			t.Fatal("live check in launch path")
			return update.CheckResult{}, nil
		}
		CachedCheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
			return update.CheckResult{Current: "1.0.0", Latest: "2.0.0", Outdated: true}, cacheErr
		}
		refreshed := false
		RefreshUpdateCache = func() { refreshed = true }
		var out bytes.Buffer
		WarnCachedIfOutdated(context.Background(), "1.0.0", &out)
		CheckForUpdate, CachedCheckForUpdate, RefreshUpdateCache = origLive, origCached, origRefresh
		if !refreshed || (strings.Contains(out.String(), "update available") != (cacheErr == nil)) {
			t.Fatalf("refresh=%v output=%q", refreshed, out.String())
		}
	}
}

func TestCachedWarningSuppressionSkipsRefresh(t *testing.T) {
	for _, env := range []string{EnvSuppress, versiondispatch.EnvNoNetwork, versiondispatch.EnvVersionOverride} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, "1")
			origCached, origRefresh := CachedCheckForUpdate, RefreshUpdateCache
			CachedCheckForUpdate = func(context.Context, string) (update.CheckResult, error) {
				t.Fatal("suppressed cache check")
				return update.CheckResult{}, nil
			}
			RefreshUpdateCache = func() { t.Fatal("suppressed refresh") }
			t.Cleanup(func() { CachedCheckForUpdate, RefreshUpdateCache = origCached, origRefresh })
			WarnCachedIfOutdated(context.Background(), "1.0.0", nil)
		})
	}
}
