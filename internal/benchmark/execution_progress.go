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
	benchmarkArtifactsDir     = "artifacts"
	benchmarkAgentDir         = "agent"
	benchmarkModelPatchFile   = "model.patch"
)

// ExecutionProgress describes the active benchmark cell phase and its deadline.
type ExecutionProgress struct {
	Phase            string
	StartedAt        time.Time
	EffectiveTimeout time.Duration
}

type benchmarkTaskTimeouts struct {
	Environment time.Duration
	Provider    time.Duration
	Verifier    time.Duration
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
		Environment: time.Duration(document.Environment.BuildTimeoutSeconds * float64(time.Second)),
		Provider:    time.Duration(document.Agent.TimeoutSeconds * agentMultiplier * float64(time.Second)),
		Verifier:    time.Duration((document.Verifier.Environment.BuildTimeoutSeconds + document.Verifier.TimeoutSeconds) * float64(time.Second)),
	}, nil
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
			notify(ExecutionProgress{Phase: phase, StartedAt: started, EffectiveTimeout: timeout})
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
