package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/conn-castle/agent-layer/internal/fsutil"
)

const (
	benchmarkPlanSchema = "deepswe-benchmark-plan"
	// legacyBenchmarkPlanSchema keeps already-paid campaign reports readable.
	legacyBenchmarkPlanSchema  = "deepswe-diagnostic-plan"
	benchmarkPlanSchemaVersion = 1
	baselineStateSchema        = "deepswe-baseline-state-v1"
	maxBenchmarkPlanBytes      = 4 << 20
)

// BaselineOptions configures a bare-model run from a website-exported plan.
type BaselineOptions struct {
	RepoRoot        string
	PlanPath        string
	PlanJSON        []byte
	TaskConcurrency int
	Confirmed       bool
}

// BaselineOutcome reports the immutable plan identity and current baseline.
type BaselineOutcome struct {
	PlanID       string
	StateDir     string
	EstimatedUSD float64
	ActualUSD    float64
	Completed    int
	Required     int
	Summary      *BaselineSummary
}

// BaselineSummary compares the fresh equal-task baseline with the published
// target evidence carried by the planner export.
type BaselineSummary struct {
	SchemaVersion          string                `json:"schema_version"`
	PlanID                 string                `json:"plan_id"`
	Model                  string                `json:"model"`
	Reasoning              string                `json:"reasoning"`
	PublishedHarnesses     []string              `json:"published_harnesses"`
	LocalHarness           string                `json:"local_harness"`
	PublishedComparable    bool                  `json:"published_comparable"`
	PublishedMean          float64               `json:"published_mean"`
	FreshBaselineMean      float64               `json:"fresh_baseline_mean"`
	FreshMinusPublished    float64               `json:"fresh_minus_published"`
	DecisionThreshold      float64               `json:"decision_threshold"`
	ActualBaselineCostUSD  ObservedCostRange     `json:"actual_baseline_cost_usd"`
	EstimatedBaselineSpend float64               `json:"estimated_baseline_spend_usd"`
	CompletedAt            time.Time             `json:"completed_at"`
	Tasks                  []BaselineTaskSummary `json:"tasks"`
	Limitations            []string              `json:"limitations"`
}

// BaselineTaskSummary preserves each task's planned repetition count and
// observed equal-repetition baseline mean.
type BaselineTaskSummary struct {
	Task          string            `json:"task"`
	Repetitions   int               `json:"repetitions"`
	PublishedMean float64           `json:"published_mean"`
	FreshMean     float64           `json:"fresh_mean"`
	Difference    float64           `json:"difference"`
	CostUSD       ObservedCostRange `json:"cost_usd"`
}

type benchmarkPlan struct {
	Schema        string `json:"schema"`
	SchemaVersion int    `json:"schemaVersion"`
	Snapshot      struct {
		URL    string `json:"url"`
		SHA256 string `json:"sha256"`
	} `json:"snapshot"`
	Target struct {
		Model     string   `json:"model"`
		Reasoning string   `json:"reasoning"`
		Harnesses []string `json:"harnesses"`
	} `json:"target"`
	Parameters struct {
		BaselineBudgetUSD         float64 `json:"baselineBudgetUsd"`
		TwoSidedSignificanceLevel float64 `json:"twoSidedSignificanceLevel"`
	} `json:"parameters"`
	Result struct {
		Valid                     bool    `json:"valid"`
		EstimatedBaselineSpendUSD float64 `json:"estimatedBaselineSpendUsd"`
		DecisionThreshold         float64 `json:"decisionThreshold"`
	} `json:"result"`
	CostAxis *struct {
		Valid                        bool    `json:"valid"`
		Scale                        string  `json:"scale"`
		ReferenceConfiguration       string  `json:"referenceConfiguration"`
		ReferenceSnapshotSHA256      string  `json:"referenceSnapshotSha256"`
		ReferenceEstimatedArmCostUSD float64 `json:"referenceEstimatedArmCostUsd"`
		RoundingIncrementUSD         float64 `json:"roundingIncrementUsd"`
		MaximumUSD                   float64 `json:"maximumUsd"`
	} `json:"costAxis"`
	Tasks []benchmarkPlanTask `json:"tasks"`
}

