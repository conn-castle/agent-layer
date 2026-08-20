package benchmark

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// StudyOptions configures the single public DeepSWE benchmark workflow.
// TaskConcurrency and Tasks scope one invocation only; neither affects identity.
type StudyOptions struct {
	RepoRoot        string
	StudyPath       string
	TaskConcurrency int
	Tasks           []string
	DryRun          bool
	// OnPrepared receives validated cached/missing progress after the full
	// runtime preflight and before any provider call.
	OnPrepared func(StudyOutcome) error
	// OnCellComplete is a serial CLI-facing progress hook. Library code never
	// writes stdout, and completed cells remain observable even if a later cell
	// fails.
	OnCellComplete func(ObservedCostRange)
}

// StudyOutcome is the user-facing progress summary for a study invocation.
type StudyOutcome struct {
	StudyID                  string
	SelectionID              string
	Required                 int
	Completed                int
	Missing                  int
	HasBareExperiment        bool
	BarePublishedEstimateUSD *float64
	ObservedInvocationCost   ObservedCostRange
	JSONPath                 string
	HTMLPath                 string
	Experiments              []StudyExperimentProgress
}

// StudyExperimentProgress reports cached and missing cells for one experiment.
type StudyExperimentProgress struct {
	Name      string
	Identity  string
	Completed int
	Required  int
	Missing   int
}

type studyDocument struct {
	Selection   string            `toml:"selection"`
	Experiments []studyExperiment `toml:"experiments"`
}

type studyExperiment struct {
	Name                  string   `toml:"name"`
	Model                 string   `toml:"model"`
	Reasoning             string   `toml:"reasoning"`
	Config                string   `toml:"config"`
	Instructions          string   `toml:"instructions"`
	Skills                string   `toml:"skills"`
	EntryPrompt           string   `toml:"entry_prompt"`
	RequiredDispatchRoles []string `toml:"required_dispatch_roles"`
}

type preparedStudyExperiment struct {
	studyExperiment
	model       Model
	effort      string
	identity    string
	inputHashes map[string]string
	inputs      studyExperimentInputs
}

type preparedStudy struct {
	document    studyDocument
	selection   matrixSelection
	selectionID string
	studyID     string
	experiments []preparedStudyExperiment
	inputRoot   string
}

// studyExperimentInputs names bytes copied into a private immutable staging
// directory during preparation. No execution path reads the mutable files
// declared by study.toml after this point.
type studyExperimentInputs struct {
	Config       string
	Instructions string
	Skills       string
	EntryPrompt  string
}

func (study preparedStudy) cleanupInputs() {
	if study.inputRoot != "" {
		_ = os.RemoveAll(study.inputRoot)
	}
}

type immutableStudyManifest struct {
	SchemaVersion string             `json:"schema_version"`
	StudyID       string             `json:"study_id"`
	SelectionID   string             `json:"selection_id"`
	Membership    []string           `json:"ordered_display_membership"`
	Checksums     map[string]string  `json:"task_checksums"`
	Environments  map[string]string  `json:"task_environment_identities"`
	Resources     map[string]any     `json:"resource_contract"`
	Arms          []studyArmContract `json:"arms"`
}

type studyArmContract struct {
	Name           string `json:"name"`
	ID             string `json:"id"`
	Target         string `json:"target"`
	Bundle         string `json:"bundle_manifest_hash,omitempty"`
	Adapter        string `json:"adapter_sha256,omitempty"`
	Runtime        string `json:"runtime_sha256,omitempty"`
	RuntimeSource  string `json:"runtime_source_kind,omitempty"`
	RuntimeVersion string `json:"runtime_version,omitempty"`
}

