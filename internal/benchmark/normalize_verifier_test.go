package benchmark

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeVerifierStageFile(t *testing.T, stage string, relativePath string, content string) {
	t.Helper()
	fullPath := filepath.Join(stage, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func buildOutputEvents(outputs ...string) string {
	var builder strings.Builder
	for _, output := range outputs {
		fmt.Fprintf(&builder, "{\"Action\":\"build-output\",\"Output\":%q}\n", output)
	}
	return builder.String()
}

// TestVerifierBuildFailureIsDetectedOnlyFromVerifierLogs covers the signal that
// separates "the agent's patch scored zero" from "the score is not evidence at
// all". A build failure the verifier hit must be reported so analysis can
// discount the run, and agent-side output that merely mentions a build failure
// must not be mistaken for one.
func TestVerifierBuildFailureIsDetectedOnlyFromVerifierLogs(t *testing.T) {
	t.Run("clean verifier reports no failure", func(t *testing.T) {
		stage := t.TempDir()
		writeVerifierStageFile(t, stage, filepath.Join("jobs", "one", "verifier", "run.log"), "ok\n")
		writeVerifierStageFile(t, stage, filepath.Join("jobs", "one", "build-events.jsonl"), buildOutputEvents("warning: unused import"))
		failed, excerpt, err := verifierBuildFailed(stage)
		if err != nil {
			t.Fatal(err)
		}
		if failed || excerpt != "" {
			t.Fatalf("clean verifier reported failed=%t excerpt=%q", failed, excerpt)
		}
	})

	t.Run("agent output outside the verifier is not a verifier failure", func(t *testing.T) {
		stage := t.TempDir()
		writeVerifierStageFile(t, stage, filepath.Join("jobs", "one", "agent", "test-stdout.txt"), "[build failed]\n")
		failed, _, err := verifierBuildFailed(stage)
		if err != nil {
			t.Fatal(err)
		}
		if failed {
			t.Fatal("agent-side log was counted as a verifier build failure")
		}
	})

	t.Run("failure is reported with its build output excerpt", func(t *testing.T) {
		stage := t.TempDir()
		writeVerifierStageFile(t, stage, filepath.Join("jobs", "one", "verifier", "test-stdout.txt"), "[build failed]\n")
		writeVerifierStageFile(t, stage, filepath.Join("jobs", "one", "build-events.jsonl"), strings.Join([]string{
			`{"Action":"run","Output":"ignored non-build output"}`,
			`not-json`,
			buildOutputEvents("main.go:3:2: undefined: helper\n\n", "main.go:9:1: syntax error"),
		}, "\n"))
		failed, excerpt, err := verifierBuildFailed(stage)
		if err != nil {
			t.Fatal(err)
		}
		if !failed {
			t.Fatal("verifier build failure was not detected")
		}
		if excerpt != "main.go:3:2: undefined: helper\nmain.go:9:1: syntax error" {
			t.Fatalf("excerpt = %q, want only the build output lines", excerpt)
		}
	})

	t.Run("excerpt is bounded", func(t *testing.T) {
		stage := t.TempDir()
		writeVerifierStageFile(t, stage, filepath.Join("jobs", "one", "verifier", "run.log"), `{"FailedBuild":"package"}`)
		outputs := make([]string, 0, buildErrorExcerptLines*2)
		for index := range buildErrorExcerptLines * 2 {
			outputs = append(outputs, fmt.Sprintf("error line %d", index))
		}
		writeVerifierStageFile(t, stage, filepath.Join("jobs", "one", "build-events.jsonl"), buildOutputEvents(outputs...))
		failed, excerpt, err := verifierBuildFailed(stage)
		if err != nil || !failed {
			t.Fatalf("failed=%t, err=%v", failed, err)
		}
		if lines := strings.Count(excerpt, "\n") + 1; lines != buildErrorExcerptLines {
			t.Fatalf("excerpt kept %d lines, want it capped at %d", lines, buildErrorExcerptLines)
		}
	})

	t.Run("missing job output is an error, not a clean verifier", func(t *testing.T) {
		if _, _, err := verifierBuildFailed(t.TempDir()); err == nil {
			t.Fatal("a stage with no job output was reported as a passing verifier build")
		}
	})
}