type benchmarkPlanTask struct {
	ID                string `json:"id"`
	RepetitionsPerArm int    `json:"repetitionsPerArm"`
	Target            struct {
		Mean float64 `json:"mean"`
	} `json:"target"`
	TargetEstimatedBaselineCostUSD float64 `json:"targetEstimatedBaselineCostUsd"`
}

type loadedBenchmarkPlan struct {
	ID       string
	Plan     benchmarkPlan
	Model    Model
	Effort   string
	RunCount int
}

type baselineManifest struct {
	SchemaVersion string            `json:"schema_version"`
	PlanID        string            `json:"plan_id"`
	CreatedAt     time.Time         `json:"created_at"`
	PlanSnapshot  string            `json:"plan_snapshot_sha256"`
	Model         string            `json:"model"`
	Reasoning     string            `json:"reasoning"`
	DeepSWECommit string            `json:"deep_swe_commit"`
	PierVersion   string            `json:"pier_version"`
	TaskChecksums map[string]string `json:"task_checksums"`
	Repetitions   map[string]int    `json:"repetitions"`
}

// CheckBaseline validates the exported plan and every local execution
// prerequisite without making a provider call.
func CheckBaseline(ctx context.Context, options BaselineOptions) (BaselineOutcome, error) {
	loaded, checksums, err := prepareBaseline(ctx, options)
	if err != nil {
		return BaselineOutcome{}, err
	}
	stateDir := baselineStateDir(options.RepoRoot, loaded.ID)
	execution := armExecution{
		repoRoot: options.RepoRoot, stateDir: stateDir, arm: ArmBaseline,
		concurrency: options.TaskConcurrency, loaded: loaded, checksums: checksums,
	}
	return BaselineOutcome{
		PlanID:       loaded.ID,
		StateDir:     stateDir,
		EstimatedUSD: loaded.Plan.Result.EstimatedBaselineSpendUSD,
		Required:     loaded.RunCount,
		Completed:    loaded.RunCount - len(missingPlanCells(execution)),
	}, nil
}

// RunBaseline executes or reuses the bare-model repetitions specified by the
// exported plan. It never runs an Agent Layer treatment.
func RunBaseline(ctx context.Context, options BaselineOptions, executor TaskExecutor) (BaselineOutcome, error) {
	if options.TaskConcurrency == 0 {
		options.TaskConcurrency = 1
	}
	loaded, checksums, err := prepareBaseline(ctx, options)
	if err != nil {
		return BaselineOutcome{}, err
	}
	stateDir := baselineStateDir(options.RepoRoot, loaded.ID)
	execution := armExecution{
		repoRoot: options.RepoRoot, stateDir: stateDir, arm: ArmBaseline,
		concurrency: options.TaskConcurrency, loaded: loaded, checksums: checksums,
	}
	outcome := BaselineOutcome{
		PlanID:       loaded.ID,
		StateDir:     stateDir,
		EstimatedUSD: loaded.Plan.Result.EstimatedBaselineSpendUSD,
		Required:     loaded.RunCount,
		Completed:    loaded.RunCount - len(missingPlanCells(execution)),
	}
	if outcome.Completed < outcome.Required && !options.Confirmed {
		return outcome, ErrConfirmationRequired
	}
	if executor == nil {
		executor = PierExecutor{}
	}
	if err := ensureBaselineManifest(stateDir, loaded, checksums); err != nil {
		return outcome, err
	}
	if err := executePlanArm(ctx, execution, executor); err != nil {
		return outcome, err
	}
	results, err := readArmResults(stateDir, loaded.Plan.Tasks, checksums)
	if err != nil {
		return outcome, err
	}
	summary, err := summarizeBaseline(loaded, results)
	if err != nil {
		return outcome, err
	}
	if err := writeJSON(filepath.Join(stateDir, "summary.json"), summary); err != nil {
		return outcome, err
	}
	outcome.Completed = outcome.Required
	outcome.ActualUSD = summary.ActualBaselineCostUSD.Midpoint
	outcome.Summary = &summary
	return outcome, nil
}

