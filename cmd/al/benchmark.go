package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	bench "github.com/conn-castle/agent-layer/internal/benchmark"
)

const (
	benchmarkCommandName   = "benchmark"
	benchmarkBaselineName  = "baseline"
	benchmarkMatrixName    = "matrix"
	benchmarkTreatmentName = "treatment"
	benchmarkCorrectName   = "correct-scores"
	benchmarkReportName    = "report"
	benchmarkReadinessName = "readiness"
	benchmarkYes           = "yes"
)

var (
	runBaseline         = bench.RunBaseline
	checkBaseline       = bench.CheckBaseline
	runTreatment        = bench.RunTreatment
	checkTreatment      = bench.CheckTreatment
	runMatrix           = bench.RunMatrix
	checkMatrix         = bench.CheckMatrix
	checkReadiness      = bench.CheckAllTaskReadiness
	buildCampaignReport = bench.BuildCampaignReport
)

func newBenchmarkCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   benchmarkCommandName,
		Short: "Run a website-planned DeepSWE comparison",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(
		newBenchmarkBaselineCmd(),
		newBenchmarkTreatmentCmd(),
		newBenchmarkReportCmd(),
		newBenchmarkMatrixCmd(),
		newBenchmarkReadinessCmd(),
		newBenchmarkCorrectScoresCmd(),
	)
	return command
}

