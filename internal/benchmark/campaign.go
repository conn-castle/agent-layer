package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

const (
	campaignTreatmentSchema       = "deepswe-treatment-v2"
	legacyCampaignTreatmentSchema = "deepswe-treatment-v1"
)

var buildCampaignTreatmentBundle = BuildTreatmentBundle

// TreatmentOptions configures one immutable skills-and-instructions campaign
// version for a website-exported plan.
type TreatmentOptions struct {
	RepoRoot        string
	PlanPath        string
	PlanJSON        []byte
	Execution       string
	Label           string
	TaskConcurrency int
	Confirmed       bool
}

// TreatmentOutcome describes the selected immutable treatment and its cached
// execution progress.
type TreatmentOutcome struct {
	PlanID               string
	CampaignID           string
	Execution            string
	CalibrationReference string
	CalibrationContrast  string
	TreatmentID          string
	Label                string
	StateDir             string
	Completed            int
	Required             int
	Missing              int
	ProviderCall         bool
}

// CampaignReportOptions selects one execution campaign for a website plan.
type CampaignReportOptions struct {
	RepoRoot            string
	PlanPath            string
	PlanJSON            []byte
	Execution           string
	LegacyAnalysisPaths []string
}

// CampaignReportOutcome identifies the generated canonical report artifacts.
type CampaignReportOutcome struct {
	PlanID            string
	CampaignID        string
	Execution         string
	Versions          int
	Report            Report
	JSONPath          string
	HTMLPath          string
	Analyses          []string
	SkippedTreatments []string
}

type campaignTreatmentManifest struct {
	SchemaVersion       string            `json:"schema_version"`
	PlanID              string            `json:"plan_id"`
	CampaignID          string            `json:"campaign_id"`
	CreatedAt           time.Time         `json:"created_at"`
	Label               string            `json:"label,omitempty"`
	Arm                 string            `json:"arm"`
	Model               string            `json:"model"`
	Reasoning           string            `json:"reasoning"`
	TaskChecksums       map[string]string `json:"task_checksums"`
	Repetitions         map[string]int    `json:"repetitions"`
	TreatmentHash       string            `json:"treatment_manifest_hash"`
	Treatment           TreatmentManifest `json:"treatment_manifest"`
	LinuxBinarySHA256   string            `json:"linux_binary_sha256"`
	AdapterSHA256       string            `json:"adapter_sha256"`
	ProviderClient      string            `json:"provider_client_version"`
	RuntimeModel        string            `json:"runtime_model"`
	TemplatesCommit     string            `json:"templates_commit,omitempty"`
	TemplatesDirty      bool              `json:"templates_dirty"`
	NoAutomaticRetry    bool              `json:"no_automatic_retry"`
	SelectionMethodOnly string            `json:"selection_method_note"`
}

// CheckTreatment validates the plan, baseline, and current treatment bundle
// without making a provider call.
func CheckTreatment(ctx context.Context, options TreatmentOptions) (TreatmentOutcome, error) {
	return prepareTreatment(ctx, options, false, nil)
}

// RunTreatment executes only the missing repetitions for the current immutable
// Agent Layer bundle. Existing evidence is never overwritten.
func RunTreatment(ctx context.Context, options TreatmentOptions, executor TaskExecutor) (TreatmentOutcome, error) {
	return prepareTreatment(ctx, options, true, executor)
}

