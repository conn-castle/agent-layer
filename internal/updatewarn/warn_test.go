package updatewarn

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/update"
)

func TestWarnIfOutdated_SkipsWhenVersionOverrideSet(t *testing.T) {
	t.Setenv(dispatch.EnvVersionOverride, "1")
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
	t.Setenv(dispatch.EnvNoNetwork, "1")
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
