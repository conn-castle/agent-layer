package agentdispatch

import (
	"errors"
	"testing"
)

func TestExitErrorReportsWrappedCauseOnce(t *testing.T) {
	cause := errors.New("open /missing: no such file or directory")
	for _, tc := range []struct {
		name string
		err  *ExitError
		want string
	}{
		{"cause appended", wrapExitError(ExitConfig, "read dispatch mapping", cause), "read dispatch mapping: open /missing: no such file or directory"},
		{"cause already in message", wrapExitError(ExitTargetFailure, "start codex: "+cause.Error(), cause), "start codex: open /missing: no such file or directory"},
		{"cause is message", wrapExitError(ExitConfig, cause.Error(), cause), cause.Error()},
		{"classifying sentinel", notFoundExitError(`dispatch run "r" was not found`, errDispatchRunNotFound), `dispatch run "r" was not found`},
		{"message only", exitError(ExitUsage, "bad flag"), "bad flag"},
		{"cause only", &ExitError{Code: ExitConfig, Err: cause}, cause.Error()},
		{"code only", &ExitError{Code: ExitConfig}, "dispatch exit 65"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
	if !errors.Is(notFoundExitError("missing", errDispatchRunNotFound), errDispatchRunNotFound) {
		t.Fatal("not-found exit error lost its sentinel")
	}
}