func prepareTreatment(ctx context.Context, options TreatmentOptions, execute bool, executor TaskExecutor) (TreatmentOutcome, error) {
	if options.TaskConcurrency == 0 {
		options.TaskConcurrency = 1
	}
	if options.RepoRoot == "" || options.PlanPath == "" || options.Execution == "" ||
		options.TaskConcurrency < 1 || options.TaskConcurrency > 8 {
		return TreatmentOutcome{}, fmt.Errorf("benchmark treatment requires a repository root, --plan, --execution, and task concurrency from 1 to 8")
	}
	loaded, err := loadBenchmarkPlanInput(options.PlanPath, options.PlanJSON)
	if err != nil {
		return TreatmentOutcome{}, err
	}
	if loaded.Legacy {
		return TreatmentOutcome{}, fmt.Errorf("schema version 1 benchmark plans are report-only; export a schema version 2 plan for new execution")
	}
	loaded, err = bindBenchmarkExecution(loaded, options.Execution)
	if err != nil {
		return TreatmentOutcome{}, err
	}
	if err := validatePlanCostAxis(loaded.Plan); err != nil {
		return TreatmentOutcome{}, err
	}
	baselineDir := baselineStateDir(options.RepoRoot, loaded.CampaignID)
	baseline, err := readCampaignBaseline(baselineDir, loaded)
	if err != nil {
		return TreatmentOutcome{}, err
	}
	if baseline.SchemaVersion != baselineStateSchema {
		return TreatmentOutcome{}, fmt.Errorf(
			"baseline state %q remains reportable but cannot seed a new treatment; run a provider-versioned baseline",
			baseline.SchemaVersion,
		)
	}
	if baseline.ProviderClient != loaded.Model.ProviderClientVersion {
		return TreatmentOutcome{}, fmt.Errorf(
			"baseline provider client version %q does not match required version %q",
			baseline.ProviderClient,
			loaded.Model.ProviderClientVersion,
		)
	}
	selections := []parsedSelection{{model: loaded.Model, effort: loaded.Effort}}
	if err := preflightBenchmark(selections); err != nil {
		return TreatmentOutcome{}, err
	}
	if err := verifyBenchmarkPier(ctx); err != nil {
		return TreatmentOutcome{}, err
	}
	if err := validateBenchmarkAuthentication(options.RepoRoot, selections); err != nil {
		return TreatmentOutcome{}, err
	}
	bundle, err := buildCampaignTreatmentBundle(
		options.RepoRoot,
		runtime.GOARCH,
		TreatmentInstructionsAndSkills,
		loaded.Model,
		loaded.Effort,
	)
	if err != nil {
		return TreatmentOutcome{}, fmt.Errorf("build Agent Layer treatment: %w", err)
	}
	defer func() { _ = os.RemoveAll(bundle.Root) }()

	stateDir := filepath.Join(campaignRoot(options.RepoRoot, loaded.CampaignID), "treatments", bundle.ManifestHash)
	manifestPath := filepath.Join(stateDir, "manifest.json")
	label := options.Label
	if label == "" {
		var existing campaignTreatmentManifest
		if readCampaignJSON(manifestPath, &existing) == nil && existing.Label != "" {
			label = existing.Label
		} else {
			label = "Agent Layer " + bundle.ManifestHash[:12]
		}
	}
	manifest := campaignTreatmentManifest{
		SchemaVersion:       campaignTreatmentSchema,
		PlanID:              loaded.ID,
		CampaignID:          loaded.CampaignID,
		CreatedAt:           time.Now().UTC(),
		Label:               label,
		Arm:                 ArmTreatment,
		Model:               loaded.Model.PublishedIdentifier,
		Reasoning:           loaded.Effort,
		TaskChecksums:       copyStringMap(baseline.TaskChecksums),
		Repetitions:         copyIntMap(baseline.Repetitions),
		TreatmentHash:       bundle.ManifestHash,
		Treatment:           bundle.Manifest,
		LinuxBinarySHA256:   bundle.LinuxBinarySHA256,
		AdapterSHA256:       bundle.AdapterSHA256,
		ProviderClient:      loaded.Model.ProviderClientVersion,
		RuntimeModel:        loaded.Model.RuntimeIdentifier,
		TemplatesCommit:     bundle.TemplatesCommit,
		TemplatesDirty:      bundle.TemplatesDirty,
		NoAutomaticRetry:    true,
		SelectionMethodOnly: "Published mini-swe-agent data selected tasks; observed local arms determine the experiment result.",
	}
	exists, err := validateCampaignTreatmentManifest(manifestPath, &manifest)
	if err != nil {
		return TreatmentOutcome{}, err
	}
	if exists {
		label = manifest.Label
	}
	execution := armExecution{
		repoRoot: options.RepoRoot, stateDir: stateDir, arm: ArmTreatment,
		concurrency: options.TaskConcurrency, loaded: loaded,
		checksums: baseline.TaskChecksums, bundle: bundle,
	}
	missing := missingPlanCells(execution)
	outcome := TreatmentOutcome{
		PlanID:               loaded.ID,
		CampaignID:           loaded.CampaignID,
		Execution:            executionLabel(loaded.Model, loaded.Effort),
		CalibrationReference: loaded.Plan.CalibrationReference.ID,
		CalibrationContrast:  loaded.Plan.CalibrationContrast.ID,
		TreatmentID:          bundle.ManifestHash,
		Label:                label,
		StateDir:             stateDir,
		Completed:            loaded.RunCount - len(missing),
		Required:             loaded.RunCount,
		Missing:              len(missing),
	}
	if !execute || len(missing) == 0 {
		return outcome, nil
	}
	if !options.Confirmed {
		return outcome, ErrConfirmationRequired
	}
	if err := ensureCampaignTreatmentManifest(manifestPath, &manifest); err != nil {
		return outcome, err
	}
	if executor == nil {
		executor = PierExecutor{}
	}
	if err := executePlanArm(ctx, execution, executor); err != nil {
		return outcome, err
	}
	outcome.Completed = outcome.Required
	outcome.Missing = 0
	outcome.ProviderCall = true
	return outcome, nil
}