func loadBenchmarkPlan(path string) (loadedBenchmarkPlan, error) {
	info, err := os.Stat(path)
	if err != nil {
		return loadedBenchmarkPlan{}, fmt.Errorf("inspect benchmark plan: %w", err)
	}
	if info.IsDir() || info.Size() <= 0 || info.Size() > maxBenchmarkPlanBytes {
		return loadedBenchmarkPlan{}, fmt.Errorf("benchmark plan must be a non-empty JSON file no larger than %d bytes", maxBenchmarkPlanBytes)
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- the user explicitly selects the plan path.
	if err != nil {
		return loadedBenchmarkPlan{}, fmt.Errorf("read benchmark plan: %w", err)
	}
	return loadBenchmarkPlanJSON(raw)
}

func loadBenchmarkPlanInput(path string, data []byte) (loadedBenchmarkPlan, error) {
	if len(data) > 0 {
		return loadBenchmarkPlanJSON(data)
	}
	return loadBenchmarkPlan(path)
}

func loadBenchmarkPlanJSON(raw []byte) (loadedBenchmarkPlan, error) {
	if len(raw) == 0 || len(raw) > maxBenchmarkPlanBytes {
		return loadedBenchmarkPlan{}, fmt.Errorf("benchmark plan must be non-empty and no larger than %d bytes", maxBenchmarkPlanBytes)
	}
	var plan benchmarkPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return loadedBenchmarkPlan{}, fmt.Errorf("decode benchmark plan: %w", err)
	}
	if (plan.Schema != benchmarkPlanSchema && plan.Schema != legacyBenchmarkPlanSchema) ||
		plan.SchemaVersion != benchmarkPlanSchemaVersion || !plan.Result.Valid {
		return loadedBenchmarkPlan{}, fmt.Errorf("unsupported or invalid DeepSWE benchmark plan")
	}
	if plan.Snapshot.URL != DeepSWETrialsSourceURL || len(plan.Snapshot.SHA256) != 64 {
		return loadedBenchmarkPlan{}, fmt.Errorf("benchmark plan is missing pinned DeepSWE snapshot provenance")
	}
	model, effort, err := ParseModelSelection(modelNameForPublished(plan.Target.Model) + ":" + plan.Target.Reasoning)
	if err != nil || model.PublishedIdentifier != plan.Target.Model {
		return loadedBenchmarkPlan{}, fmt.Errorf("benchmark plan target is unsupported: %s:%s", plan.Target.Model, plan.Target.Reasoning)
	}
	if len(plan.Target.Harnesses) == 0 || len(plan.Tasks) == 0 ||
		plan.Parameters.BaselineBudgetUSD <= 0 ||
		plan.Parameters.TwoSidedSignificanceLevel <= 0 ||
		plan.Parameters.TwoSidedSignificanceLevel >= 1 ||
		plan.Result.EstimatedBaselineSpendUSD <= 0 ||
		plan.Result.EstimatedBaselineSpendUSD-plan.Parameters.BaselineBudgetUSD > 1e-9 ||
		plan.Result.DecisionThreshold <= 0 || plan.Result.DecisionThreshold >= 1 {
		return loadedBenchmarkPlan{}, fmt.Errorf("benchmark plan has invalid budget, result, or task selection")
	}
	for _, harness := range plan.Target.Harnesses {
		if harness == "" {
			return loadedBenchmarkPlan{}, fmt.Errorf("benchmark plan has an invalid published harness")
		}
	}
	seen := make(map[string]bool, len(plan.Tasks))
	runCount := 0
	estimated := 0.0
	for _, task := range plan.Tasks {
		if !validTaskName(task.ID) || seen[task.ID] || task.RepetitionsPerArm < 2 || task.RepetitionsPerArm > 4 || task.Target.Mean < 0 || task.Target.Mean > 1 || task.TargetEstimatedBaselineCostUSD <= 0 {
			return loadedBenchmarkPlan{}, fmt.Errorf("benchmark plan contains an invalid task allocation")
		}
		seen[task.ID] = true
		runCount += task.RepetitionsPerArm
		estimated += task.TargetEstimatedBaselineCostUSD
	}
	if math.Abs(estimated-plan.Result.EstimatedBaselineSpendUSD) > 1e-9 {
		return loadedBenchmarkPlan{}, fmt.Errorf("benchmark plan task costs do not match its estimated baseline spend")
	}
	hash := sha256.Sum256(raw)
	return loadedBenchmarkPlan{
		ID:       hex.EncodeToString(hash[:]),
		Plan:     plan,
		Model:    model,
		Effort:   effort,
		RunCount: runCount,
	}, nil
}

