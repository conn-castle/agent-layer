package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	bench "github.com/conn-castle/agent-layer/internal/benchmark"
)

const (
	benchmarkCommandName   = "benchmark"
	benchmarkInitName      = "init"
	benchmarkRunName       = "run"
	benchmarkReadinessName = "readiness"
	benchmarkHeartbeatWait = time.Minute
	benchmarkCellCompleted = "Completed benchmark cell"
)

var (
	runStudy                                    = bench.RunStudy
	checkReadiness                              = bench.CheckAllTaskReadiness
	initStudy                                   = bench.InitStudy
	benchmarkArchitectureWarning                = bench.TaskContainerEmulationWarning
	benchmarkHeartbeatClock      benchmarkClock = wallBenchmarkClock{}
)

func printBenchmarkArchitectureWarning(out io.Writer) {
	if warning := benchmarkArchitectureWarning(); warning != "" {
		_, _ = fmt.Fprintf(out, "[warning] %s\n", warning)
	}
}

type benchmarkTimer interface {
	C() <-chan time.Time
	Reset(time.Duration)
	Stop()
}

type benchmarkClock interface {
	Now() time.Time
	NewTimer(time.Duration) benchmarkTimer
}

type wallBenchmarkClock struct{}

func (wallBenchmarkClock) Now() time.Time { return time.Now() }

func (wallBenchmarkClock) NewTimer(wait time.Duration) benchmarkTimer {
	return wallBenchmarkTimer{Timer: time.NewTimer(wait)}
}

type wallBenchmarkTimer struct{ *time.Timer }

func (timer wallBenchmarkTimer) C() <-chan time.Time { return timer.Timer.C }

func (timer wallBenchmarkTimer) Reset(wait time.Duration) {
	if !timer.Timer.Stop() {
		select {
		case <-timer.Timer.C:
		default:
		}
	}
	timer.Timer.Reset(wait)
}

func (timer wallBenchmarkTimer) Stop() { timer.Timer.Stop() }

func formatBenchmarkStudyProgress(progress bench.StudyProgress) string {
	details := make([]string, 0, 2)
	if progress.Experiment != "" {
		details = append(details, progress.Experiment)
	}
	if progress.Task != "" {
		details = append(details, progress.Task)
	}
	if progress.Required == 0 {
		status := fmt.Sprintf("[%s] %s", progress.Phase, progress.Message)
		if progress.EffectiveTimeout > 0 {
			status += fmt.Sprintf(" (configured timeout budget %s)", progress.EffectiveTimeout)
		}
		if progress.MaximumAttempts > 1 {
			status += fmt.Sprintf(" (up to %d attempts)", progress.MaximumAttempts)
		}
		if len(details) > 0 {
			status += ": " + strings.Join(details, " ")
		}
		return status
	}
	percent := progress.Completed * 100 / progress.Required
	status := fmt.Sprintf("[%3d%% %d/%d] %s", percent, progress.Completed, progress.Required, progress.Message)
	if len(details) > 0 {
		status += ": " + strings.Join(details, " ")
	}
	return status
}

type benchmarkProgressKey struct {
	experiment string
	task       string
	attempt    int
}

type benchmarkActiveProgress struct {
	progress bench.StudyProgress
	sequence uint64
}

// benchmarkProgressState keeps completion updates from hiding a concurrently
// active paid cell. Progress callbacks are synchronized by the CLI's output
// mutex, so this state deliberately needs no internal lock.
type benchmarkProgressState struct {
	latest   bench.StudyProgress
	active   map[benchmarkProgressKey]benchmarkActiveProgress
	sequence uint64
}

func newBenchmarkProgressState() *benchmarkProgressState {
	return &benchmarkProgressState{
		latest: bench.StudyProgress{Message: "setting up benchmark"},
		active: make(map[benchmarkProgressKey]benchmarkActiveProgress),
	}
}

func (state *benchmarkProgressState) observe(progress bench.StudyProgress) {
	state.latest = progress
	if progress.Experiment == "" || progress.Task == "" || progress.Attempt < 1 {
		return
	}
	key := benchmarkProgressKey{experiment: progress.Experiment, task: progress.Task, attempt: progress.Attempt}
	if progress.Message == benchmarkCellCompleted {
		delete(state.active, key)
		return
	}
	state.sequence++
	state.active[key] = benchmarkActiveProgress{progress: progress, sequence: state.sequence}
}

func (state *benchmarkProgressState) current() bench.StudyProgress {
	latest := state.latest
	var latestSequence uint64
	for _, candidate := range state.active {
		if candidate.sequence > latestSequence {
			latest, latestSequence = candidate.progress, candidate.sequence
		}
	}
	return latest
}