func readCampaignBaseline(stateDir string, loaded loadedBenchmarkPlan) (baselineManifest, error) {
	var manifest baselineManifest
	if err := readCampaignJSON(filepath.Join(stateDir, "manifest.json"), &manifest); err != nil {
		return baselineManifest{}, fmt.Errorf("read completed baseline manifest: %w", err)
	}
	if loaded.Legacy {
		if manifest.SchemaVersion != legacyBaselineStateSchema ||
			manifest.PlanID != loaded.ID ||
			manifest.Model != loaded.Model.PublishedIdentifier ||
			manifest.Reasoning != loaded.Effort {
			return baselineManifest{}, fmt.Errorf("legacy baseline manifest does not match the selected plan")
		}
		expected := repetitionsForPlan(loaded.Plan)
		if !sameIntMap(manifest.Repetitions, expected) {
			return baselineManifest{}, fmt.Errorf("legacy baseline repetitions do not match the selected plan")
		}
		if _, err := readArmResults(stateDir, loaded.Plan.Tasks, manifest.TaskChecksums); err != nil {
			return baselineManifest{}, fmt.Errorf("legacy baseline evidence is incomplete: %w", err)
		}
		return manifest, nil
	}
	if (manifest.SchemaVersion != baselineStateSchema &&
		manifest.SchemaVersion != providerlessBaselineSchema) ||
		manifest.PlanID != loaded.ID ||
		manifest.CampaignID != loaded.CampaignID ||
		manifest.Model != loaded.Model.PublishedIdentifier || manifest.Reasoning != loaded.Effort {
		return baselineManifest{}, fmt.Errorf("baseline manifest does not match the selected plan")
	}
	expected := repetitionsForPlan(loaded.Plan)
	if !sameIntMap(manifest.Repetitions, expected) {
		return baselineManifest{}, fmt.Errorf("baseline repetitions do not match the selected plan")
	}
	if _, err := readArmResults(stateDir, loaded.Plan.Tasks, manifest.TaskChecksums); err != nil {
		return baselineManifest{}, fmt.Errorf("baseline evidence is incomplete: %w", err)
	}
	return manifest, nil
}

func repetitionsForPlan(plan benchmarkPlan) map[string]int {
	repetitions := make(map[string]int, len(plan.Tasks))
	for _, task := range plan.Tasks {
		repetitions[task.ID] = task.RepetitionsPerArm
	}
	return repetitions
}

