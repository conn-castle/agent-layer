package benchmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	verifierOutcomeTestTimeout = "test_timeout"
	verifierTimeoutException   = "VerifierTimeoutError"
)

type pierExceptionDetails struct {
	Type      string `json:"exception_type"`
	Traceback string `json:"exception_traceback"`
}

// terminalVerifierTestTimeout recognizes only timeouts that Pier recorded
// while an invoked verifier test script was executing. VerifierTimeoutError
// alone is deliberately insufficient because Pier applies the same timeout to
// verifier environment startup, artifact upload, test execution, and grading.
func terminalVerifierTestTimeout(stage string, result pierTaskResult) (bool, error) {
	var exception pierExceptionDetails
	if err := json.Unmarshal(result.ExceptionInfo, &exception); err != nil {
		return false, fmt.Errorf("decode Pier verifier exception: %w", err)
	}
	if exception.Type != verifierTimeoutException {
		return false, nil
	}
	if result.VerifierExecution == nil || result.VerifierExecution.StartedAt.IsZero() || result.VerifierExecution.FinishedAt.IsZero() {
		return false, nil
	}
	// Pier 0.3.0 creates test-stdout.txt as the verifier script's stdout
	// redirection target. The traceback boundary proves the timeout occurred in
	// that environment exec rather than during environment startup or upload.
	if !strings.Contains(exception.Traceback, "pier/verifier/verifier.py") ||
		!strings.Contains(exception.Traceback, "await self._environment.exec") {
		return false, nil
	}
	stdoutFiles := 0
	err := filepath.WalkDir(filepath.Join(stage, "jobs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || entry.Name() != verifierTestStdoutFile || filepath.Base(filepath.Dir(path)) != executionPhaseVerifier {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > 0 {
			stdoutFiles++
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("inspect verifier test-timeout evidence: %w", err)
	}
	return stdoutFiles == 1, nil
}

// internalVerifierTestTimeout recognizes a test framework's own terminal
// timeout after Pier completed the verifier process normally. Go emits the
// timeout as a structured test2json output event, so Pier receives an ordinary
// verifier result unless the host inspects the preserved raw suite log.
func internalVerifierTestTimeout(stage string) (bool, error) {
	jobsRoot, err := os.OpenRoot(filepath.Join(stage, "jobs"))
	if err != nil {
		return false, fmt.Errorf("open verifier timeout diagnostics root: %w", err)
	}
	defer func() { _ = jobsRoot.Close() }()
	timedOut := false
	err = fs.WalkDir(jobsRoot.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if timedOut || entry.IsDir() ||
			(entry.Name() != verifierRunLogFile && entry.Name() != verifierTestStdoutFile) ||
			filepath.Base(filepath.Dir(path)) != executionPhaseVerifier {
			return nil
		}
		data, err := jobsRoot.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range bytes.Split(data, []byte{'\n'}) {
			if !bytes.Contains(line, []byte("panic: test timed out after ")) {
				continue
			}
			var event struct {
				Action string `json:"Action"`
				Output string `json:"Output"`
			}
			if json.Unmarshal(line, &event) == nil && event.Action == "output" &&
				strings.HasPrefix(event.Output, "panic: test timed out after ") {
				timedOut = true
				break
			}
		}
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("inspect verifier internal test-timeout evidence: %w", err)
	}
	return timedOut, nil
}

func pinnedTaskF2PTotal(request ExecutionRequest) (int, error) {
	taskRoot := filepath.Join(
		request.RepoRoot, ".agent-layer", "state", "benchmarks", "deepswe",
		"checkouts", DeepSWECommit, "tasks", request.Task,
	)
	checksum, err := TaskTreeChecksum(taskRoot)
	if err != nil {
		return 0, fmt.Errorf("verify pinned task before scoring verifier timeout: %w", err)
	}
	if checksum != request.TaskChecksum {
		return 0, fmt.Errorf("pinned task checksum %q does not match timeout evidence checksum %q", checksum, request.TaskChecksum)
	}
	data, err := os.ReadFile(filepath.Join(taskRoot, "tests", "config.json")) // #nosec G304 -- task name and checkout identity are validated benchmark inputs.
	if err != nil {
		return 0, fmt.Errorf("read pinned verifier test contract: %w", err)
	}
	var config struct {
		F2PNodeIDs []string `json:"f2p_node_ids"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return 0, fmt.Errorf("decode pinned verifier test contract: %w", err)
	}
	unique := map[string]bool{}
	for _, nodeID := range config.F2PNodeIDs {
		if nodeID = strings.TrimSpace(nodeID); nodeID != "" {
			unique[nodeID] = true
		}
	}
	if len(unique) == 0 {
		return 0, fmt.Errorf("pinned verifier test contract has no f2p tests")
	}
	return len(unique), nil
}

func normalizeTerminalVerifierTestTimeout(stage string, request ExecutionRequest) (AttemptResult, bool, error) {
	raw, err := readPierTaskResult(stage, request)
	if err != nil {
		return AttemptResult{}, false, nil //nolint:nilerr // no readable result means this optional classifier has no terminal evidence.
	}
	terminal, err := terminalVerifierTestTimeout(stage, raw)
	if err != nil || !terminal {
		return AttemptResult{}, terminal, err
	}
	result, err := normalizePier(stage, request)
	if err != nil {
		return AttemptResult{}, true, err
	}
	if result.VerifierOutcome != verifierOutcomeTestTimeout {
		return AttemptResult{}, true, fmt.Errorf("terminal verifier timeout normalization omitted its outcome")
	}
	return result, true, nil
}
