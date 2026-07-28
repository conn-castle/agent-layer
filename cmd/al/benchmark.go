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
	benchmarkTreatmentName = "treatment"
	benchmarkYes           = "yes"
)

var (
	runBaseline         = bench.RunBaseline
	checkBaseline       = bench.CheckBaseline
	runTreatment        = bench.RunTreatment
	checkTreatment      = bench.CheckTreatment
	buildCampaignReport = bench.BuildCampaignReport
)

func newBenchmarkCmd() *cobra.Command {
	command := &cobra.Command{
		Use:   benchmarkCommandName,
		Short: "Run a website-planned DeepSWE comparison",
		Args:  cobra.NoArgs,
	}
	command.AddCommand(newBenchmarkBaselineCmd(), newBenchmarkTreatmentCmd(), newBenchmarkReportCmd())
	return command
}

func newBenchmarkBaselineCmd() *cobra.Command {
	var planPath string
	var taskConcurrency int
	var yes, check bool
	command := &cobra.Command{
		Use:   benchmarkBaselineName,
		Short: "Run or inspect the shared bare-model baseline",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if planPath == "" {
				return errors.New("benchmark baseline requires --plan")
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
				TaskConcurrency: taskConcurrency, Confirmed: yes,
			}
			if check {
				outcome, checkErr := checkBaseline(context.Background(), options)
				if checkErr != nil {
					return checkErr
				}
				_, err = fmt.Fprintf(
					cmd.OutOrStdout(),
					"Baseline %.12s is ready: %d of %d runs cached, %d paid calls missing, published cost estimate $%.2f. No provider call was made.\n",
					outcome.PlanID, outcome.Completed, outcome.Required,
					outcome.Required-outcome.Completed, outcome.EstimatedUSD,
				)
				return err
			}
			return runBenchmarkBaselineInteractive(cmd, options)
		},
	}
	command.Flags().StringVar(&planPath, "plan", "", "website-exported DeepSWE benchmark plan, or - for standard input")
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
			"Baseline requires %d paid model calls; published estimate $%.2f. Continue? [y/N]: ",
			outcome.Required-outcome.Completed, outcome.EstimatedUSD,
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
		"Baseline %.12s complete: %.1f%% mean score, $%.2f midpoint actual cost ($%.2f–$%.2f accounting range) across %d runs.\nEvidence: %s\n",
		outcome.PlanID, outcome.Summary.FreshBaselineMean*100,
		outcome.Summary.ActualBaselineCostUSD.Midpoint,
		outcome.Summary.ActualBaselineCostUSD.Minimum,
		outcome.Summary.ActualBaselineCostUSD.Maximum,
		outcome.Completed, outcome.StateDir,
	)
	return err
}

func newBenchmarkTreatmentCmd() *cobra.Command {
	var planPath, label string
	var taskConcurrency int
	var yes, check bool
	command := &cobra.Command{
		Use:   benchmarkTreatmentName,
		Short: "Run or inspect one immutable skills-and-instructions version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if planPath == "" {
				return errors.New("benchmark treatment requires --plan")
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
				TaskConcurrency: taskConcurrency, Confirmed: yes,
			}
			if check {
				outcome, checkErr := checkTreatment(context.Background(), options)
				if checkErr != nil {
					return checkErr
				}
				_, err = fmt.Fprintf(
					cmd.OutOrStdout(),
					"Treatment %q (%.12s) is ready: %d of %d runs cached, %d paid calls missing. No provider call was made.\n",
					outcome.Label, outcome.TreatmentID, outcome.Completed, outcome.Required, outcome.Missing,
				)
				return err
			}
			return runBenchmarkTreatmentInteractive(cmd, options)
		},
	}
	command.Flags().StringVar(&planPath, "plan", "", "website-exported DeepSWE benchmark plan, or - for standard input")
	command.Flags().StringVar(&label, "label", "", "report label for this immutable treatment version")
	command.Flags().IntVar(&taskConcurrency, "task-concurrency", 1, "parallel task executions, from 1 to 8")
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
	_, err = fmt.Fprintf(
		cmd.OutOrStdout(),
		"Treatment %q (%.12s) complete: %d of %d runs cached.\nEvidence: %s\n",
		outcome.Label, outcome.TreatmentID, outcome.Completed, outcome.Required, outcome.StateDir,
	)
	return err
}

func newBenchmarkReportCmd() *cobra.Command {
	var planPath string
	var analyses []string
	var open bool
	command := &cobra.Command{
		Use:   "report",
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
				LegacyAnalysisPaths: analyses,
			})
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(
				cmd.OutOrStdout(),
				"Campaign %.12s with %d treatment version(s)\nReport: %s\nJSON: %s\n",
				outcome.PlanID, outcome.Versions, outcome.HTMLPath, outcome.JSONPath,
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
	command.Flags().StringArrayVar(&analyses, "analysis", nil, "explicit canonical analysis for a legacy plan without cost-axis provenance")
	command.Flags().BoolVar(&open, "open", false, "open the generated HTML report")
	return command
}

func readBenchmarkConfirmation(cmd *cobra.Command) (bool, error) {
	answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read benchmark confirmation: %w", err)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == benchmarkYes, nil
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
