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

func readinessOverlayTestFS() fs.FS {
	root := "readiness/" + DeepSWECommit + "/" + testReadinessTask + "/"
	contract := `{
  "schema": "deepswe-task-readiness-v1",
  "task": "expr-try-catch-errors",
  "image": "registry.example/task:v1",
  "image_digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "check": "check.sh",
  "agent_image_overlay": "agent.Dockerfile"
}`
	return fstest.MapFS{
		root + "contract.json":    {Data: []byte(contract)},
		root + "check.sh":         {Data: []byte("#!/bin/bash\ncheck-tools\n")},
		root + "agent.Dockerfile": {Data: []byte("FROM " + testReadinessImage + "@" + testReadinessDigest + "\nRUN install-browser\n")},
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

func TestTaskDockerImageDefinitionFailsExplicitly(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name, contents, want string
	}{
		{name: "malformed", contents: "[environment\n", want: "decode benchmark task environment identity"},
		{name: "missing image", contents: "[environment]\n", want: "must declare one Docker image"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(root, test.name+".toml")
			if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readTaskDockerImage(path); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("task image error=%v, want %q", err, test.want)
			}
		})
	}
	if _, err := readTaskDockerImage(filepath.Join(root, "missing.toml")); err == nil || !strings.Contains(err.Error(), "read benchmark task environment identity") {
		t.Fatalf("missing task definition error=%v", err)
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

func TestPlanTaskCertificationReturnsOnlyCompleteEnvironmentSet(t *testing.T) {
	repoRoot, checkout := t.TempDir(), t.TempDir()
	writeAuditCatalogFixture(t, checkout, "first-task", "second-task")
	runs := 0
	installReadinessTestBoundaries(t, auditContractsFixture("first-task", "second-task"), func(_ context.Context, arguments ...string) ([]byte, error) {
		runs++
		if len(arguments) == 0 || arguments[0] != commandRun {
			t.Fatalf("unexpected readiness command: %#v", arguments)
		}
		return nil, nil
	})
	tasks := []benchmarkPlanTask{{ID: "first-task", RepetitionsPerArm: 1}, {ID: "second-task", RepetitionsPerArm: 1}}
	checksums := map[string]string{"first-task": strings.Repeat("1", 64), "second-task": strings.Repeat("2", 64)}

	identities, err := certifyPlanTaskEnvironments(context.Background(), repoRoot, checkout, tasks, checksums)
	if err != nil {
		t.Fatal(err)
	}
	if len(identities) != 2 || identities["first-task"] == "" || identities["second-task"] == "" || runs != 2 {
		t.Fatalf("identities=%#v readiness runs=%d", identities, runs)
	}

	readinessContracts = fstest.MapFS{}
	if _, err := certifyPlanTaskEnvironments(context.Background(), repoRoot, checkout, tasks, checksums); err == nil ||
		!strings.Contains(err.Error(), "selected benchmark tasks failed readiness certification") ||
		!strings.Contains(err.Error(), "first-task") || !strings.Contains(err.Error(), "second-task") {
		t.Fatalf("partial certification error=%v", err)
	}
}

func TestTaskReadinessOverlayBuildsAndRunsIdenticallyThroughPier(t *testing.T) {
	builds := 0
	readinessRuns := 0
	buildImageIDs := []string{"sha256:" + strings.Repeat("b", 64), "sha256:" + strings.Repeat("c", 64)}
	installReadinessTestBoundaries(t, readinessOverlayTestFS(), func(_ context.Context, arguments ...string) ([]byte, error) {
		switch arguments[0] {
		case "image":
			t.Fatalf("mutable overlay tag was trusted: %#v", arguments)
			return nil, nil
		case "build":
			builds++
			if !strings.Contains(strings.Join(arguments, " "), "agent-layer-benchmark/expr-try-catch-errors:") {
				t.Fatalf("overlay build arguments = %#v", arguments)
			}
			for index, argument := range arguments {
				if argument == "--iidfile" && index+1 < len(arguments) {
					if err := os.WriteFile(arguments[index+1], []byte(buildImageIDs[builds-1]+"\n"), 0o600); err != nil {
						t.Fatal(err)
					}
					return nil, nil
				}
			}
			t.Fatalf("overlay build omitted --iidfile: %#v", arguments)
			return nil, nil
		case commandRun:
			readinessRuns++
			if builds == 0 || !strings.Contains(strings.Join(arguments, " "), buildImageIDs[builds-1]) {
				t.Fatalf("overlay readiness arguments = %#v", arguments)
			}
			return nil, nil
		default:
			t.Fatalf("unexpected Docker command %#v", arguments)
			return nil, nil
		}
	})
	repoRoot := t.TempDir()
	checkout := t.TempDir()
	writeReadinessTaskFixture(t, checkout, testReadinessTask, testReadinessImage, "#!/bin/bash\nhidden-tests\n")
	identity, err := certifyTaskEnvironment(context.Background(), repoRoot, checkout, testReadinessTask, strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	reusedIdentity, err := certifyTaskEnvironment(context.Background(), repoRoot, checkout, testReadinessTask, strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	if reusedIdentity != identity || builds != 2 || readinessRuns != 1 {
		t.Fatalf("overlay certification reuse identity=%q builds=%d readiness runs=%d", reusedIdentity, builds, readinessRuns)
	}
	var receipt taskReadinessCertification
	if err := readStudyJSON(filepath.Join(repoRoot, ".agent-layer", "state", "benchmarks", "deepswe", "environment-certifications", identity+".json"), &receipt); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(receipt.PinnedImage, "agent-layer-overlay-sha256:") ||
		strings.Contains(receipt.PinnedImage, strings.TrimPrefix(buildImageIDs[0], "sha256:")) {
		t.Fatalf("certified overlay identity follows Docker build metadata: %#v", receipt)
	}
	arguments, err := prepareTaskStartup(checkout, testReadinessTask, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(arguments, " ")
	if !strings.Contains(joined, "agent_context=") || strings.Contains(joined, "pinned_image=agent-layer-benchmark/") {
		t.Fatalf("Pier arguments omit the derived agent context: %#v", arguments)
	}
}

func TestTaskReadinessOverlayRequiresDockerBuildIdentity(t *testing.T) {
	for _, test := range []struct {
		name     string
		imageID  string
		buildErr error
		want     string
	}{
		{name: "build failure", buildErr: errors.New("builder unavailable"), want: "builder unavailable"},
		{name: "missing iid file", want: "read benchmark task"},
		{name: "malformed iid", imageID: "not-an-image", want: "invalid immutable identity"},
		{name: "non-hex iid", imageID: "sha256:" + strings.Repeat("z", 64), want: "invalid immutable identity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			installReadinessTestBoundaries(t, readinessOverlayTestFS(), func(_ context.Context, arguments ...string) ([]byte, error) {
				if len(arguments) == 0 || arguments[0] != "build" {
					t.Fatalf("unexpected Docker command: %#v", arguments)
				}
				if test.imageID != "" {
					for index, argument := range arguments {
						if argument == "--iidfile" && index+1 < len(arguments) {
							if err := os.WriteFile(arguments[index+1], []byte(test.imageID), 0o600); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
				return []byte("build diagnostics"), test.buildErr
			})
			checkout := t.TempDir()
			writeReadinessTaskFixture(t, checkout, testReadinessTask, testReadinessImage, "#!/bin/bash\nhidden-tests\n")
			if _, err := certifyTaskEnvironment(context.Background(), t.TempDir(), checkout, testReadinessTask, strings.Repeat("1", 64)); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("overlay identity error=%v, want %q", err, test.want)
			}
		})
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
	if err := validateTaskEnvironmentParity(tasks, baseline, mismatched); err == nil || !strings.Contains(err.Error(), "run benchmark run again") {
		t.Fatalf("mismatched task environments = %v", err)
	}
	if err := validateTaskEnvironmentParity(tasks, nil, matching); err == nil {
		t.Fatal("baseline without readiness provenance was accepted")
	}
}