func writeStudyManifest(study *preparedStudy, preparation matrixPreparation) error {
	manifest := immutableStudyManifest{SchemaVersion: "deepswe-study-manifest-v1", StudyID: study.studyID, SelectionID: study.selectionID, Checksums: copyStringMap(preparation.checksums), Environments: copyStringMap(preparation.environments), Resources: map[string]any{studyResourceSchemaKey: studyResourceSchema, studyResourceTimeoutKey: skillsAgentTimeoutFactor}}
	for _, arm := range preparation.arms {
		manifest.Membership = append(manifest.Membership, arm.Label)
		contract := studyArmContract{Name: arm.Label, ID: arm.ID, Target: arm.Loaded.Model.RuntimeIdentifier + ":" + arm.Loaded.Effort}
		if arm.Bundle != nil {
			contract.Bundle, contract.Adapter, contract.Runtime, contract.RuntimeSource, contract.RuntimeVersion = arm.Bundle.ManifestHash, arm.Bundle.AdapterSHA256, arm.Bundle.LinuxBinarySHA256, arm.Bundle.RuntimeSourceKind, arm.Bundle.RuntimeVersion
		}
		manifest.Arms = append(manifest.Arms, contract)
	}
	path := filepath.Join(preparation.stateDir, "study-manifest.json")
	if _, err := os.Stat(path); err == nil {
		var existing immutableStudyManifest
		if err := readStudyJSON(path, &existing); err != nil {
			return fmt.Errorf("read immutable study manifest: %w", err)
		}
		left, leftErr := hashCanonical(existing)
		right, rightErr := hashCanonical(manifest)
		if leftErr != nil || rightErr != nil {
			return fmt.Errorf("hash immutable study manifest: %w", errors.Join(leftErr, rightErr))
		}
		if left != right {
			return fmt.Errorf("study %s conflicts with its immutable manifest", study.studyID)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect immutable study manifest: %w", err)
	}
	return writeJSON(path, manifest)
}

// RunStudy is intentionally the only paid public entry point. The command itself
// is authorization; callers must use DryRun when they need the no-call path.
func RunStudy(ctx context.Context, options StudyOptions, executor TaskExecutor) (StudyOutcome, error) {
	prepared, err := prepareStudy(options)
	if err != nil {
		return StudyOutcome{}, err
	}
	defer prepared.cleanupInputs()
	preparation, err := prepareStudyExecution(ctx, options, &prepared)
	if err != nil {
		return StudyOutcome{}, err
	}
	outcome := studyProgress(prepared, options)
	progress, err := studyProgressChecked(preparation)
	if err != nil {
		return StudyOutcome{}, err
	}
	outcome.Completed, outcome.Missing = progress.Completed, progress.Missing
	for i := range outcome.Experiments {
		outcome.Experiments[i].Completed = progress.Arms[i].Completed
		outcome.Experiments[i].Missing = progress.Arms[i].Missing
	}
	if options.OnPrepared != nil {
		if err := options.OnPrepared(outcome); err != nil {
			return StudyOutcome{}, err
		}
	}
	if options.DryRun {
		return outcome, nil
	}
	if executor == nil {
		executor = PierExecutor{}
	}
	priorCost, err := studyStoredCostChecked(preparation)
	if err != nil {
		return StudyOutcome{}, err
	}
	invocationCost := ObservedCostRange{}
	if err := executeMatrix(ctx, options.RepoRoot, preparation.checksums, preparation.environments, preparation.arms, options.Tasks, preparation.taskConcurrency, executor, func(result AttemptResult) {
		minimum, maximum, boundsErr := result.CostBounds()
		if boundsErr != nil {
			return
		}
		invocationCost.Midpoint += *result.CostUSD
		invocationCost.Minimum += minimum
		invocationCost.Maximum += maximum
		if options.OnCellComplete != nil {
			options.OnCellComplete(addObservedCost(priorCost, invocationCost))
		}
	}); err != nil {
		return StudyOutcome{}, err
	}
	report, jsonPath, htmlPath, err := buildStudyReport(prepared, preparation)
	if err != nil {
		return StudyOutcome{}, err
	}
	outcome = studyProgress(prepared, options)
	progress, err = studyProgressChecked(preparation)
	if err != nil {
		return StudyOutcome{}, err
	}
	outcome.Completed, outcome.Missing = progress.Completed, progress.Missing
	outcome.JSONPath, outcome.HTMLPath = jsonPath, htmlPath
	outcome.ObservedInvocationCost = subtractObservedCost(studyReportCost(report), priorCost)
	for i := range outcome.Experiments {
		outcome.Experiments[i].Completed = progress.Arms[i].Completed
		outcome.Experiments[i].Missing = progress.Arms[i].Missing
	}
	return outcome, nil
}

func prepareStudyExecution(ctx context.Context, options StudyOptions, prepared *preparedStudy) (matrixPreparation, error) {
	if options.TaskConcurrency == 0 {
		options.TaskConcurrency = 1
	}
	tasks := make([]benchmarkPlanTask, len(prepared.selection.Tasks))
	for i, task := range prepared.selection.Tasks {
		tasks[i] = benchmarkPlanTask{ID: task.ID, RepetitionsPerArm: task.Repetitions}
	}
	selections := make([]parsedSelection, len(prepared.experiments))
	for i, experiment := range prepared.experiments {
		selections[i] = parsedSelection{model: experiment.model, effort: experiment.effort}
	}
	if err := preflightBenchmark(selections); err != nil {
		return matrixPreparation{}, err
	}
	if err := verifyBenchmarkPier(ctx); err != nil {
		return matrixPreparation{}, err
	}
	if err := validateBenchmarkAuthentication(options.RepoRoot, selections); err != nil {
		return matrixPreparation{}, err
	}
	checksums, environments, err := prepareBenchmarkTaskSet(ctx, options.RepoRoot, tasks)
	if err != nil {
		return matrixPreparation{}, err
	}
	preparation := matrixPreparation{selection: prepared.selection, selectionID: prepared.selectionID, tasks: tasks, checksums: checksums, environments: environments, taskConcurrency: options.TaskConcurrency}
	for _, experiment := range prepared.experiments {
		mode := ArmBaseline
		var bundle *TreatmentBundle
		if experiment.Config != "" || experiment.Instructions != "" || experiment.Skills != "" {
			mode = ArmTreatment
			bundle, err = BuildStudyTreatmentBundle(options.RepoRoot, experiment)
			if err != nil {
				return matrixPreparation{}, fmt.Errorf("stage experiment %q: %w", experiment.Name, err)
			}
			bundle, err = pinStudyTreatmentBundle(options.RepoRoot, bundle)
			if err != nil {
				return matrixPreparation{}, fmt.Errorf("pin experiment %q effective bundle: %w", experiment.Name, err)
			}
		}
		identityInput := struct {
			Schema, Selection, Experiment          string
			TaskChecksums, Environments            map[string]string
			Resource                               map[string]any
			ManifestHash, AdapterHash, RuntimeHash string
		}{Schema: "deepswe-study-arm-v2", Selection: prepared.selectionID, Experiment: experiment.identity,
			TaskChecksums: checksums, Environments: environments,
			Resource: map[string]any{studyResourceSchemaKey: studyResourceSchema, studyResourceTimeoutKey: skillsAgentTimeoutFactor}}
		if bundle != nil {
			identityInput.ManifestHash, identityInput.AdapterHash, identityInput.RuntimeHash = bundle.ManifestHash, bundle.AdapterSHA256, bundle.LinuxBinarySHA256
		}
		identity, err := hashCanonical(identityInput)
		if err != nil {
			return matrixPreparation{}, err
		}
		loaded := loadedBenchmarkPlan{Model: experiment.model, Effort: experiment.effort}
		loaded.Plan.Tasks = tasks
		arm := matrixArm{ID: identity, Label: experiment.Name, Mode: mode, Loaded: loaded, Bundle: bundle, AgentTimeoutMultiplier: skillsAgentTimeoutFactor, IgnoreProviderClientInManifest: true}
		preparation.arms = append(preparation.arms, arm)
	}
	membership := make([]struct{ Name, Arm string }, len(preparation.arms))
	for i, arm := range preparation.arms {
		membership[i] = struct{ Name, Arm string }{arm.Label, arm.ID}
	}
	studyID, err := identifyStudy(prepared.selectionID, membership, checksums, environments)
	if err != nil {
		return matrixPreparation{}, err
	}
	prepared.studyID = studyID
	preparation.stateDir = filepath.Join(options.RepoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "studies", studyID)
	for index := range preparation.arms {
		arm := &preparation.arms[index]
		arm.StateDir = filepath.Join(preparation.stateDir, "arms", arm.ID)
		if err := ensureStudyArmManifest(prepared.selectionID, tasks, checksums, arm); err != nil {
			return matrixPreparation{}, err
		}
		if err := reuseMatchingEvidence(options.RepoRoot, prepared.selectionID, tasks, checksums, environments, arm); err != nil {
			return matrixPreparation{}, fmt.Errorf("reuse immutable historical evidence for experiment %q: %w", arm.Label, err)
		}
		if arm.Bundle == nil {
			continue
		}
		seen := map[string]bool{}
		for _, task := range tasks {
			if seen[environments[task.ID]] {
				continue
			}
			seen[environments[task.ID]] = true
			eventID, eventErr := NewEventID()
			if eventErr != nil {
				return matrixPreparation{}, eventErr
			}
			if err := preflightTreatmentRuntime(ctx, ExecutionRequest{RepoRoot: options.RepoRoot, EvidenceDir: arm.StateDir, EventID: eventID, Attempt: 1, Task: task.ID, Model: arm.Loaded.Model, Effort: arm.Loaded.Effort, Arm: arm.Mode, Bundle: arm.Bundle, TaskChecksum: checksums[task.ID], EnvironmentIdentity: environments[task.ID], AgentTimeoutMultiplier: arm.AgentTimeoutMultiplier}); err != nil {
				return matrixPreparation{}, fmt.Errorf("preflight experiment %q task %q runtime: %w", arm.Label, task.ID, err)
			}
		}
	}
	if err := writeStudyManifest(prepared, preparation); err != nil {
		return matrixPreparation{}, err
	}
	return preparation, nil
}

// reuseMatchingEvidence reuses only arms requested by this study. It checks
// sibling study compositions first, then compatible public matrix history;
// private campaign state is never traversed.
func reuseMatchingEvidence(repoRoot, selectionID string, tasks []benchmarkPlanTask, checksums, environments map[string]string, destination *matrixArm) error {
	if err := reuseMatchingStudyEvidence(repoRoot, selectionID, tasks, checksums, environments, destination); err != nil {
		return err
	}
	return reuseMatchingMatrixEvidence(repoRoot, selectionID, tasks, checksums, environments, destination)
}

func reuseMatchingStudyEvidence(repoRoot, selectionID string, tasks []benchmarkPlanTask, checksums, environments map[string]string, destination *matrixArm) error {
	root := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "studies")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect compatible study evidence: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sourceDir := filepath.Join(root, entry.Name(), "arms", destination.ID)
		if sourceDir == destination.StateDir {
			continue
		}
		manifest, found, err := reusableStudyArmManifest(sourceDir)
		if err != nil {
			return err
		}
		if !found || !compatibleStudyArmManifest(manifest, selectionID, tasks, checksums, destination) {
			continue
		}
		source := *destination
		source.StateDir = sourceDir
		if err := reuseArmCells(source, destination, tasks, checksums, environments); err != nil {
			return err
		}
	}
	return nil
}