func validateCampaignTreatmentManifest(path string, manifest *campaignTreatmentManifest) (bool, error) {
	var existing campaignTreatmentManifest
	if err := readCampaignJSON(path, &existing); err == nil {
		manifest.CreatedAt = existing.CreatedAt
		if existing.ProviderClient != manifest.ProviderClient {
			return false, fmt.Errorf(
				"existing treatment state used provider client version %q; current campaign requires %q",
				existing.ProviderClient,
				manifest.ProviderClient,
			)
		}
		expected, _ := json.Marshal(manifest)
		actual, _ := json.Marshal(existing)
		if string(expected) != string(actual) {
			return false, fmt.Errorf("existing treatment state uses a different immutable bundle or label")
		}
		manifest.Label = existing.Label
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read treatment manifest: %w", err)
	}
	return false, nil
}

func ensureCampaignTreatmentManifest(path string, manifest *campaignTreatmentManifest) error {
	if exists, err := validateCampaignTreatmentManifest(path, manifest); err != nil {
		return err
	} else if exists {
		return nil
	}
	return writeJSON(path, manifest)
}

// BuildCampaignReport derives statistics and presentation artifacts from
// immutable arm evidence. It never makes provider calls.
func BuildCampaignReport(options CampaignReportOptions) (CampaignReportOutcome, error) {
	if options.RepoRoot == "" || options.PlanPath == "" {
		return CampaignReportOutcome{}, fmt.Errorf("benchmark report requires a repository root and --plan")
	}
	loaded, err := loadBenchmarkPlanInput(options.PlanPath, options.PlanJSON)
	if err != nil {
		return CampaignReportOutcome{}, err
	}
	if loaded.Legacy {
		if options.Execution != "" {
			selectedModel, selectedEffort, selectionErr := ParseModelSelection(options.Execution)
			if selectionErr != nil {
				return CampaignReportOutcome{}, selectionErr
			}
			if selectedModel.PublishedIdentifier != loaded.Model.PublishedIdentifier ||
				selectedEffort != loaded.Effort {
				return CampaignReportOutcome{}, fmt.Errorf(
					"legacy campaign executed %s; --execution selected %s",
					executionLabel(loaded.Model, loaded.Effort),
					options.Execution,
				)
			}
		}
	} else {
		if options.Execution == "" {
			return CampaignReportOutcome{}, fmt.Errorf("schema version 2 benchmark reports require --execution")
		}
		loaded, err = bindBenchmarkExecution(loaded, options.Execution)
		if err != nil {
			return CampaignReportOutcome{}, err
		}
	}
	var documents [][]byte
	var analysisPaths, skippedTreatments []string
	if len(options.LegacyAnalysisPaths) > 0 {
		if !loaded.Legacy {
			return CampaignReportOutcome{}, fmt.Errorf("--analysis is only supported for schema version 1 benchmark reports")
		}
		for _, path := range options.LegacyAnalysisPaths {
			data, readErr := os.ReadFile(path) // #nosec G304 -- the user explicitly selected this immutable legacy analysis.
			if readErr != nil {
				return CampaignReportOutcome{}, fmt.Errorf("read legacy analysis %s: %w", path, readErr)
			}
			upgraded, upgradeErr := upgradeLegacyObservedAnalysis(data, loaded)
			if upgradeErr != nil {
				return CampaignReportOutcome{}, fmt.Errorf("upgrade legacy analysis %s: %w", path, upgradeErr)
			}
			documents = append(documents, upgraded)
			analysisPaths = append(analysisPaths, path)
		}
	} else {
		if err := validatePlanCostAxis(loaded.Plan); err != nil {
			if loaded.Legacy && loaded.Plan.CostAxis == nil {
				return CampaignReportOutcome{}, fmt.Errorf("legacy benchmark plan has no cost-axis provenance; pass its explicit canonical analysis with --analysis")
			}
			return CampaignReportOutcome{}, err
		}
		baselineDir := baselineStateDir(options.RepoRoot, loaded.CampaignID)
		baseline, baselineErr := readCampaignBaseline(baselineDir, loaded)
		if baselineErr != nil {
			return CampaignReportOutcome{}, baselineErr
		}
		treatments, skipped, treatmentErr := discoverCampaignTreatments(options.RepoRoot, loaded, baseline)
		if treatmentErr != nil {
			return CampaignReportOutcome{}, treatmentErr
		}
		skippedTreatments = skipped
		for _, treatment := range treatments {
			document, buildErr := analyzeCampaignVersion(loaded, baselineDir, baseline, treatment.path, treatment.manifest)
			if buildErr != nil {
				return CampaignReportOutcome{}, buildErr
			}
			data, marshalErr := json.MarshalIndent(document, "", "  ")
			if marshalErr != nil {
				return CampaignReportOutcome{}, fmt.Errorf("encode observed analysis: %w", marshalErr)
			}
			path := filepath.Join(treatment.path, "analysis.json")
			if writeErr := writeJSONBytes(path, append(data, '\n')); writeErr != nil {
				return CampaignReportOutcome{}, writeErr
			}
			documents = append(documents, data)
			analysisPaths = append(analysisPaths, path)
		}
	}
	report, err := BuildObservedCampaignReport(documents...)
	if err != nil {
		return CampaignReportOutcome{}, err
	}
	report.ObservedCampaign.Warnings = append(report.ObservedCampaign.Warnings, skippedTreatments...)
	jsonData, err := RenderJSON(report)
	if err != nil {
		return CampaignReportOutcome{}, err
	}
	htmlData, err := RenderHTML(report)
	if err != nil {
		return CampaignReportOutcome{}, err
	}
	reportDir := filepath.Join(campaignRoot(options.RepoRoot, loaded.CampaignID), "report")
	jsonPath := filepath.Join(reportDir, "report.json")
	htmlPath := filepath.Join(reportDir, "index.html")
	if err := writeJSONBytes(jsonPath, append(jsonData, '\n')); err != nil {
		return CampaignReportOutcome{}, err
	}
	if err := writeJSONBytes(htmlPath, htmlData); err != nil {
		return CampaignReportOutcome{}, err
	}
	return CampaignReportOutcome{
		PlanID: loaded.ID, CampaignID: loaded.CampaignID,
		Execution: executionLabel(loaded.Model, loaded.Effort),
		Versions:  len(documents), Report: report,
		JSONPath: jsonPath, HTMLPath: htmlPath, Analyses: analysisPaths,
		SkippedTreatments: skippedTreatments,
	}, nil
}

