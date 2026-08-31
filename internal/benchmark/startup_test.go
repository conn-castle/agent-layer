package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractVerifierStartupPreservesArbitraryCommands(t *testing.T) {
	verifier := `#!/bin/bash
log() { echo "$*"; }

# --- Service startup (shared by the agent and verifier) ---
# The commands and readiness checks are task-owned.
start-first --config /etc/first.conf &
start-second --depends-on first
until dependencies-ready; do sleep 1; done
log "dependencies are ready"

# --- Run suites ---
run-hidden-tests
`
	body, found, err := extractVerifierStartup(verifier)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("startup section was not found")
	}
	for _, required := range []string{
		"start-first --config /etc/first.conf &",
		"start-second --depends-on first",
		"until dependencies-ready; do sleep 1; done",
		`log "dependencies are ready"`,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("startup body omitted %q:\n%s", required, body)
		}
	}
	if strings.Contains(body, "run-hidden-tests") {
		t.Fatalf("startup body leaked the verifier suite:\n%s", body)
	}
}

func TestExtractVerifierStartupRejectsAmbiguousDefinitions(t *testing.T) {
	tests := map[string]string{
		"multiple": `# --- Service startup ---
one
# --- Next ---
# --- Service startup ---
two
# --- Done ---`,
		"unterminated": `# --- Service startup ---
one`,
		"empty": `# --- Service startup ---

# --- Tests ---`,
	}
	for name, verifier := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := extractVerifierStartup(verifier); err == nil {
				t.Fatal("invalid startup definition was accepted")
			}
		})
	}
	if body, found, err := extractVerifierStartup("# no startup requirements\n"); err != nil || found || body != "" {
		t.Fatalf("task without startup section = %q, %t, %v", body, found, err)
	}
}

func TestPrepareTaskStartupUsesPinnedVerifierAsSingleSource(t *testing.T) {
	checkout := t.TempDir()
	task := "expr-try-catch-errors"
	verifier := `#!/bin/bash
# --- Service startup ---
custom-dependency --listen 127.0.0.1:4321 &
until custom-ready; do sleep 1; done
# --- Tests ---
hidden-test-command
`
	writeReadinessTaskFixture(t, checkout, task, "public.ecr.aws/d3j8x8q7/swe-bench-202605:kh71gkadwafw4ry4r6g37era0182qpms-v1.1", verifier)
	stage := t.TempDir()
	arguments, err := prepareTaskStartup(checkout, task, stage)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(arguments, " ")
	for _, required := range []string{
		pierEnvironmentImportPath,
		taskEnvironmentClass,
		pierEnvironmentKwarg,
		"readiness_script=" + filepath.Join(stage, "task-readiness.sh"),
		"pinned_image=public.ecr.aws/d3j8x8q7/swe-bench-202605:kh71gkadwafw4ry4r6g37era0182qpms-v1.1@sha256:8fc7209012a27d12aeaa96f4bd30579442c2817c64bca45bf10e634950892486",
		"verifier_source_root=" + filepath.Join(checkout, "tasks", task, "tests"),
		"verifier_context=" + filepath.Join(stage, "verifier-context"),
		"startup_script=" + filepath.Join(stage, "task-startup.sh"),
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("Pier startup arguments omitted %q: %#v", required, arguments)
		}
	}
	script, err := os.ReadFile(filepath.Join(stage, "task-startup.sh")) // #nosec G304 -- test-owned temporary path.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), "custom-dependency --listen 127.0.0.1:4321 &") ||
		strings.Contains(string(script), "hidden-test-command") {
		t.Fatalf("derived startup program =\n%s", script)
	}
	adapter, err := os.ReadFile(filepath.Join(stage, "pier_task_environment.py")) // #nosec G304 -- test-owned temporary path.
	if err != nil {
		t.Fatal(err)
	}
	if string(adapter) != string(pierTaskEnvironment) {
		t.Fatal("materialized Pier environment adapter differs from the embedded asset")
	}
	verifierDockerfile, err := os.ReadFile(filepath.Join(stage, "verifier-context", "Dockerfile")) // #nosec G304 -- test-owned temporary path.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(verifierDockerfile), "FROM public.ecr.aws/d3j8x8q7/swe-bench-202605:kh71gkadwafw4ry4r6g37era0182qpms-v1.1@sha256:8fc7209012a27d12aeaa96f4bd30579442c2817c64bca45bf10e634950892486") {
		t.Fatalf("verifier Dockerfile did not pin the certified base image:\n%s", verifierDockerfile)
	}
}