func reuseMatchingMatrixEvidence(repoRoot, selectionID string, tasks []benchmarkPlanTask, checksums, environments map[string]string, destination *matrixArm) error {
	root := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "matrices", selectionID, "arms")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect historical matrix evidence: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sourceDir := filepath.Join(root, entry.Name())
		manifest, found, err := reusableLegacyMatrixArmManifest(sourceDir)
		if err != nil {
			return err
		}
		if !found || !compatibleLegacyMatrixArmManifest(manifest, selectionID, tasks, checksums, destination, sourceDir) {
			continue
		}
		source := *destination
		source.StateDir = sourceDir
		if err := reuseArmCells(source, destination, tasks, checksums, environments); err != nil {
			return err
		}
	}
	return nil
}

func reusableStudyArmManifest(root string) (studyArmManifest, bool, error) {
	var manifest studyArmManifest
	if err := readStudyJSON(filepath.Join(root, "manifest.json"), &manifest); errors.Is(err, os.ErrNotExist) {
		return manifest, false, nil
	} else if err != nil {
		return manifest, false, fmt.Errorf("read reusable arm manifest %s: %w", root, err)
	}
	return manifest, true, nil
}

func compatibleStudyArmManifest(manifest studyArmManifest, selectionID string, tasks []benchmarkPlanTask, checksums map[string]string, destination *matrixArm) bool {
	return manifest.SchemaVersion == studyArmManifestSchema && manifest.SelectionID == selectionID &&
		manifest.Mode == destination.Mode && manifest.Model == destination.Loaded.Model.PublishedIdentifier &&
		manifest.Reasoning == destination.Loaded.Effort && sameStringMap(manifest.TaskChecksums, checksums) &&
		sameIntMap(manifest.Repetitions, repetitionsForTasks(tasks)) && manifest.AgentTimeoutMultiplier == destination.AgentTimeoutMultiplier &&
		manifest.TreatmentHash == bundleManifestHash(destination.Bundle)
}

