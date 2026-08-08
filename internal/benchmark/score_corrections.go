package benchmark

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	obsidianAutoTOCTask     = "obsidian-linter-auto-table-of-contents"
	obsidianAutoTOCChecksum = "d6fdefea1eb2d0e1c30ac507c1f738957e45ce49029bfc3601183d4294a5fbaf"
	obsidianAutoTOCPrefix   = "Auto Table of Contents "
	canonicalResultSchema   = "deepswe-canonical-result-v1"
	obsidianCorrectionID    = "deepswe-v1.1-obsidian-auto-toc-display-name-v1"
	ctrfStatusPassed        = "passed"
	ctrfStatusSkipped       = "skipped"
)

type canonicalAttemptResult struct {
	SchemaVersion      string        `json:"schema_version"`
	CorrectionID       string        `json:"correction_id"`
	SourceResultSHA256 string        `json:"source_result_sha256"`
	Result             AttemptResult `json:"result"`
}

type ctrfReport struct {
	Results struct {
		Tests []struct {
			Name   string `json:"name"`
			Suite  string `json:"suite"`
			Status string `json:"status"`
		} `json:"tests"`
	} `json:"results"`
}

// applyScoreCorrections returns the canonical score used by benchmark analysis.
// The immutable upstream result and verifier artifacts remain unchanged.
func applyScoreCorrections(result AttemptResult, resultPath string) (AttemptResult, error) {
	if result.Task != obsidianAutoTOCTask || result.TaskChecksum != obsidianAutoTOCChecksum {
		return result, nil
	}
	artifactRoot := filepath.Join(filepath.Dir(resultPath), "artifacts", result.EventID)
	gradedPath, err := uniqueArtifactPath(artifactRoot, filepath.Join("verifier", "ctrf.json"))
	if err != nil {
		return AttemptResult{}, fmt.Errorf("locate canonical verifier report for score correction: %w", err)
	}
	featurePath, err := uniqueArtifactPath(artifactRoot, filepath.Join("verifier", "reports", "new_ctrf.json"))
	if err != nil {
		return AttemptResult{}, fmt.Errorf("locate feature report for score correction: %w", err)
	}
	graded, err := readCTRFReport(gradedPath)
	if err != nil {
		return AttemptResult{}, err
	}
	feature, err := readCTRFReport(featurePath)
	if err != nil {
		return AttemptResult{}, err
	}

	expected := make(map[string]struct{})
	p2pTotal, p2pPassed := 0, 0
	for _, test := range graded.Results.Tests {
		switch {
		case strings.HasPrefix(test.Name, "[f2p] "+obsidianAutoTOCPrefix):
			expected[strings.TrimPrefix(test.Name, "[f2p] "+obsidianAutoTOCPrefix)] = struct{}{}
		case strings.HasPrefix(test.Name, "[p2p] "):
			p2pTotal++
			if test.Status == ctrfStatusPassed {
				p2pPassed++
			}
		}
	}
	if len(expected) != result.F2PTotal {
		return AttemptResult{}, fmt.Errorf(
			"score correction expected %d feature identifiers, found %d",
			result.F2PTotal, len(expected),
		)
	}

	statuses := make(map[string]string, len(expected))
	for _, test := range feature.Results.Tests {
		caseName, ok := obsidianFeatureCaseName(test.Name, test.Suite)
		if !ok {
			continue
		}
		if _, scored := expected[caseName]; !scored {
			continue
		}
		if previous := statuses[caseName]; previous == statusFailed || previous == ctrfStatusSkipped {
			continue
		}
		statuses[caseName] = test.Status
	}

	passed := 0
	for caseName := range expected {
		if statuses[caseName] == ctrfStatusPassed {
			passed++
		}
	}
	result.F2PPassed = passed
	result.F2PScore = float64(passed) / float64(result.F2PTotal)
	if total := result.F2PTotal + p2pTotal; total > 0 {
		result.PartialScore = float64(passed+p2pPassed) / float64(total)
	}
	if passed == result.F2PTotal && p2pPassed == p2pTotal {
		result.Reward = 1
	} else {
		result.Reward = 0
	}
	if err := result.Validate(); err != nil {
		return AttemptResult{}, fmt.Errorf("validate corrected benchmark result: %w", err)
	}
	return result, nil
}