// benchmarkHeartbeatShouldEmit gives already-pending activity priority over an
// expired timer, so a timer/activity race cannot produce a misleading heartbeat.
func benchmarkHeartbeatShouldEmit(stop <-chan struct{}, activity <-chan struct{}) bool {
	select {
	case <-stop:
		return false
	default:
	}
	select {
	case <-activity:
		return false
	default:
		return true
	}
}

func runBenchmarkInactivityHeartbeat(stop <-chan struct{}, activity <-chan struct{}, clock benchmarkClock, status func() string, output func(string)) {
	timer := clock.NewTimer(benchmarkHeartbeatWait)
	defer timer.Stop()
	started := clock.Now()
	for {
		select {
		case <-stop:
			return
		case <-activity:
			timer.Reset(benchmarkHeartbeatWait)
		case <-timer.C():
			if !benchmarkHeartbeatShouldEmit(stop, activity) {
				timer.Reset(benchmarkHeartbeatWait)
				continue
			}
			output(fmt.Sprintf("[running] %s; elapsed %s.\n", status(), clock.Now().Sub(started).Round(time.Second)))
			timer.Reset(benchmarkHeartbeatWait)
		}
	}
}

func newBenchmarkCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   benchmarkCommandName,
		Short: "Run a reproducible DeepSWE comparison",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newBenchmarkInitCmd(), newBenchmarkRunCmd(), newBenchmarkReadinessCmd())
	return command
}

func newBenchmarkInitCmd() *cobra.Command {
	var directory string
	command := &cobra.Command{
		Use:   benchmarkInitName + " <selection.json>",
		Short: "Create a ready-to-run benchmark study from a website selection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if directory == "" {
				directory = "benchmark-study"
			}
			path, err := initStudy(bench.InitStudyOptions{RepoRoot: root, SelectionPath: args[0], Directory: directory})
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Benchmark study created: %s\nNext: al benchmark run %s --dry-run\n", path, path)
			return err
		},
	}
	command.Flags().StringVar(&directory, "directory", "", "study directory (default: benchmark-study)")
	return command
}