func reusableLegacyMatrixArmManifest(root string) (matrixArmManifest, bool, error) {
	var manifest matrixArmManifest
	if err := readStudyJSON(filepath.Join(root, "manifest.json"), &manifest); errors.Is(err, os.ErrNotExist) {
		return manifest, false, nil
	} else if err != nil {
		return manifest, false, fmt.Errorf("read reusable legacy matrix arm manifest %s: %w", root, err)
	}
	return manifest, true, nil
}

func compatibleLegacyMatrixArmManifest(manifest matrixArmManifest, selectionID string, tasks []benchmarkPlanTask, checksums map[string]string, destination *matrixArm, sourceDir string) bool {
	multiplier := manifest.AgentTimeoutMultiplier
	if multiplier == 0 && manifest.Mode == ArmTreatment {
		multiplier = historicalTreatmentMultiplier(sourceDir, manifest.TreatmentHash)
	}
	return manifest.SchemaVersion == matrixManifestSchema && manifest.SelectionID == selectionID &&
		manifest.Mode == destination.Mode && manifest.Model == destination.Loaded.Model.PublishedIdentifier &&
		manifest.Reasoning == destination.Loaded.Effort && sameStringMap(manifest.TaskChecksums, checksums) &&
		sameIntMap(manifest.Repetitions, repetitionsForTasks(tasks)) && multiplier == destination.AgentTimeoutMultiplier &&
		manifest.TreatmentHash == bundleManifestHash(destination.Bundle)
}