func campaignRoot(repoRoot, campaignID string) string {
	return filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "campaigns", campaignID)
}

func validatePlanCostAxis(plan benchmarkPlan) error {
	axis := plan.CostAxis
	if axis == nil {
		return fmt.Errorf("benchmark plan has no cost-axis provenance; export a new plan from the current planner")
	}
	if !axis.Valid || axis.Scale != costAxisLogarithmic ||
		axis.ReferenceConfiguration != observedCostAxisReference ||
		len(axis.ReferenceSnapshotSHA256) != 64 ||
		axis.ReferenceEstimatedArmCostUSD <= 0 ||
		axis.RoundingIncrementUSD != observedCostAxisRoundingUSD ||
		!closeEnough(axis.MaximumUSD, math.Ceil(axis.ReferenceEstimatedArmCostUSD/axis.RoundingIncrementUSD)*axis.RoundingIncrementUSD) {
		return fmt.Errorf("benchmark plan has an invalid cost-axis contract")
	}
	return nil
}

type discoveredTreatment struct {
	path     string
	manifest campaignTreatmentManifest
}

func discoverCampaignTreatments(repoRoot string, loaded loadedBenchmarkPlan, baseline baselineManifest) ([]discoveredTreatment, []string, error) {
	root := campaignRoot(repoRoot, loaded.CampaignID)
	candidates, err := filepath.Glob(filepath.Join(root, "treatments", "*"))
	if err != nil {
		return nil, nil, fmt.Errorf("discover treatment versions: %w", err)
	}
	var treatments []discoveredTreatment
	var skipped []string
	for _, path := range candidates {
		var manifest campaignTreatmentManifest
		if err := readCampaignJSON(filepath.Join(path, "manifest.json"), &manifest); err != nil {
			return nil, nil, fmt.Errorf("read treatment manifest %s: %w", path, err)
		}
		expectedSchema := campaignTreatmentSchema
		if loaded.Legacy {
			expectedSchema = legacyCampaignTreatmentSchema
		}
		if manifest.SchemaVersion != expectedSchema {
			return nil, nil, fmt.Errorf("treatment %s has an unsupported manifest schema", path)
		}
		if manifest.PlanID != loaded.ID || manifest.Arm != ArmTreatment ||
			manifest.Model != loaded.Model.PublishedIdentifier || manifest.Reasoning != loaded.Effort ||
			!sameStringMap(manifest.TaskChecksums, baseline.TaskChecksums) ||
			!sameIntMap(manifest.Repetitions, baseline.Repetitions) ||
			manifest.Treatment.Mode != TreatmentInstructionsAndSkills ||
			manifest.Treatment.AgentTimeoutMultiplier != skillsAgentTimeoutFactor {
			return nil, nil, fmt.Errorf("treatment %s does not match the campaign baseline", path)
		}
		if !loaded.Legacy && manifest.CampaignID != loaded.CampaignID {
			return nil, nil, fmt.Errorf("treatment %s does not match the campaign identity", path)
		}
		if manifest.Label == "" {
			manifest.Label = "Agent Layer instructions and skills"
		}
		execution := armExecution{
			repoRoot: repoRoot, stateDir: path, arm: ArmTreatment, concurrency: 1,
			loaded: loaded, checksums: baseline.TaskChecksums,
		}
		if missing := missingPlanCells(execution); len(missing) > 0 {
			skipped = append(skipped, fmt.Sprintf(
				"Skipped incomplete treatment %q: %d of %d runs are missing.",
				manifest.Label, len(missing), loaded.RunCount,
			))
			continue
		}
		treatments = append(treatments, discoveredTreatment{path: path, manifest: manifest})
	}
	if len(treatments) == 0 {
		return nil, skipped, fmt.Errorf("campaign has no completed treatment versions")
	}
	sort.SliceStable(treatments, func(i, j int) bool {
		return treatments[i].manifest.CreatedAt.Before(treatments[j].manifest.CreatedAt)
	})
	return treatments, skipped, nil
}

