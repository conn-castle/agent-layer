package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/spf13/cobra"

	bench "github.com/conn-castle/agent-layer/internal/benchmark"
)

const (
	benchmarkCommandName   = "benchmark"
	benchmarkRunName       = "run"
	benchmarkReadinessName = "readiness"
)

var (
	runStudy       = bench.RunStudy
	checkReadiness = bench.CheckAllTaskReadiness
)

func newBenchmarkCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   benchmarkCommandName,
		Short: "Run a reproducible DeepSWE comparison",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newBenchmarkRunCmd(), newBenchmarkReadinessCmd())
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
			options := bench.StudyOptions{
				RepoRoot: root, StudyPath: args[0], DryRun: dryRun,
				TaskConcurrency: taskConcurrency, Tasks: tasks,
			}
			options.OnPrepared = func(outcome bench.StudyOutcome) error {
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
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cumulative observed cost: $%.2f (range $%.2f–$%.2f).\n", cost.Midpoint, cost.Minimum, cost.Maximum)
			}
			outcome, err := runStudy(context.Background(), options, nil)
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
	command.Flags().IntVar(&taskConcurrency, "task-concurrency", 1, "parallel task cells, from 1 to 8")
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
	command := &cobra.Command{
		Use:   benchmarkReadinessName,
		Short: "Preflight every task in the pinned DeepSWE catalog",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			options := bench.ReadinessAuditOptions{
				RepoRoot: root, TaskConcurrency: taskConcurrency, RemoveTaskImages: removeTaskImages,
				Tasks: tasks, TaskShardIndex: taskShardIndex, TaskShardCount: taskShardCount, TaskTimeout: taskTimeout,
			}
			var outputMu sync.Mutex
			options.OnTaskProgress = func(progress bench.ReadinessAuditProgress) {
				outputMu.Lock()
				defer outputMu.Unlock()
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "[%d/%d] %s: %s\n", progress.Completed, progress.Required, progress.Task, progress.Status)
			}
			outcome, err := checkReadiness(context.Background(), options)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "DeepSWE readiness %s: %d of %d tasks certified, %d failed, %d blocked by shared infrastructure. No provider call was made.\n", outcome.DeepSWECommit, outcome.Certified, outcome.Required, outcome.Failed, outcome.Blocked); err != nil {
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
				return fmt.Errorf("DeepSWE readiness audit is blocked by shared infrastructure for %d task(s)", outcome.Blocked)
			}
			if outcome.Failed > 0 {
				return fmt.Errorf("DeepSWE readiness audit failed for %d task(s)", outcome.Failed)
			}
			return nil
		},
	}
	command.Flags().IntVar(&taskConcurrency, "task-concurrency", 1, "parallel task readiness checks, from 1 to 8")
	command.Flags().BoolVar(&removeTaskImages, "remove-task-images", false, "remove exact task images after certification to bound Docker disk usage; requires task concurrency 1")
	command.Flags().StringArrayVar(&tasks, "task", nil, "certify only this catalog task; repeatable")
	command.Flags().IntVar(&taskShardIndex, "task-shard-index", 1, "one-based deterministic task shard to certify")
	command.Flags().IntVar(&taskShardCount, "task-shard-count", 1, "total deterministic task shards, from 1 to 32")
	command.Flags().DurationVar(&taskTimeout, "task-timeout", 0, "maximum certification time per task; zero disables the per-task timeout")
	return command
}