func historicalTreatmentMultiplier(sourceDir, treatmentHash string) float64 {
	if treatmentHash == "" {
		return 0
	}
	matrixRoot := filepath.Dir(filepath.Dir(sourceDir))
	entries, err := os.ReadDir(filepath.Join(matrixRoot, "treatment-pins"))
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		var pin historicalMatrixTreatmentPin
		if entry.IsDir() && readStudyJSON(filepath.Join(matrixRoot, "treatment-pins", entry.Name(), "pin.json"), &pin) == nil && pin.ManifestHash == treatmentHash {
			return pin.Manifest.AgentTimeoutMultiplier
		}
	}
	return 0
}

// historicalMatrixTreatmentPin is intentionally a narrow reader for the
// predecessor runner's immutable pin evidence. New studies write
// studyTreatmentPin instead and never extend this retired schema.
type historicalMatrixTreatmentPin struct {
	ManifestHash string            `json:"manifest_hash"`
	Manifest     TreatmentManifest `json:"manifest"`
}

func reuseArmCells(source matrixArm, destination *matrixArm, tasks []benchmarkPlanTask, checksums, environments map[string]string) error {
	for _, task := range tasks {
		for attempt := 1; attempt <= task.RepetitionsPerArm; attempt++ {
			state, _, err := inspectStudyCell(*destination, task.ID, attempt, checksums[task.ID], environments[task.ID])
			if err != nil {
				return err
			}
			if state == studyCellValid {
				continue
			}
			sourcePath := armResultPath(source.StateDir, task.ID, attempt)
			result, err := readStudyResult(sourcePath, task.ID, attempt, checksums[task.ID], environments[task.ID], source)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return fmt.Errorf("validate reusable %s repetition %d: %w", task.ID, attempt, err)
			}
			if err := promoteReusableCell(sourcePath, result, destination, task.ID, attempt, checksums[task.ID], environments[task.ID]); err != nil {
				return err
			}
		}
	}
	return nil
}