func prepareBaseline(ctx context.Context, options BaselineOptions) (loadedBenchmarkPlan, map[string]string, error) {
	if options.RepoRoot == "" || options.PlanPath == "" {
		return loadedBenchmarkPlan{}, nil, fmt.Errorf("benchmark baseline requires a repository root and --plan")
	}
	if options.TaskConcurrency == 0 {
		options.TaskConcurrency = 1
	}
	if options.TaskConcurrency < 1 || options.TaskConcurrency > 8 {
		return loadedBenchmarkPlan{}, nil, fmt.Errorf("benchmark baseline task concurrency must be from 1 to 8")
	}
	loaded, err := loadBenchmarkPlanInput(options.PlanPath, options.PlanJSON)
	if err != nil {
		return loadedBenchmarkPlan{}, nil, err
	}
	if err := validatePlanCostAxis(loaded.Plan); err != nil {
		return loadedBenchmarkPlan{}, nil, err
	}
	selections := []parsedSelection{{model: loaded.Model, effort: loaded.Effort}}
	if err := preflightBenchmark(selections); err != nil {
		return loadedBenchmarkPlan{}, nil, err
	}
	if err := verifyBenchmarkPier(ctx); err != nil {
		return loadedBenchmarkPlan{}, nil, err
	}
	if err := validateBenchmarkAuthentication(options.RepoRoot, selections); err != nil {
		return loadedBenchmarkPlan{}, nil, err
	}
	checkout, err := ensurePinnedBenchmarkCheckout(ctx, options.RepoRoot)
	if err != nil {
		return loadedBenchmarkPlan{}, nil, err
	}
	checksums := make(map[string]string, len(loaded.Plan.Tasks))
	for _, task := range loaded.Plan.Tasks {
		root := filepath.Join(checkout, "tasks", task.ID)
		for _, required := range []string{taskTOMLFile, taskInstructionFile, taskPreArtifactsFile, filepath.Join("tests", "test.sh")} {
			info, statErr := os.Stat(filepath.Join(root, required))
			if statErr != nil || info.IsDir() {
				return loadedBenchmarkPlan{}, nil, fmt.Errorf("benchmark task %s is missing %s", task.ID, required)
			}
		}
		checksum, checksumErr := TaskTreeChecksum(root)
		if checksumErr != nil {
			return loadedBenchmarkPlan{}, nil, fmt.Errorf("checksum benchmark task %s: %w", task.ID, checksumErr)
		}
		checksums[task.ID] = checksum
	}
	return loaded, checksums, nil
}

func baselineStateDir(repoRoot, planID string) string {
	return filepath.Join(campaignRoot(repoRoot, planID), ArmBaseline)
}

func ensureBaselineManifest(stateDir string, loaded loadedBenchmarkPlan, checksums map[string]string) error {
	repetitions := make(map[string]int, len(loaded.Plan.Tasks))
	for _, task := range loaded.Plan.Tasks {
		repetitions[task.ID] = task.RepetitionsPerArm
	}
	manifest := baselineManifest{
		SchemaVersion: baselineStateSchema,
		PlanID:        loaded.ID,
		CreatedAt:     time.Now().UTC(),
		PlanSnapshot:  loaded.Plan.Snapshot.SHA256,
		Model:         loaded.Plan.Target.Model,
		Reasoning:     loaded.Plan.Target.Reasoning,
		DeepSWECommit: DeepSWECommit,
		PierVersion:   PierVersion,
		TaskChecksums: checksums,
		Repetitions:   repetitions,
	}
	path := filepath.Join(stateDir, "manifest.json")
	if data, err := os.ReadFile(path); err == nil { // #nosec G304 -- stateDir is content-addressed below the private benchmark store.
		var existing baselineManifest
		if json.Unmarshal(data, &existing) != nil {
			return fmt.Errorf("decode cached baseline manifest")
		}
		manifest.CreatedAt = existing.CreatedAt
		expected, _ := json.Marshal(manifest)
		actual, _ := json.Marshal(existing)
		if string(expected) != string(actual) {
			return fmt.Errorf("benchmark plan state conflicts with its immutable manifest")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read baseline manifest: %w", err)
	}
	return writeJSON(path, manifest)
}

func armResultPath(stateDir, task string, attempt int) string {
	return filepath.Join(stateDir, "attempts", fmt.Sprintf("%d", attempt), "tasks", task, "result.json")
}

func readArmResults(stateDir string, tasks []benchmarkPlanTask, checksums map[string]string) (map[string][]AttemptResult, error) {
	results := make(map[string][]AttemptResult, len(tasks))
	for _, task := range tasks {
		for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
			path := armResultPath(stateDir, task.ID, attempt)
			data, err := os.ReadFile(path) // #nosec G304 -- task and attempt were validated from the plan.
			if err != nil {
				return nil, fmt.Errorf("read benchmark result %s repetition %d: %w", task.ID, attempt, err)
			}
			var result AttemptResult
			if json.Unmarshal(data, &result) != nil || result.Validate() != nil || result.Status != statusSuccess || result.Task != task.ID || result.Attempt != attempt || result.TaskChecksum != checksums[task.ID] {
				return nil, fmt.Errorf("invalid benchmark result %s repetition %d", task.ID, attempt)
			}
			result, err = canonicalizeAttemptResult(path, data, result)
			if err != nil {
				return nil, fmt.Errorf("read canonical benchmark result %s repetition %d: %w", task.ID, attempt, err)
			}
			results[task.ID] = append(results[task.ID], result)
		}
	}
	return results, nil
}

