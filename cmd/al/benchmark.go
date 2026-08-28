package main

import (
	"fmt"
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
)

var (
	runStudy       = bench.RunStudy
	checkReadiness = bench.CheckAllTaskReadiness
	initStudy      = bench.InitStudy
)

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
	var taskConcurrency int
	var tasks []string
	command := &cobra.Command{
		Use:   benchmarkRunName + " <study.toml>",
		Short: "Run or resume the experiments declared by one study manifest",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if taskConcurrency == 0 {
				taskConcurrency = bench.AutomaticTaskConcurrency(true)
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[setup] Automatic task concurrency: %d (host and container architecture aware).\n", taskConcurrency)
			}
			var outputMu sync.Mutex
			lastStatus := "setting up benchmark"
			options := bench.StudyOptions{
				RepoRoot: root, StudyPath: args[0], DryRun: dryRun,
				TaskConcurrency: taskConcurrency, Tasks: tasks, ResourcePreflight: true, ReclaimTaskImages: true,
			}
			options.OnProgress = func(progress bench.StudyProgress) {
				outputMu.Lock()
				defer outputMu.Unlock()
				lastStatus = progress.Message
				if progress.Required > 0 {
					percent := progress.Completed * 100 / progress.Required
					_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%3d%% %d/%d] %s", percent, progress.Completed, progress.Required, progress.Message)
					if progress.Experiment != "" || progress.Task != "" {
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), ": %s %s", progress.Experiment, progress.Task)
					}
					_, _ = fmt.Fprintln(cmd.OutOrStdout())
					return
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s\n", progress.Phase, progress.Message)
			}
			options.OnPrepared = func(outcome bench.StudyOutcome) error {
				outputMu.Lock()
				defer outputMu.Unlock()
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Study %.12s: %d of %d cells cached, %d missing.\n", outcome.StudyID, outcome.Completed, outcome.Required, outcome.Missing); err != nil {
					return err
				}
				for _, experiment := range outcome.Experiments {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "- %s: %d of %d cells cached, %d missing.\n", experiment.Name, experiment.Completed, experiment.Required, experiment.Missing); err != nil {
						return err
					}
				}
				if outcome.Missing > 0 {
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
			}
			heartbeatStop := make(chan struct{})
			heartbeatDone := make(chan struct{})
			started := time.Now()
			go func() {
				defer close(heartbeatDone)
				ticker := time.NewTicker(30 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-heartbeatStop:
						return
					case <-ticker.C:
						outputMu.Lock()
						_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[running] %s; elapsed %s.\n", lastStatus, time.Since(started).Round(time.Second))
						outputMu.Unlock()
					}
				}
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
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Study %.12s: %d of %d cells cached, $%.2f cumulative observed cost in this invocation (range $%.2f–$%.2f). Report: %s\n", outcome.StudyID, outcome.Completed, outcome.Required, outcome.ObservedInvocationCost.Midpoint, outcome.ObservedInvocationCost.Minimum, outcome.ObservedInvocationCost.Maximum, outcome.JSONPath)
			return err
		},
	}
	command.Flags().BoolVar(&dryRun, "dry-run", false, "validate and preflight without inference calls")
	command.Flags().IntVar(&taskConcurrency, "task-concurrency", 0, "parallel task cells, from 1 to 8 (default: automatic)")
	command.Flags().StringArrayVar(&tasks, "task", nil, "execute only this selected task; repeatable")
	return command
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