func promoteReusableCell(sourcePath string, result AttemptResult, destination *matrixArm, task string, attempt int, checksum, environment string) error {
	// readStudyResult has already validated this value, including any narrowly
	// authorized correction to predecessor matrix evidence. A study deliberately
	// does not consume legacy canonical-result sidecars, so promote the returned
	// canonical result itself rather than copying the obsolete source bytes.
	if err := result.Validate(); err != nil {
		return fmt.Errorf("validate canonical reusable result: %w", err)
	}
	destinationPath := armResultPath(destination.StateDir, task, attempt)
	stage, err := os.MkdirTemp(filepath.Dir(destination.StateDir), ".reuse-cell-")
	if err != nil {
		return fmt.Errorf("stage reusable cell: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	stagedResult := armResultPath(stage, task, attempt)
	if err := copyRequiredTree(filepath.Join(filepath.Dir(sourcePath), "artifacts", result.EventID), filepath.Join(filepath.Dir(stagedResult), "artifacts", result.EventID)); err != nil {
		return fmt.Errorf("copy reusable artifacts: %w", err)
	}
	if err := writeJSON(stagedResult, result); err != nil {
		return fmt.Errorf("stage canonical reusable result: %w", err)
	}
	stagedArm := *destination
	stagedArm.StateDir = stage
	if _, _, err := inspectStudyCell(stagedArm, task, attempt, checksum, environment); err != nil {
		return fmt.Errorf("validate staged reusable cell: %w", err)
	}
	if err := copyRequiredTree(filepath.Join(filepath.Dir(stagedResult), "artifacts"), filepath.Join(filepath.Dir(destinationPath), "artifacts")); err != nil {
		return err
	}
	// Publish the result last and atomically. Until this succeeds the copied
	// artifacts are not a valid study cell, so a failed promotion is retriable
	// without allowing a raw legacy score or a sidecar to enter study analysis.
	if err := writeJSON(destinationPath, result); err != nil {
		return fmt.Errorf("publish canonical reusable result: %w", err)
	}
	return nil
}

func studyProgress(prepared preparedStudy, _ StudyOptions) StudyOutcome {
	required := 0
	for _, task := range prepared.selection.Tasks {
		required += task.Repetitions
	}
	outcome := StudyOutcome{StudyID: prepared.studyID, SelectionID: prepared.selectionID}
	for _, experiment := range prepared.experiments {
		if experiment.Config == "" && experiment.Instructions == "" && experiment.Skills == "" {
			outcome.HasBareExperiment = true
			if experiment.model.PublishedIdentifier == prepared.selection.Selector.Model && experiment.effort == prepared.selection.Selector.Reasoning {
				estimate := prepared.selection.EstimatedPublishedSpendUSD
				outcome.BarePublishedEstimateUSD = &estimate
			}
		}
		outcome.Experiments = append(outcome.Experiments, StudyExperimentProgress{Name: experiment.Name, Identity: experiment.identity, Required: required, Missing: required})
		outcome.Required += required
		outcome.Missing += required
	}
	return outcome
}

func prepareStudy(options StudyOptions) (preparedStudy, error) {
	if options.RepoRoot == "" || options.StudyPath == "" {
		return preparedStudy{}, fmt.Errorf("benchmark run requires one study.toml path")
	}
	if options.TaskConcurrency == 0 {
		options.TaskConcurrency = 1
	}
	if options.TaskConcurrency < 1 || options.TaskConcurrency > 8 {
		return preparedStudy{}, fmt.Errorf("benchmark run task concurrency must be from 1 to 8")
	}
	studyPath, err := filepath.Abs(options.StudyPath)
	if err != nil {
		return preparedStudy{}, fmt.Errorf("resolve benchmark study: %w", err)
	}
	data, err := readRegularNonempty(studyPath)
	if err != nil {
		return preparedStudy{}, fmt.Errorf("read benchmark study: %w", err)
	}
	var document studyDocument
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return preparedStudy{}, fmt.Errorf("decode benchmark study: %w", err)
	}
	if strings.TrimSpace(document.Selection) == "" || len(document.Experiments) == 0 {
		return preparedStudy{}, fmt.Errorf("benchmark study requires selection and one or more experiments")
	}
	stageParent := filepath.Join(options.RepoRoot, ".agent-layer", "tmp")
	if err := os.MkdirAll(stageParent, 0o700); err != nil {
		return preparedStudy{}, fmt.Errorf("create study input snapshot root: %w", err)
	}
	inputRoot, err := os.MkdirTemp(stageParent, "benchmark-study-input-")
	if err != nil {
		return preparedStudy{}, fmt.Errorf("create study input snapshot: %w", err)
	}
	prepared := preparedStudy{document: document, inputRoot: inputRoot}
	keepInputs := false
	defer func() {
		if !keepInputs {
			prepared.cleanupInputs()
		}
	}()
	if err := writeSnapshotFile(filepath.Join(inputRoot, "study.toml"), data); err != nil {
		return preparedStudy{}, fmt.Errorf("snapshot benchmark study: %w", err)
	}
	base := filepath.Dir(studyPath)
	selectionPath, err := resolveStudyPath(base, document.Selection)
	if err != nil {
		return preparedStudy{}, fmt.Errorf("benchmark study selection: %w", err)
	}
	selectionSnapshot := filepath.Join(inputRoot, "selection.json")
	selectionData, err := snapshotStudySelection(selectionPath, selectionSnapshot)
	if err != nil {
		return preparedStudy{}, fmt.Errorf("benchmark study selection: %w", err)
	}
	selection, selectionID, err := loadMatrixSelection(selectionSnapshot, selectionData)
	if err != nil {
		return preparedStudy{}, err
	}
	if err := validateMatrixTaskFilter(selection, options.Tasks); err != nil {
		return preparedStudy{}, err
	}
	prepared.selection, prepared.selectionID = selection, selectionID
	seenNames := map[string]bool{}
	seenIdentities := map[string]bool{}
	for index, experiment := range document.Experiments {
		if strings.TrimSpace(experiment.Name) == "" || seenNames[experiment.Name] {
			return preparedStudy{}, fmt.Errorf("benchmark study experiment names must be unique and non-empty")
		}
		seenNames[experiment.Name] = true
		model, effort, err := ParseModelSelection(experiment.Model + ":" + experiment.Reasoning)
		if err != nil {
			return preparedStudy{}, fmt.Errorf("benchmark experiment %q: %w", experiment.Name, err)
		}
		if experiment.Model != modelNameForPublished(model.PublishedIdentifier) || experiment.Reasoning != effort {
			return preparedStudy{}, fmt.Errorf("benchmark experiment %q has an invalid explicit model or reasoning", experiment.Name)
		}
		roles, err := canonicalizeRequiredDispatchRoles(experiment.RequiredDispatchRoles, experiment.Skills != "")
		if err != nil {
			return preparedStudy{}, fmt.Errorf("benchmark experiment %q: %w", experiment.Name, err)
		}
		experiment.RequiredDispatchRoles = roles
		inputs, err := snapshotStudyExperimentInputs(inputRoot, index, base, experiment)
		if err != nil {
			return preparedStudy{}, fmt.Errorf("benchmark experiment %q: %w", experiment.Name, err)
		}
		hashes, err := validateExperimentInputs(inputs)
		if err != nil {
			return preparedStudy{}, fmt.Errorf("benchmark experiment %q: %w", experiment.Name, err)
		}
		identity, err := hashCanonical(struct {
			Schema    string            `json:"schema"`
			Model     string            `json:"model"`
			Reasoning string            `json:"reasoning"`
			Resources map[string]any    `json:"resources"`
			Inputs    map[string]string `json:"inputs"`
			Roles     []string          `json:"required_dispatch_roles,omitempty"`
		}{
			Schema: "deepswe-benchmark-experiment-v1", Model: model.PublishedIdentifier, Reasoning: effort,
			Resources: map[string]any{studyResourceSchemaKey: studyResourceSchema, studyResourceTimeoutKey: skillsAgentTimeoutFactor}, Inputs: hashes,
			Roles: roles,
		})
		if err != nil {
			return preparedStudy{}, fmt.Errorf("identify benchmark experiment %q: %w", experiment.Name, err)
		}
		if seenIdentities[identity] {
			return preparedStudy{}, fmt.Errorf("benchmark study has duplicate content-addressed experiment identities")
		}
		seenIdentities[identity] = true
		prepared.experiments = append(prepared.experiments, preparedStudyExperiment{
			studyExperiment: experiment, model: model, effort: effort, identity: identity, inputHashes: hashes, inputs: inputs,
		})
	}
	keepInputs = true
	return prepared, nil
}

// identifyStudy defines the production v3 state-directory identity after the
// task environment and immutable arm identities are known.
func identifyStudy(selectionID string, membership []struct{ Name, Arm string }, checksums, environments map[string]string) (string, error) {
	return hashCanonical(struct {
		Schema, Selection       string
		Membership              []struct{ Name, Arm string }
		Checksums, Environments map[string]string
		Resources               map[string]any
	}{Schema: "deepswe-study-v3", Selection: selectionID, Membership: membership, Checksums: checksums, Environments: environments, Resources: map[string]any{studyResourceSchemaKey: studyResourceSchema, studyResourceTimeoutKey: skillsAgentTimeoutFactor}})
}

func canonicalizeRequiredDispatchRoles(roles []string, hasSkills bool) ([]string, error) {
	if roles == nil {
		if hasSkills {
			return nil, fmt.Errorf("skills require required_dispatch_roles")
		}
		return nil, nil
	}
	if !hasSkills {
		return nil, fmt.Errorf("required_dispatch_roles is only valid with skills")
	}
	seen := make(map[string]bool, len(roles))
	normalized := make([]string, 0, len(roles))
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			return nil, fmt.Errorf("required_dispatch_roles must not contain blank values")
		}
		if !validRequiredDispatchRole(role) {
			return nil, fmt.Errorf("unsupported required_dispatch_roles value %q", role)
		}
		if seen[role] {
			return nil, fmt.Errorf("required_dispatch_roles must not contain duplicate values")
		}
		seen[role] = true
		normalized = append(normalized, role)
	}
	sort.Strings(normalized)
	if len(normalized) == 0 {
		return nil, nil
	}
	return normalized, nil
}

