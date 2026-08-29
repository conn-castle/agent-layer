package benchmark

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadBenchmarkTaskTimeoutsReportsEffectiveProviderAndVerifierDeadlines(t *testing.T) {
	checkout := t.TempDir()
	root := filepath.Join(checkout, "tasks", "example-task")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	document := `[agent]
timeout_sec = 90
[environment]
build_timeout_sec = 120
[verifier]
timeout_sec = 30
[verifier.environment]
build_timeout_sec = 180
`
	if err := os.WriteFile(filepath.Join(root, taskTOMLFile), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	timeouts, err := loadBenchmarkTaskTimeouts(checkout, "example-task", 4)
	if err != nil {
		t.Fatal(err)
	}
	if timeouts.Environment != 2*time.Minute || timeouts.Provider != 6*time.Minute || timeouts.Verifier != 210*time.Second {
		t.Fatalf("effective timeouts = %#v", timeouts)
	}
}

func TestDetectPierExecutionPhaseTracksProviderAndVerifierBoundaries(t *testing.T) {
	stage := t.TempDir()
	if got, err := detectPierExecutionPhase(stage); err != nil || got != executionPhaseEnvironment {
		t.Fatalf("empty stage phase = %q, %v", got, err)
	}
	trial := filepath.Join(stage, "jobs", "event", "trial")
	if err := os.MkdirAll(filepath.Join(trial, "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := detectPierExecutionPhase(stage); err != nil || got != executionPhaseEnvironment {
		t.Fatalf("empty agent stage phase = %q, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(trial, "agent", "provider.txt"), []byte("started"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := detectPierExecutionPhase(stage); err != nil || got != executionPhaseProvider {
		t.Fatalf("populated agent stage phase = %q, %v", got, err)
	}
	if err := os.MkdirAll(filepath.Join(trial, "artifacts"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trial, "artifacts", "model.patch"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := detectPierExecutionPhase(stage); err != nil || got != executionPhaseVerifier {
		t.Fatalf("patch stage phase = %q, %v", got, err)
	}
}
