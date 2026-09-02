package benchmark

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadinessDiskPreflightRefusesImpossiblePlanBeforePulls(t *testing.T) {
	original := readinessDiskCapacity
	readinessDiskCapacity = func(context.Context) (int64, error) { return 2 << 30, nil }
	t.Cleanup(func() { readinessDiskCapacity = original })
	tasks := make([]benchmarkPlanTask, 113)
	if err := preflightReadinessDisk(context.Background(), tasks, true); err == nil ||
		!strings.Contains(err.Error(), "113 benchmark task image") ||
		!strings.Contains(err.Error(), "452.0 GiB") || !strings.Contains(err.Error(), "2.0 GiB available") {
		t.Fatalf("capacity error = %v", err)
	}
	if err := preflightReadinessDisk(context.Background(), tasks, false); err == nil || !strings.Contains(err.Error(), "need about 4.0 GiB") {
		t.Fatalf("bounded capacity error = %v", err)
	}
	readinessDiskCapacity = func(context.Context) (int64, error) { return 5 << 30, nil }
	if err := preflightReadinessDisk(context.Background(), tasks, false); err != nil {
		t.Fatalf("bounded plan rejected: %v", err)
	}
}

func TestTaskContainerEmulationWarningOnlyForNonAMD64Hosts(t *testing.T) {
	if got := taskContainerEmulationWarning("amd64"); got != "" {
		t.Fatalf("native amd64 warning = %q", got)
	}
	got := taskContainerEmulationWarning("arm64")
	if !strings.Contains(got, "linux/amd64") || !strings.Contains(got, "Host architecture arm64 requires emulation") || !strings.Contains(got, "30+ minutes") {
		t.Fatalf("arm64 emulation warning = %q", got)
	}
}

func TestStudyDiskPreflightAccountsForDisabledReclamation(t *testing.T) {
	original := readinessDiskCapacity
	readinessDiskCapacity = func(context.Context) (int64, error) { return 5 << 30, nil }
	t.Cleanup(func() { readinessDiskCapacity = original })
	tasks := make([]benchmarkPlanTask, 2)
	if err := preflightStudyDisk(context.Background(), tasks, true); err != nil {
		t.Fatalf("bounded serial plan rejected: %v", err)
	}
	if err := preflightStudyDisk(context.Background(), tasks, false); err == nil || !strings.Contains(err.Error(), "need about 8.0 GiB") {
		t.Fatalf("retained parallel plan capacity error = %v", err)
	}
}

func TestSerialTaskReclaimingExecutorRetainsImageAcrossTaskCells(t *testing.T) {
	originalCleanup := reclaimStudyTaskImages
	t.Cleanup(func() { reclaimStudyTaskImages = originalCleanup })
	var events []string
	reclaimStudyTaskImages = func(_ context.Context, readiness loadedTaskReadiness) error {
		events = append(events, "reclaim:"+readiness.pinnedImage)
		return nil
	}
	delegate := taskExecutorFunc(func(_ context.Context, request ExecutionRequest) (AttemptResult, error) {
		events = append(events, "execute:"+request.Task)
		return AttemptResult{}, nil
	})
	executor := &serialTaskReclaimingExecutor{
		delegate: delegate,
		readiness: map[string]loadedTaskReadiness{
			"first":  {pinnedImage: "first-image"},
			"second": {pinnedImage: "second-image"},
		},
	}
	for _, task := range []string{"first", "first", "second", "second"} {
		if _, err := executor.Execute(context.Background(), ExecutionRequest{Task: task}); err != nil {
			t.Fatal(err)
		}
	}
	if err := executor.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(events, ","), "execute:first,execute:first,reclaim:first-image,execute:second,execute:second,reclaim:second-image"; got != want {
		t.Fatalf("events = %q, want %q", got, want)
	}
}

