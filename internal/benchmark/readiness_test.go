package benchmark

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	testReadinessTask   = "expr-try-catch-errors"
	testReadinessImage  = "registry.example/task:v1"
	testReadinessDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func writeReadinessTaskFixture(t *testing.T, checkout, task, image, verifier string) {
	t.Helper()
	root := filepath.Join(checkout, "tasks", task)
	if err := os.MkdirAll(filepath.Join(root, "tests"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, taskTOMLFile), []byte("[environment]\ndocker_image = \""+image+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tests", "test.sh"), []byte(verifier), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "tests", "Dockerfile"), []byte("FROM "+image+"\nCOPY test.sh /tests/test.sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readinessTestFS(check string) fs.FS {
	root := "readiness/" + DeepSWECommit + "/" + testReadinessTask + "/"
	contract := `{
  "schema": "deepswe-task-readiness-v1",
  "task": "expr-try-catch-errors",
  "image": "registry.example/task:v1",
  "image_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "check": "check.sh"
}`
	return fstest.MapFS{
		root + "contract.json": {Data: []byte(contract)},
		root + "check.sh":      {Data: []byte(check)},
	}
}

func installReadinessTestBoundaries(t *testing.T, contractFS fs.FS, run func(context.Context, ...string) ([]byte, error)) {
	t.Helper()
	originalFS := readinessContracts
	originalRun := runTaskReadinessCommand
	readinessContracts = contractFS
	runTaskReadinessCommand = run
	t.Cleanup(func() {
		readinessContracts = originalFS
		runTaskReadinessCommand = originalRun
	})
}

func TestTaskReadinessRejectsMissingContractBeforeDockerOrProviderWork(t *testing.T) {
	runs := 0
	installReadinessTestBoundaries(t, fstest.MapFS{}, func(context.Context, ...string) ([]byte, error) {
		runs++
		return nil, nil
	})
	checkout := t.TempDir()
	writeReadinessTaskFixture(t, checkout, "missing-task", testReadinessImage, "#!/bin/bash\nhidden-tests\n")
	_, err := certifyTaskEnvironment(context.Background(), t.TempDir(), checkout, "missing-task", strings.Repeat("1", 64))
	if err == nil || !strings.Contains(err.Error(), "no mandatory environment readiness contract") {
		t.Fatalf("missing readiness contract error = %v", err)
	}
	if runs != 0 {
		t.Fatalf("missing contract started %d Docker commands", runs)
	}
}

func TestTaskReadinessFailureIsHarnessFailureBeforeProviderWork(t *testing.T) {
	installReadinessTestBoundaries(t, readinessTestFS("#!/bin/bash\nexit 19\n"), func(context.Context, ...string) ([]byte, error) {
		return []byte("firefox is unavailable"), errors.New("exit status 19")
	})
	checkout := t.TempDir()
	writeReadinessTaskFixture(t, checkout, testReadinessTask, testReadinessImage, "#!/bin/bash\nhidden-tests\n")
	_, err := certifyTaskEnvironment(context.Background(), t.TempDir(), checkout, testReadinessTask, strings.Repeat("1", 64))
	if err == nil || !strings.Contains(err.Error(), "environment readiness failed before provider execution") ||
		!strings.Contains(err.Error(), "firefox is unavailable") {
		t.Fatalf("readiness failure = %v", err)
	}
}

func TestTaskReadinessCertificationReuseAndInvalidation(t *testing.T) {
	runs := 0
	installReadinessTestBoundaries(t, readinessTestFS("#!/bin/bash\ntrue\n"), func(_ context.Context, arguments ...string) ([]byte, error) {
		runs++
		joined := strings.Join(arguments, " ")
		if !strings.Contains(joined, testReadinessImage+"@"+testReadinessDigest) || !strings.Contains(joined, "--network none") {
			t.Fatalf("readiness Docker arguments = %#v", arguments)
		}
		return nil, nil
	})
	repoRoot := t.TempDir()
	checkout := t.TempDir()
	writeReadinessTaskFixture(t, checkout, testReadinessTask, testReadinessImage, "#!/bin/bash\nhidden-tests\n")

	for range 2 {
		if _, err := certifyTaskEnvironment(context.Background(), repoRoot, checkout, testReadinessTask, strings.Repeat("1", 64)); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 1 {
		t.Fatalf("identical certification ran %d times, want 1", runs)
	}
	if _, err := certifyTaskEnvironment(context.Background(), repoRoot, checkout, testReadinessTask, strings.Repeat("2", 64)); err != nil {
		t.Fatal(err)
	}
	if runs != 2 {
		t.Fatalf("task checksum change ran %d certifications, want 2", runs)
	}
	readinessContracts = readinessTestFS("#!/bin/bash\necho changed\n")
	if _, err := certifyTaskEnvironment(context.Background(), repoRoot, checkout, testReadinessTask, strings.Repeat("2", 64)); err != nil {
		t.Fatal(err)
	}
	if runs != 3 {
		t.Fatalf("contract change ran %d certifications, want 3", runs)
	}
}

func TestTaskReadinessImageMustMatchPinnedTaskDefinition(t *testing.T) {
	runs := 0
	installReadinessTestBoundaries(t, readinessTestFS("#!/bin/bash\ntrue\n"), func(context.Context, ...string) ([]byte, error) {
		runs++
		return nil, nil
	})
	checkout := t.TempDir()
	writeReadinessTaskFixture(t, checkout, testReadinessTask, "registry.example/changed:v2", "#!/bin/bash\nhidden-tests\n")
	_, err := certifyTaskEnvironment(context.Background(), t.TempDir(), checkout, testReadinessTask, strings.Repeat("1", 64))
	if err == nil || !strings.Contains(err.Error(), "does not match task.toml") {
		t.Fatalf("image drift error = %v", err)
	}
	if runs != 0 {
		t.Fatalf("image drift started %d Docker commands", runs)
	}
}

func TestTaskReadinessPierArgumentsAreIdenticalAcrossArms(t *testing.T) {
	installReadinessTestBoundaries(t, readinessTestFS("#!/bin/bash\ncheck-runtime\n"), func(context.Context, ...string) ([]byte, error) {
		return nil, nil
	})
	checkout := t.TempDir()
	writeReadinessTaskFixture(t, checkout, testReadinessTask, testReadinessImage, "#!/bin/bash\nrun-hidden-tests\n")
	baseline, err := prepareTaskStartup(checkout, testReadinessTask, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	treatment, err := prepareTaskStartup(checkout, testReadinessTask, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	normalize := func(arguments []string) string {
		joined := strings.Join(arguments, " ")
		joined = strings.ReplaceAll(joined, filepath.Dir(strings.TrimPrefix(arguments[3], "readiness_script=")), "<stage>")
		return joined
	}
	if normalize(baseline) != normalize(treatment) {
		t.Fatalf("baseline readiness %q differs from treatment readiness %q", normalize(baseline), normalize(treatment))
	}
	if !strings.Contains(normalize(baseline), testReadinessImage+"@"+testReadinessDigest) {
		t.Fatalf("Pier arguments do not pin the task image: %#v", baseline)
	}
}

func TestTaskReadinessRejectsBaselineTreatmentIdentityMismatch(t *testing.T) {
	tasks := []benchmarkPlanTask{{ID: "task-one"}, {ID: "task-two"}}
	baseline := map[string]string{"task-one": strings.Repeat("1", 64), "task-two": strings.Repeat("2", 64)}
	matching := map[string]string{"task-one": strings.Repeat("1", 64), "task-two": strings.Repeat("2", 64)}
	if err := validateTaskEnvironmentParity(tasks, baseline, matching); err != nil {
		t.Fatalf("matching task environments: %v", err)
	}
	mismatched := map[string]string{"task-one": strings.Repeat("1", 64), "task-two": strings.Repeat("3", 64)}
	if err := validateTaskEnvironmentParity(tasks, baseline, mismatched); err == nil || !strings.Contains(err.Error(), "fresh baseline") {
		t.Fatalf("mismatched task environments = %v", err)
	}
	if err := validateTaskEnvironmentParity(tasks, nil, matching); err == nil {
		t.Fatal("baseline without readiness provenance was accepted")
	}
}