func snapshotStudyExperimentInputs(snapshotRoot string, index int, base string, experiment studyExperiment) (studyExperimentInputs, error) {
	usesLayer := experiment.Config != "" || experiment.Instructions != "" || experiment.Skills != ""
	if usesLayer && experiment.Config == "" {
		return studyExperimentInputs{}, fmt.Errorf("agent layer inputs require config")
	}
	if experiment.Skills != "" && experiment.EntryPrompt == "" {
		return studyExperimentInputs{}, fmt.Errorf("skills require entry_prompt")
	}
	if experiment.Skills == "" && experiment.EntryPrompt != "" {
		return studyExperimentInputs{}, fmt.Errorf("entry_prompt is only valid with skills")
	}
	root := filepath.Join(snapshotRoot, "experiments", fmt.Sprintf("%03d", index))
	inputs := studyExperimentInputs{}
	for _, input := range []struct {
		name, value string
		directory   bool
		set         func(string)
	}{
		{"config", experiment.Config, false, func(path string) { inputs.Config = path }},
		{studyInputInstructions, experiment.Instructions, true, func(path string) { inputs.Instructions = path }},
		{studyInputSkills, experiment.Skills, true, func(path string) { inputs.Skills = path }},
		{studyInputEntryPrompt, experiment.EntryPrompt, false, func(path string) { inputs.EntryPrompt = path }},
	} {
		if input.value == "" {
			continue
		}
		source, err := resolveStudyPath(base, input.value)
		if err != nil {
			return studyExperimentInputs{}, fmt.Errorf("%s: %w", input.name, err)
		}
		destination := filepath.Join(root, input.name)
		if input.directory {
			info, statErr := os.Lstat(source)
			if statErr != nil {
				return studyExperimentInputs{}, fmt.Errorf("%s: %w", input.name, statErr)
			}
			if !info.IsDir() {
				return studyExperimentInputs{}, fmt.Errorf("%s: must be a directory", input.name)
			}
			if err := copyRequiredTree(source, destination); err != nil {
				return studyExperimentInputs{}, fmt.Errorf("%s: %w", input.name, err)
			}
		} else if err := snapshotStudyFile(source, destination); err != nil {
			return studyExperimentInputs{}, fmt.Errorf("%s: %w", input.name, err)
		}
		input.set(destination)
	}
	return inputs, nil
}

