package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	bench "github.com/conn-castle/agent-layer/internal/benchmark"
)

func TestBenchmarkOnlyExposesRunAndReadiness(t *testing.T) {
	command := newBenchmarkCmd()
	var names []string
	for _, child := range command.Commands() {
		names = append(names, child.Name())
	}
	if strings.Join(names, ",") != "readiness,run" {
		t.Fatalf("benchmark commands = %v", names)
	}
}

func TestBenchmarkRunDryRunDoesNotInvokeProvider(t *testing.T) {
	original := runStudy
	runStudy = func(_ context.Context, options bench.StudyOptions, _ bench.TaskExecutor) (bench.StudyOutcome, error) {
		if !options.DryRun || options.StudyPath != "study.toml" || options.TaskConcurrency != 4 {
			t.Fatalf("options = %#v", options)
		}
		outcome := bench.StudyOutcome{StudyID: strings.Repeat("a", 64), Required: 2, Missing: 2, Experiments: []bench.StudyExperimentProgress{{Name: "Bare", Required: 1, Missing: 1}}}
		if err := options.OnPrepared(outcome); err != nil {
			return bench.StudyOutcome{}, err
		}
		return outcome, nil
	}
	t.Cleanup(func() { runStudy = original })
	root := newRootCmd()
	root.SetArgs([]string{"benchmark", "run", "study.toml", "--dry-run", "--task-concurrency", "4"})
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Bare: 0 of 1 cells cached") || !strings.Contains(output.String(), "authorizes paid") || !strings.Contains(output.String(), "No inference call was made") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestBenchmarkReadinessRunsWithoutProviderCalls(t *testing.T) {
	original := checkReadiness
	checkReadiness = func(_ context.Context, options bench.ReadinessAuditOptions) (bench.ReadinessAuditOutcome, error) {
		if options.TaskConcurrency != 4 {
			t.Fatalf("options = %#v", options)
		}
		return bench.ReadinessAuditOutcome{DeepSWECommit: strings.Repeat("d", 40), Required: 2, Certified: 2}, nil
	}
	t.Cleanup(func() { checkReadiness = original })
	root := newRootCmd()
	root.SetArgs([]string{"benchmark", "readiness", "--task-concurrency", "4"})
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "2 of 2 tasks certified") || !strings.Contains(output.String(), "No provider call was made") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestBenchmarkReadinessPrintsActionableTaskFailures(t *testing.T) {
	original := checkReadiness
	checkReadiness = func(_ context.Context, _ bench.ReadinessAuditOptions) (bench.ReadinessAuditOutcome, error) {
		return bench.ReadinessAuditOutcome{DeepSWECommit: strings.Repeat("d", 40), Required: 2, Failed: 1, Blocked: 1, Tasks: []bench.ReadinessAuditTask{
			{Task: "missing-contract", Status: "failed", Error: "no mandatory environment readiness contract"},
			{Task: "shared-docker", Status: "blocked", Error: "no space left on device"},
		}}, nil
	}
	t.Cleanup(func() { checkReadiness = original })
	root := newRootCmd()
	root.SetArgs([]string{"benchmark", "readiness"})
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err == nil {
		t.Fatal("readiness command accepted failed tasks")
	}
	for _, wanted := range []string{"missing-contract: failed: no mandatory environment readiness contract", "shared-docker: blocked: no space left on device"} {
		if !strings.Contains(output.String(), wanted) {
			t.Fatalf("output = %q, missing %q", output.String(), wanted)
		}
	}
}
