package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	bench "github.com/conn-castle/agent-layer/internal/benchmark"
)

type benchmarkTestClock struct {
	mu              sync.Mutex
	now             time.Time
	timer           *benchmarkTestTimer
	changed         chan struct{}
	resetGeneration uint64
}

type benchmarkTestTimer struct {
	clock    *benchmarkTestClock
	deadline time.Time
	active   bool
	channel  chan time.Time
}

func newBenchmarkTestClock() *benchmarkTestClock {
	return &benchmarkTestClock{now: time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC), changed: make(chan struct{}, 16)}
}

func (clock *benchmarkTestClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *benchmarkTestClock) NewTimer(wait time.Duration) benchmarkTimer {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.timer = &benchmarkTestTimer{clock: clock, deadline: clock.now.Add(wait), active: true, channel: make(chan time.Time, 1)}
	clock.changedLocked()
	return clock.timer
}

func (clock *benchmarkTestClock) Advance(wait time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(wait)
	if clock.timer != nil && clock.timer.active && !clock.timer.deadline.After(clock.now) {
		clock.timer.active = false
		clock.timer.channel <- clock.now
	}
	clock.changedLocked()
}

func (clock *benchmarkTestClock) changedLocked() {
	select {
	case clock.changed <- struct{}{}:
	default:
	}
}

func (clock *benchmarkTestClock) timerState() (uint64, time.Time, bool) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	if clock.timer == nil {
		return clock.resetGeneration, time.Time{}, false
	}
	return clock.resetGeneration, clock.timer.deadline, clock.timer.active
}

func (timer *benchmarkTestTimer) C() <-chan time.Time { return timer.channel }

func (timer *benchmarkTestTimer) Reset(wait time.Duration) {
	timer.clock.mu.Lock()
	defer timer.clock.mu.Unlock()
	select {
	case <-timer.channel:
	default:
	}
	timer.deadline, timer.active = timer.clock.now.Add(wait), true
	timer.clock.resetGeneration++
	timer.clock.changedLocked()
}

func (timer *benchmarkTestTimer) Stop() {
	timer.clock.mu.Lock()
	defer timer.clock.mu.Unlock()
	timer.active = false
	timer.clock.changedLocked()
}

type benchmarkTestOutput struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	written chan struct{}
}

func newBenchmarkTestOutput() *benchmarkTestOutput {
	return &benchmarkTestOutput{written: make(chan struct{}, 16)}
}

func (output *benchmarkTestOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	n, err := output.buffer.Write(data)
	select {
	case output.written <- struct{}{}:
	default:
	}
	return n, err
}

func (output *benchmarkTestOutput) String() string {
	output.mu.Lock()
	defer output.mu.Unlock()
	return output.buffer.String()
}

const benchmarkTestTimeout = 2 * time.Second