func TestSerialTaskReclaimingExecutorStopsBeforeNextTaskOnCleanupFailure(t *testing.T) {
	originalCleanup := reclaimStudyTaskImages
	t.Cleanup(func() { reclaimStudyTaskImages = originalCleanup })
	cleanupFailure := errors.New("cleanup failed")
	var executed []string
	cleanupCalls := 0
	reclaimStudyTaskImages = func(context.Context, loadedTaskReadiness) error {
		cleanupCalls++
		if cleanupCalls == 1 {
			return cleanupFailure
		}
		return nil
	}
	executor := &serialTaskReclaimingExecutor{
		delegate: taskExecutorFunc(func(_ context.Context, request ExecutionRequest) (AttemptResult, error) {
			executed = append(executed, request.Task)
			return AttemptResult{}, nil
		}),
		readiness: map[string]loadedTaskReadiness{"first": {pinnedImage: "first-image"}},
	}
	if _, err := executor.Execute(context.Background(), ExecutionRequest{Task: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := executor.Execute(context.Background(), ExecutionRequest{Task: "second"}); !errors.Is(err, cleanupFailure) {
		t.Fatalf("transition error = %v", err)
	}
	if got := strings.Join(executed, ","); got != "first" {
		t.Fatalf("executed tasks = %q", got)
	}
	if err := executor.close(context.Background()); err != nil {
		t.Fatalf("retry failed cleanup at close: %v", err)
	}
	if cleanupCalls != 2 {
		t.Fatalf("cleanup calls = %d, want transition attempt plus close retry", cleanupCalls)
	}
}

type taskExecutorFunc func(context.Context, ExecutionRequest) (AttemptResult, error)

func (execute taskExecutorFunc) Execute(ctx context.Context, request ExecutionRequest) (AttemptResult, error) {
	return execute(ctx, request)
}

func TestReadinessDiskPreflightPropagatesCancellationContext(t *testing.T) {
	original := readinessDiskCapacity
	type contextKeyType string
	const contextKey contextKeyType = "readiness caller"
	ctx := context.WithValue(context.Background(), contextKey, "caller")
	readinessDiskCapacity = func(got context.Context) (int64, error) {
		if got.Value(contextKey) != "caller" {
			t.Fatal("disk capacity check lost its caller context")
		}
		return estimatedTaskImageBytes, nil
	}
	t.Cleanup(func() { readinessDiskCapacity = original })
	if err := preflightReadinessDisk(ctx, []benchmarkPlanTask{{ID: "first-task"}}, false); err != nil {
		t.Fatal(err)
	}
}

func TestStudyTreatmentPreflightReclaimsImagesEvenOnFailure(t *testing.T) {
	originalPreflight := preflightTreatmentRuntime
	originalLoad := loadStudyTaskReadiness
	originalCleanup := reclaimStudyTaskImages
	t.Cleanup(func() {
		preflightTreatmentRuntime = originalPreflight
		loadStudyTaskReadiness = originalLoad
		reclaimStudyTaskImages = originalCleanup
	})
	preflightFailure := errors.New("runtime unavailable")
	var preflightCalls, cleanupCalls int
	preflightTreatmentRuntime = func(_ context.Context, request ExecutionRequest) error {
		preflightCalls++
		if request.Arm == "second" {
			return preflightFailure
		}
		return nil
	}
	readiness := loadedTaskReadiness{pinnedImage: "registry.example/task@sha256:" + strings.Repeat("1", 64)}
	loadStudyTaskReadiness = func(string, string) (loadedTaskReadiness, error) { return readiness, nil }
	reclaimStudyTaskImages = func(_ context.Context, got loadedTaskReadiness) error {
		cleanupCalls++
		if got.pinnedImage != readiness.pinnedImage {
			t.Fatalf("cleanup readiness = %#v, want %#v", got, readiness)
		}
		return nil
	}
	completed := 0
	evidenceDir := t.TempDir()
	err := preflightStudyTaskRuntimes(context.Background(), "task", []studyRuntimePreflight{
		{experiment: "first", request: ExecutionRequest{EvidenceDir: evidenceDir, Arm: "first"}},
		{experiment: "second", request: ExecutionRequest{EvidenceDir: evidenceDir, Arm: "second"}},
	}, true, "checkout", &completed, 2, StudyOptions{}, "amd64")
	if !errors.Is(err, preflightFailure) {
		t.Fatalf("preflight failure = %v", err)
	}
	if preflightCalls != 2 || completed != 1 || cleanupCalls != 1 {
		t.Fatalf("preflights=%d completed=%d cleanup=%d", preflightCalls, completed, cleanupCalls)
	}
	completed = 0
	err = preflightStudyTaskRuntimes(context.Background(), "task", []studyRuntimePreflight{
		{experiment: "first", request: ExecutionRequest{EvidenceDir: evidenceDir, Arm: "first"}},
		{experiment: "second", request: ExecutionRequest{EvidenceDir: evidenceDir, Arm: "second"}},
	}, true, "checkout", &completed, 2, StudyOptions{}, "amd64")
	if !errors.Is(err, preflightFailure) {
		t.Fatalf("retry preflight failure = %v", err)
	}
	if preflightCalls != 3 || completed != 1 || cleanupCalls != 2 {
		t.Fatalf("retry preflights=%d completed=%d cleanup=%d", preflightCalls, completed, cleanupCalls)
	}
}

func TestStudyTreatmentPreflightDoesNotPersistReceiptWhenCleanupFails(t *testing.T) {
	originalPreflight := preflightTreatmentRuntime
	originalLoad := loadStudyTaskReadiness
	originalCleanup := reclaimStudyTaskImages
	t.Cleanup(func() {
		preflightTreatmentRuntime = originalPreflight
		loadStudyTaskReadiness = originalLoad
		reclaimStudyTaskImages = originalCleanup
	})
	cleanupFailure := errors.New("cleanup failed")
	var preflightCalls, cleanupCalls int
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error {
		preflightCalls++
		return nil
	}
	loadStudyTaskReadiness = func(string, string) (loadedTaskReadiness, error) {
		return loadedTaskReadiness{pinnedImage: "task-image"}, nil
	}
	reclaimStudyTaskImages = func(context.Context, loadedTaskReadiness) error {
		cleanupCalls++
		if cleanupCalls == 1 {
			return cleanupFailure
		}
		return nil
	}
	evidenceDir := t.TempDir()
	request := ExecutionRequest{EvidenceDir: evidenceDir, Arm: "arm", Task: "task"}
	work := []studyRuntimePreflight{{experiment: "arm", request: request}}
	completed := 0
	err := preflightStudyTaskRuntimes(context.Background(), "task", work, true, "checkout", &completed, 1, StudyOptions{}, "amd64")
	if !errors.Is(err, cleanupFailure) {
		t.Fatalf("cleanup failure = %v", err)
	}
	if preflightCalls != 1 || cleanupCalls != 1 {
		t.Fatalf("preflights=%d cleanup=%d", preflightCalls, cleanupCalls)
	}
	entries, readErr := os.ReadDir(filepath.Join(evidenceDir, "runtime-preflights"))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("cleanup failure persisted receipts: %#v", entries)
	}
	completed = 0
	if err := preflightStudyTaskRuntimes(context.Background(), "task", work, true, "checkout", &completed, 1, StudyOptions{}, "amd64"); err != nil {
		t.Fatal(err)
	}
	if preflightCalls != 2 || cleanupCalls != 2 {
		t.Fatalf("retry preflights=%d cleanup=%d", preflightCalls, cleanupCalls)
	}
	cached, _, _, err := completedRuntimePreflight(request, "amd64")
	if err != nil || !cached {
		t.Fatalf("successful retry receipt = cached %t err %v", cached, err)
	}
}

func TestCompletedRuntimePreflightRejectsMismatchedReceipt(t *testing.T) {
	request := ExecutionRequest{EvidenceDir: t.TempDir(), Arm: "arm"}
	cached, receipt, path, err := completedRuntimePreflight(request, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if cached {
		t.Fatal("missing receipt was treated as complete")
	}
	if receipt.HostOS != runtime.GOOS || receipt.HostArchitecture != "amd64" {
		t.Fatalf("host platform = %s/%s, want %s/amd64", receipt.HostOS, receipt.HostArchitecture, runtime.GOOS)
	}
	if err := writeJSON(path, receipt); err != nil {
		t.Fatal(err)
	}
	cached, _, _, err = completedRuntimePreflight(request, "amd64")
	if err != nil || !cached {
		t.Fatalf("matching receipt reuse = cached %t err %v", cached, err)
	}
	cached, _, _, err = completedRuntimePreflight(request, "arm64")
	if err != nil || cached {
		t.Fatalf("different Docker host architecture reused receipt: cached %t err %v", cached, err)
	}
	mismatch := receipt
	mismatch.HostArchitecture = "other"
	if err := writeJSON(path, mismatch); err != nil {
		t.Fatal(err)
	}
	cached, _, _, err = completedRuntimePreflight(request, "amd64")
	if cached || err == nil || !strings.Contains(err.Error(), "does not match its content-addressed identity") {
		t.Fatalf("mismatched receipt = cached %t err %v", cached, err)
	}
}

func TestStudyTreatmentPreflightReclaimsImageAfterCancellation(t *testing.T) {
	originalPreflight := preflightTreatmentRuntime
	originalLoad := loadStudyTaskReadiness
	originalCleanup := reclaimStudyTaskImages
	t.Cleanup(func() {
		preflightTreatmentRuntime = originalPreflight
		loadStudyTaskReadiness = originalLoad
		reclaimStudyTaskImages = originalCleanup
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	preflightTreatmentRuntime = func(ctx context.Context, _ ExecutionRequest) error { return ctx.Err() }
	loadStudyTaskReadiness = func(string, string) (loadedTaskReadiness, error) {
		return loadedTaskReadiness{pinnedImage: "task-image"}, nil
	}
	cleaned := false
	reclaimStudyTaskImages = func(ctx context.Context, _ loadedTaskReadiness) error {
		if err := ctx.Err(); err != nil {
			t.Fatalf("cleanup inherited canceled context: %v", err)
		}
		cleaned = true
		return nil
	}
	completed := 0
	err := preflightStudyTaskRuntimes(ctx, "task", []studyRuntimePreflight{{experiment: "arm", request: ExecutionRequest{EvidenceDir: t.TempDir(), Arm: "arm"}}}, true, "checkout", &completed, 1, StudyOptions{}, "amd64")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("preflight cancellation = %v", err)
	}
	if !cleaned {
		t.Fatal("task image was not reclaimed after cancellation")
	}
}

func TestStudyRuntimePreflightsGroupArmsByTaskAndReclaimOnce(t *testing.T) {
	originalPreflight := preflightTreatmentRuntime
	originalLoad := loadStudyTaskReadiness
	originalCleanup := reclaimStudyTaskImages
	originalArchitecture := dockerHostArchitecture
	t.Cleanup(func() {
		preflightTreatmentRuntime = originalPreflight
		loadStudyTaskReadiness = originalLoad
		reclaimStudyTaskImages = originalCleanup
		dockerHostArchitecture = originalArchitecture
	})
	dockerHostArchitecture = func(context.Context) (string, error) { return "amd64", nil }
	tasks := []benchmarkPlanTask{{ID: "first"}, {ID: "duplicate"}, {ID: "second"}}
	checksums := map[string]string{"first": "first-checksum", "duplicate": "duplicate-checksum", "second": "second-checksum"}
	environments := map[string]string{"first": "shared", "duplicate": "shared", "second": "second"}
	controlBundle, treatmentBundle := fakeStudyTreatmentBundle(), fakeStudyTreatmentBundle()
	controlBundle.ManifestHash, treatmentBundle.ManifestHash = "control", "treatment"
	arms := []matrixArm{
		{Label: "control", Mode: ArmTreatment, StateDir: t.TempDir(), Bundle: controlBundle},
		{Label: "treatment", Mode: ArmTreatment, StateDir: t.TempDir(), Bundle: treatmentBundle},
	}
	work := scheduleStudyRuntimePreflights("repo", tasks, checksums, environments, arms)
	var calls []string
	preflightTreatmentRuntime = func(_ context.Context, request ExecutionRequest) error {
		calls = append(calls, request.Bundle.ManifestHash+":"+request.Task)
		return nil
	}
	var loaded, reclaimed []string
	loadStudyTaskReadiness = func(_ string, task string) (loadedTaskReadiness, error) {
		loaded = append(loaded, task)
		return loadedTaskReadiness{pinnedImage: task}, nil
	}
	reclaimStudyTaskImages = func(_ context.Context, readiness loadedTaskReadiness) error {
		reclaimed = append(reclaimed, readiness.pinnedImage)
		return nil
	}
	var progress []StudyProgress
	err := preflightStudyRuntimes(context.Background(), StudyOptions{OnProgress: func(event StudyProgress) {
		progress = append(progress, event)
	}}, tasks, work, true, "checkout")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(calls, ","), "control:first,treatment:first,control:second,treatment:second"; got != want {
		t.Fatalf("runtime preflights = %q, want %q", got, want)
	}
	if got := strings.Join(loaded, ","); got != "first,second" {
		t.Fatalf("readiness loads = %q", got)
	}
	if got := strings.Join(reclaimed, ","); got != "first,second" {
		t.Fatalf("image reclamation = %q", got)
	}
	if len(progress) != 5 {
		t.Fatalf("progress events = %#v", progress)
	}
	for index, want := range []struct {
		task, experiment string
		completed        int
	}{
		{"first", "control", 0}, {"first", "treatment", 1}, {"second", "control", 2}, {"second", "treatment", 3}, {"second", "treatment", 4},
	} {
		got := progress[index]
		if got.Message != "Preflighting benchmark runtime" || got.Task != want.task || got.Experiment != want.experiment || got.Completed != want.completed || got.Required != 4 {
			t.Fatalf("progress[%d] = %#v", index, got)
		}
	}
}

func TestStudyTaskPreparationEmitsItsOwnStage(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	stubStudyInfrastructure(t, root)
	originalAuthentication := validateBenchmarkAuthentication
	originalPrepare := prepareBenchmarkTaskSet
	originalDiskCapacity := readinessDiskCapacity
	t.Cleanup(func() {
		validateBenchmarkAuthentication = originalAuthentication
		prepareBenchmarkTaskSet = originalPrepare
		readinessDiskCapacity = originalDiskCapacity
	})
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	readinessDiskCapacity = func(context.Context) (int64, error) { return 9 << 30, nil }
	var progress []StudyProgress
	prepareBenchmarkTaskSet = func(ctx context.Context, gotRoot string, tasks []benchmarkPlanTask) (map[string]string, map[string]string, error) {
		if len(progress) < 2 || progress[len(progress)-2].Message != "Checking Docker disk capacity before image pulls" || progress[len(progress)-1].Message != "Preparing and certifying task environments" {
			t.Fatalf("task preparation inherited stale status: %#v", progress)
		}
		return originalPrepare(ctx, gotRoot, tasks)
	}
	if _, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), DryRun: true, ResourcePreflight: true, OnProgress: func(event StudyProgress) {
		progress = append(progress, event)
	}}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunStudyPreflightsApplicableArmsTaskFirstAndReclaimsOnce(t *testing.T) {
	root := t.TempDir()
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	selection, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	writeStudyInputFixture(t, root, "selection.json", string(selection))
	writeStudyTreatmentConfig(t, root)
	for directory, contents := range map[string]string{"control-instructions": "control instructions\n", "treatment-instructions": "treatment instructions\n"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, directory, "INSTRUCTIONS.md"), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeStudyInputFixture(t, root, "study.toml", "selection = \"selection.json\"\n[[experiments]]\nname = \"control\"\nmodel = \""+model.Name+"\"\nreasoning = \""+effort+"\"\nconfig = \"config.toml\"\ninstructions = \"control-instructions\"\n[[experiments]]\nname = \"treatment\"\nmodel = \""+model.Name+"\"\nreasoning = \""+effort+"\"\nconfig = \"config.toml\"\ninstructions = \"treatment-instructions\"\n")
	stubStudyInfrastructure(t, root)
	originalAuthentication := validateBenchmarkAuthentication
	originalBundles := stageBenchmarkExperimentBundles
	originalCheckout := ensurePinnedBenchmarkCheckout
	originalPreflight := preflightTreatmentRuntime
	originalLoad := loadStudyTaskReadiness
	originalCleanup := reclaimStudyTaskImages
	t.Cleanup(func() {
		validateBenchmarkAuthentication = originalAuthentication
		stageBenchmarkExperimentBundles = originalBundles
		ensurePinnedBenchmarkCheckout = originalCheckout
		preflightTreatmentRuntime = originalPreflight
		loadStudyTaskReadiness = originalLoad
		reclaimStudyTaskImages = originalCleanup
	})
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	controlBundle, treatmentBundle := fakeStudyTreatmentBundle(), fakeStudyTreatmentBundle()
	controlBundle.ManifestHash, treatmentBundle.ManifestHash = "control", "treatment"
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		return []*TreatmentBundle{controlBundle, treatmentBundle}, nil
	}
	ensurePinnedBenchmarkCheckout = func(context.Context, string) (string, error) { return "checkout", nil }
	var preflights, reclaimed []string
	preflightTreatmentRuntime = func(_ context.Context, request ExecutionRequest) error {
		preflights = append(preflights, request.Bundle.ManifestHash+":"+request.Task)
		return nil
	}
	loadStudyTaskReadiness = func(_ string, task string) (loadedTaskReadiness, error) {
		return loadedTaskReadiness{pinnedImage: task}, nil
	}
	reclaimStudyTaskImages = func(_ context.Context, readiness loadedTaskReadiness) error {
		reclaimed = append(reclaimed, readiness.pinnedImage)
		return nil
	}
	if _, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), DryRun: true, ReclaimTaskImages: true}, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(preflights, ","), "control:first-task,treatment:first-task,control:second-task,treatment:second-task"; got != want {
		t.Fatalf("runtime preflights = %q, want %q", got, want)
	}
	if got, want := strings.Join(reclaimed, ","), "first-task,second-task"; got != want {
		t.Fatalf("task image reclamation = %q, want %q", got, want)
	}
}

