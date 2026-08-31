package benchmark

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const verifierExecTimeoutTraceback = `Traceback:
  File "[PATH]/pier/trial/trial.py", line 373, in _verify_once
    return await self._verify_with_separate_environment(
  File "[PATH]/pier/verifier/verifier.py", line 192, in verify
    await self._environment.exec(
  File "[PATH]/pier/environments/docker/docker.py", line 511, in _run_docker_compose_command
    stdout_bytes, stderr_bytes = await process.communicate()
`

func terminalVerifierTimeoutFixture(t *testing.T) (ExecutionRequest, string) {
	t.Helper()
	repoRoot := t.TempDir()
	taskRoot := filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "checkouts", DeepSWECommit, "tasks", "example-task")
	if err := os.MkdirAll(filepath.Join(taskRoot, "tests"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskRoot, "instruction.md"), []byte("Implement the task.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := `{"f2p_node_ids":["one","two"," ","two","three"],"p2p_node_ids":[]}`
	if err := os.WriteFile(filepath.Join(taskRoot, "tests", "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	checksum, err := TaskTreeChecksum(taskRoot)
	if err != nil {
		t.Fatal(err)
	}
	model, effort, err := ParseModelSelection(modelGrok45 + ":low")
	if err != nil {
		t.Fatal(err)
	}
	request := ExecutionRequest{
		RepoRoot: repoRoot, EvidenceDir: t.TempDir(), EventID: "terminal-timeout-event",
		Attempt: 1, Task: "example-task", Model: model, Effort: effort, Arm: ArmBaseline,
		TaskChecksum: checksum, EnvironmentIdentity: strings.Repeat("e", 64), executionCheckpointed: true,
	}
	stageRoot := filepath.Join(repoRoot, ".agent-layer", "tmp")
	if err := os.MkdirAll(stageRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	stage, err := os.MkdirTemp(stageRoot, "benchmark-"+request.EventID+"-")
	if err != nil {
		t.Fatal(err)
	}
	if err := copyRequiredTree(retainedGrokStageFixture(t, request), stage); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(stage, "jobs", "one", "result.json")
	var result map[string]any
	if err := readStudyJSON(resultPath, &result); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Minute)
	result["verifier_result"] = nil
	result["exception_info"] = map[string]any{
		"exception_type": "VerifierTimeoutError", "exception_message": "timed out", "exception_traceback": verifierExecTimeoutTraceback,
	}
	result["verifier"] = map[string]any{"started_at": started, "finished_at": started.Add(30 * time.Second)}
	if err := writeJSON(resultPath, result); err != nil {
		t.Fatal(err)
	}
	stdout := filepath.Join(stage, "jobs", "one", "verifier", "test-stdout.txt")
	if err := os.WriteFile(stdout, []byte("running candidate tests\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePierExecutionCheckpoint(request, stage); err != nil {
		t.Fatal(err)
	}
	if err := markPierProviderCompleted(request, started.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := sanitizePierArtifacts(request, stage); err != nil {
		t.Fatal(err)
	}
	return request, stage
}

func TestTerminalVerifierTestTimeoutRequiresExecutionBoundaryAndOutput(t *testing.T) {
	request, stage := terminalVerifierTimeoutFixture(t)
	raw, err := readPierTaskResult(stage, request)
	if err != nil {
		t.Fatal(err)
	}
	if terminal, err := terminalVerifierTestTimeout(stage, raw); err != nil || !terminal {
		t.Fatalf("terminal timeout = %t, %v", terminal, err)
	}
	stdout := filepath.Join(stage, "jobs", "one", "verifier", "test-stdout.txt")
	if err := os.WriteFile(stdout, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if terminal, err := terminalVerifierTestTimeout(stage, raw); err != nil || terminal {
		t.Fatalf("empty test output terminal timeout = %t, %v", terminal, err)
	}
	if err := os.WriteFile(stdout, []byte("running\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw.ExceptionInfo = []byte(`{"exception_type":"VerifierTimeoutError","exception_traceback":"await env.start(force_build=False)"}`)
	if terminal, err := terminalVerifierTestTimeout(stage, raw); err != nil || terminal {
		t.Fatalf("environment-start timeout = %t, %v", terminal, err)
	}
	raw.ExceptionInfo = []byte(`{"exception_type":"VerifierTimeoutError","exception_traceback":"pier/verifier/verifier.py: await self._environment.download_dir(output_dir)"}`)
	if terminal, err := terminalVerifierTestTimeout(stage, raw); err != nil || terminal {
		t.Fatalf("post-execution download timeout = %t, %v", terminal, err)
	}
}

func TestNormalizeTerminalVerifierTestTimeoutAsExplicitZeroScore(t *testing.T) {
	request, stage := terminalVerifierTimeoutFixture(t)
	result, err := normalizePier(stage, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != statusSuccess || result.VerifierOutcome != verifierOutcomeTestTimeout ||
		result.F2PTotal != 3 || result.F2PPassed != 0 || result.F2PScore != 0 || result.Reward != 0 ||
		result.CostUSD == nil || *result.CostUSD != .25 {
		t.Fatalf("terminal timeout result = %#v", result)
	}
}

func TestNormalizeInternalGoTestTimeoutAsExplicitZeroScore(t *testing.T) {
	request, stage := terminalVerifierTimeoutFixture(t)
	resultPath := filepath.Join(stage, "jobs", "one", "result.json")
	var result map[string]any
	if err := readStudyJSON(resultPath, &result); err != nil {
		t.Fatal(err)
	}
	result["exception_info"] = nil
	result["verifier_result"] = map[string]any{"rewards": map[string]any{
		"reward": 0, "f2p_total": 3, "f2p_passed": 1, "f2p": 1.0 / 3.0, "partial": 0.25,
	}}
	if err := writeJSON(resultPath, result); err != nil {
		t.Fatal(err)
	}
	runLog := filepath.Join(stage, "jobs", "one", "verifier", "run.log")
	line := `{"Time":"2026-08-30T04:10:40Z","Action":"output","Package":"example","Test":"TestHangs","Output":"panic: test timed out after 5m0s\n"}` + "\n"
	if err := os.WriteFile(runLog, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}

	normalized, err := normalizePier(stage, request)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Status != statusSuccess || normalized.VerifierOutcome != verifierOutcomeTestTimeout ||
		normalized.F2PTotal != 3 || normalized.F2PPassed != 0 || normalized.F2PScore != 0 ||
		normalized.PartialScore != 0 || normalized.Reward != 0 {
		t.Fatalf("internal timeout result = %#v", normalized)
	}
}

func TestInternalVerifierTimeoutRejectsUnstructuredTimeoutText(t *testing.T) {
	request, stage := terminalVerifierTimeoutFixture(t)
	runLog := filepath.Join(stage, "jobs", "one", "verifier", "run.log")
	if err := os.WriteFile(runLog, []byte("panic: test timed out after 5m0s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if timedOut, err := internalVerifierTestTimeout(stage); err != nil || timedOut {
		t.Fatalf("unstructured timeout = %t, %v", timedOut, err)
	}
	_ = request
}

func TestInternalVerifierTimeoutAcceptsStructuredLineLargerThanScannerLimit(t *testing.T) {
	_, stage := terminalVerifierTimeoutFixture(t)
	runLog := filepath.Join(stage, "jobs", "one", "verifier", "run.log")
	line := `{"Action":"output","Output":"` + strings.Repeat("x", 70*1024) + `"}` + "\n" +
		`{"Action":"output","Output":"panic: test timed out after 5m0s\n"}` + "\n"
	if err := os.WriteFile(runLog, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if timedOut, err := internalVerifierTestTimeout(stage); err != nil || !timedOut {
		t.Fatalf("large structured timeout log = %t, %v", timedOut, err)
	}
}

func TestFinalizeTerminalVerifierTestTimeoutIgnoresExpectedPierExitFailure(t *testing.T) {
	request, stage := terminalVerifierTimeoutFixture(t)
	result, durable, err := finalizePierExecution(request, stage, errors.New("pier exited with failed trial"), nil)
	if err != nil || !durable || result.VerifierOutcome != verifierOutcomeTestTimeout {
		t.Fatalf("finalize terminal timeout = %#v durable=%t err=%v", result, durable, err)
	}
	destination, err := artifactDestination(request)
	if err != nil {
		t.Fatal(err)
	}
	var receipt pierExecutionReceipt
	if err := readStudyJSON(filepath.Join(destination, "execution-receipt.json"), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Succeeded || !receipt.CleanupSucceeded {
		t.Fatalf("terminal timeout receipt = %#v", receipt)
	}
	if _, found, err := matchingPierExecutionCheckpoint(request); err != nil || found {
		t.Fatalf("terminal timeout checkpoint remained: found=%t err=%v", found, err)
	}
}

func TestRetainedTerminalVerifierTestTimeoutCanonicalizesWithoutReplay(t *testing.T) {
	request, stage := terminalVerifierTimeoutFixture(t)
	installDockerCleanupStub(t, func(context.Context, ...string) ([]byte, error) { return nil, nil })
	checkpoint, found, err := matchingPierExecutionCheckpoint(request)
	if err != nil || !found {
		t.Fatalf("checkpoint = %#v found=%t err=%v", checkpoint, found, err)
	}
	result, err := (PierExecutor{}).replayVerifier(context.Background(), request, checkpoint)
	if err != nil || result.VerifierOutcome != verifierOutcomeTestTimeout {
		t.Fatalf("canonicalized timeout = %#v err=%v", result, err)
	}
	if _, err := os.Stat(stage); err != nil {
		t.Fatalf("retained diagnostic stage was removed: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(request.RepoRoot, ".agent-layer", "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "benchmark-verifier-replay-") {
			t.Fatalf("terminal timeout started verifier replay: %s", entry.Name())
		}
	}
}

func TestVerifierReplayTerminalTestTimeoutFinalizesInsteadOfCheckpointingAgain(t *testing.T) {
	request, providerStage := terminalVerifierTimeoutFixture(t)
	patch, agentDir, original, err := retainedProviderEvidence(providerStage, request)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, found, err := matchingPierExecutionCheckpoint(request)
	if err != nil || !found {
		t.Fatalf("checkpoint = %#v found=%t err=%v", checkpoint, found, err)
	}
	replayStage := writePierStage(t, request.TaskChecksum, .5, 0)
	resultPath := filepath.Join(replayStage, "jobs", "one", "result.json")
	var replay map[string]any
	if err := readStudyJSON(resultPath, &replay); err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Add(-time.Minute)
	replay["verifier_result"] = nil
	replay["exception_info"] = map[string]any{
		"exception_type": "VerifierTimeoutError", "exception_traceback": verifierExecTimeoutTraceback,
	}
	replay["verifier"] = map[string]any{"started_at": started, "finished_at": started.Add(30 * time.Second)}
	if err := writeJSON(resultPath, replay); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replayStage, "jobs", "one", "verifier", "test-stdout.txt"), []byte("candidate tests running\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := finalizeVerifierReplayEvidence(replayStage, providerStage, agentDir, patch, original, checkpoint, request)
	if err != nil || result.VerifierOutcome != verifierOutcomeTestTimeout {
		t.Fatalf("terminal replay result = %#v err=%v", result, err)
	}
	destination, err := artifactDestination(request)
	if err != nil {
		t.Fatal(err)
	}
	var receipt pierExecutionReceipt
	if err := readStudyJSON(filepath.Join(destination, "execution-receipt.json"), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.VerifierReplayed || !receipt.Succeeded {
		t.Fatalf("terminal replay receipt = %#v", receipt)
	}
}

func TestCanonicalizedTerminalTimeoutIsSkippedAndNextCellRuns(t *testing.T) {
	request, _ := terminalVerifierTimeoutFixture(t)
	installDockerCleanupStub(t, func(context.Context, ...string) ([]byte, error) { return nil, nil })
	checkpoint, found, err := matchingPierExecutionCheckpoint(request)
	if err != nil || !found {
		t.Fatalf("checkpoint = %#v found=%t err=%v", checkpoint, found, err)
	}
	result, err := (PierExecutor{}).replayVerifier(context.Background(), request, checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	result.InvocationWorkers = 1
	if err := writeJSON(armResultPath(request.EvidenceDir, request.Task, request.Attempt), result); err != nil {
		t.Fatal(err)
	}
	tasks := []benchmarkPlanTask{{ID: request.Task, RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}}
	arm := matrixArmFixture(filepath.Dir(request.EvidenceDir), "resume", request.Arm, request.Model, request.Effort, tasks)
	arm.StateDir = request.EvidenceDir
	executor := &schedulerExecutor{}
	checksums := map[string]string{request.Task: request.TaskChecksum, "second-task": "second-checksum"}
	environments := map[string]string{request.Task: request.EnvironmentIdentity, "second-task": "second-environment"}
	if err := executeMatrix(context.Background(), request.RepoRoot, checksums, environments, []matrixArm{arm}, nil, 1, executor, nil); err != nil {
		t.Fatal(err)
	}
	calls := executor.requests()
	if len(calls) != 1 || calls[0].Task != "second-task" {
		t.Fatalf("resume scheduled %#v", calls)
	}
}

func TestRecoveryOnlyCanonicalizesTerminalTimeoutWithoutSchedulingExecution(t *testing.T) {
	request, stage := terminalVerifierTimeoutFixture(t)
	installDockerCleanupStub(t, func(context.Context, ...string) ([]byte, error) { return nil, nil })
	tasks := []benchmarkPlanTask{{ID: request.Task, RepetitionsPerArm: 1}}
	arm := matrixArmFixture(filepath.Dir(request.EvidenceDir), "recovery", request.Arm, request.Model, request.Effort, tasks)
	arm.StateDir = request.EvidenceDir
	preparation := matrixPreparation{
		arms: []matrixArm{arm}, tasks: tasks,
		checksums:    map[string]string{request.Task: request.TaskChecksum},
		environments: map[string]string{request.Task: request.EnvironmentIdentity},
	}
	recovered, err := recoverTerminalVerifierTimeoutCells(context.Background(), request.RepoRoot, preparation, nil)
	if err != nil || recovered != 1 {
		t.Fatalf("recovery-only result = %d, %v", recovered, err)
	}
	var result AttemptResult
	if err := readStudyJSON(armResultPath(arm.StateDir, request.Task, 1), &result); err != nil {
		t.Fatal(err)
	}
	if result.VerifierOutcome != verifierOutcomeTestTimeout || result.F2PTotal != 3 {
		t.Fatalf("recovered result = %#v", result)
	}
	if _, err := os.Stat(stage); err != nil {
		t.Fatalf("recovery-only removed retained diagnostics: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(request.RepoRoot, ".agent-layer", "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "benchmark-verifier-replay-") {
			t.Fatalf("recovery-only started a verifier replay: %s", entry.Name())
		}
	}
}

func TestRecoveryOnlySkipsUnclassifiedVerifierTimeout(t *testing.T) {
	request, stage := retainedVerifierFailureFixture(t, "VerifierTimeoutError")
	tasks := []benchmarkPlanTask{{ID: request.Task, RepetitionsPerArm: 1}}
	arm := matrixArmFixture(filepath.Dir(request.EvidenceDir), "recovery", request.Arm, request.Model, request.Effort, tasks)
	arm.StateDir = request.EvidenceDir
	preparation := matrixPreparation{
		arms: []matrixArm{arm}, tasks: tasks,
		checksums:    map[string]string{request.Task: request.TaskChecksum},
		environments: map[string]string{request.Task: request.EnvironmentIdentity},
	}
	recovered, err := recoverTerminalVerifierTimeoutCells(context.Background(), request.RepoRoot, preparation, nil)
	if err != nil || recovered != 0 {
		t.Fatalf("unclassified recovery = %d, %v", recovered, err)
	}
	if _, err := os.Stat(stage); err != nil {
		t.Fatalf("unclassified timeout stage was removed: %v", err)
	}
	if _, found, err := matchingPierExecutionCheckpoint(request); err != nil || !found {
		t.Fatalf("unclassified timeout checkpoint = found=%t err=%v", found, err)
	}
	checkpoint, _, _ := matchingPierExecutionCheckpoint(request)
	recoveryRequest := request
	recoveryRequest.EventID = checkpoint.EventID
	recoveryRequest.recoveryOnly = true
	installDockerCleanupStub(t, func(context.Context, ...string) ([]byte, error) { return nil, nil })
	if _, err := (PierExecutor{}).replayVerifier(context.Background(), recoveryRequest, checkpoint); err == nil || !strings.Contains(err.Error(), "recovery-only mode refuses verifier replay") {
		t.Fatalf("recovery-only replay guard = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(request.RepoRoot, ".agent-layer", "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "benchmark-verifier-replay-") {
			t.Fatalf("recovery-only guard staged verifier replay: %s", entry.Name())
		}
	}
}

func TestRecoveryOnlyBindsRetainedStudyBeforeCurrentAdapterIdentity(t *testing.T) {
	request, _ := terminalVerifierTimeoutFixture(t)
	tasks := []benchmarkPlanTask{{ID: request.Task, RepetitionsPerArm: 1}}
	selection := matrixSelectionFixture()
	selection.Tasks = []matrixSelectionTask{{ID: request.Task, Repetitions: 1, Weight: 1}}
	selectionID := strings.Repeat("s", 64)
	studyID := strings.Repeat("h", 64)
	armID := strings.Repeat("a", 64)
	label := ArmBaseline
	stateDir := filepath.Join(request.RepoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "studies", studyID)
	armStateDir := filepath.Join(stateDir, "arms", armID)
	if err := copyRequiredTree(request.EvidenceDir, armStateDir); err != nil {
		t.Fatal(err)
	}
	oldAdapter := strings.Repeat("f", 64)
	manifest := immutableStudyManifest{
		SchemaVersion: immutableStudyManifestSchema, StudyID: studyID, SelectionID: selectionID,
		Membership: []string{label}, Checksums: map[string]string{request.Task: request.TaskChecksum},
		Environments: map[string]string{request.Task: request.EnvironmentIdentity}, Resources: studyResourceContract(),
		Arms: []studyArmContract{{Name: label, ID: armID, Target: request.Model.RuntimeIdentifier + ":" + request.Effort, Adapter: oldAdapter}},
	}
	if err := writeJSON(filepath.Join(stateDir, "study-manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	prepared := preparedStudy{
		selection: selection, selectionID: selectionID,
		experiments: []preparedStudyExperiment{{studyExperiment: studyExperiment{Name: label}, model: request.Model, effort: request.Effort}},
	}
	historical := recoveryPreparationFromManifest(&prepared, manifest, stateDir, tasks, 1)
	if err := ensureStudyArmManifest(selectionID, tasks, manifest.Checksums, &historical.arms[0]); err != nil {
		t.Fatal(err)
	}
	currentAdapter, err := embeddedPierAdapterSHA256()
	if err != nil {
		t.Fatal(err)
	}
	if currentAdapter == oldAdapter {
		t.Fatal("test requires a historical adapter identity")
	}
	loaded, found, err := loadRecoverableCachedStudy(request.RepoRoot, &prepared, []cachedStudyCandidate{{stateDir: stateDir}}, tasks, 1)
	if err != nil || !found {
		t.Fatalf("historical recovery binding = found=%t err=%v", found, err)
	}
	if prepared.studyID != studyID || loaded.stateDir != stateDir || loaded.arms[0].AdapterSHA256 != oldAdapter {
		t.Fatalf("historical recovery selected %#v", loaded)
	}
}