func newBenchmarkRunCmd() *cobra.Command {
	var dryRun bool
	var recoveryOnly bool
	var taskConcurrency int
	var tasks []string
	command := &cobra.Command{
		Use:   benchmarkRunName + " <study.toml>",
		Short: "Run or resume the experiments declared by one study manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRun && recoveryOnly {
				return fmt.Errorf("--dry-run and --recover-only cannot be combined")
			}
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if !recoveryOnly {
				printBenchmarkArchitectureWarning(cmd.OutOrStdout())
			}
			if taskConcurrency == 0 {
				if recoveryOnly {
					taskConcurrency = 1
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "[setup] Recovery-only mode: no provider or verifier process will be started.")
				} else {
					taskConcurrency = bench.AutomaticTaskConcurrency(true)
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[setup] Automatic paid task concurrency: %d (serialized by safety policy; use --task-concurrency to override).\n", taskConcurrency)
				}
			}
			var outputMu sync.Mutex
			progressState := newBenchmarkProgressState()
			activity := make(chan struct{}, 1)
			markActivity := func() {
				select {
				case activity <- struct{}{}:
				default:
				}
			}
			options := bench.StudyOptions{
				RepoRoot: root, StudyPath: args[0], DryRun: dryRun, RecoveryOnly: recoveryOnly,
				TaskConcurrency: taskConcurrency, Tasks: tasks, ResourcePreflight: true, ReclaimTaskImages: true,
			}
			options.OnProgress = func(progress bench.StudyProgress) {
				outputMu.Lock()
				defer outputMu.Unlock()
				progressState.observe(progress)
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), formatBenchmarkStudyProgress(progress))
				markActivity()
			}
			options.OnPrepared = func(outcome bench.StudyOutcome) error {
				outputMu.Lock()
				defer outputMu.Unlock()
				defer markActivity()
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Study %.12s: %d of %d cells cached, %d missing.\n", outcome.StudyID, outcome.Completed, outcome.Required, outcome.Missing); err != nil {
					return err
				}
				for _, experiment := range outcome.Experiments {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "- %s: %d of %d cells cached, %d missing.\n", experiment.Name, experiment.Completed, experiment.Required, experiment.Missing); err != nil {
						return err
					}
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), formatBenchmarkWorkflow(experiment)); err != nil {
						return err
					}
				}
				if !recoveryOnly && outcome.Missing > 0 && outcome.ExecutionTimeoutSum > 0 {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Configured timeout sum across missing cells: %s at concurrency %d. This excludes cleanup overhead and is not an ETA or hard deadline.\n", outcome.ExecutionTimeoutSum, taskConcurrency); err != nil {
						return err
					}
				}
				if !recoveryOnly && outcome.Missing > 0 {
					disclosure := "This command authorizes paid provider calls for missing cells. Agent Layer cost is not reliably estimable in advance."
					switch {
					case outcome.HasBareExperiment && outcome.BarePublishedEstimateUSD != nil:
						disclosure += fmt.Sprintf(" Published-data bare estimate: $%.2f (not an Agent Layer estimate).", *outcome.BarePublishedEstimateUSD)
					case outcome.HasBareExperiment:
						disclosure += " No published-data estimate is available for this study's bare target."
					default:
						disclosure += " No published-data bare estimate is available because this study has no bare experiment."
					}
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), disclosure); err != nil {
						return err
					}
				}
				if dryRun {
					_, err := fmt.Fprintln(cmd.OutOrStdout(), "Dry run completed validation and preflight discovery. No inference call was made.")
					return err
				}
				return nil
			}
			options.OnCellComplete = func(cost bench.ObservedCostRange) {
				outputMu.Lock()
				defer outputMu.Unlock()
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cumulative observed cost: $%.2f (range $%.2f–$%.2f).\n", cost.Midpoint, cost.Minimum, cost.Maximum)
				markActivity()
			}
			heartbeatStop := make(chan struct{})
			heartbeatDone := make(chan struct{})
			go func() {
				defer close(heartbeatDone)
				runBenchmarkInactivityHeartbeat(heartbeatStop, activity, benchmarkHeartbeatClock, func() string {
					outputMu.Lock()
					defer outputMu.Unlock()
					progress := progressState.current()
					status := formatBenchmarkStudyProgress(progress)
					if !progress.StartedAt.IsZero() {
						status += fmt.Sprintf("; phase elapsed %s", benchmarkHeartbeatClock.Now().Sub(progress.StartedAt).Round(time.Second))
					}
					return status
				}, func(message string) {
					outputMu.Lock()
					defer outputMu.Unlock()
					_, _ = fmt.Fprint(cmd.OutOrStdout(), message)
				})
			}()
			outcome, err := runStudy(cmd.Context(), options, nil)
			close(heartbeatStop)
			<-heartbeatDone
			if err != nil {
				return err
			}
			if dryRun {
				return nil
			}
			if recoveryOnly {
				report := ""
				if outcome.JSONPath != "" {
					report = fmt.Sprintf(" Report: %s.", outcome.JSONPath)
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Recovery completed: %d terminal verifier timeout cell(s) canonicalized; %d of %d cells cached, %d still missing.%s No provider or verifier process was started.\n", outcome.RecoveredCells, outcome.Completed, outcome.Required, outcome.Missing, report)
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Study %.12s: %d of %d cells cached, $%.2f cumulative observed cost in this invocation (range $%.2f–$%.2f). Report: %s\n", outcome.StudyID, outcome.Completed, outcome.Required, outcome.ObservedInvocationCost.Midpoint, outcome.ObservedInvocationCost.Minimum, outcome.ObservedInvocationCost.Maximum, outcome.JSONPath)
			return err
		},
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate and preflight without inference calls")
	command.Flags().BoolVar(&recoveryOnly, "recover-only", false, "finalize retained terminal verifier test timeouts or regenerate a completed study report without provider or verifier execution")
	command.Flags().IntVar(&taskConcurrency, "task-concurrency", 0, "parallel task cells, from 1 to 8 (default: automatic)")
	command.Flags().StringArrayVar(&tasks, "task", nil, "execute only this selected task; repeatable")
	return command
}

func formatBenchmarkWorkflow(experiment bench.StudyExperimentProgress) string {
	switch {
	case !experiment.AgentLayer:
		return "  workflow: bare provider; no Agent Layer skills or dispatches"
	case !experiment.Skills:
		return "  workflow: Agent Layer treatment without skills; no dispatch workflow"
	case len(experiment.RequiredDispatchRoles) == 0:
		return "  workflow: Agent Layer skills with no required dispatch roles; single-agent execution is allowed"
	default:
		parts := make([]string, 0, len(experiment.DispatchTargets))
		for _, target := range experiment.DispatchTargets {
			parts = append(parts, fmt.Sprintf("%s={agent=%s, model=%s, reasoning=%s, dispatch_start role=%s}", target.Role, target.Agent, target.Model, target.ReasoningEffort, target.Role))
		}
		return fmt.Sprintf("  workflow: Agent Layer skills; mandatory dispatches: %s", strings.Join(parts, ", "))
	}
}