func TestAutomaticTaskConcurrencyIsAlwaysSafe(t *testing.T) {
	for _, providerCalls := range []bool{false, true} {
		workers := AutomaticTaskConcurrency(providerCalls)
		if workers < 1 || workers > 4 || (providerCalls && workers > 2) {
			t.Fatalf("automatic workers(provider=%t) = %d", providerCalls, workers)
		}
	}
}

func TestStudyTaskIDsReadsSelectionWithoutTreatmentSetup(t *testing.T) {
	root := t.TempDir()
	selection := matrixSelectionFixture()
	data, err := json.Marshal(selection)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "selection.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "study.toml"), []byte("selection = \"selection.json\"\n[[experiments]]\nname = \"intentionally incomplete\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tasks, err := StudyTaskIDs(filepath.Join(root, "study.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(tasks, ","); got != "first-task,second-task" {
		t.Fatalf("study tasks = %q", got)
	}
}

func TestDetectDockerHostArchitecture(t *testing.T) {
	original := runBenchmarkDockerCommand
	t.Cleanup(func() { runBenchmarkDockerCommand = original })
	runBenchmarkDockerCommand = func(context.Context, ...string) ([]byte, error) {
		return []byte("  arm64\n"), nil
	}
	got, err := detectDockerHostArchitecture(context.Background())
	if err != nil || got != "arm64" {
		t.Fatalf("architecture = %q err %v", got, err)
	}
	runBenchmarkDockerCommand = func(context.Context, ...string) ([]byte, error) {
		return []byte("denied"), errors.New("daemon unavailable")
	}
	if _, err := detectDockerHostArchitecture(context.Background()); err == nil || !strings.Contains(err.Error(), "daemon unavailable") {
		t.Fatalf("command failure = %v", err)
	}
	for _, output := range []string{"\n", "<no value>\n"} {
		runBenchmarkDockerCommand = func(context.Context, ...string) ([]byte, error) {
			return []byte(output), nil
		}
		if _, err := detectDockerHostArchitecture(context.Background()); err == nil || !strings.Contains(err.Error(), "did not report a server architecture") {
			t.Fatalf("missing architecture %q = %v", output, err)
		}
	}
}