func snapshotStudyFile(source, destination string) error {
	data, err := readRegularNonempty(source)
	if err != nil {
		return err
	}
	return writeSnapshotFile(destination, data)
}

func snapshotStudySelection(source, destination string) ([]byte, error) {
	info, err := os.Lstat(source)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxStudySelectionBytes {
		return nil, fmt.Errorf("must be a non-empty JSON file no larger than %d bytes", maxStudySelectionBytes)
	}
	data, err := os.ReadFile(source) // #nosec G304 -- source is a validated declared study input.
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxStudySelectionBytes {
		return nil, fmt.Errorf("must be non-empty and no larger than %d bytes", maxStudySelectionBytes)
	}
	if err := writeSnapshotFile(destination, data); err != nil {
		return nil, err
	}
	return data, nil
}

func writeSnapshotFile(destination string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o600) // #nosec G703 -- destination is beneath the private snapshot root.
}

func validateExperimentInputs(inputs studyExperimentInputs) (map[string]string, error) {
	hashes := map[string]string{}
	for _, input := range []struct {
		name, path string
		directory  bool
	}{
		{"config", inputs.Config, false}, {studyInputInstructions, inputs.Instructions, true},
		{studyInputSkills, inputs.Skills, true}, {studyInputEntryPrompt, inputs.EntryPrompt, false},
	} {
		if input.path == "" {
			continue
		}
		var digest string
		var contents []byte
		var err error
		if input.directory {
			digest, err = treeDigest(input.path)
		} else {
			contents, err = readRegularNonempty(input.path)
			if err == nil {
				digest = digestBytes(contents)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", input.name, err)
		}
		hashes[input.name] = digest
		if input.name == studyInputEntryPrompt {
			if count := bytes.Count(contents, []byte("{{task}}")); count != 1 {
				return nil, fmt.Errorf("entry_prompt must contain exactly one literal {{task}} placeholder")
			}
		}
	}
	return hashes, nil
}

func resolveStudyPath(base, value string) (string, error) {
	if filepath.IsAbs(value) {
		return "", fmt.Errorf("paths must be relative to study.toml")
	}
	path := filepath.Clean(filepath.Join(base, value))
	relative, err := filepath.Rel(base, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes study directory")
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("resolve study directory: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	resolvedRelative, err := filepath.Rel(resolvedBase, resolvedPath)
	if err != nil || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes study directory through a symlink")
	}
	return path, nil
}

func readRegularNonempty(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return nil, fmt.Errorf("must be a non-empty regular file")
	}
	return os.ReadFile(path) // #nosec G304 -- validated explicit study input.
}

func treeDigest(root string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("must be a directory")
	}
	hash := sha256.New()
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("contains non-regular file %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, relative)
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("must not be empty")
	}
	sort.Strings(paths)
	for _, relative := range paths {
		data, err := os.ReadFile(filepath.Join(root, relative)) // #nosec G304 -- relative was collected from WalkDir below the validated root.
		if err != nil {
			return "", err
		}
		hash.Write([]byte(filepath.ToSlash(relative)))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func digestBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
