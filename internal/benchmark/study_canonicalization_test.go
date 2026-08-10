package benchmark

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStudyCanonicalizationReconcilesHistoricalSelectionEvidence(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "attempts", "1", "tasks", obsidianAutoTOCTask, "result.json")
	result := correctionTestResult()
	writeStudyCorrectionReports(t, path, result.EventID,
		completeStudyCorrectionGradedTests(
			map[string]string{"name": "[f2p] Auto Table of Contents first case", "status": statusFailed},
			map[string]string{"name": "[f2p] Auto Table of Contents second case", "status": statusFailed},
		),
		[]map[string]string{
			{"name": "Auto TOC first case", "suite": "auto-toc.test.ts > Auto TOC", "status": ctrfStatusPassed},
			{"name": "Auto TOC second case", "suite": "auto-toc.test.ts > Auto TOC", "status": statusFailed},
		},
	)
	corrected, err := applyScoreCorrections(result, path)
	if err != nil {
		t.Fatal(err)
	}
	if corrected.F2PPassed != 1 || corrected.F2PScore != .5 {
		t.Fatalf("corrected F2P = %d (%v)", corrected.F2PPassed, corrected.F2PScore)
	}
}

func TestStudyReadAutomaticallyCanonicalizesAffectedLegacyMatrixEvidence(t *testing.T) {
	arm, path := writeCanonicalizableEvidence(t, "matrices", true)
	result, err := readStudyResult(path, obsidianAutoTOCTask, 1, obsidianAutoTOCChecksum, "env-1", arm)
	if err != nil {
		t.Fatal(err)
	}
	if result.F2PPassed != 1 || result.F2PScore != .5 {
		t.Fatalf("canonical result = %#v", result)
	}
	if _, err := os.Stat(canonicalResultPath(path)); err != nil {
		t.Fatalf("canonical cache = %v", err)
	}
}

