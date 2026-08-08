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

func TestApplyScoreCorrectionsReconcilesObsidianDisplayName(t *testing.T) {
	root := t.TempDir()
	resultPath := filepath.Join(root, "attempts", "1", "tasks", obsidianAutoTOCTask, "result.json")
	result := correctionTestResult()
	writeCorrectionReports(t, resultPath, result.EventID,
		completeCorrectionGradedTests(
			map[string]string{"name": "[f2p] Auto Table of Contents first case", "status": statusFailed},
			map[string]string{"name": "[f2p] Auto Table of Contents second case", "status": statusFailed},
		),
		[]map[string]string{
			{"name": "Auto TOC first case", "suite": "auto-toc.test.ts > Auto TOC", "status": ctrfStatusPassed},
			{"name": "Auto TOC second case", "suite": "auto-toc.test.ts > Auto TOC", "status": statusFailed},
		},
	)

	corrected, err := applyScoreCorrections(result, resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if corrected.F2PPassed != 1 || corrected.F2PTotal != 2 || corrected.F2PScore != .5 {
		t.Fatalf("corrected F2P = %d/%d (%v)", corrected.F2PPassed, corrected.F2PTotal, corrected.F2PScore)
	}
	if corrected.PartialScore != float64(obsidianAutoTOCP2PTotal+1)/float64(obsidianAutoTOCP2PTotal+2) || corrected.Reward != 0 {
		t.Fatalf("corrected partial/reward = %v/%v", corrected.PartialScore, corrected.Reward)
	}
}

func TestCorrectStoredScoresWritesVersionedCanonicalResult(t *testing.T) {
	root := t.TempDir()
	resultPath := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "matrices", "matrix", "arms", "arm", "attempts", "1", "tasks", obsidianAutoTOCTask, "result.json")
	result := correctionTestResult()
	writeCorrectionReports(t, resultPath, result.EventID,
		completeCorrectionGradedTests(
			map[string]string{"name": "[f2p] Auto Table of Contents first case", "status": statusFailed},
			map[string]string{"name": "[f2p] Auto Table of Contents second case", "status": statusFailed},
		),
		[]map[string]string{
			{"name": "Auto TOC first case", "suite": "auto-toc.test.ts > Auto TOC", "status": "passed"},
			{"name": "Auto TOC second case", "suite": "auto-toc.test.ts > Auto TOC", "status": "failed"},
		},
	)
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(resultPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	count, err := CorrectStoredScores(root)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("corrected count = %d, want 1", count)
	}
	data, err := os.ReadFile(canonicalResultPath(resultPath))
	if err != nil {
		t.Fatal(err)
	}
	var record canonicalAttemptResult
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.SchemaVersion != canonicalResultSchema || record.CorrectionID != obsidianCorrectionID ||
		record.SourceResultSHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) || record.Result.F2PPassed != 1 {
		t.Fatalf("canonical result = %#v", record)
	}
}

func TestCorrectStoredScoresTreatsMissingStateAsEmpty(t *testing.T) {
	count, err := CorrectStoredScores(t.TempDir())
	if err != nil {
		t.Fatalf("CorrectStoredScores: %v", err)
	}
	if count != 0 {
		t.Fatalf("corrected count = %d, want 0", count)
	}
}

func TestReadCanonicalResultRejectsChangedAttemptIdentity(t *testing.T) {
	root := t.TempDir()
	resultPath := filepath.Join(root, "result.json")
	source := correctionTestResult()
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	canonical := source
	canonical.Attempt++
	record := canonicalAttemptResult{
		SchemaVersion: canonicalResultSchema, CorrectionID: obsidianCorrectionID,
		SourceResultSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), Result: canonical,
	}
	if err := writeJSON(canonicalResultPath(resultPath), record); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCanonicalResult(resultPath, raw, source); err == nil {
		t.Fatal("canonical result with changed attempt identity was accepted")
	}
}