func newBenchmarkReadinessCmd() *cobra.Command {
	var taskConcurrency int
	command := &cobra.Command{
		Use:   benchmarkReadinessName,
		Short: "Preflight every task in the pinned DeepSWE catalog",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			outcome, err := checkReadiness(context.Background(), bench.ReadinessAuditOptions{
				RepoRoot: root, TaskConcurrency: taskConcurrency,
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(
				cmd.OutOrStdout(),
				"DeepSWE readiness %s: %d of %d tasks certified, %d failed, %d blocked by shared infrastructure. No provider call was made.\n",
				outcome.DeepSWECommit, outcome.Certified, outcome.Required, outcome.Failed, outcome.Blocked,
			); err != nil {
				return err
			}
			for _, task := range outcome.Tasks {
				if task.Status == "failed" || task.Status == "blocked" {
					if _, err := fmt.Fprintf(cmd.OutOrStdout(), "- %s: %s\n", task.Task, task.Error); err != nil {
						return err
					}
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
	return command
}

func newBenchmarkCorrectScoresCmd() *cobra.Command {
	return &cobra.Command{
		Use:   benchmarkCorrectName,
		Short: "Regenerate canonical scores for affected stored DeepSWE runs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			count, err := bench.CorrectStoredScores(root)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Regenerated %d canonical benchmark scores.\n", count)
			return err
		},
	}
}

func newBenchmarkBaselineCmd() *cobra.Command {
	var planPath, execution string
	var taskConcurrency int
	var yes, check bool
	command := &cobra.Command{
		Use:   benchmarkBaselineName,
		Short: "Run or inspect the shared bare-model baseline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if planPath == "" || execution == "" {
				return errors.New("benchmark baseline requires --plan and --execution")
			}
			if check && yes {
				return errors.New("benchmark baseline --check does not accept --yes")
			}
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			planJSON, err := readBenchmarkPlanInput(cmd, planPath)
			if err != nil {
				return err
			}
			options := bench.BaselineOptions{
				RepoRoot: root, PlanPath: planPath, PlanJSON: planJSON,
				Execution: execution, TaskConcurrency: taskConcurrency, Confirmed: yes,
			}
			if check {
				outcome, checkErr := checkBaseline(context.Background(), options)
				if checkErr != nil {
					return checkErr
				}
				_, err = fmt.Fprintf(
					cmd.OutOrStdout(),
					"Calibration: %s vs %s; executing both arms with %s.\nBaseline campaign %.12s is ready: %d of %d runs cached, %d paid calls missing. No provider call was made.\n",
					outcome.CalibrationReference, outcome.CalibrationContrast, outcome.Execution,
					outcome.CampaignID, outcome.Completed, outcome.Required,
					outcome.Required-outcome.Completed,
				)
				return err
			}
			return runBenchmarkBaselineInteractive(cmd, options)
		},
	}
	command.Flags().StringVar(&planPath, "plan", "", "website-exported DeepSWE benchmark plan, or - for standard input")
	command.Flags().StringVar(&execution, "execution", "", "model and reasoning to execute, in the form <model>:<effort>")
	command.Flags().IntVar(&taskConcurrency, "task-concurrency", 1, "parallel task executions, from 1 to 8")
	command.Flags().BoolVar(&yes, "yes", false, "confirm paid model calls without prompting")
	command.Flags().BoolVar(&check, "check", false, "validate and show cached progress without provider calls")
	return command
}

func runBenchmarkBaselineInteractive(cmd *cobra.Command, options bench.BaselineOptions) error {
	outcome, err := runBaseline(context.Background(), options, nil)
	if errors.Is(err, bench.ErrConfirmationRequired) {
		if !isTerminal() {
			return errors.New("benchmark paid execution requires --yes outside a terminal")
		}
		if _, writeErr := fmt.Fprintf(
			cmd.OutOrStdout(),
			"Baseline with %s requires %d paid model calls. Continue? [y/N]: ",
			outcome.Execution, outcome.Required-outcome.Completed,
		); writeErr != nil {
			return writeErr
		}
		confirmed, readErr := readBenchmarkConfirmation(cmd)
		if readErr != nil {
			return readErr
		}
		if !confirmed {
			return errors.New("benchmark confirmation declined")
		}
		options.Confirmed = true
		outcome, err = runBaseline(context.Background(), options, nil)
	}
	if err != nil {
		return err
	}
	if outcome.Summary == nil {
		return errors.New("benchmark baseline completed without a summary")
	}
	_, err = fmt.Fprintf(
		cmd.OutOrStdout(),
		"Baseline campaign %.12s (%s) complete: %.1f%% mean score, $%.2f midpoint actual cost ($%.2f–$%.2f accounting range) across %d runs.\nEvidence: %s\n",
		outcome.CampaignID, outcome.Execution, outcome.Summary.FreshBaselineMean*100,
		outcome.Summary.ActualBaselineCostUSD.Midpoint,
		outcome.Summary.ActualBaselineCostUSD.Minimum,
		outcome.Summary.ActualBaselineCostUSD.Maximum,
		outcome.Completed, outcome.StateDir,
	)
	return err
}

func newBenchmarkMatrixCmd() *cobra.Command {
	var selectionPath, treatmentExecution, treatmentLabel, treatmentPin, treatmentMode, dispatchConfig string
	var baselineExecutions []string
	var tasks []string
	var taskConcurrency int
	var yes, check, open bool
	command := &cobra.Command{
		Use:   benchmarkMatrixName,
		Short: "Run a descriptive cross-model matrix",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if selectionPath == "" || len(baselineExecutions) == 0 {
				return errors.New(
					"benchmark matrix requires --selection and at least one --baseline-execution",
				)
			}
			if treatmentExecution == "" && treatmentLabel != "" {
				return errors.New("benchmark matrix --treatment-label requires --treatment-execution")
			}
			if treatmentExecution == "" && treatmentPin != "" {
				return errors.New("benchmark matrix --treatment-pin requires --treatment-execution")
			}
			if treatmentExecution == "" && dispatchConfig != "" {
				return errors.New("benchmark matrix --dispatch-config requires --treatment-execution")
			}
			if check && (yes || open) {
				return errors.New("benchmark matrix --check does not accept --yes or --open")
			}
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			selectionJSON, err := readBenchmarkPlanInput(cmd, selectionPath)
			if err != nil {
				return err
			}
			options := bench.MatrixOptions{
				RepoRoot: root, SelectionPath: selectionPath,
				SelectionJSON:      selectionJSON,
				BaselineExecutions: baselineExecutions,
				TreatmentExecution: treatmentExecution,
				TreatmentLabel:     treatmentLabel,
				TreatmentPin:       treatmentPin,
				TreatmentMode:      treatmentMode,
				DispatchConfigPath: dispatchConfig,
				Tasks:              tasks,
				TaskConcurrency:    taskConcurrency, Confirmed: yes,
			}
			if check {
				outcome, checkErr := checkMatrix(context.Background(), options)
				if checkErr != nil {
					return checkErr
				}
				return printMatrixProgress(cmd, outcome, true)
			}
			outcome, err := runBenchmarkMatrixInteractive(cmd, options)
			if err != nil {
				return err
			}
			if err := printMatrixProgress(cmd, outcome, false); err != nil {
				return err
			}
			if outcome.Missing > 0 {
				return nil
			}
			if outcome.HTMLPath == "" || outcome.Report == nil {
				return errors.New("benchmark matrix completed without a report")
			}
			if _, err := fmt.Fprintf(
				cmd.OutOrStdout(),
				"Report: %s\nJSON: %s\n",
				outcome.HTMLPath, outcome.JSONPath,
			); err != nil {
				return err
			}
			if open {
				return openBenchmarkReport(outcome.HTMLPath)
			}
			return nil
		},
	}
	command.Flags().StringVar(
		&selectionPath, "selection", "",
		"versioned DeepSWE task selection JSON, or - for standard input",
	)
	command.Flags().StringArrayVar(
		&baselineExecutions, "baseline-execution", nil,
		"bare model and reasoning to execute; repeat for each point",
	)
	command.Flags().StringVar(
		&treatmentExecution, "treatment-execution", "",
		"Agent Layer model and reasoning to execute",
	)
	command.Flags().StringVar(
		&treatmentLabel, "treatment-label", "",
		"report label for the Agent Layer point",
	)
	command.Flags().StringVar(
		&treatmentPin, "treatment-pin", "",
		"stable name for the persisted Agent Layer bundle; defaults to the treatment label",
	)
	command.Flags().StringVar(
		&treatmentMode, "treatment-mode", bench.TreatmentInstructionsAndSkills,
		"Agent Layer treatment mode: instructions-and-skills or instructions-only",
	)
	command.Flags().StringVar(
		&dispatchConfig, "dispatch-config", "",
		"TOML role-to-target configuration for the Agent Layer point",
	)
	command.Flags().StringArrayVar(
		&tasks, "task", nil,
		"execute only this selected task; repeat to execute multiple tasks",
	)
	command.Flags().IntVar(
		&taskConcurrency, "task-concurrency", 1,
		"total parallel task executions across all arms, from 1 to 8",
	)
	command.Flags().BoolVar(
		&yes, "yes", false, "confirm paid model calls without prompting",
	)
	command.Flags().BoolVar(
		&check, "check", false,
		"validate and show cached progress without provider calls",
	)
	command.Flags().BoolVar(
		&open, "open", false, "open the generated HTML report",
	)
	return command
}