func summarizeBaseline(loaded loadedBenchmarkPlan, results map[string][]AttemptResult) (BaselineSummary, error) {
	summary := BaselineSummary{
		SchemaVersion:          baselineStateSchema,
		PlanID:                 loaded.ID,
		Model:                  loaded.Plan.Target.Model,
		Reasoning:              loaded.Plan.Target.Reasoning,
		PublishedHarnesses:     append([]string(nil), loaded.Plan.Target.Harnesses...),
		LocalHarness:           loaded.Model.Adapter,
		PublishedComparable:    containsString(loaded.Plan.Target.Harnesses, loaded.Model.Adapter),
		DecisionThreshold:      loaded.Plan.Result.DecisionThreshold,
		EstimatedBaselineSpend: loaded.Plan.Result.EstimatedBaselineSpendUSD,
		CompletedAt:            time.Now().UTC(),
	}
	if !summary.PublishedComparable {
		summary.Limitations = append(summary.Limitations,
			"The published planning evidence and fresh baseline used different agent harnesses; the published mean and decision threshold are planning references, not like-for-like local A/B results.",
		)
	}
	for _, task := range loaded.Plan.Tasks {
		observed := results[task.ID]
		if len(observed) != task.RepetitionsPerArm {
			return BaselineSummary{}, fmt.Errorf("benchmark task %s has %d results; expected %d", task.ID, len(observed), task.RepetitionsPerArm)
		}
		item := BaselineTaskSummary{Task: task.ID, Repetitions: task.RepetitionsPerArm, PublishedMean: task.Target.Mean}
		for _, result := range observed {
			item.FreshMean += result.F2PScore / float64(len(observed))
			minimum, maximum, err := result.CostBounds()
			if err != nil {
				return BaselineSummary{}, fmt.Errorf("baseline task %s has invalid cost", task.ID)
			}
			item.CostUSD.Midpoint += *result.CostUSD
			item.CostUSD.Minimum += minimum
			item.CostUSD.Maximum += maximum
		}
		item.Difference = item.FreshMean - item.PublishedMean
		summary.PublishedMean += item.PublishedMean / float64(len(loaded.Plan.Tasks))
		summary.FreshBaselineMean += item.FreshMean / float64(len(loaded.Plan.Tasks))
		summary.ActualBaselineCostUSD = addObservedCost(summary.ActualBaselineCostUSD, item.CostUSD)
		summary.Tasks = append(summary.Tasks, item)
	}
	summary.FreshMinusPublished = summary.FreshBaselineMean - summary.PublishedMean
	sort.SliceStable(summary.Tasks, func(i, j int) bool { return summary.Tasks[i].Task < summary.Tasks[j].Task })
	return summary, nil
}

func writeJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode benchmark evidence: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create benchmark evidence directory: %w", err)
	}
	return fsutil.WriteFileAtomic(path, data, 0o600)
}
