package benchmark

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installFakePier puts a `uvx` stand-in on PATH that records the exact command
// line it was invoked with and materializes the job output a real Pier run
// would leave behind, so an execution can be driven end to end without paying
// a provider.
func installFakePier(t *testing.T, checksum string) string {
	t.Helper()
	binDir := t.TempDir()
	argvPath := filepath.Join(binDir, "argv.txt")
	script := `#!/bin/sh
: > "` + argvPath + `"
jobs_dir=""
job_name=""
while [ "$#" -gt 0 ]; do
  printf '%s\n' "$1" >> "` + argvPath + `"
  case "$1" in
    --jobs-dir) jobs_dir="$2" ;;
    --job-name) job_name="$2" ;;
  esac
  shift
done
job="$jobs_dir/$job_name"
mkdir -p "$job/verifier" "$job/artifacts"
printf 'diff --git a/a b/a\n' > "$job/artifacts/model.patch"
printf 'verifier passed\n' > "$job/verifier/run.log"
cat > "$job/result.json" <<'RESULT'
{
  "task_checksum": "` + checksum + `",
  "started_at": "2026-07-27T15:00:00Z",
  "finished_at": "2026-07-27T15:04:00Z",
  "agent_info": {"model_info": {"provider": "anthropic"}},
  "agent_result": {"cost_usd": 2.5},
  "verifier_result": {"rewards": {"reward": 0.8, "f2p_total": 10, "f2p_passed": 8, "f2p": 0.8, "partial": 0.8}},
  "exception_info": null
}
RESULT
`
	path := filepath.Join(binDir, commandUVX)
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // #nosec G306 -- the fake Pier runner must be executable.
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argvPath
}