func TestPrepareVerifierBuildContextPreservesGoBuildEventsBeforeReporterFilter(t *testing.T) {
	source := t.TempDir()
	image := "example/verifier:v1"
	if err := os.WriteFile(filepath.Join(source, "Dockerfile"), []byte("FROM "+image+"\nCOPY test.sh /tests/test.sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	verifier := `#!/bin/bash
export RUN_LOG=/logs/verifier/run.log
set +e
go test -json ./... 2>>"$RUN_LOG" \
  | grep -v '"Action":"build-' \
  | tee -a "$RUN_LOG" | go-ctrf-json-reporter
set -e
`
	if err := os.WriteFile(filepath.Join(source, "test.sh"), []byte(verifier), 0o600); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := prepareVerifierBuildContext(source, target, image, image+"@sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	prepared, err := os.ReadFile(filepath.Join(target, "test.sh")) // #nosec G304 -- target is the test-owned temporary verifier context.
	if err != nil {
		t.Fatal(err)
	}
	text := string(prepared)
	buildTee := "tee -a /logs/verifier/build-events.jsonl"
	if strings.Count(text, buildTee) != 1 || strings.Index(text, buildTee) > strings.Index(text, `grep -v '"Action":"build-'`) {
		t.Fatalf("build evidence was not preserved before filtering:\n%s", text)
	}
	if !strings.Contains(text, ": > /logs/verifier/build-events.jsonl") {
		t.Fatalf("build evidence was not reset per verifier attempt:\n%s", text)
	}
}

func TestInstrumentVerifierBuildEvidenceSupportsPinnedGoPipelineForms(t *testing.T) {
	forms := map[string]string{
		"multiline with multiple set toggles": `set +e
go build ./...
set -e
set +e
go test -json ./... \
  | grep -v '"Action":"build-' \
  | tee -a "$RUN_LOG"
`,
		"same line": `set +e
go test -json ./... | grep -v '"Action":"build-' | tee -a "$RUN_LOG" | go-ctrf-json-reporter
`,
		"brace group and two suites": `set +e
{ go test -json ./...; } | grep -v '"Action":"build-' \
  | tee -a "$RUN_LOG"
go test -json ./other/... | grep -v '"Action":"build-' | tee -a "$RUN_LOG"
`,
	}
	for name, pipeline := range forms {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.sh")
			original := "#!/bin/bash\nexport RUN_LOG=/logs/verifier/run.log\n" + pipeline
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := instrumentVerifierBuildEvidence(path); err != nil {
				t.Fatal(err)
			}
			prepared, err := os.ReadFile(path) // #nosec G304 -- test-owned temporary path.
			if err != nil {
				t.Fatal(err)
			}
			text := string(prepared)
			filters := strings.Count(original, `grep -v '"Action":"build-'`)
			if got := strings.Count(text, "tee -a /logs/verifier/build-events.jsonl"); got != filters {
				t.Fatalf("instrumented streams = %d, want %d:\n%s", got, filters, text)
			}
			if !strings.Contains(text, "export RUN_LOG=/logs/verifier/run.log\n: > /logs/verifier/build-events.jsonl") {
				t.Fatalf("build evidence reset is not anchored to run-log initialization:\n%s", text)
			}
		})
	}
}

func TestPrepareTaskStartupAlwaysCertifiesTaskWithoutExposingVerifier(t *testing.T) {
	checkout := t.TempDir()
	task := "expr-try-catch-errors"
	writeReadinessTaskFixture(t, checkout, task, "public.ecr.aws/d3j8x8q7/swe-bench-202605:kh71gkadwafw4ry4r6g37era0182qpms-v1.1", "#!/bin/bash\nhidden-verifier-command\n")
	stage := t.TempDir()
	arguments, err := prepareTaskStartup(checkout, task, stage)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(arguments, " ")
	if !strings.Contains(joined, "readiness_script=") || !strings.Contains(joined, "pinned_image=") {
		t.Fatalf("task certification arguments = %#v", arguments)
	}
	if _, err := os.Stat(filepath.Join(stage, "task-startup.sh")); !os.IsNotExist(err) {
		t.Fatalf("plain task materialized startup program: %v", err)
	}
	readiness, err := os.ReadFile(filepath.Join(stage, "task-readiness.sh")) // #nosec G304 -- test-owned temporary path.
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(readiness), "hidden-verifier-command") {
		t.Fatal("task readiness program exposed verifier code")
	}
}

func TestValidatePlanTaskStartupsChecksEveryTaskWithoutExecuting(t *testing.T) {
	checkout := t.TempDir()
	marker := filepath.Join(checkout, "startup-executed")
	writeVerifier := func(task, image, body string) {
		t.Helper()
		writeReadinessTaskFixture(t, checkout, task, image, body)
	}
	writeVerifier("expr-try-catch-errors", "public.ecr.aws/d3j8x8q7/swe-bench-202605:kh71gkadwafw4ry4r6g37era0182qpms-v1.1", `# --- Service startup ---
touch `+marker+`
# --- Tests ---
run-tests
`)
	writeVerifier("testem-bail-on-test-failure", "public.ecr.aws/d3j8x8q7/swe-bench-202605:kh77k18d31qx7jj0c7nyv0xd8s82cznp-v1.1", `# --- Service startup ---
unterminated
`)

	err := validatePlanTaskStartups(checkout, []benchmarkPlanTask{
		{ID: "expr-try-catch-errors"},
		{ID: "testem-bail-on-test-failure"},
	})
	if err == nil || !strings.Contains(err.Error(), "testem-bail-on-test-failure") {
		t.Fatalf("startup validation error = %v", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("startup validation executed task commands: %v", statErr)
	}
}