func analyzeCampaignVersion(loaded loadedBenchmarkPlan, baselineDir string, baseline baselineManifest, treatmentDir string, treatment campaignTreatmentManifest) (observedAnalysisDocument, error) {
	var document observedAnalysisDocument
	document.Schema = observedAnalysisSchema
	document.SchemaVersion = observedAnalysisSchemaVersion
	document.GeneratedAt = treatment.CreatedAt.UTC()
	document.PlanID = loaded.ID
	document.CampaignID = loaded.CampaignID
	document.Experiment.Model = loaded.Model.PublishedIdentifier
	document.Experiment.Reasoning = loaded.Effort
	document.Experiment.CalibrationReference = loaded.Plan.CalibrationReference.ID
	document.Experiment.CalibrationContrast = loaded.Plan.CalibrationContrast.ID
	document.Experiment.Baseline = "Bare model"
	document.Experiment.Treatment = treatment.Label
	document.Experiment.Tasks = len(loaded.Plan.Tasks)
	document.Experiment.RepetitionsPerArm = repetitionsForPlan(loaded.Plan)
	document.Experiment.TwoSidedSignificanceLevel = loaded.Plan.Parameters.TwoSidedSignificanceLevel
	document.Experiment.EqualTaskWeighting = true
	document.Experiment.AgentTimeoutMultiplier = treatment.Treatment.AgentTimeoutMultiplier
	axis := loaded.Plan.CostAxis
	document.CostAxis.Scale = axis.Scale
	document.CostAxis.ReferenceConfiguration = axis.ReferenceConfiguration
	document.CostAxis.ReferenceSnapshotSHA256 = axis.ReferenceSnapshotSHA256
	document.CostAxis.ReferenceEstimatedArmCostUSD = axis.ReferenceEstimatedArmCostUSD
	document.CostAxis.RoundingIncrementUSD = axis.RoundingIncrementUSD
	document.CostAxis.MaximumUSD = axis.MaximumUSD
	document.Limitations = []string{
		"The result applies only to this selected task suite, model, reasoning level, harness, and immutable treatment bundle.",
		"The threshold uses observed per-task variance from both arms and a Welch-Satterthwaite Student's t approximation.",
		"Published mini-swe-agent trials selected the suite but are not used in this observed-arm threshold or verdict.",
		"Dispatch-conformance diagnostics do not affect score eligibility or the outcome-only verdict.",
	}

	taskCount := float64(len(loaded.Plan.Tasks))
	var propagatedVariance, welchDenominator float64
	var baselineMeans, treatmentMeans []float64
	for _, task := range loaded.Plan.Tasks {
		item := ObservedTaskReport{Task: task.ID, RepetitionsPerArm: task.RepetitionsPerArm}
		for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
			baselineResult, err := readCampaignResult(baselineDir, task.ID, attempt, baseline.TaskChecksums[task.ID], loaded, false)
			if err != nil {
				return observedAnalysisDocument{}, err
			}
			treatmentResult, err := readCampaignResult(treatmentDir, task.ID, attempt, baseline.TaskChecksums[task.ID], loaded, true)
			if err != nil {
				return observedAnalysisDocument{}, err
			}
			item.BaselineScores = append(item.BaselineScores, baselineResult.F2PScore)
			item.TreatmentScores = append(item.TreatmentScores, treatmentResult.F2PScore)
			if baselineResult.VerifierBuildFailed {
				item.BaselineVerifierBuildFailedRuns++
			}
			if treatmentResult.VerifierBuildFailed {
				item.TreatmentVerifierBuildFailedRuns++
			}
			addResultCost(&document.CostUSD.Baseline, baselineResult)
			addResultCost(&document.CostUSD.Treatment, treatmentResult)
			document.TreatmentDiagnostics.InvocationCount += treatmentResult.InvocationCount
			if treatmentResult.DispatchConformant {
				document.TreatmentDiagnostics.DispatchConformantRuns++
			}
			document.TreatmentDiagnostics.TotalRuns++
		}
		var err error
		item.BaselineMean, item.BaselineSampleVariance, err = observedMoments(item.BaselineScores)
		if err != nil {
			return observedAnalysisDocument{}, err
		}
		item.TreatmentMean, item.TreatmentSampleVariance, err = observedMoments(item.TreatmentScores)
		if err != nil {
			return observedAnalysisDocument{}, err
		}
		item.Difference = item.TreatmentMean - item.BaselineMean
		baselineMeans = append(baselineMeans, item.BaselineMean)
		treatmentMeans = append(treatmentMeans, item.TreatmentMean)
		for _, variance := range []float64{item.BaselineSampleVariance, item.TreatmentSampleVariance} {
			component := variance / (taskCount * taskCount * float64(item.RepetitionsPerArm))
			propagatedVariance += component
			welchDenominator += component * component / float64(item.RepetitionsPerArm-1)
		}
		document.Tasks = append(document.Tasks, item)
	}
	if propagatedVariance <= 0 || welchDenominator <= 0 {
		return observedAnalysisDocument{}, fmt.Errorf("both observed arms have zero variance across the complete suite; threshold is not estimable")
	}
	document.Result.BaselineMean = observedMean(baselineMeans)
	document.Result.TreatmentMean = observedMean(treatmentMeans)
	document.Result.ObservedDifference = document.Result.TreatmentMean - document.Result.BaselineMean
	document.Result.StandardError = math.Sqrt(propagatedVariance)
	document.Result.EffectiveDegreesOfFreedom = propagatedVariance * propagatedVariance / welchDenominator
	document.Result.TCriticalValue = studentTCritical(loaded.Plan.Parameters.TwoSidedSignificanceLevel, document.Result.EffectiveDegreesOfFreedom)
	document.Result.DecisionThreshold = document.Result.StandardError * document.Result.TCriticalValue
	document.Result.Verdict = verdictInconclusive
	if document.Result.ObservedDifference > document.Result.DecisionThreshold {
		document.Result.Verdict = verdictBetter
	} else if document.Result.ObservedDifference < -document.Result.DecisionThreshold {
		document.Result.Verdict = verdictWorse
	}
	document.CostUSD.Total = addObservedCost(document.CostUSD.Baseline, document.CostUSD.Treatment)
	return document, document.validate()
}

