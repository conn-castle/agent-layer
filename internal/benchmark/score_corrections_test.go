package benchmark

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyScoreCorrectionsReconcilesObsidianDisplayName(t *testing.T) {
	root := t.TempDir()
	resultPath := filepath.Join(root, "attempts", "1", "tasks", obsidianAutoTOCTask, "result.json")
	result := correctionTestResult()
	writeCorrectionReports(t, resultPath, result.EventID,
		[]map[string]string{
			{"name": "[p2p] existing behavior", "status": ctrfStatusPassed},
			{"name": "[f2p] Auto Table of Contents first case", "status": statusFailed},
			{"name": "[f2p] Auto Table of Contents second case", "status": statusFailed},
		},
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
	if corrected.PartialScore != float64(2)/3 || corrected.Reward != 0 {
		t.Fatalf("corrected partial/reward = %v/%v", corrected.PartialScore, corrected.Reward)
	}
}

func TestCorrectStoredScoresWritesVersionedCanonicalResult(t *testing.T) {
	root := t.TempDir()
	resultPath := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "matrices", "matrix", "arms", "arm", "attempts", "1", "tasks", obsidianAutoTOCTask, "result.json")
	result := correctionTestResult()
	writeCorrectionReports(t, resultPath, result.EventID,
		[]map[string]string{
			{"name": "[p2p] existing behavior", "status": ctrfStatusPassed},
			{"name": "[f2p] Auto Table of Contents first case", "status": statusFailed},
			{"name": "[f2p] Auto Table of Contents second case", "status": statusFailed},
		},
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
	t.Parallel()
	count, err := CorrectStoredScores(t.TempDir())
	if err != nil {
		t.Fatalf("CorrectStoredScores: %v", err)
	}
	if count != 0 {
		t.Fatalf("corrected count = %d, want 0", count)
	}
}

func TestReadCanonicalResultRejectsChangedAttemptIdentity(t *testing.T) {
	t.Parallel()
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
		[]map[string]string{
			{"name": "[p2p] existing behavior", "status": "passed"},
			{"name": "[f2p] Auto Table of Contents first case", "status": "failed"},
			{"name": "[f2p] Auto Table of Contents second case", "status": "failed"},
		},
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