func TestApplyScoreCorrectionsUsesWorstDuplicateStatus(t *testing.T) {
	root := t.TempDir()
	resultPath := filepath.Join(root, "attempts", "1", "tasks", obsidianAutoTOCTask, "result.json")
	result := correctionTestResult()
	writeCorrectionReports(t, resultPath, result.EventID,
		completeCorrectionGradedTests(
			map[string]string{"name": "[f2p] Auto Table of Contents first case", "status": "failed"},
			map[string]string{"name": "[f2p] Auto Table of Contents second case", "status": "failed"},
		),
		[]map[string]string{
			{"name": "Auto TOC first case", "suite": "auto-toc.test.ts > Auto TOC", "status": ctrfStatusPassed},
			{"name": "Auto TOC first case", "suite": "auto-toc.test.ts > Auto TOC", "status": statusFailed},
			{"name": "unrelated", "suite": "another.test.ts > another", "status": ctrfStatusPassed},
		},
	)

	corrected, err := applyScoreCorrections(result, resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if corrected.F2PPassed != 0 {
		t.Fatalf("corrected passes = %d, want 0", corrected.F2PPassed)
	}
}

func TestApplyScoreCorrectionsRejectsIncompleteP2PReport(t *testing.T) {
	root := t.TempDir()
	resultPath := filepath.Join(root, "attempts", "1", "tasks", obsidianAutoTOCTask, "result.json")
	result := correctionTestResult()
	writeCorrectionReports(t, resultPath, result.EventID,
		[]map[string]string{
			{"name": "[f2p] Auto Table of Contents first case", "status": statusFailed},
			{"name": "[f2p] Auto Table of Contents second case", "status": statusFailed},
		},
		[]map[string]string{
			{"name": "Auto TOC first case", "suite": "auto-toc.test.ts > Auto TOC", "status": ctrfStatusPassed},
			{"name": "Auto TOC second case", "suite": "auto-toc.test.ts > Auto TOC", "status": ctrfStatusPassed},
		},
	)

	if _, err := applyScoreCorrections(result, resultPath); err == nil || !strings.Contains(err.Error(), "pass-to-pass tests") {
		t.Fatalf("incomplete P2P report error = %v", err)
	}
}

func TestReadCampaignResultUsesCanonicalResult(t *testing.T) {
	root := t.TempDir()
	path := armResultPath(root, obsidianAutoTOCTask, 1)
	source := correctionTestResult()
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	corrected := source
	corrected.F2PPassed = 2
	corrected.F2PScore = 1
	record := canonicalAttemptResult{
		SchemaVersion: canonicalResultSchema, CorrectionID: obsidianCorrectionID,
		SourceResultSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), Result: corrected,
	}
	if err := writeJSON(canonicalResultPath(path), record); err != nil {
		t.Fatal(err)
	}
	loaded := loadedBenchmarkPlan{
		Model:  Model{RuntimeIdentifier: source.RuntimeModel},
		Effort: source.ReasoningEffort,
	}
	got, err := readCampaignResult(root, source.Task, source.Attempt, source.TaskChecksum, loaded, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.F2PPassed != corrected.F2PPassed || got.F2PScore != corrected.F2PScore {
		t.Fatalf("campaign result score = %d/%d (%v), want canonical %d/%d (%v)",
			got.F2PPassed, got.F2PTotal, got.F2PScore,
			corrected.F2PPassed, corrected.F2PTotal, corrected.F2PScore)
	}
}

func correctionTestResult() AttemptResult {
	cost, duration := 1.0, 1.0
	now := time.Now().UTC()
	return AttemptResult{
		SchemaVersion: StorageSchemaVersion, EventID: "event", Attempt: 1,
		Task: obsidianAutoTOCTask, Status: statusSuccess,
		F2PPassed: 0, F2PTotal: 2, F2PScore: 0, PartialScore: 1.0 / 3,
		CostUSD: &cost, CostKind: costKindProviderReported, DurationSeconds: &duration,
		TaskChecksum: obsidianAutoTOCChecksum, StartedAt: now, FinishedAt: now,
		Provider: "openai", PublishedModel: "model", RuntimeModel: "runtime",
		ReasoningEffort: "low", ProviderClientVersion: "version", InvocationCount: 1,
	}
}

func completeCorrectionGradedTests(f2pTests ...map[string]string) []map[string]string {
	tests := make([]map[string]string, 0, obsidianAutoTOCP2PTotal+len(f2pTests))
	for index := 0; index < obsidianAutoTOCP2PTotal; index++ {
		tests = append(tests, map[string]string{
			"name": fmt.Sprintf("[p2p] existing behavior %d", index), "status": ctrfStatusPassed,
		})
	}
	return append(tests, f2pTests...)
}

func writeCorrectionReports(t *testing.T, resultPath, eventID string, gradedTests, featureTests []map[string]string) {
	t.Helper()
	verifier := filepath.Join(filepath.Dir(resultPath), "artifacts", eventID, "jobs", eventID, "task", "verifier")
	if err := os.MkdirAll(filepath.Join(verifier, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, tests := range map[string][]map[string]string{
		filepath.Join(verifier, "ctrf.json"):                gradedTests,
		filepath.Join(verifier, "reports", "new_ctrf.json"): featureTests,
	} {
		data, err := json.Marshal(map[string]any{"results": map[string]any{"tests": tests}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