// CorrectStoredScores writes reproducible derived results for every affected
// historical run while preserving the immutable upstream result files.
func CorrectStoredScores(repoRoot string) (int, error) {
	stateRoot := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe")
	var resultPaths []string
	err := filepath.WalkDir(stateRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == stateRoot && os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if !entry.IsDir() && entry.Name() == "result.json" &&
			filepath.Base(filepath.Dir(path)) == obsidianAutoTOCTask {
			resultPaths = append(resultPaths, path)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("find stored benchmark results: %w", err)
	}
	written := 0
	for _, resultPath := range resultPaths {
		raw, err := os.ReadFile(resultPath) // #nosec G304 -- path was discovered below benchmark state.
		if err != nil {
			return written, err
		}
		var result AttemptResult
		if err := json.Unmarshal(raw, &result); err != nil || result.Validate() != nil {
			return written, fmt.Errorf("invalid stored benchmark result %s", resultPath)
		}
		if result.TaskChecksum != obsidianAutoTOCChecksum {
			continue
		}
		corrected, err := applyScoreCorrections(result, resultPath)
		if err != nil {
			return written, fmt.Errorf("correct stored benchmark result %s: %w", resultPath, err)
		}
		record := canonicalAttemptResult{
			SchemaVersion: canonicalResultSchema, CorrectionID: obsidianCorrectionID,
			SourceResultSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), Result: corrected,
		}
		if err := writeJSON(canonicalResultPath(resultPath), record); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func canonicalResultPath(resultPath string) string {
	return filepath.Join(filepath.Dir(resultPath), "canonical-result.json")
}

func readCanonicalResult(resultPath string, raw []byte, source AttemptResult) (AttemptResult, bool, error) {
	data, err := os.ReadFile(canonicalResultPath(resultPath)) // #nosec G304 -- derived from validated benchmark state path.
	if os.IsNotExist(err) {
		return AttemptResult{}, false, nil
	}
	if err != nil {
		return AttemptResult{}, false, err
	}
	var record canonicalAttemptResult
	if err := json.Unmarshal(data, &record); err != nil ||
		record.SchemaVersion != canonicalResultSchema ||
		record.CorrectionID != obsidianCorrectionID ||
		record.SourceResultSHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) ||
		record.Result.Validate() != nil ||
		record.Result.Status != source.Status ||
		record.Result.Task != source.Task ||
		record.Result.Attempt != source.Attempt ||
		record.Result.TaskChecksum != source.TaskChecksum {
		return AttemptResult{}, false, fmt.Errorf("invalid canonical benchmark result %s", canonicalResultPath(resultPath))
	}
	return record.Result, true, nil
}

func obsidianFeatureCaseName(name, suite string) (string, bool) {
	const separator = " > "
	index := strings.LastIndex(suite, separator)
	if index < 0 {
		return "", false
	}
	displayName := strings.TrimSpace(suite[index+len(separator):])
	if displayName == "" || !strings.HasPrefix(name, displayName+" ") {
		return "", false
	}
	return strings.TrimPrefix(name, displayName+" "), true
}

func readCTRFReport(path string) (ctrfReport, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is discovered below immutable benchmark artifacts.
	if err != nil {
		return ctrfReport{}, fmt.Errorf("read verifier report: %w", err)
	}
	var report ctrfReport
	if err := json.Unmarshal(data, &report); err != nil {
		return ctrfReport{}, fmt.Errorf("decode verifier report: %w", err)
	}
	return report, nil
}

func uniqueArtifactPath(root, suffix string) (string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(path, suffix) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("found %d files ending in %s under %s", len(matches), suffix, root)
	}
	return matches[0], nil
}