func runBenchmarkMatrixInteractive(
	cmd *cobra.Command,
	options bench.MatrixOptions,
) (bench.MatrixOutcome, error) {
	outcome, err := runMatrix(context.Background(), options, nil)
	if errors.Is(err, bench.ErrConfirmationRequired) {
		if !isTerminal() {
			return outcome, errors.New(
				"benchmark paid execution requires --yes outside a terminal",
			)
		}
		if _, writeErr := fmt.Fprintf(
			cmd.OutOrStdout(),
			"Matrix requires %d paid model calls. Continue? [y/N]: ",
			outcome.Missing,
		); writeErr != nil {
			return outcome, writeErr
		}
		confirmed, readErr := readBenchmarkConfirmation(cmd)
		if readErr != nil {
			return outcome, readErr
		}
		if !confirmed {
			return outcome, errors.New("benchmark confirmation declined")
		}
		options.Confirmed = true
		outcome, err = runMatrix(context.Background(), options, nil)
	}
	return outcome, err
}

func printMatrixProgress(
	cmd *cobra.Command,
	outcome bench.MatrixOutcome,
	check bool,
) error {
	status := "complete"
	if check {
		status = "ready"
	} else if outcome.Missing > 0 {
		status = "progress saved"
	}
	if _, err := fmt.Fprintf(
		cmd.OutOrStdout(),
		"Matrix %.12s %s: %d of %d runs cached, %d missing.\n",
		outcome.SelectionID, status, outcome.Completed, outcome.Required,
		outcome.Missing,
	); err != nil {
		return err
	}
	for _, arm := range outcome.Arms {
		if _, err := fmt.Fprintf(
			cmd.OutOrStdout(),
			"- %s (%s, %s): %d of %d cached\n",
			arm.Label, arm.Execution, arm.Mode, arm.Completed, arm.Required,
		); err != nil {
			return err
		}
	}
	if check {
		_, err := fmt.Fprintln(
			cmd.OutOrStdout(), "No paid provider call was made.",
		)
		return err
	}
	return nil
}

func newBenchmarkTreatmentCmd() *cobra.Command {
	var planPath, execution, label string
	var taskConcurrency, maxNewRuns int
	var yes, check bool
	command := &cobra.Command{
		Use:   benchmarkTreatmentName,
		Short: "Run or inspect one immutable skills-and-instructions version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if planPath == "" || execution == "" {
				return errors.New("benchmark treatment requires --plan and --execution")
			}
			if check && yes {
				return errors.New("benchmark treatment --check does not accept --yes")
			}
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			planJSON, err := readBenchmarkPlanInput(cmd, planPath)
			if err != nil {
				return err
			}
			options := bench.TreatmentOptions{
				RepoRoot: root, PlanPath: planPath, PlanJSON: planJSON, Label: label,
				Execution: execution, TaskConcurrency: taskConcurrency,
				MaxNewRuns: maxNewRuns, Confirmed: yes,
			}
			if check {
				outcome, checkErr := checkTreatment(context.Background(), options)
				if checkErr != nil {
					return checkErr
				}
				_, err = fmt.Fprintf(
					cmd.OutOrStdout(),
					"Calibration: %s vs %s; executing both arms with %s.\nTreatment %q (%.12s) is ready: %d of %d runs cached, %d paid calls missing. No provider call was made.\n",
					outcome.CalibrationReference, outcome.CalibrationContrast, outcome.Execution,
					outcome.Label, outcome.TreatmentID,
					outcome.Completed, outcome.Required, outcome.Missing,
				)
				return err
			}
			return runBenchmarkTreatmentInteractive(cmd, options)
		},
	}
	command.Flags().StringVar(&planPath, "plan", "", "website-exported DeepSWE benchmark plan, or - for standard input")
	command.Flags().StringVar(&execution, "execution", "", "model and reasoning to execute, in the form <model>:<effort>")
	command.Flags().StringVar(&label, "label", "", "report label for this immutable treatment version")
	command.Flags().IntVar(&taskConcurrency, "task-concurrency", 1, "parallel task executions, from 1 to 8")
	command.Flags().IntVar(&maxNewRuns, "max-new-runs", 0, "execute at most this many missing runs; zero executes all")
	command.Flags().BoolVar(&yes, "yes", false, "confirm paid model calls without prompting")
	command.Flags().BoolVar(&check, "check", false, "validate and show cached progress without provider calls")
	return command
}