func TestStudyRuntimePreflightsFailWhenDockerArchitectureIsUnknown(t *testing.T) {
	originalArchitecture := dockerHostArchitecture
	originalPreflight := preflightTreatmentRuntime
	t.Cleanup(func() {
		dockerHostArchitecture = originalArchitecture
		preflightTreatmentRuntime = originalPreflight
	})
	lookupErr := errors.New("daemon architecture unavailable")
	dockerHostArchitecture = func(context.Context) (string, error) { return "", lookupErr }
	called := false
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error {
		called = true
		return nil
	}
	err := preflightStudyRuntimes(context.Background(), StudyOptions{}, []benchmarkPlanTask{{ID: "task"}}, [][]studyRuntimePreflight{{{
		experiment: "arm",
		request:    ExecutionRequest{EvidenceDir: t.TempDir(), Arm: "arm"},
	}}}, false, "")
	if !errors.Is(err, lookupErr) {
		t.Fatalf("architecture failure = %v", err)
	}
	if called {
		t.Fatal("ran runtime preflight without a Docker host architecture")
	}
}

func TestStudyRuntimePreflightsKeyReceiptsByDockerHostArchitecture(t *testing.T) {
	originalArchitecture := dockerHostArchitecture
	originalPreflight := preflightTreatmentRuntime
	t.Cleanup(func() {
		dockerHostArchitecture = originalArchitecture
		preflightTreatmentRuntime = originalPreflight
	})
	dockerHostArchitecture = func(context.Context) (string, error) { return "s390x", nil }
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error { return nil }
	request := ExecutionRequest{EvidenceDir: t.TempDir(), Arm: "arm", Task: "task"}
	if err := preflightStudyRuntimes(context.Background(), StudyOptions{}, []benchmarkPlanTask{{ID: "task"}}, [][]studyRuntimePreflight{{{
		experiment: "arm", request: request,
	}}}, false, ""); err != nil {
		t.Fatal(err)
	}
	cached, receipt, _, err := completedRuntimePreflight(request, "s390x")
	if err != nil || !cached || receipt.HostArchitecture != "s390x" {
		t.Fatalf("docker-host receipt = cached %t arch %q err %v", cached, receipt.HostArchitecture, err)
	}
	cached, _, _, err = completedRuntimePreflight(request, "amd64")
	if err != nil || cached {
		t.Fatalf("different Docker host architecture reused receipt: cached %t err %v", cached, err)
	}
}