func readCampaignResult(root, task string, attempt int, checksum string, loaded loadedBenchmarkPlan, treatment bool) (AttemptResult, error) {
	var result AttemptResult
	path := armResultPath(root, task, attempt)
	if err := readCampaignJSON(path, &result); err != nil {
		return AttemptResult{}, fmt.Errorf("read %s repetition %d: %w", task, attempt, err)
	}
	if !validPlanResult(path, task, attempt, checksum, loaded.Model, loaded.Effort, treatment) {
		return AttemptResult{}, fmt.Errorf("invalid %s repetition %d evidence", task, attempt)
	}
	return result, nil
}

func addResultCost(total *ObservedCostRange, result AttemptResult) {
	minimum, maximum, _ := result.CostBounds()
	total.Midpoint += *result.CostUSD
	total.Minimum += minimum
	total.Maximum += maximum
}

func studentTCritical(alpha, degreesOfFreedom float64) float64 {
	low, high := 0.0, 10000.0
	for iteration := 0; iteration < 200; iteration++ {
		middle := (low + high) / 2
		if twoSidedStudentTTail(middle, degreesOfFreedom) > alpha {
			low = middle
		} else {
			high = middle
		}
	}
	return (low + high) / 2
}

func twoSidedStudentTTail(value, degreesOfFreedom float64) float64 {
	return regularizedBeta(
		degreesOfFreedom/2,
		0.5,
		degreesOfFreedom/(degreesOfFreedom+value*value),
	)
}