func TestStudyReadPrefersPreexistingValidCanonicalRecordForEligibleLegacyEvidence(t *testing.T) {
	arm, path := writeCanonicalizableEvidence(t, "matrices", true)
	raw := readStoredStudyResult(t, path)
	corrected, err := applyScoreCorrections(raw, path)
	if err != nil {
		t.Fatal(err)
	}
	writeCanonicalStudyResult(t, path, corrected)
	featureReport := filepath.Join(filepath.Dir(path), "artifacts", raw.EventID, "jobs", raw.EventID, "task", "verifier", "reports", "new_ctrf.json")
	if err := os.WriteFile(featureReport, []byte("not JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := readStudyResult(path, obsidianAutoTOCTask, 1, obsidianAutoTOCChecksum, "env-1", arm)
	if err != nil {
		t.Fatal(err)
	}
	if second.F2PScore != corrected.F2PScore {
		t.Fatalf("cached canonical score = %v, want %v", second.F2PScore, corrected.F2PScore)
	}
}

func TestStudyReadIgnoresPreexistingCanonicalRecordOutsideEligibleHistoricalEvidence(t *testing.T) {
	for _, test := range []struct {
		name, root, task, checksum string
	}{
		{name: "native study", root: "studies", task: obsidianAutoTOCTask, checksum: obsidianAutoTOCChecksum},
		{name: "private campaign", root: "campaigns", task: obsidianAutoTOCTask, checksum: obsidianAutoTOCChecksum},
		{name: "unaffected task", root: "matrices", task: "unaffected-task", checksum: obsidianAutoTOCChecksum},
		{name: "unaffected checksum", root: "matrices", task: obsidianAutoTOCTask, checksum: "unaffected-checksum"},
	} {
		t.Run(test.name, func(t *testing.T) {
			arm, path := writeStudyEvidence(t, test.root, test.task, test.checksum, false)
			raw := readStoredStudyResult(t, path)
			canonical := raw
			canonical.F2PPassed, canonical.F2PScore = 1, .5
			writeCanonicalStudyResult(t, path, canonical)

			result, err := readStudyResult(path, test.task, 1, test.checksum, "env-1", arm)
			if err != nil {
				t.Fatal(err)
			}
			if result.F2PScore != 0 || result.F2PPassed != 0 {
				t.Fatalf("preexisting canonical result was consumed: %#v", result)
			}
		})
	}
}

func TestStudyCanonicalizationFailsLoudlyWhenAffectedEvidenceIsIncomplete(t *testing.T) {
	arm, path := writeCanonicalizableEvidence(t, "matrices", false)
	_, err := readStudyResult(path, obsidianAutoTOCTask, 1, obsidianAutoTOCChecksum, "env-1", arm)
	if err == nil || !strings.Contains(err.Error(), "canonical verifier report") {
		t.Fatalf("incomplete canonical evidence error = %v", err)
	}
}

func TestHistoricalCanonicalMatrixReusePromotesCorrectedResultIntoStudyReport(t *testing.T) {
	repository := t.TempDir()
	selectionID := "selection"
	tasks := []benchmarkPlanTask{{ID: obsidianAutoTOCTask, RepetitionsPerArm: 1}}
	checksums := map[string]string{obsidianAutoTOCTask: obsidianAutoTOCChecksum}
	environments := map[string]string{obsidianAutoTOCTask: "env-1"}

	source, _ := writeStudyEvidenceAt(t, repository, "matrices", obsidianAutoTOCTask, obsidianAutoTOCChecksum, true)
	if err := writeJSON(filepath.Join(source.StateDir, "manifest.json"), matrixArmManifest{
		SchemaVersion: matrixManifestSchema, SelectionID: selectionID, Mode: source.Mode,
		Model: source.Loaded.Model.PublishedIdentifier, Reasoning: source.Loaded.Effort,
		TaskChecksums: checksums, Repetitions: repetitionsForTasks(tasks),
	}); err != nil {
		t.Fatal(err)
	}

	destination := source
	destination.ID = "recovered-study-arm"
	destination.Label = "Recovered study"
	destination.StateDir = filepath.Join(repository, ".agent-layer", "state", "benchmarks", "deepswe", "studies", "new-study", "arms", destination.ID)
	if err := ensureStudyArmManifest(selectionID, tasks, checksums, &destination); err != nil {
		t.Fatal(err)
	}
	if err := reuseMatchingMatrixEvidence(repository, selectionID, tasks, checksums, environments, &destination); err != nil {
		t.Fatal(err)
	}

	destinationPath := armResultPath(destination.StateDir, obsidianAutoTOCTask, 1)
	promoted := readStoredStudyResult(t, destinationPath)
	if promoted.F2PPassed != 1 || promoted.F2PScore != .5 {
		t.Fatalf("promoted study result retained obsolete score: %#v", promoted)
	}
	if _, err := os.Stat(canonicalResultPath(destinationPath)); !os.IsNotExist(err) {
		t.Fatalf("study promotion copied legacy canonical sidecar: %v", err)
	}
	var receipt pierExecutionReceipt
	if err := readStudyJSON(filepath.Join(filepath.Dir(destinationPath), "artifacts", promoted.EventID, "execution-receipt.json"), &receipt); err != nil || receipt.EventID != promoted.EventID {
		t.Fatalf("promotion did not preserve immutable execution provenance: %#v, %v", receipt, err)
	}

	// A valid-looking sidecar cannot change native study evidence. This proves
	// the report reads the corrected result promoted above, not a legacy cache.
	poisoned := promoted
	poisoned.F2PPassed, poisoned.F2PScore, poisoned.PartialScore, poisoned.Reward = 0, 0, 0, 0
	writeCanonicalStudyResult(t, destinationPath, poisoned)

	selection := matrixSelectionFixture()
	selection.Tasks = selection.Tasks[:1]
	selection.Tasks[0].ID = obsidianAutoTOCTask
	selection.Tasks[0].Weight = 1
	selection.Tasks[0].Calibration.Intercept, selection.Tasks[0].Calibration.Slope = 0, 1
	study := preparedStudy{selection: selection, selectionID: selectionID, studyID: strings.Repeat("s", 64), experiments: []preparedStudyExperiment{{studyExperiment: studyExperiment{Name: "Recovered"}, model: destination.Loaded.Model, effort: destination.Loaded.Effort, identity: destination.ID}}}
	report, _, _, err := buildStudyReport(study, matrixPreparation{selection: selection, selectionID: selectionID, stateDir: filepath.Dir(filepath.Dir(filepath.Dir(destination.StateDir))), tasks: tasks, checksums: checksums, environments: environments, arms: []matrixArm{destination}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Experiments[0].Score == nil || *report.Experiments[0].Score != .5 || report.Experiments[0].Tasks[0].F2PMean == nil || *report.Experiments[0].Tasks[0].F2PMean != .5 {
		t.Fatalf("study report trusted legacy sidecar or obsolete source score: %#v", report.Experiments[0])
	}
}

func writeCanonicalizableEvidence(t *testing.T, evidenceRoot string, reports bool) (matrixArm, string) {
	return writeStudyEvidence(t, evidenceRoot, obsidianAutoTOCTask, obsidianAutoTOCChecksum, reports)
}

func writeStudyEvidence(t *testing.T, evidenceRoot, task, checksum string, reports bool) (matrixArm, string) {
	t.Helper()
	return writeStudyEvidenceAt(t, t.TempDir(), evidenceRoot, task, checksum, reports)
}

func writeStudyEvidenceAt(t *testing.T, root, evidenceRoot, task, checksum string, reports bool) (matrixArm, string) {
	t.Helper()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	arm := matrixArm{Mode: ArmBaseline, StateDir: filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", evidenceRoot, "selection", "arms", "arm"), Loaded: loadedBenchmarkPlan{Model: model, Effort: effort}}
	path := armResultPath(arm.StateDir, task, 1)
	result := correctionTestResult()
	result.Task, result.TaskChecksum = task, checksum
	result.EventID = strings.Repeat("e", 32)
	result.EnvironmentIdentity = "env-1"
	result.Provider = model.Adapter
	result.PublishedModel = model.PublishedIdentifier
	result.RuntimeModel = model.RuntimeIdentifier
	result.ReasoningEffort = effort
	if err := writeJSON(path, result); err != nil {
		t.Fatal(err)
	}
	receipt := pierExecutionReceipt{SchemaVersion: pierExecutionReceiptSchema, EventID: result.EventID, Attempt: 1, Task: result.Task, TaskChecksum: result.TaskChecksum, EnvironmentIdentity: result.EnvironmentIdentity, Arm: arm.Mode, RuntimeModel: model.RuntimeIdentifier, ReasoningEffort: effort, CompletedAt: time.Now().UTC(), Succeeded: true, CleanupSucceeded: true}
	if err := writeJSON(filepath.Join(filepath.Dir(path), "artifacts", result.EventID, "execution-receipt.json"), receipt); err != nil {
		t.Fatal(err)
	}
	if reports {
		writeStudyCorrectionReports(t, path, result.EventID,
			completeStudyCorrectionGradedTests(
				map[string]string{"name": "[f2p] Auto Table of Contents first case", "status": statusFailed},
				map[string]string{"name": "[f2p] Auto Table of Contents second case", "status": statusFailed},
			),
			[]map[string]string{
				{"name": "Auto TOC first case", "suite": "auto-toc.test.ts > Auto TOC", "status": ctrfStatusPassed},
				{"name": "Auto TOC second case", "suite": "auto-toc.test.ts > Auto TOC", "status": statusFailed},
			},
		)
	}
	return arm, path
}

func readStoredStudyResult(t *testing.T, path string) AttemptResult {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- path is a test-owned temporary file.
	if err != nil {
		t.Fatal(err)
	}
	var result AttemptResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func writeCanonicalStudyResult(t *testing.T, resultPath string, corrected AttemptResult) {
	t.Helper()
	data, err := os.ReadFile(resultPath) // #nosec G304 -- resultPath is a test-owned temporary file.
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(canonicalResultPath(resultPath), canonicalAttemptResult{
		SchemaVersion: canonicalResultSchema, CorrectionID: obsidianCorrectionID,
		SourceResultSHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Result: corrected,
	}); err != nil {
		t.Fatal(err)
	}
}

func correctionTestResult() AttemptResult {
	cost, duration := 1.0, 1.0
	now := time.Now().UTC()
	return AttemptResult{SchemaVersion: StorageSchemaVersion, EventID: "event", Attempt: 1, Task: obsidianAutoTOCTask, Status: statusSuccess, F2PTotal: 2, PartialScore: 1.0 / 3, CostUSD: &cost, CostKind: costKindProviderReported, DurationSeconds: &duration, TaskChecksum: obsidianAutoTOCChecksum, StartedAt: now, FinishedAt: now, Provider: "openai", PublishedModel: "model", RuntimeModel: "runtime", ReasoningEffort: "low", ProviderClientVersion: "version", InvocationCount: 1}
}

func completeStudyCorrectionGradedTests(f2pTests ...map[string]string) []map[string]string {
	tests := make([]map[string]string, 0, obsidianAutoTOCP2PTotal+len(f2pTests))
	for index := 0; index < obsidianAutoTOCP2PTotal; index++ {
		tests = append(tests, map[string]string{"name": fmt.Sprintf("[p2p] existing behavior %d", index), "status": ctrfStatusPassed})
	}
	return append(tests, f2pTests...)
}

func writeStudyCorrectionReports(t *testing.T, resultPath, eventID string, gradedTests, featureTests []map[string]string) {
	t.Helper()
	verifier := filepath.Join(filepath.Dir(resultPath), "artifacts", eventID, "jobs", eventID, "task", "verifier")
	if err := os.MkdirAll(filepath.Join(verifier, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, tests := range map[string][]map[string]string{filepath.Join(verifier, "ctrf.json"): gradedTests, filepath.Join(verifier, "reports", "new_ctrf.json"): featureTests} {
		data, err := json.Marshal(map[string]any{"results": map[string]any{"tests": tests}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