func TestInitStudyCreatesSelfContainedSafeSnapshot(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".agent-layer", "instructions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".agents", "skills", "implement"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agent-layer", "instructions", "rules.md"), []byte("rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agent-layer", "instructions", "memory.md"), []byte("Read CONTEXT.md before acting.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".agents", "skills", "implement", "SKILL.md"), []byte("skill\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	selectionData, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	selectionPath := filepath.Join(repo, "website-selection.json")
	if err := os.WriteFile(selectionPath, selectionData, 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(repo, "benchmarks", "study-one")
	studyPath, err := InitStudy(InitStudyOptions{RepoRoot: repo, SelectionPath: selectionPath, Directory: filepath.Join("benchmarks", "study-one")})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		studyPath, filepath.Join(destination, "selection.json"), filepath.Join(destination, "treatment", "config.toml"),
		filepath.Join(destination, "treatment", "project-instructions", "rules.md"),
		filepath.Join(destination, "treatment", "project-instructions", "memory.md"),
		filepath.Join(destination, "treatment", "official-instructions", "00_rules.md"),
		filepath.Join(destination, "treatment", "project-skills", "implement", "SKILL.md"),
		filepath.Join(destination, "treatment", "official-skills", "implement", "SKILL.md"),
		filepath.Join(destination, "treatment", "prompt.md"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing scaffold %s: %v", path, err)
		}
	}
	config, err := os.ReadFile(filepath.Join(destination, "treatment", "config.toml")) // #nosec G304 -- test-owned destination.
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(config), "statusline") {
		t.Fatalf("scaffold copied host-only statusline config: %s", config)
	}
	if !strings.Contains(string(config), `model = "gpt-5.6-luna"`) {
		t.Fatalf("scaffold did not configure the exact provider model: %s", config)
	}
	if _, _, err := studyMCPContract(config, filepath.Join(destination, "treatment", "config.toml")); err != nil {
		t.Fatalf("generated Agent Layer config is invalid: %v", err)
	}
	studyData, err := os.ReadFile(studyPath) // #nosec G304 -- test-owned scaffold path.
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`required_dispatch_roles = ["plan-reviewer", "implementer", "code-reviewer"]`, `entry_prompt = "treatment/prompt.md"`, `skills = "treatment/official-skills"`, `instructions = "treatment/official-instructions"`} {
		if !strings.Contains(string(studyData), required) {
			t.Fatalf("generated study omitted mandatory workflow %q:\n%s", required, studyData)
		}
	}
	prompt, err := os.ReadFile(filepath.Join(destination, "treatment", "prompt.md")) // #nosec G304 -- test-owned scaffold path.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "requires completed Agent Dispatch plan-reviewer, implementer, and code-reviewer roles") ||
		!strings.Contains(string(prompt), "dispatch role fields") || strings.Count(string(prompt), "{{task}}") != 1 {
		t.Fatalf("generated workflow prompt = %q", prompt)
	}
	prepared, err := prepareStudy(StudyOptions{RepoRoot: repo, StudyPath: studyPath})
	if err != nil {
		t.Fatalf("generated study does not validate: %v", err)
	}
	defer prepared.cleanupInputs()
	if got := strings.Join(prepared.experiments[1].RequiredDispatchRoles, ","); got != "code-reviewer,implementer,plan-reviewer" {
		t.Fatalf("generated required dispatch roles = %q", got)
	}
	outcome := studyProgress(prepared, StudyOptions{})
	if len(outcome.Experiments) != 2 || !outcome.Experiments[1].AgentLayer || !outcome.Experiments[1].Skills || len(outcome.Experiments[1].DispatchTargets) != 3 {
		t.Fatalf("generated workflow disclosure = %#v", outcome.Experiments)
	}
	for _, target := range outcome.Experiments[1].DispatchTargets {
		if target.Agent != adapterCodex || target.Model != "gpt-5.6-luna" || target.ReasoningEffort != "low" {
			t.Fatalf("generated workflow target = %#v", target)
		}
	}
}

func TestCopyScaffoldTreeReportsNonDirectorySourceClearly(t *testing.T) {
	source := filepath.Join(t.TempDir(), "skills")
	if err := os.WriteFile(source, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := copyScaffoldTree(source, filepath.Join(t.TempDir(), "snapshot"))
	if err == nil || !strings.Contains(err.Error(), "not a directory") || strings.Contains(err.Error(), "%!w") {
		t.Fatalf("non-directory source error = %v", err)
	}
}