func fakePierArgv(t *testing.T, argvPath string) []string {
	t.Helper()
	data, err := os.ReadFile(argvPath) // #nosec G304 -- test-owned path.
	if err != nil {
		t.Fatalf("the fake Pier runner was never invoked: %v", err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// stubPinnedCheckout points benchmark execution at a fixed checkout path.
//
// This replaces a package-level function variable and restores it with
// t.Cleanup, so every test in this package must run sequentially. Do not call
// t.Parallel() in a test that uses this helper, or in any test that could run
// beside one: a parallel test would observe another test's stub.
func stubPinnedCheckout(t *testing.T, checkout string) {
	t.Helper()
	original := ensurePinnedBenchmarkCheckout
	ensurePinnedBenchmarkCheckout = func(context.Context, string) (string, error) { return checkout, nil }
	t.Cleanup(func() { ensurePinnedBenchmarkCheckout = original })
}

func argvContainsPair(argv []string, flag, value string) bool {
	for index := range argv {
		if argv[index] == flag && index+1 < len(argv) && argv[index+1] == value {
			return true
		}
	}
	return false
}

// TestPierExecutionPinsTheRunAndNormalizesItsEvidence covers the boundary where
// money is spent: every paid Pier run must be pinned to the checked-out task
// set and the requested model, must run the requested arm's agent, and must
// come back as validated, sanitized evidence rather than raw output. A wrong
// flag here silently measures something other than what the campaign claims.
func TestPierExecutionPinsTheRunAndNormalizesItsEvidence(t *testing.T) {
	repository := t.TempDir()
	checkout := filepath.Join(repository, "pinned-checkout")
	stubPinnedCheckout(t, checkout)

	argvPath := installFakePier(t, "task-checksum")
	model, effort, err := ParseModelSelection("fable:high")
	if err != nil {
		t.Fatal(err)
	}
	request := ExecutionRequest{
		RepoRoot:     repository,
		EvidenceDir:  filepath.Join(repository, "evidence"),
		EventID:      "event-one",
		Attempt:      1,
		Task:         "example-task",
		Model:        model,
		Effort:       effort,
		Arm:          ArmBaseline,
		TaskChecksum: "task-checksum",
	}

	result, err := PierExecutor{}.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("baseline execution failed: %v", err)
	}

	argv := fakePierArgv(t, argvPath)
	for _, pair := range [][2]string{
		{"--from", "datacurve-pier==" + PierVersion},
		{"--path", filepath.Join(checkout, "tasks")},
		{"--model", model.RuntimeIdentifier},
		{"--include-task-name", "example-task"},
		{"--job-name", "event-one"},
		{"--n-attempts", "1"},
		{"--max-retries", "0"},
		{"--agent", model.Adapter},
		{pierAgentKwarg, "reasoning_effort=" + effort},
		{pierAgentKwarg, "version=" + model.ProviderClientVersion},
	} {
		if !argvContainsPair(argv, pair[0], pair[1]) {
			t.Fatalf("paid run did not pin %s %s; argv = %#v", pair[0], pair[1], argv)
		}
	}

	if result.Status != statusSuccess || result.F2PScore != .8 || *result.CostUSD != 2.5 ||
		result.Provider != "anthropic" || result.TaskChecksum != "task-checksum" ||
		result.PatchBytes == 0 || result.VerifierBuildFailed {
		t.Fatalf("normalized result = %#v", result)
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("execution returned evidence that does not validate: %v", err)
	}
	promoted := filepath.Join(request.EvidenceDir, "attempts", "1", "tasks", "example-task", "artifacts", "event-one")
	for _, artifact := range []string{
		filepath.Join(promoted, "pier-command.log"),
		filepath.Join(promoted, "jobs", "event-one", "result.json"),
	} {
		if _, err := os.Stat(artifact); err != nil {
			t.Fatalf("execution did not promote %s: %v", artifact, err)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(repository, ".agent-layer", "tmp")); err != nil || len(entries) != 0 {
		t.Fatalf("execution left its staging directory behind: %#v, %v", entries, err)
	}
}

// TestPierTreatmentExecutionRunsTheImmutableBundle covers the treatment arm's
// distinguishing contract: the run must load the Agent Layer adapter and carry
// the bundle's identity and execution policy, because that bundle is the only
// thing separating the treatment from the baseline it is compared against.
func TestPierTreatmentExecutionRunsTheImmutableBundle(t *testing.T) {
	repository := t.TempDir()
	credentials := filepath.Join(repository, ".claude-config", ".credentials.json")
	if err := os.MkdirAll(filepath.Dir(credentials), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentials, []byte(`{"accessToken":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stubPinnedCheckout(t, filepath.Join(repository, "pinned-checkout"))

	argvPath := installFakePier(t, "task-checksum")
	model, effort, err := ParseModelSelection("fable:high")
	if err != nil {
		t.Fatal(err)
	}
	bundle := &TreatmentBundle{
		Root:        filepath.Join(repository, "bundle"),
		AdapterPath: filepath.Join(repository, "bundle", "pier_agent_layer", "__init__.py"),
		Manifest: TreatmentManifest{
			Mode:                   TreatmentInstructionsAndSkills,
			RequiredRoles:          []string{requiredRolePlanReviewer, requiredRoleImplementer},
			AgentTimeoutMultiplier: skillsAgentTimeoutFactor,
		},
	}
	request := ExecutionRequest{
		RepoRoot:     repository,
		EvidenceDir:  filepath.Join(repository, "evidence"),
		EventID:      "event-two",
		Attempt:      1,
		Task:         "example-task",
		Model:        model,
		Effort:       effort,
		Arm:          ArmTreatment,
		Bundle:       bundle,
		TaskChecksum: "task-checksum",
	}

	result, err := PierExecutor{}.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("treatment execution failed: %v", err)
	}
	// Treatment cost is accounted per component so a dispatched child's spend
	// cannot be hidden inside the coordinator's reported total.
	if result.CostKind != costKindProviderTotal || *result.CoordinatorCostUSD != 2.5 ||
		*result.ChildCostUSD != 0 || *result.CostUSD != 2.5 {
		t.Fatalf("treatment cost accounting = %#v", result)
	}
	// The fake run performs no dispatch, so the required roles are unmet and the
	// run must be reported as non-conformant instead of silently counting.
	if result.DispatchConformant {
		t.Fatal("a run with no dispatch lifecycle was reported as dispatch-conformant")
	}

	argv := fakePierArgv(t, argvPath)
	for _, pair := range [][2]string{
		{"--agent-import-path", "pier_agent_layer:AgentLayerClaudeCode"},
		{"--agent-timeout-multiplier", "4"},
		{pierAgentKwarg, "treatment_bundle=" + bundle.Root},
		{pierAgentKwarg, "treatment_mode=" + TreatmentInstructionsAndSkills},
		{pierAgentKwarg, "required_dispatch_roles=" + requiredRolePlanReviewer + "," + requiredRoleImplementer},
		{pierAgentKwarg, "claude_credentials_path=" + credentials},
	} {
		if !argvContainsPair(argv, pair[0], pair[1]) {
			t.Fatalf("treatment run did not pin %s %s; argv = %#v", pair[0], pair[1], argv)
		}
	}
	for _, argument := range argv {
		if argument == "--agent" {
			t.Fatalf("treatment run fell back to the baseline agent; argv = %#v", argv)
		}
	}
}

// TestPierExecutionRejectsIncompleteRequests covers the request contract that
// runs before any provider is reached. Each case names the rejection it expects,
// so a case cannot pass because of unrelated setup, and the baseline request is
// asserted to clear this gate so no case can pass vacuously.
func TestPierExecutionRejectsIncompleteRequests(t *testing.T) {
	const invalidRequest = "invalid Pier execution request"
	valid := ExecutionRequest{
		RepoRoot: t.TempDir(), EvidenceDir: t.TempDir(), EventID: "event",
		Attempt: 1, Task: "example-task", Arm: ArmBaseline,
	}
	tests := []struct {
		name   string
		broken func(*ExecutionRequest)
		want   string
	}{
		{"no repository root", func(r *ExecutionRequest) { r.RepoRoot = "" }, invalidRequest},
		{"no evidence directory", func(r *ExecutionRequest) { r.EvidenceDir = "" }, invalidRequest},
		{"no event identity", func(r *ExecutionRequest) { r.EventID = "" }, invalidRequest},
		{"unnumbered attempt", func(r *ExecutionRequest) { r.Attempt = 0 }, invalidRequest},
		{"task is not a catalog identifier", func(r *ExecutionRequest) { r.Task = "../escape" }, invalidRequest},
		{"arm is neither baseline nor treatment", func(r *ExecutionRequest) { r.Arm = "control" }, invalidRequest},
		{"treatment without a bundle", func(r *ExecutionRequest) { r.Arm = ArmTreatment }, "requires an immutable treatment bundle"},
	}
	stubPinnedCheckout(t, "")
	// The fake runner keeps the baseline probe below offline; without it the
	// real `uvx` would be invoked and would try to resolve Pier from the network.
	installFakePier(t, "task-checksum")

	// The baseline request must clear request validation, otherwise every case
	// below would be rejected for the same reason no matter what it broke. It
	// still fails afterwards because it records no task checksum to match.
	if _, err := (PierExecutor{}).Execute(context.Background(), valid); err == nil ||
		strings.Contains(err.Error(), invalidRequest) {
		t.Fatalf("baseline request did not clear request validation: %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := valid
			test.broken(&request)
			_, err := PierExecutor{}.Execute(context.Background(), request)
			if err == nil {
				t.Fatal("a paid run was started from an incomplete request")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("rejection = %v, want it to name %q", err, test.want)
			}
		})
	}
}