func runBenchmarkTreatmentInteractive(cmd *cobra.Command, options bench.TreatmentOptions) error {
	outcome, err := runTreatment(context.Background(), options, nil)
	if errors.Is(err, bench.ErrConfirmationRequired) {
		if !isTerminal() {
			return errors.New("benchmark paid execution requires --yes outside a terminal")
		}
		if _, writeErr := fmt.Fprintf(
			cmd.OutOrStdout(),
			"Treatment %q requires %d paid model calls. Treatment cost cannot be estimated reliably from the bare-model baseline. Continue? [y/N]: ",
			outcome.Label, outcome.Missing,
		); writeErr != nil {
			return writeErr
		}
		confirmed, readErr := readBenchmarkConfirmation(cmd)
		if readErr != nil {
			return readErr
		}
		if !confirmed {
			return errors.New("benchmark confirmation declined")
		}
		options.Confirmed = true
		outcome, err = runTreatment(context.Background(), options, nil)
	}
	if err != nil {
		return err
	}
	status := "complete"
	if outcome.Missing > 0 {
		status = "progress saved"
	}
	_, err = fmt.Fprintf(
		cmd.OutOrStdout(),
		"Treatment %q (%.12s) %s: %d of %d runs cached, %d missing.\nEvidence: %s\n",
		outcome.Label, outcome.TreatmentID, status, outcome.Completed, outcome.Required,
		outcome.Missing, outcome.StateDir,
	)
	return err
}

func newBenchmarkReportCmd() *cobra.Command {
	var planPath, execution string
	var analyses []string
	var open bool
	command := &cobra.Command{
		Use:   benchmarkReportName,
		Short: "Calculate and render the campaign from immutable evidence",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if planPath == "" {
				return errors.New("benchmark report requires --plan")
			}
			root, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			planJSON, err := readBenchmarkPlanInput(cmd, planPath)
			if err != nil {
				return err
			}
			outcome, err := buildCampaignReport(bench.CampaignReportOptions{
				RepoRoot: root, PlanPath: planPath, PlanJSON: planJSON,
				Execution: execution, LegacyAnalysisPaths: analyses,
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(
				cmd.OutOrStdout(),
				"Campaign %.12s (%s) with %d treatment version(s)\nReport: %s\nJSON: %s\n",
				outcome.CampaignID, outcome.Execution, outcome.Versions, outcome.HTMLPath, outcome.JSONPath,
			); err != nil {
				return err
			}
			for _, warning := range outcome.SkippedTreatments {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Warning: %s\n", warning); err != nil {
					return err
				}
			}
			if open {
				return openBenchmarkReport(outcome.HTMLPath)
			}
			return nil
		},
	}
	command.Flags().StringVar(&planPath, "plan", "", "website-exported DeepSWE benchmark plan, or - for standard input")
	command.Flags().StringVar(&execution, "execution", "", "model and reasoning executed by both arms for a schema version 2 plan, in the form <model>:<effort>")
	command.Flags().StringArrayVar(&analyses, "analysis", nil, "explicit canonical analysis for a schema version 1 plan without cost-axis provenance")
	command.Flags().BoolVar(&open, "open", false, "open the generated HTML report")
	return command
}

func readBenchmarkConfirmation(cmd *cobra.Command) (bool, error) {
	answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read benchmark confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == affirmativeResponse, nil
}

func readBenchmarkPlanInput(cmd *cobra.Command, planPath string) ([]byte, error) {
	if planPath != "-" {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(cmd.InOrStdin(), (4<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("read benchmark plan from standard input: %w", err)
	}
	if len(data) == 0 || len(data) > 4<<20 {
		return nil, errors.New("benchmark plan from standard input must be non-empty and no larger than 4 MiB")
	}
	return data, nil
}

func openBenchmarkReport(path string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.CommandContext(context.Background(), name, path).Start() // #nosec G204 -- command and report path are controlled by the benchmark store.
}