func waitForBenchmarkSignal(t *testing.T, description string, signal <-chan struct{}) {
	t.Helper()
	timer := time.NewTimer(benchmarkTestTimeout)
	defer timer.Stop()
	select {
	case <-signal:
	case <-timer.C:
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForBenchmarkTimerReset(t *testing.T, clock *benchmarkTestClock, after uint64) uint64 {
	t.Helper()
	timer := time.NewTimer(benchmarkTestTimeout)
	defer timer.Stop()
	for {
		generation, _, _ := clock.timerState()
		if generation > after {
			return generation
		}
		select {
		case <-clock.changed:
		case <-timer.C:
			generation, deadline, active := clock.timerState()
			t.Fatalf("timed out waiting for timer reset after generation %d (got %d, deadline %s, active=%t)", after, generation, deadline, active)
		}
	}
}

func waitForBenchmarkHeartbeat(t *testing.T, output *benchmarkTestOutput, count int) {
	t.Helper()
	timer := time.NewTimer(benchmarkTestTimeout)
	defer timer.Stop()
	for strings.Count(output.String(), "[running]") < count {
		select {
		case <-output.written:
		case <-timer.C:
			t.Fatalf("timed out waiting for heartbeat %d: %q", count, output.String())
		}
	}
}

func advanceBenchmarkRun(t *testing.T, advance chan<- struct{}) {
	t.Helper()
	timer := time.NewTimer(benchmarkTestTimeout)
	defer timer.Stop()
	select {
	case advance <- struct{}{}:
	case <-timer.C:
		t.Fatal("timed out advancing benchmark run")
	}
}

func waitForBenchmarkStage(t *testing.T, stages <-chan string, want string) {
	t.Helper()
	timer := time.NewTimer(benchmarkTestTimeout)
	defer timer.Stop()
	select {
	case got := <-stages:
		if got != want {
			t.Fatalf("stage = %q, want %q", got, want)
		}
	case <-timer.C:
		t.Fatalf("timed out waiting for benchmark stage %q", want)
	}
}

func TestBenchmarkExposesBrainlessWorkflow(t *testing.T) {
	command := newBenchmarkCmd()
	var names []string
	for _, child := range command.Commands() {
		names = append(names, child.Name())
	}
	if strings.Join(names, ",") != "init,readiness,run" {
		t.Fatalf("benchmark commands = %v", names)
	}
}

func TestFormatBenchmarkStudyProgressIncludesOnlyPresentDetails(t *testing.T) {
	for _, test := range []struct {
		name       string
		experiment string
		task       string
		want       string
	}{
		{name: "both", experiment: "treatment", task: "task-a", want: "[ 50% 1/2] Running cell: treatment task-a"},
		{name: "experiment only", experiment: "treatment", want: "[ 50% 1/2] Running cell: treatment"},
		{name: "task only", task: "task-a", want: "[ 50% 1/2] Running cell: task-a"},
		{name: "neither", want: "[ 50% 1/2] Running cell"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := formatBenchmarkStudyProgress(bench.StudyProgress{
				Message: "Running cell", Experiment: test.experiment, Task: test.task, Completed: 1, Required: 2,
			})
			if got != test.want {
				t.Fatalf("formatBenchmarkStudyProgress() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestBenchmarkRunDryRunDoesNotInvokeProviderInference(t *testing.T) {
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

func TestBenchmarkCommandsWarnOnlyWhenTaskContainersRequireEmulation(t *testing.T) {
	originalWarning := benchmarkArchitectureWarning
	benchmarkArchitectureWarning = func() string {
		return "DeepSWE uses linux/amd64 task containers. Host architecture arm64 requires emulation; the first wait can take 30+ minutes."
	}
	t.Cleanup(func() { benchmarkArchitectureWarning = originalWarning })

	originalRun := runStudy
	runStudy = func(_ context.Context, options bench.StudyOptions, _ bench.TaskExecutor) (bench.StudyOutcome, error) {
		return bench.StudyOutcome{}, nil
	}
	t.Cleanup(func() { runStudy = originalRun })

	root := newRootCmd()
	root.SetArgs([]string{"benchmark", "run", "study.toml", "--dry-run", "--task-concurrency", "1"})
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "[warning]") || !strings.Contains(got, "requires emulation") || !strings.Contains(got, "30+ minutes") {
		t.Fatalf("emulation warning output = %q", got)
	}

	originalReadiness := checkReadiness
	checkReadiness = func(_ context.Context, _ bench.ReadinessAuditOptions) (bench.ReadinessAuditOutcome, error) {
		return bench.ReadinessAuditOutcome{}, nil
	}
	t.Cleanup(func() { checkReadiness = originalReadiness })
	output.Reset()
	root = newRootCmd()
	root.SetArgs([]string{"benchmark", "readiness", "--task-concurrency", "1"})
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); !strings.Contains(got, "[warning]") || !strings.Contains(got, "requires emulation") {
		t.Fatalf("readiness emulation warning output = %q", got)
	}

	benchmarkArchitectureWarning = func() string { return "" }
	output.Reset()
	root = newRootCmd()
	root.SetArgs([]string{"benchmark", "run", "study.toml", "--dry-run", "--task-concurrency", "1"})
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "[warning]") {
		t.Fatalf("native architecture output contained warning: %q", output.String())
	}
}

func TestBenchmarkReadinessDoesNotWarnBeforeFlagValidation(t *testing.T) {
	originalWarning := benchmarkArchitectureWarning
	benchmarkArchitectureWarning = func() string {
		return "DeepSWE uses linux/amd64 task containers. Host architecture arm64 requires emulation; the first wait can take 30+ minutes."
	}
	t.Cleanup(func() { benchmarkArchitectureWarning = originalWarning })

	root := newRootCmd()
	root.SetArgs([]string{"benchmark", "readiness", "--study", "study.toml", "--task", "first-task"})
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "use either --study or --task, not both") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(output.String(), "[warning]") {
		t.Fatalf("usage error was preceded by emulation warning: %q", output.String())
	}
}

func TestBenchmarkRunHeartbeatWaitsForInactivityAndUsesLatestProgress(t *testing.T) {
	originalRun, originalClock := runStudy, benchmarkHeartbeatClock
	clock := newBenchmarkTestClock()
	benchmarkHeartbeatClock = clock
	t.Cleanup(func() {
		runStudy, benchmarkHeartbeatClock = originalRun, originalClock
	})
	advanceRun := make(chan struct{})
	stages := make(chan string, 4)
	waitForAdvance := func() error {
		timer := time.NewTimer(benchmarkTestTimeout)
		defer timer.Stop()
		select {
		case <-advanceRun:
			return nil
		case <-timer.C:
			return errors.New("timed out waiting for benchmark test advancement")
		}
	}
	runStudy = func(_ context.Context, options bench.StudyOptions, _ bench.TaskExecutor) (bench.StudyOutcome, error) {
		options.OnProgress(bench.StudyProgress{Phase: "resources", Message: "Checking Docker disk capacity before image pulls"})
		stages <- "capacity"
		if err := waitForAdvance(); err != nil {
			return bench.StudyOutcome{}, err
		}
		options.OnProgress(bench.StudyProgress{Phase: "runtime-preflight", Message: "Preflighting benchmark runtime", Experiment: "treatment", Task: "task-a", Completed: 1, Required: 2})
		stages <- "progress"
		if err := waitForAdvance(); err != nil {
			return bench.StudyOutcome{}, err
		}
		if err := options.OnPrepared(bench.StudyOutcome{StudyID: strings.Repeat("a", 64), Required: 2, Missing: 1, Experiments: []bench.StudyExperimentProgress{{Name: "treatment", Required: 2, Missing: 1}}}); err != nil {
			return bench.StudyOutcome{}, err
		}
		stages <- "prepared"
		if err := waitForAdvance(); err != nil {
			return bench.StudyOutcome{}, err
		}
		options.OnCellComplete(bench.ObservedCostRange{})
		stages <- "cell"
		if err := waitForAdvance(); err != nil {
			return bench.StudyOutcome{}, err
		}
		return bench.StudyOutcome{StudyID: strings.Repeat("a", 64)}, nil
	}
	root := newRootCmd()
	root.SetArgs([]string{"benchmark", "run", "study.toml", "--task-concurrency", "1"})
	output := newBenchmarkTestOutput()
	root.SetOut(output)
	root.SetErr(output)
	done := make(chan error, 1)
	resetGeneration, _, _ := clock.timerState()
	go func() { done <- root.Execute() }()
	waitForBenchmarkStage(t, stages, "capacity")
	waitForBenchmarkTimerReset(t, clock, resetGeneration)
	clock.Advance(59 * time.Second)
	if strings.Contains(output.String(), "[running]") {
		t.Fatalf("heartbeat before inactivity: %q", output.String())
	}
	resetGeneration, _, _ = clock.timerState()
	advanceBenchmarkRun(t, advanceRun)
	waitForBenchmarkStage(t, stages, "progress")
	waitForBenchmarkTimerReset(t, clock, resetGeneration)
	clock.Advance(59 * time.Second)
	if strings.Contains(output.String(), "[running]") {
		t.Fatalf("progress output did not reset inactivity: %q", output.String())
	}
	resetGeneration, _, _ = clock.timerState()
	advanceBenchmarkRun(t, advanceRun)
	waitForBenchmarkStage(t, stages, "prepared")
	waitForBenchmarkTimerReset(t, clock, resetGeneration)
	clock.Advance(59 * time.Second)
	if strings.Contains(output.String(), "[running]") {
		t.Fatalf("prepared output did not reset inactivity: %q", output.String())
	}
	resetGeneration, _, _ = clock.timerState()
	advanceBenchmarkRun(t, advanceRun)
	waitForBenchmarkStage(t, stages, "cell")
	waitForBenchmarkTimerReset(t, clock, resetGeneration)
	clock.Advance(59 * time.Second)
	if strings.Contains(output.String(), "[running]") {
		t.Fatalf("cell output did not reset inactivity: %q", output.String())
	}
	resetGeneration, _, _ = clock.timerState()
	clock.Advance(time.Second)
	waitForBenchmarkHeartbeat(t, output, 1)
	waitForBenchmarkTimerReset(t, clock, resetGeneration)
	if got := output.String(); !strings.Contains(got, "Preflighting benchmark runtime: treatment task-a") || strings.Contains(got, "[running] [resources]") {
		t.Fatalf("heartbeat did not retain latest formatted progress: %q", got)
	}
	clock.Advance(59 * time.Second)
	if got := strings.Count(output.String(), "[running]"); got != 1 {
		t.Fatalf("heartbeats during continued inactivity = %d", got)
	}
	clock.Advance(time.Second)
	waitForBenchmarkHeartbeat(t, output, 2)
	advanceBenchmarkRun(t, advanceRun)
	doneSignal := make(chan struct{})
	var runErr error
	go func() {
		runErr = <-done
		close(doneSignal)
	}()
	waitForBenchmarkSignal(t, "benchmark command shutdown", doneSignal)
	if runErr != nil {
		t.Fatal(runErr)
	}
}

func TestBenchmarkHeartbeatExpiryDefersToPendingActivity(t *testing.T) {
	activity := make(chan struct{}, 1)
	activity <- struct{}{}
	if benchmarkHeartbeatShouldEmit(make(chan struct{}), activity) {
		t.Fatal("heartbeat would emit despite pending activity")
	}
}

func TestBenchmarkReadinessRunsWithoutProviderCalls(t *testing.T) {
	original := checkReadiness
	checkReadiness = func(_ context.Context, options bench.ReadinessAuditOptions) (bench.ReadinessAuditOutcome, error) {
		if options.TaskConcurrency != 1 || !options.RemoveTaskImages ||
			!options.ResourcePreflight ||
			strings.Join(options.Tasks, ",") != "first-task,second-task" ||
			options.TaskShardIndex != 2 || options.TaskShardCount != 8 || options.TaskTimeout.String() != "10m0s" {
			t.Fatalf("options = %#v", options)
		}
		options.OnTaskProgress(bench.ReadinessAuditProgress{Task: "first-task", Status: "checking", Required: 2})
		options.OnTaskProgress(bench.ReadinessAuditProgress{Task: "first-task", Status: "certified", Completed: 1, Required: 2})
		return bench.ReadinessAuditOutcome{DeepSWECommit: strings.Repeat("d", 40), Required: 2, Certified: 2}, nil
	}
	t.Cleanup(func() { checkReadiness = original })
	root := newRootCmd()
	root.SetArgs([]string{"benchmark", "readiness", "--task-concurrency", "1", "--remove-task-images", "--task", "first-task", "--task", "second-task", "--task-shard-index", "2", "--task-shard-count", "8", "--task-timeout", "10m"})
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "[  0% 0/2] first-task: checking") || !strings.Contains(output.String(), "[ 50% 1/2] first-task: certified") ||
		!strings.Contains(output.String(), "2 of 2 tasks certified") || !strings.Contains(output.String(), "No provider call was made") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestBenchmarkReadinessExplicitParallelismRetainsImages(t *testing.T) {
	original := checkReadiness
	checkReadiness = func(_ context.Context, options bench.ReadinessAuditOptions) (bench.ReadinessAuditOutcome, error) {
		if options.TaskConcurrency != 4 || options.RemoveTaskImages {
			t.Fatalf("options = %#v", options)
		}
		return bench.ReadinessAuditOutcome{DeepSWECommit: strings.Repeat("d", 40)}, nil
	}
	t.Cleanup(func() { checkReadiness = original })
	root := newRootCmd()
	root.SetArgs([]string{"benchmark", "readiness", "--task-concurrency", "4"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
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
	if !strings.Contains(output.String(), "blocked by Docker disk exhaustion") {
		t.Fatalf("disk exhaustion was concealed by a generic summary: %q", output.String())
	}
}