func regularizedBeta(a, b, x float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	factor := math.Exp(logGamma(a+b) - logGamma(a) - logGamma(b) + a*math.Log(x) + b*math.Log(1-x))
	if x < (a+1)/(a+b+2) {
		return factor * betaFraction(a, b, x) / a
	}
	return 1 - factor*betaFraction(b, a, 1-x)/b
}

func logGamma(value float64) float64 {
	coefficients := []float64{
		76.18009172947146, -86.50532032941677, 24.01409824083091,
		-1.231739572450155, 0.001208650973866179, -0.000005395239384953,
	}
	cursor := value
	temporary := value + 5.5 - (value+0.5)*math.Log(value+5.5)
	series := 1.000000000190015
	for _, coefficient := range coefficients {
		cursor++
		series += coefficient / cursor
	}
	return -temporary + math.Log(2.5066282746310005*series/value)
}

func betaFraction(a, b, x float64) float64 {
	const floor = 1e-300
	qab, qap, qam := a+b, a+1, a-1
	c := 1.0
	d := 1 - qab*x/qap
	if math.Abs(d) < floor {
		d = floor
	}
	d = 1 / d
	result := d
	for iteration := 1; iteration <= 200; iteration++ {
		doubled := float64(2 * iteration)
		i := float64(iteration)
		coefficient := i * (b - i) * x / ((qam + doubled) * (a + doubled))
		d = 1 + coefficient*d
		if math.Abs(d) < floor {
			d = floor
		}
		c = 1 + coefficient/c
		if math.Abs(c) < floor {
			c = floor
		}
		d = 1 / d
		result *= d * c
		coefficient = -(a + i) * (qab + i) * x / ((a + doubled) * (qap + doubled))
		d = 1 + coefficient*d
		if math.Abs(d) < floor {
			d = floor
		}
		c = 1 + coefficient/c
		if math.Abs(c) < floor {
			c = floor
		}
		d = 1 / d
		delta := d * c
		result *= delta
		if math.Abs(delta-1) < 3e-14 {
			break
		}
	}
	return result
}

func writeJSONBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create benchmark report directory: %w", err)
	}
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return fmt.Errorf("write benchmark report: %w", err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return fmt.Errorf("publish benchmark report: %w", err)
	}
	return nil
}

func copyStringMap(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func copyIntMap(source map[string]int) map[string]int {
	clone := make(map[string]int, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func readCampaignJSON(path string, value any) error {
	data, err := os.ReadFile(path) // #nosec G304 -- paths are validated campaign state or explicit plan-derived paths.
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}