func newBenchmarkReadinessCmd() *cobra.Command {
	var taskConcurrency int
	var removeTaskImages bool
	var tasks []string
	var taskShardIndex int
	var taskShardCount int
	var taskTimeout time.Duration
	var studyPath string
	command := &cobra.Command{
		Use:   benchmarkReadinessName,
		Short: "Preflight every task in the pinned DeepSWE catalog",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if studyPath != "" {
				if len(tasks) > 0 {
					return fmt.Errorf("use either --study or --task, not both")
				}
				tasks, err = bench.StudyTaskIDs(studyPath)
				if err != nil {
					return err
				}
			}
			if taskConcurrency > 1 && removeTaskImages && !cmd.Flags().Changed("remove-task-images") {
				removeTaskImages = false
			}
			printBenchmarkArchitectureWarning(cmd.OutOrStdout())
			if taskConcurrency == 0 {
				if removeTaskImages {
					taskConcurrency = 1
				} else {
					taskConcurrency = bench.AutomaticTaskConcurrency(false)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[setup] Automatic task concurrency: %d; disposable task images: %s.\n", taskConcurrency, map[bool]string{true: "reclaimed", false: "retained"}[removeTaskImages])
			}
			options := bench.ReadinessAuditOptions{
				RepoRoot: root, TaskConcurrency: taskConcurrency, RemoveTaskImages: removeTaskImages,
				Tasks: tasks, TaskShardIndex: taskShardIndex, TaskShardCount: taskShardCount, TaskTimeout: taskTimeout, ResourcePreflight: true,
			}
			var outputMu sync.Mutex
			options.OnTaskProgress = func(progress bench.ReadinessAuditProgress) {
				outputMu.Lock()
				defer outputMu.Unlock()
				if progress.Task == "" {
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s\n", progress.Phase, progress.Message)
					return
				}
				percent := 0
				if progress.Required > 0 {
					percent = progress.Completed * 100 / progress.Required
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%3d%% %d/%d] %s: %s\n", percent, progress.Completed, progress.Required, progress.Task, progress.Status)
			}
			outcome, err := checkReadiness(cmd.Context(), options)
			if err != nil {
				return err
			}
			blockedReason := "shared infrastructure"
			for _, task := range outcome.Tasks {
				if strings.Contains(strings.ToLower(task.Error), "no space left on device") {
					blockedReason = "Docker disk exhaustion"
					break
				}
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "DeepSWE readiness %s: %d of %d tasks certified, %d failed, %d blocked by %s. No provider call was made.\n", outcome.DeepSWECommit, outcome.Certified, outcome.Required, outcome.Failed, outcome.Blocked, blockedReason); err != nil {
				return err
			}
			for _, task := range outcome.Tasks {
				if task.Status == "certified" {
					continue
				}
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "- %s: %s: %s\n", task.Task, task.Status, task.Error); err != nil {
					return err
				}
			}
			if outcome.Blocked > 0 {
				for _, task := range outcome.Tasks {
					if strings.Contains(strings.ToLower(task.Error), "no space left on device") {
						return fmt.Errorf("docker disk capacity was exhausted while certifying %s; task images are reclaimed automatically, but more free Docker disk is required", task.Task)
					}
				}
				return fmt.Errorf("DeepSWE readiness audit is blocked by shared infrastructure for %d task(s)", outcome.Blocked)
			}
			if outcome.Failed > 0 {
				return fmt.Errorf("DeepSWE readiness audit failed for %d task(s)", outcome.Failed)
			}
			return nil
		},
	}
	command.Flags().IntVar(&taskConcurrency, "task-concurrency", 0, "parallel task readiness checks, from 1 to 8 (default: automatic)")
	command.Flags().BoolVar(&removeTaskImages, "remove-task-images", true, "remove exact task images after certification to bound Docker disk usage; requires task concurrency 1 and is disabled by explicit parallelism")
	command.Flags().StringVar(&studyPath, "study", "", "certify only tasks selected by this study.toml")
	command.Flags().StringArrayVar(&tasks, "task", nil, "certify only this catalog task; repeatable")
	command.Flags().IntVar(&taskShardIndex, "task-shard-index", 1, "one-based deterministic task shard to certify")
	command.Flags().IntVar(&taskShardCount, "task-shard-count", 1, "total deterministic task shards, from 1 to 32")
	command.Flags().DurationVar(&taskTimeout, "task-timeout", 0, "maximum certification time per task; zero disables the per-task timeout")
	return command
}
