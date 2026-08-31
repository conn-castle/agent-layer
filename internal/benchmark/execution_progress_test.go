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
	if timeouts.Environment != 10*time.Minute+time.Second || timeouts.Provider != 6*time.Minute || timeouts.Verifier != 30*time.Second || timeouts.VerifierAttempts != 1 || timeouts.EnvironmentAttempts != 2 {
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

func TestMissingStudyCellsTimeoutSumUsesEveryConfiguredPhaseAndAttempt(t *testing.T) {
	repoRoot := t.TempDir()
	checkout := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "checkouts", DeepSWECommit)
	document := `[agent]
timeout_sec = 90
[environment]
build_timeout_sec = 120
[verifier]
timeout_sec = 30
[verifier.environment]
build_timeout_sec = 180
`
	for _, task := range []string{"first-task", "second-task"} {
		root := filepath.Join(checkout, "tasks", task)
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, taskTOMLFile), []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}}
	arm := matrixArmFixture(t.TempDir(), "ceiling", ArmBaseline, model, effort, tasks)
	arm.AgentTimeoutMultiplier = 2
	preparation := matrixPreparation{
		arms: []matrixArm{arm}, checksums: map[string]string{"first-task": "one", "second-task": "two"},
		environments: map[string]string{"first-task": "env-one", "second-task": "env-two"},
	}
	ceiling, err := missingStudyCellsTimeoutSum(repoRoot, preparation)
	if err != nil {
		t.Fatal(err)
	}
	// Each missing cell: two 120s environment attempts + 1s backoff + 360s
	// agent setup + 180s provider + one 30s verifier attempt.
	if ceiling != 27*time.Minute+2*time.Second {
		t.Fatalf("timeout sum = %s, want 27m2s", ceiling)
	}
}
