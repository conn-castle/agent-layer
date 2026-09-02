package benchmark

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const (
	executionPhaseEnvironment = "environment"
	executionPhaseProvider    = "provider"
	executionPhaseVerifier    = "verifier"
	verifierTestStdoutFile    = "test-stdout.txt"
	verifierRunLogFile        = "run.log"
	benchmarkArtifactsDir     = "artifacts"
	benchmarkAgentDir         = "agent"
	benchmarkModelPatchFile   = "model.patch"
	pierVerifierAttempts      = 1
	pierEnvironmentAttempts   = 2
	pierRetryBackoff          = time.Second
	pierAgentSetupTimeout     = 6 * time.Minute
)

// ExecutionProgress describes the active benchmark cell phase and its deadline.
type ExecutionProgress struct {
	Phase            string
	StartedAt        time.Time
	EffectiveTimeout time.Duration
	MaximumAttempts  int
}

type benchmarkTaskTimeouts struct {
	Environment         time.Duration
	Provider            time.Duration
	Verifier            time.Duration
	VerifierAttempts    int
	EnvironmentAttempts int
}

func loadBenchmarkTaskTimeouts(checkout, task string, agentMultiplier float64) (benchmarkTaskTimeouts, error) {
	var document struct {
		Agent struct {
			TimeoutSeconds float64 `toml:"timeout_sec"`
		} `toml:"agent"`
		Verifier struct {
			TimeoutSeconds float64 `toml:"timeout_sec"`
			Environment    struct {
				BuildTimeoutSeconds float64 `toml:"build_timeout_sec"`
			} `toml:"environment"`
		} `toml:"verifier"`
		Environment struct {
			BuildTimeoutSeconds float64 `toml:"build_timeout_sec"`
		} `toml:"environment"`
	}
	data, err := os.ReadFile(filepath.Join(checkout, "tasks", task, taskTOMLFile)) // #nosec G304 -- task is validated against the pinned checkout.
	if err != nil {
		return benchmarkTaskTimeouts{}, err
	}
	if err := toml.Unmarshal(data, &document); err != nil {
		return benchmarkTaskTimeouts{}, err
	}
	if agentMultiplier == 0 {
		agentMultiplier = 1
	}
	if agentMultiplier < 0 || document.Agent.TimeoutSeconds <= 0 || document.Verifier.TimeoutSeconds <= 0 ||
		document.Environment.BuildTimeoutSeconds <= 0 || document.Verifier.Environment.BuildTimeoutSeconds <= 0 {
		return benchmarkTaskTimeouts{}, fmt.Errorf("benchmark task %s has invalid execution timeouts", task)
	}
	return benchmarkTaskTimeouts{
		// Pier retries environment start once, waits one second between the
		// attempts, and then gives agent installation its own six-minute setup
		// allowance before provider inference begins.
		Environment: time.Duration(pierEnvironmentAttempts*document.Environment.BuildTimeoutSeconds*float64(time.Second)) + pierRetryBackoff + pierAgentSetupTimeout,
		Provider:    time.Duration(document.Agent.TimeoutSeconds * agentMultiplier * float64(time.Second)),
		// The pinned adapter removes Pier 0.3.0's unconditional retry for
		// VerifierTimeoutError. Environment startup and tests share this single
		// envelope; the nested build timeout is not additive.
		Verifier:            time.Duration(pierVerifierAttempts * document.Verifier.TimeoutSeconds * float64(time.Second)),
		VerifierAttempts:    pierVerifierAttempts,
		EnvironmentAttempts: pierEnvironmentAttempts,
	}, nil
}

func missingStudyCellsTimeoutSum(repoRoot string, preparation matrixPreparation) (time.Duration, error) {
	checkout := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "checkouts", DeepSWECommit)
	var total time.Duration
	for armIndex := range preparation.arms {
		arm := preparation.arms[armIndex]
		for _, task := range arm.Loaded.Plan.Tasks {
			missing := 0
			for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
				state, _, err := inspectStudyCell(arm, task.ID, attempt, preparation.checksums[task.ID], preparation.environments[task.ID])
				if err != nil {
					return 0, err
				}
				if state == studyCellMissing {
					missing++
				}
			}
			if missing == 0 {
				continue
			}
			timeouts, err := loadBenchmarkTaskTimeouts(checkout, task.ID, arm.AgentTimeoutMultiplier)
			if err != nil {
				// Complete-study regeneration and cheap cached-state narrowing do
				// not require a local task checkout. The ceiling is operator
				// guidance only, so leave it unavailable rather than turning that
				// valid cache path into task setup work.
				if errors.Is(err, os.ErrNotExist) {
					return 0, nil
				}
				return 0, fmt.Errorf("load timeout ceiling for %s: %w", task.ID, err)
			}
			total += time.Duration(missing) * (timeouts.Environment + timeouts.Provider + timeouts.Verifier)
		}
	}
	return total, nil
}

func detectPierExecutionPhase(stage string) (string, error) {
	phase := executionPhaseEnvironment
	err := filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == benchmarkModelPatchFile && filepath.Base(filepath.Dir(path)) == benchmarkArtifactsDir {
			phase = executionPhaseVerifier
			return fs.SkipAll
		}
		if filepath.Base(filepath.Dir(path)) == benchmarkAgentDir && phase == executionPhaseEnvironment {
			phase = executionPhaseProvider
		}
		return nil
	})
	return phase, err
}

func watchPierExecutionProgress(stage string, timeouts benchmarkTaskTimeouts, notify func(ExecutionProgress), providerCompleted func(time.Time), stop <-chan struct{}) {
	started := time.Now().UTC()
	last := ""
	providerRecorded := false
	emit := func() {
		phase, err := detectPierExecutionPhase(stage)
		if err != nil || phase == last {
			return
		}
		last = phase
		var timeout time.Duration
		switch phase {
		case executionPhaseProvider:
			timeout = timeouts.Provider
		case executionPhaseVerifier:
			timeout = timeouts.Verifier
		default:
			timeout = timeouts.Environment
		}
		started = time.Now().UTC()
		if phase == executionPhaseVerifier && !providerRecorded {
			providerRecorded = true
			if providerCompleted != nil {
				providerCompleted(started)
			}
		}
		if notify != nil {
			attempts := 0
			switch phase {
			case executionPhaseVerifier:
				attempts = timeouts.VerifierAttempts
			case executionPhaseEnvironment:
				attempts = timeouts.EnvironmentAttempts
			}
			notify(ExecutionProgress{Phase: phase, StartedAt: started, EffectiveTimeout: timeout, MaximumAttempts: attempts})
		}
	}
	emit()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			emit()
		}
	}
}
