package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const authPreflightSecret = "super-secret-credential-token"

func TestGrokPaidCredentialLifetimeStopsBeforeInference(t *testing.T) {
	repository := t.TempDir()
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	write := func(expiry time.Time) {
		t.Helper()
		writeJSONCredential(t, filepath.Join(repository, ".grok-config", "auth.json"), []byte(
			`{"https://auth.x.ai::profile":{"expires_at":"`+expiry.Format(time.RFC3339)+`"}}`,
		))
	}
	write(now.Add(31 * time.Minute))
	if err := validateGrokPaidCredentialLifetime(repository, now); err != nil {
		t.Fatalf("fresh Grok credential rejected: %v", err)
	}
	write(now.Add(30 * time.Minute))
	if err := validateGrokPaidCredentialLifetime(repository, now); err == nil ||
		!strings.Contains(err.Error(), "expires too soon") || !strings.Contains(err.Error(), "grok login --device-code") {
		t.Fatalf("expiring Grok credential = %v", err)
	}
}

func TestCodexAuthenticationPreflightUsesRepoLocalStatus(t *testing.T) {
	repository := t.TempDir()
	path := writeJSONCredential(t, filepath.Join(repository, ".codex", "auth.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
	before, err := os.ReadFile(path) // #nosec G304 -- test-owned credential path.
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "unrelated-codex-home"))
	logDir := installAuthCommandStubs(t, successfulCodexStatusScript(), "")
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	selections := []parsedSelection{{model: model, effort: effort}, {model: model, effort: effortHigh}}
	evidence, err := validateAuthentication(context.Background(), repository, selections)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path) // #nosec G304 -- test-owned credential path.
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("credential bytes changed")
	}
	item, ok := evidence[adapterCodex]
	if !ok || item.Provider != adapterCodex || item.Check != codexLoginStatusCheck || item.AuthenticationMethod != codexAuthMethodChatGPT {
		t.Fatalf("evidence=%#v", evidence)
	}
	if item.VerifiedAt.Location() != time.UTC || item.VerifiedAt.IsZero() || time.Since(item.VerifiedAt) > time.Minute {
		t.Fatalf("verified_at=%v", item.VerifiedAt)
	}
	if item.Check == CodexClientVersion || strings.Contains(item.Check, CodexClientVersion) {
		t.Fatalf("host status check used in-container client pin: %#v", item)
	}
	if calls := readStubFile(t, logDir, "calls"); strings.Count(calls, "invoked") != 1 {
		t.Fatalf("codex invocations=%q", calls)
	}
	if args := readStubFile(t, logDir, "args"); args != "login\nstatus\n" {
		t.Fatalf("codex args=%q", args)
	}
	if home := strings.TrimSpace(readStubFile(t, logDir, "codex_home")); home != filepath.Join(repository, ".codex") {
		t.Fatalf("CODEX_HOME=%q", home)
	}
	if _, err := os.Stat(filepath.Join(logDir, "claude-calls")); !os.IsNotExist(err) {
		t.Fatalf("claude invoked: %v", err)
	}
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(payload)
	for _, leaked := range []string{authPreflightSecret, "Logged in using", CodexClientVersion} {
		if strings.Contains(serialized, leaked) {
			t.Fatalf("evidence leaked %q: %s", leaked, serialized)
		}
	}
}

func TestCodexAuthenticationPreflightNormalizesAPIKeyStatusWithoutSecrets(t *testing.T) {
	repository := t.TempDir()
	writeJSONCredential(t, filepath.Join(repository, ".codex", "auth.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
	logDir := installAuthCommandStubs(t, "printf 'Logged in using an API key - sk-proj-***LEAK\\n' >&2\nexit 0\n", "")
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := validateAuthentication(context.Background(), repository, []parsedSelection{{model: model, effort: effort}})
	if err != nil {
		t.Fatal(err)
	}
	item := evidence[adapterCodex]
	if item.AuthenticationMethod != codexAuthMethodAPIKey {
		t.Fatalf("method=%q", item.AuthenticationMethod)
	}
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "sk-proj") || strings.Contains(string(payload), "LEAK") || strings.Contains(string(payload), authPreflightSecret) {
		t.Fatalf("api key evidence leaked secrets: %s", payload)
	}
	if _, err := os.Stat(filepath.Join(logDir, "claude-calls")); !os.IsNotExist(err) {
		t.Fatalf("claude invoked: %v", err)
	}
}

func TestCodexAuthenticationPreflightFailuresStayPrivate(t *testing.T) {
	repository := t.TempDir()
	writeJSONCredential(t, filepath.Join(repository, ".codex", "auth.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	selection := []parsedSelection{{model: model, effort: effort}}
	for _, test := range []struct {
		name, script, want string
		timeout            bool
	}{
		{name: "unauthenticated", script: "printf 'Not logged in " + authPreflightSecret + "\\n' >&2\nexit 1\n", want: "command failed"},
		{name: "command failed", script: "printf 'Error checking login status: " + authPreflightSecret + "\\n' >&2\nexit 2\n", want: "command failed"},
		{name: "unrecognized", script: "printf 'unexpected status " + authPreflightSecret + "\\n' >&2\nexit 0\n", want: "unrecognized"},
		{name: "timeout", script: "sleep 5\nexit 0\n", want: "timed out", timeout: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			installAuthCommandStubs(t, test.script, "")
			ctx := context.Background()
			if test.timeout {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 200*time.Millisecond)
				defer cancel()
			}
			_, err := validateAuthentication(ctx, repository, selection)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v", err)
			}
			message := err.Error()
			for _, leaked := range []string{authPreflightSecret, "Not logged in", "unexpected status", "Error checking login status"} {
				if strings.Contains(message, leaked) {
					t.Fatalf("error leaked %q: %s", leaked, message)
				}
			}
		})
	}
}

func TestClaudeAuthenticationFailsClosedWithoutInvokingClient(t *testing.T) {
	repository := t.TempDir()
	writeJSONCredential(t, filepath.Join(repository, ".claude-config", ".credentials.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
	logDir := installAuthCommandStubs(t, successfulCodexStatusScript(), "printf 'claude-output "+authPreflightSecret+"\\n' >&2\nexit 0\n")
	model, effort, err := ParseModelSelection("opus:high")
	if err != nil {
		t.Fatal(err)
	}
	_, err = validateAuthentication(context.Background(), repository, []parsedSelection{{model: model, effort: effort}})
	if err == nil || !strings.Contains(err.Error(), "cannot be validated before task setup") {
		t.Fatalf("claude error=%v", err)
	}
	if strings.Contains(err.Error(), authPreflightSecret) {
		t.Fatalf("claude error leaked credential: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(logDir, "claude-calls")); !os.IsNotExist(statErr) {
		t.Fatalf("claude client invoked: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(logDir, "calls")); !os.IsNotExist(statErr) {
		t.Fatalf("codex invoked for claude-only selection: %v", statErr)
	}
}

func TestReplaceEnvValueReplacesWithoutDuplicateKeys(t *testing.T) {
	got := replaceEnvValue([]string{"PATH=/bin", "CODEX_HOME=/old", "CODEX_HOME=/other", "LANG=C"}, "CODEX_HOME", "/repo/.codex")
	count := 0
	for _, entry := range got {
		if strings.HasPrefix(entry, "CODEX_HOME=") {
			count++
			if entry != "CODEX_HOME=/repo/.codex" {
				t.Fatalf("entry=%q", entry)
			}
		}
	}
	if count != 1 || strings.Join(got, ",") != "PATH=/bin,LANG=C,CODEX_HOME=/repo/.codex" {
		t.Fatalf("env=%q", got)
	}
}

func TestRunStudyRejectsAuthenticationFailuresBeforeTaskSetup(t *testing.T) {
	t.Run("missing codex credentials", func(t *testing.T) {
		root := t.TempDir()
		preparedCalls := stubStudyInfrastructure(t, root)
		writeParsedBareStudy(t, root, "luna:low")
		installAuthCommandStubs(t, successfulCodexStatusScript(), "")
		_, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
		if err == nil || !strings.Contains(err.Error(), "must be a non-empty JSON file") {
			t.Fatalf("error=%v", err)
		}
		if *preparedCalls != 0 {
			t.Fatalf("task preparation ran %d times", *preparedCalls)
		}
	})
	t.Run("unauthenticated codex", func(t *testing.T) {
		root := t.TempDir()
		preparedCalls := stubStudyInfrastructure(t, root)
		writeParsedBareStudy(t, root, "luna:low")
		writeJSONCredential(t, filepath.Join(root, ".codex", "auth.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
		installAuthCommandStubs(t, "printf 'Not logged in "+authPreflightSecret+"\\n' >&2\nexit 1\n", "")
		_, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
		if err == nil || !strings.Contains(err.Error(), "command failed") {
			t.Fatalf("error=%v", err)
		}
		if strings.Contains(err.Error(), authPreflightSecret) || strings.Contains(err.Error(), "Not logged in") {
			t.Fatalf("error leaked provider output: %v", err)
		}
		if *preparedCalls != 0 {
			t.Fatalf("task preparation ran %d times", *preparedCalls)
		}
	})
	t.Run("claude fail closed", func(t *testing.T) {
		root := t.TempDir()
		preparedCalls := stubStudyInfrastructure(t, root)
		writeParsedBareStudy(t, root, "opus:high")
		writeJSONCredential(t, filepath.Join(root, ".claude-config", ".credentials.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
		logDir := installAuthCommandStubs(t, successfulCodexStatusScript(), "echo invoked\nexit 0\n")
		_, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
		if err == nil || !strings.Contains(err.Error(), "cannot be validated before task setup") {
			t.Fatalf("error=%v", err)
		}
		if *preparedCalls != 0 {
			t.Fatalf("task preparation ran %d times", *preparedCalls)
		}
		if _, statErr := os.Stat(filepath.Join(logDir, "claude-calls")); !os.IsNotExist(statErr) {
			t.Fatalf("claude client invoked: %v", statErr)
		}
	})
}

func TestRunStudyRecordsAuthenticationPreflightWithoutChangingIdentity(t *testing.T) {
	writeStudy := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		writeParsedBareStudy(t, root, "luna:low")
		writeJSONCredential(t, filepath.Join(root, ".codex", "auth.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
		stubStudyInfrastructure(t, root)
		return root
	}

	baselineRoot := writeStudy(t)
	originalAuth := validateBenchmarkAuthentication
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	baseline, err := RunStudy(context.Background(), StudyOptions{RepoRoot: baselineRoot, StudyPath: filepath.Join(baselineRoot, "study.toml"), DryRun: true}, &studyWorkflowExecutor{})
	validateBenchmarkAuthentication = originalAuth
	if err != nil {
		t.Fatal(err)
	}

	root := writeStudy(t)
	installAuthCommandStubs(t, successfulCodexStatusScript(), "")
	executor := &studyWorkflowExecutor{}
	outcome, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.StudyID != baseline.StudyID || len(outcome.Experiments) != 1 || outcome.Experiments[0].Identity != baseline.Experiments[0].Identity {
		t.Fatalf("identity changed: outcome=%#v baseline=%#v", outcome, baseline)
	}
	if outcome.JSONPath == "" {
		t.Fatal("missing canonical report")
	}
	raw, err := os.ReadFile(outcome.JSONPath)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(raw)
	if strings.Contains(serialized, authPreflightSecret) || strings.Contains(serialized, "Logged in using") {
		t.Fatalf("report leaked secrets: %s", serialized)
	}
	var report StudyReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Experiments) != 1 || report.Experiments[0].AuthenticationPreflight == nil {
		t.Fatalf("report experiments=%#v", report.Experiments)
	}
	item := *report.Experiments[0].AuthenticationPreflight
	if item.Provider != adapterCodex || item.Check != codexLoginStatusCheck || item.AuthenticationMethod != codexAuthMethodChatGPT || item.VerifiedAt.Location() != time.UTC {
		t.Fatalf("authentication_preflight=%#v", item)
	}
	if item.Check == CodexClientVersion || strings.Contains(serialized, `"check":"`+CodexClientVersion+`"`) {
		t.Fatalf("report used pinned client version as status check: %s", serialized)
	}
}

func TestRunStudyRegeneratesCompleteCachedStudiesWithoutAuthentication(t *testing.T) {
	for _, test := range []struct {
		name, selection string
	}{
		{name: adapterCodex, selection: "luna:low"},
		{name: providerClaude, selection: "opus:high"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, effort, err := ParseModelSelection(test.selection)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			writeBareStudy(t, root, model.Name, effort)
			preparedCalls := stubStudyInfrastructure(t, root)
			originalAuth := validateBenchmarkAuthentication
			t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
			validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
				return map[string]AuthenticationPreflight{}, nil
			}
			if model.Adapter == adapterCodex {
				writeJSONCredential(t, filepath.Join(root, ".codex", "auth.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
				installAuthCommandStubs(t, successfulCodexStatusScript(), "")
				validateBenchmarkAuthentication = originalAuth
			}
			first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
			if err != nil {
				t.Fatal(err)
			}
			if first.Completed != 2 || first.Missing != 0 || first.JSONPath == "" {
				t.Fatalf("seeded study=%#v", first)
			}
			if *preparedCalls != 1 {
				t.Fatalf("seeded task preparation calls=%d", *preparedCalls)
			}

			validateBenchmarkAuthentication = originalAuth
			preflightBenchmark = func([]parsedSelection) error {
				return fmt.Errorf("cached regeneration reached prerequisite discovery")
			}
			verifyBenchmarkPier = func(context.Context) error {
				return fmt.Errorf("cached regeneration reached Pier verification")
			}
			prepareBenchmarkTaskSet = func(context.Context, string, []benchmarkPlanTask) (map[string]string, map[string]string, error) {
				return nil, nil, fmt.Errorf("cached regeneration reached task preparation")
			}
			originalTreatment := preflightTreatmentRuntime
			preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error {
				return fmt.Errorf("cached regeneration reached treatment runtime preflight")
			}
			t.Cleanup(func() { preflightTreatmentRuntime = originalTreatment })
			switch model.Adapter {
			case adapterCodex:
				if err := os.Remove(filepath.Join(root, ".codex", "auth.json")); err != nil {
					t.Fatal(err)
				}
			case adapterClaudeCode:
				writeJSONCredential(t, filepath.Join(root, ".claude-config", ".credentials.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
			}

			second, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
			if err != nil {
				t.Fatal(err)
			}
			if *preparedCalls != 1 {
				t.Fatalf("cached regeneration prepared tasks %d times", *preparedCalls)
			}
			if second.StudyID != first.StudyID || second.Experiments[0].Identity != first.Experiments[0].Identity {
				t.Fatalf("cached identity changed: first=%#v second=%#v", first, second)
			}
			if second.Completed != 2 || second.Missing != 0 || second.JSONPath == "" {
				t.Fatalf("cached regeneration=%#v", second)
			}
			raw, err := os.ReadFile(second.JSONPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), authPreflightSecret) {
				t.Fatalf("regenerated report leaked credential: %s", raw)
			}
			var report StudyReport
			if err := json.Unmarshal(raw, &report); err != nil {
				t.Fatal(err)
			}
			if report.StudyID != first.StudyID || len(report.Experiments) != 1 || report.Experiments[0].CompletedCells != 2 {
				t.Fatalf("regenerated report=%#v", report)
			}
			if model.Adapter == adapterCodex {
				if report.Experiments[0].AuthenticationPreflight == nil ||
					report.Experiments[0].AuthenticationPreflight.AuthenticationMethod != codexAuthMethodChatGPT {
					t.Fatalf("cached regeneration lost authentication provenance: %#v", report.Experiments[0].AuthenticationPreflight)
				}
			} else if report.Experiments[0].AuthenticationPreflight != nil {
				t.Fatalf("Claude cached report invented authentication provenance: %#v", report.Experiments[0].AuthenticationPreflight)
			}
		})
	}
}

func TestRunStudyRejectsCorruptCachedEvidenceWithoutTaskSetup(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	var manifest immutableStudyManifest
	if err := readStudyJSON(filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", first.StudyID, "study-manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	corrupt := armResultPath(filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", first.StudyID, "arms", manifest.Arms[0].ID), "first-task", 1)
	if err := os.WriteFile(corrupt, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	validateBenchmarkAuthentication = validateAuthentication
	prepareBenchmarkTaskSet = func(context.Context, string, []benchmarkPlanTask) (map[string]string, map[string]string, error) {
		return nil, nil, fmt.Errorf("corrupt cached evidence reached task preparation")
	}
	if _, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{}); err == nil || !strings.Contains(err.Error(), "inspect immutable study cell") {
		t.Fatalf("corrupt cached error=%v", err)
	}
	if *preparedCalls != 1 {
		t.Fatalf("corrupt cached evidence prepared tasks %d times", *preparedCalls)
	}
}

func TestRunStudyRejectsMissingAndConflictingCachedArmManifests(t *testing.T) {
	for _, test := range []struct {
		name, want string
		mutate     func(*testing.T, string)
	}{
		{
			name: "missing",
			want: "missing its immutable manifest",
			mutate: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "conflicting",
			want: "conflicts with its immutable manifest",
			mutate: func(t *testing.T, path string) {
				t.Helper()
				var manifest studyArmManifest
				if err := readStudyJSON(path, &manifest); err != nil {
					t.Fatal(err)
				}
				manifest.SelectionID = "conflicting-selection"
				if err := writeJSON(path, manifest); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeParsedBareStudy(t, root, "luna:low")
			preparedCalls := stubStudyInfrastructure(t, root)
			originalAuth := validateBenchmarkAuthentication
			t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
			validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
				return map[string]AuthenticationPreflight{}, nil
			}
			first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
			if err != nil {
				t.Fatal(err)
			}
			armRoot := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", first.StudyID, "arms")
			entries, err := os.ReadDir(armRoot)
			if err != nil {
				t.Fatal(err)
			}
			armDir := ""
			for _, entry := range entries {
				if entry.IsDir() {
					if armDir != "" {
						t.Fatalf("multiple arm directories: %v", entries)
					}
					armDir = entry.Name()
				}
			}
			if armDir == "" {
				t.Fatalf("arm entries=%v", entries)
			}
			manifestPath := filepath.Join(armRoot, armDir, "manifest.json")
			test.mutate(t, manifestPath)
			_, statErr := os.Stat(manifestPath)

			validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
				return nil, fmt.Errorf("cached regeneration reached authentication")
			}
			prepareBenchmarkTaskSet = func(context.Context, string, []benchmarkPlanTask) (map[string]string, map[string]string, error) {
				return nil, nil, fmt.Errorf("cached regeneration reached task preparation")
			}
			if _, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{}); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v", err)
			}
			if *preparedCalls != 1 {
				t.Fatalf("task preparation ran %d times", *preparedCalls)
			}
			if _, err := os.Stat(manifestPath); (err == nil) != (statErr == nil) {
				t.Fatalf("arm manifest existence changed: before=%v after=%v", statErr, err)
			}
		})
	}
}

func TestRunStudyIgnoresUnrelatedMalformedReportsDuringCheapNarrowing(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", "unrelated")
	if err := os.MkdirAll(filepath.Join(unrelated, "report"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unrelated, "report", "report.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unrelated, "study-manifest.json"), []byte("also-not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return nil, fmt.Errorf("cached regeneration reached authentication")
	}
	prepareBenchmarkTaskSet = func(context.Context, string, []benchmarkPlanTask) (map[string]string, map[string]string, error) {
		return nil, nil, fmt.Errorf("cached regeneration reached task preparation")
	}
	second, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if *preparedCalls != 1 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
	if second.StudyID != first.StudyID || second.Completed != 2 || second.JSONPath == "" {
		t.Fatalf("cached regeneration=%#v", second)
	}
}

func TestRunStudyRejectsUnboundedHistoricalStudyInspection(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	studies := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies")
	for i := 0; i < maxHistoricalStudyDirectories+1; i++ {
		if err := os.MkdirAll(filepath.Join(studies, fmt.Sprintf("%04d", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return nil, fmt.Errorf("unbounded inspection reached authentication")
	}
	_, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("exceeded %d historical study directories", maxHistoricalStudyDirectories)) {
		t.Fatalf("error=%v", err)
	}
	if !strings.Contains(err.Error(), studies) || !strings.Contains(err.Error(), "remove unused study state") {
		t.Fatalf("error was not actionable: %v", err)
	}
	if *preparedCalls != 0 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
}

func TestRunStudyAuthenticatesBeforeTreatmentBundleStagingWhenNoCachedCandidate(t *testing.T) {
	root := t.TempDir()
	writeParsedTreatmentStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	originalStage := stageBenchmarkExperimentBundles
	t.Cleanup(func() { stageBenchmarkExperimentBundles = originalStage })
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		return nil, fmt.Errorf("bundle staging reached before authentication")
	}
	_, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err == nil || !strings.Contains(err.Error(), "must be a non-empty JSON file") {
		t.Fatalf("error=%v", err)
	}
	if strings.Contains(err.Error(), "bundle staging") {
		t.Fatalf("staged before authentication: %v", err)
	}
	if *preparedCalls != 0 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
}

func TestRunStudyStagesTreatmentBundlesAfterAuthenticationForMissingWork(t *testing.T) {
	root := t.TempDir()
	writeParsedTreatmentStudy(t, root, "luna:low")
	writeJSONCredential(t, filepath.Join(root, ".codex", "auth.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
	preparedCalls := stubStudyInfrastructure(t, root)
	order := []string{}
	originalAuth := validateBenchmarkAuthentication
	originalStage := stageBenchmarkExperimentBundles
	t.Cleanup(func() {
		validateBenchmarkAuthentication = originalAuth
		stageBenchmarkExperimentBundles = originalStage
	})
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		order = append(order, "auth")
		return map[string]AuthenticationPreflight{
			adapterCodex: {Provider: adapterCodex, Check: codexLoginStatusCheck, AuthenticationMethod: codexAuthMethodChatGPT, VerifiedAt: time.Now().UTC()},
		}, nil
	}
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		order = append(order, "stage")
		return nil, fmt.Errorf("stop after staging observation")
	}
	_, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err == nil || !strings.Contains(err.Error(), "stop after staging observation") {
		t.Fatalf("error=%v", err)
	}
	if strings.Join(order, ",") != "auth,stage" {
		t.Fatalf("order=%q", order)
	}
	if *preparedCalls != 0 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
}

func TestRunStudyRegeneratesCompleteTreatmentStudyWithoutAuthentication(t *testing.T) {
	root := t.TempDir()
	writeParsedTreatmentStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	stubFakeStudyTreatmentBundles(t)
	originalTreatment := preflightTreatmentRuntime
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error { return nil }
	t.Cleanup(func() { preflightTreatmentRuntime = originalTreatment })
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Completed != 2 || first.Missing != 0 {
		t.Fatalf("seeded study=%#v", first)
	}
	if *preparedCalls != 1 {
		t.Fatalf("seeded task preparation calls=%d", *preparedCalls)
	}

	stageCalls := 0
	originalStage := stageBenchmarkExperimentBundles
	bundle := fakeStudyTreatmentBundle()
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		stageCalls++
		return []*TreatmentBundle{bundle}, nil
	}
	t.Cleanup(func() { stageBenchmarkExperimentBundles = originalStage })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return nil, fmt.Errorf("cached regeneration reached authentication")
	}
	preflightBenchmark = func([]parsedSelection) error {
		return fmt.Errorf("cached regeneration reached prerequisite discovery")
	}
	verifyBenchmarkPier = func(context.Context) error {
		return fmt.Errorf("cached regeneration reached Pier verification")
	}
	prepareBenchmarkTaskSet = func(context.Context, string, []benchmarkPlanTask) (map[string]string, map[string]string, error) {
		return nil, nil, fmt.Errorf("cached regeneration reached task preparation")
	}
	second, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if stageCalls != 1 {
		t.Fatalf("treatment cache verification staged %d times", stageCalls)
	}
	if *preparedCalls != 1 {
		t.Fatalf("cached regeneration prepared tasks %d times", *preparedCalls)
	}
	if second.StudyID != first.StudyID || second.Completed != 2 || second.JSONPath == "" {
		t.Fatalf("cached regeneration=%#v", second)
	}
}

func TestRunStudyDoesNotUseCachedPathForIncompleteReports(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	prepared, err := prepareStudy(StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")})
	if err != nil {
		t.Fatal(err)
	}
	prepared.cleanupInputs()
	writeMatchingStudyReport(t, root, "incomplete", prepared, 0, 2)

	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return nil, fmt.Errorf("missing-cell run reached authentication")
	}
	_, err = RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err == nil || !strings.Contains(err.Error(), "missing-cell run reached authentication") {
		t.Fatalf("error=%v", err)
	}
	if *preparedCalls != 0 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
}

func TestRunStudyFailsClosedWhenCompletedReportLacksStudyManifest(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	prepared, err := prepareStudy(StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")})
	if err != nil {
		t.Fatal(err)
	}
	prepared.cleanupInputs()
	writeMatchingStudyReport(t, root, "complete-without-manifest", prepared, 2, 2)

	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return nil, fmt.Errorf("corrupt cached candidate reached authentication")
	}
	_, err = RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err == nil || !strings.Contains(err.Error(), "missing its immutable manifest") {
		t.Fatalf("error=%v", err)
	}
	if *preparedCalls != 0 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
}

func TestRunStudyRegeneratesCompletedStudyWithoutReportOrAuthentication(t *testing.T) {
	for _, test := range []struct {
		name, selection string
	}{
		{name: adapterCodex, selection: "luna:low"},
		{name: providerClaude, selection: "opus:high"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, effort, err := ParseModelSelection(test.selection)
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			writeBareStudy(t, root, model.Name, effort)
			preparedCalls := stubStudyInfrastructure(t, root)
			originalAuth := validateBenchmarkAuthentication
			t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
			validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
				return map[string]AuthenticationPreflight{}, nil
			}
			if model.Adapter == adapterCodex {
				writeJSONCredential(t, filepath.Join(root, ".codex", "auth.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
				installAuthCommandStubs(t, successfulCodexStatusScript(), "")
				validateBenchmarkAuthentication = originalAuth
			}
			first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
			if err != nil {
				t.Fatal(err)
			}
			if first.Completed != 2 || first.Missing != 0 || first.JSONPath == "" {
				t.Fatalf("seeded study=%#v", first)
			}
			removeStudyReport(t, root, first.StudyID)

			blockCachedStudySidecars(t)
			switch model.Adapter {
			case adapterCodex:
				if err := os.Remove(filepath.Join(root, ".codex", "auth.json")); err != nil {
					t.Fatal(err)
				}
			case adapterClaudeCode:
				writeJSONCredential(t, filepath.Join(root, ".claude-config", ".credentials.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
			}
			executor := &studyWorkflowExecutor{}
			second, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, executor)
			if err != nil {
				t.Fatal(err)
			}
			if *preparedCalls != 1 {
				t.Fatalf("cached regeneration prepared tasks %d times", *preparedCalls)
			}
			if calls := executor.requests(); len(calls) != 0 {
				t.Fatalf("cached regeneration reached provider execution: %#v", calls)
			}
			if second.StudyID != first.StudyID || second.Completed != 2 || second.Missing != 0 || second.JSONPath == "" {
				t.Fatalf("cached regeneration=%#v", second)
			}
			raw, err := os.ReadFile(second.JSONPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), authPreflightSecret) {
				t.Fatalf("regenerated report leaked credential: %s", raw)
			}
			var report StudyReport
			if err := json.Unmarshal(raw, &report); err != nil {
				t.Fatal(err)
			}
			if report.StudyID != first.StudyID || len(report.Experiments) != 1 || report.Experiments[0].CompletedCells != 2 {
				t.Fatalf("regenerated report=%#v", report)
			}
			if report.Experiments[0].AuthenticationPreflight != nil {
				t.Fatalf("regenerated report invented authentication provenance: %#v", report.Experiments[0].AuthenticationPreflight)
			}
		})
	}
}

func TestRunStudyRetainsAuthenticationProvenanceFromIncompletePriorReport(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	writeJSONCredential(t, filepath.Join(root, ".codex", "auth.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
	installAuthCommandStubs(t, successfulCodexStatusScript(), "")
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	var prior StudyReport
	if err := readStudyJSON(first.JSONPath, &prior); err != nil {
		t.Fatal(err)
	}
	if len(prior.Experiments) != 1 || prior.Experiments[0].AuthenticationPreflight == nil {
		t.Fatalf("seeded report lacks authentication provenance: %#v", prior.Experiments)
	}
	prior.Experiments[0].CompletedCells--
	if err := writeJSON(first.JSONPath, prior); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".codex", "auth.json")); err != nil {
		t.Fatal(err)
	}

	blockCachedStudySidecars(t)
	second, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if *preparedCalls != 1 || second.StudyID != first.StudyID || second.Completed != 2 {
		t.Fatalf("cached regeneration=%#v task preparation calls=%d", second, *preparedCalls)
	}
	var regenerated StudyReport
	if err := readStudyJSON(second.JSONPath, &regenerated); err != nil {
		t.Fatal(err)
	}
	if len(regenerated.Experiments) != 1 || regenerated.Experiments[0].AuthenticationPreflight == nil ||
		regenerated.Experiments[0].AuthenticationPreflight.AuthenticationMethod != codexAuthMethodChatGPT {
		t.Fatalf("regenerated report lost authentication provenance: %#v", regenerated.Experiments)
	}
}

func TestRunStudyRejectsAuthenticationProvenanceFromUnrelatedPriorReport(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	stubStudyInfrastructure(t, root)
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	writeJSONCredential(t, filepath.Join(root, ".codex", "auth.json"), []byte(`{"token":"`+authPreflightSecret+`"}`))
	installAuthCommandStubs(t, successfulCodexStatusScript(), "")
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	var prior StudyReport
	if err := readStudyJSON(first.JSONPath, &prior); err != nil {
		t.Fatal(err)
	}
	prior.Experiments[0].Identity = "unrelated-experiment"
	prior.Experiments[0].CompletedCells--
	if err := writeJSON(first.JSONPath, prior); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ".codex", "auth.json")); err != nil {
		t.Fatal(err)
	}

	blockCachedStudySidecars(t)
	_, err = RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err == nil || !strings.Contains(err.Error(), "report declaration does not match its immutable study") {
		t.Fatalf("error=%v", err)
	}
}

func TestRunStudyRegeneratesCompletedTreatmentStudyWithoutReport(t *testing.T) {
	root := t.TempDir()
	writeParsedTreatmentStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	bundle := fakeStudyTreatmentBundle()
	bundle.TemplatesCommit = "current-templates"
	originalStage := stageBenchmarkExperimentBundles
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		return []*TreatmentBundle{bundle}, nil
	}
	t.Cleanup(func() { stageBenchmarkExperimentBundles = originalStage })
	originalTreatment := preflightTreatmentRuntime
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error { return nil }
	t.Cleanup(func() { preflightTreatmentRuntime = originalTreatment })
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Completed != 2 || first.Missing != 0 {
		t.Fatalf("seeded study=%#v", first)
	}
	removeStudyReport(t, root, first.StudyID)

	stageCalls := 0
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		stageCalls++
		return []*TreatmentBundle{bundle}, nil
	}
	blockCachedStudySidecars(t)
	executor := &studyWorkflowExecutor{}
	second, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if stageCalls != 1 {
		t.Fatalf("treatment cache verification staged %d times", stageCalls)
	}
	if *preparedCalls != 1 {
		t.Fatalf("cached regeneration prepared tasks %d times", *preparedCalls)
	}
	if calls := executor.requests(); len(calls) != 0 {
		t.Fatalf("cached regeneration reached provider execution: %#v", calls)
	}
	if second.StudyID != first.StudyID || second.Completed != 2 || second.JSONPath == "" {
		t.Fatalf("cached regeneration=%#v", second)
	}
	var report StudyReport
	if err := readStudyJSON(second.JSONPath, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Experiments) != 1 || report.Experiments[0].AuthenticationPreflight != nil {
		t.Fatalf("regenerated report invented authentication provenance: %#v", report.Experiments)
	}
	item := report.Experiments[0]
	if item.LinuxBinarySHA256 != bundle.LinuxBinarySHA256 || item.AdapterSHA256 != bundle.AdapterSHA256 ||
		item.SourceCommit != bundle.TemplatesCommit || item.BundleManifest == nil || item.BundleManifest.Mode != bundle.Manifest.Mode {
		t.Fatalf("regenerated report lost current staged bundle provenance: %#v", item)
	}
}

func TestRunStudySkipsDeclarationCompatibleSiblingWithDifferentIdentity(t *testing.T) {
	root := t.TempDir()
	writeParsedTreatmentStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	stubFakeStudyTreatmentBundles(t)
	originalTreatment := preflightTreatmentRuntime
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error { return nil }
	t.Cleanup(func() { preflightTreatmentRuntime = originalTreatment })
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	removeStudyReport(t, root, first.StudyID)
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte("changed-config\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return nil, fmt.Errorf("declaration-compatible sibling reached authentication")
	}
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		return nil, fmt.Errorf("declaration-compatible sibling reached bundle staging")
	}
	_, err = RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err == nil || !strings.Contains(err.Error(), "declaration-compatible sibling reached authentication") {
		t.Fatalf("error=%v", err)
	}
	if strings.Contains(err.Error(), "more than one historical state directory") || strings.Contains(err.Error(), "conflicts with its immutable manifest") {
		t.Fatalf("sibling was not skipped: %v", err)
	}
	if *preparedCalls != 1 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
}

func TestRunStudyFallsThroughWhenCurrentTreatmentPinsDoNotMatch(t *testing.T) {
	root := t.TempDir()
	writeParsedTreatmentStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	bundle := fakeStudyTreatmentBundle()
	originalStage := stageBenchmarkExperimentBundles
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		return []*TreatmentBundle{bundle}, nil
	}
	t.Cleanup(func() { stageBenchmarkExperimentBundles = originalStage })
	originalTreatment := preflightTreatmentRuntime
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error { return nil }
	t.Cleanup(func() { preflightTreatmentRuntime = originalTreatment })
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	removeStudyReport(t, root, first.StudyID)

	mismatched := *bundle
	mismatched.ManifestHash = "other-manifest-hash"
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		return []*TreatmentBundle{&mismatched}, nil
	}
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return nil, fmt.Errorf("current pin mismatch reached authentication")
	}
	_, err = RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err == nil || !strings.Contains(err.Error(), "current pin mismatch reached authentication") {
		t.Fatalf("error=%v", err)
	}
	if strings.Contains(err.Error(), "execution receipt does not match") || strings.Contains(err.Error(), "inspect immutable study cell") {
		t.Fatalf("pin mismatch was treated as corruption: %v", err)
	}
	if *preparedCalls != 1 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
}

func TestRunStudyAuthenticatesBeforeStagingIncompleteTreatmentWithoutReport(t *testing.T) {
	root := t.TempDir()
	writeParsedTreatmentStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	stubFakeStudyTreatmentBundles(t)
	originalTreatment := preflightTreatmentRuntime
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error { return nil }
	t.Cleanup(func() { preflightTreatmentRuntime = originalTreatment })
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	if _, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), DryRun: true}, &studyWorkflowExecutor{}); err != nil {
		t.Fatal(err)
	}

	order := []string{}
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		order = append(order, "auth")
		return map[string]AuthenticationPreflight{
			adapterCodex: {Provider: adapterCodex, Check: codexLoginStatusCheck, AuthenticationMethod: codexAuthMethodChatGPT, VerifiedAt: time.Now().UTC()},
		}, nil
	}
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		order = append(order, "stage")
		return nil, fmt.Errorf("stop after staging observation")
	}
	_, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err == nil || !strings.Contains(err.Error(), "stop after staging observation") {
		t.Fatalf("error=%v", err)
	}
	if strings.Join(order, ",") != "auth,stage" {
		t.Fatalf("order=%q", order)
	}
	if *preparedCalls != 1 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
}

func TestRunStudyRejectsCorruptManifestOnlyEvidenceWithoutTaskSetup(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	var manifest immutableStudyManifest
	if err := readStudyJSON(filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", first.StudyID, "study-manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	corrupt := armResultPath(filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", first.StudyID, "arms", manifest.Arms[0].ID), "first-task", 1)
	if err := os.WriteFile(corrupt, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	removeStudyReport(t, root, first.StudyID)

	blockCachedStudySidecars(t)
	if _, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{}); err == nil || !strings.Contains(err.Error(), "inspect immutable study cell") {
		t.Fatalf("corrupt cached error=%v", err)
	}
	if *preparedCalls != 1 {
		t.Fatalf("corrupt cached evidence prepared tasks %d times", *preparedCalls)
	}
}

func TestRunStudyAuthenticatesWhenManifestOnlyCellsAreMissing(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	var manifest immutableStudyManifest
	if err := readStudyJSON(filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", first.StudyID, "study-manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	missing := armResultPath(filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", first.StudyID, "arms", manifest.Arms[0].ID), "first-task", 1)
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	removeStudyReport(t, root, first.StudyID)

	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return nil, fmt.Errorf("missing-cell run reached authentication")
	}
	originalStage := stageBenchmarkExperimentBundles
	t.Cleanup(func() { stageBenchmarkExperimentBundles = originalStage })
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		return nil, fmt.Errorf("missing-cell run reached bundle staging")
	}
	_, err = RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err == nil || !strings.Contains(err.Error(), "missing-cell run reached authentication") {
		t.Fatalf("error=%v", err)
	}
	if strings.Contains(err.Error(), "missing required cell evidence") {
		t.Fatalf("missing cells were treated as completed-report corruption: %v", err)
	}
	if *preparedCalls != 1 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
}

func TestManifestOnlyCachedStudyCompleteDoesNotMutateCallerStudyID(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	stubStudyInfrastructure(t, root)
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	removeStudyReport(t, root, first.StudyID)
	studies := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies")
	rejected := filepath.Join(studies, "rejected-identity")
	if err := os.Rename(filepath.Join(studies, first.StudyID), rejected); err != nil {
		t.Fatal(err)
	}

	prepared, err := prepareStudy(StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.cleanupInputs()
	prepared.studyID = "sentinel-study-id"
	tasks := make([]benchmarkPlanTask, len(prepared.selection.Tasks))
	for i, task := range prepared.selection.Tasks {
		tasks[i] = benchmarkPlanTask{ID: task.ID, RepetitionsPerArm: task.Repetitions}
	}
	complete, err := manifestOnlyCachedStudyComplete(root, &prepared, rejected, tasks, 1)
	if err != nil {
		t.Fatal(err)
	}
	if complete {
		t.Fatal("identity-mismatched candidate was treated as complete")
	}
	if prepared.studyID != "sentinel-study-id" {
		t.Fatalf("rejected probe mutated studyID to %q", prepared.studyID)
	}
}

func TestRecoveryOnlyRegeneratesCompletedHistoricalTreatmentReportWithoutRestagingCurrentInputs(t *testing.T) {
	root := t.TempDir()
	selectionData, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	writeStudyInputFixture(t, root, "selection.json", string(selectionData))
	writeStudyTreatmentConfig(t, root)
	if err := os.Mkdir(filepath.Join(root, "instructions"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeStudyInputFixture(t, filepath.Join(root, "instructions"), "01_prime_directive.md", "Read CONTEXT.md before starting.\n")
	writeStudyInputFixture(t, root, "study.toml", "selection = \"selection.json\"\n[[experiments]]\nname = \"Treatment\"\nmodel = \"luna\"\nreasoning = \"low\"\nconfig = \"config.toml\"\ninstructions = \"instructions\"\n")
	if err := validateTreatmentInstructionDependencies(filepath.Join(root, "instructions")); err == nil {
		t.Fatal("test instructions must be rejected by current treatment staging")
	}

	preparedCalls := stubStudyInfrastructure(t, root)
	originalAuth := validateBenchmarkAuthentication
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	originalRuntime := preflightTreatmentRuntime
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error { return nil }
	t.Cleanup(func() { preflightTreatmentRuntime = originalRuntime })
	originalBundles := stageBenchmarkExperimentBundles
	stageCalls := 0
	manifest := TreatmentManifest{
		SchemaVersion: TreatmentSchemaVersion,
		Mode:          TreatmentInstructionsOnly,
		Files:         []TreatmentFile{{Path: "AGENTS.md", SHA256: strings.Repeat("a", 64)}},
	}
	manifestHash, err := hashCanonical(manifest)
	if err != nil {
		t.Fatal(err)
	}
	bundle := &TreatmentBundle{
		Manifest: manifest, ManifestHash: manifestHash, LinuxArchitecture: benchmarkTaskContainerArchitecture,
		AdapterSHA256: "adapter-hash", LinuxBinarySHA256: "runtime-hash", TemplatesCommit: strings.Repeat("c", 40),
		RuntimeSourceKind: treatmentRuntimeSourceRelease, RuntimeVersion: "test",
	}
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		stageCalls++
		return []*TreatmentBundle{bundle}, nil
	}
	t.Cleanup(func() { stageBenchmarkExperimentBundles = originalBundles })

	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	var originalReport StudyReport
	if err := readStudyJSON(first.JSONPath, &originalReport); err != nil {
		t.Fatal(err)
	}
	pinRoot := studyTreatmentPinRoot(root, bundle.ManifestHash)
	pin := studyTreatmentPin{
		SchemaVersion: studyTreatmentPinSchema, PinID: bundle.ManifestHash, Architecture: bundle.LinuxArchitecture,
		ManifestHash: bundle.ManifestHash, Manifest: bundle.Manifest, LinuxBinarySHA256: bundle.LinuxBinarySHA256,
		AdapterSHA256: bundle.AdapterSHA256, TemplatesCommit: bundle.TemplatesCommit, TemplatesDirty: bundle.TemplatesDirty,
		RuntimeSourceKind: bundle.RuntimeSourceKind, RuntimeVersion: bundle.RuntimeVersion,
	}
	if err := writeJSON(filepath.Join(pinRoot, "pin.json"), pin); err != nil {
		t.Fatal(err)
	}
	removeStudyReport(t, root, first.StudyID)
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		t.Fatal("recovery-only restaged mutable treatment inputs")
		return nil, nil
	}

	recovered, err := RunStudy(context.Background(), StudyOptions{
		RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), RecoveryOnly: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stageCalls != 1 || *preparedCalls != 1 {
		t.Fatalf("historical report regeneration restaged inputs: stage=%d tasks=%d", stageCalls, *preparedCalls)
	}
	if recovered.StudyID != first.StudyID || recovered.Completed != recovered.Required || recovered.Missing != 0 || recovered.JSONPath == "" || recovered.HTMLPath == "" {
		t.Fatalf("recovered outcome = %#v", recovered)
	}
	for _, path := range []string{recovered.JSONPath, recovered.HTMLPath} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("regenerated report %q: info=%v err=%v", path, info, err)
		}
	}
	var recoveredReport StudyReport
	if err := readStudyJSON(recovered.JSONPath, &recoveredReport); err != nil {
		t.Fatal(err)
	}
	want, got := originalReport.Experiments[0], recoveredReport.Experiments[0]
	if got.BundleManifest == nil || got.BundleManifest.SchemaVersion != want.BundleManifest.SchemaVersion ||
		len(got.BundleManifest.Files) != 1 || got.BundleManifest.Files[0] != want.BundleManifest.Files[0] ||
		got.SourceCommit != want.SourceCommit || got.LinuxBinarySHA256 != want.LinuxBinarySHA256 {
		t.Fatalf("regenerated report lost immutable treatment provenance: want=%#v got=%#v", want, got)
	}
}

func TestRunStudyRejectsMultipleCompleteManifestOnlyMatches(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	tag := "one"
	prepareBenchmarkTaskSet = func(_ context.Context, gotRoot string, tasks []benchmarkPlanTask) (map[string]string, map[string]string, error) {
		*preparedCalls++
		if gotRoot != root {
			return nil, nil, fmt.Errorf("unexpected task preparation root %q", gotRoot)
		}
		checksums := make(map[string]string, len(tasks))
		environments := make(map[string]string, len(tasks))
		for _, task := range tasks {
			checksums[task.ID] = task.ID + "-checksum-" + tag
			environments[task.ID] = task.ID + "-env-" + tag
		}
		return checksums, environments, nil
	}
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	studies := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies")
	hidden := filepath.Join(t.TempDir(), first.StudyID)
	if err := os.Rename(filepath.Join(studies, first.StudyID), hidden); err != nil {
		t.Fatal(err)
	}
	tag = "two"
	second, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if second.StudyID == first.StudyID {
		t.Fatalf("second study reused first identity: %#v", second)
	}
	if err := os.Rename(hidden, filepath.Join(studies, first.StudyID)); err != nil {
		t.Fatal(err)
	}
	removeStudyReport(t, root, first.StudyID)
	removeStudyReport(t, root, second.StudyID)

	blockCachedStudySidecars(t)
	if _, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{}); err == nil || !strings.Contains(err.Error(), "matches more than one historical state directory") {
		t.Fatalf("error=%v", err)
	}
	if *preparedCalls != 2 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
}

func TestListPlausibleCompletedCachedStudiesDeduplicatesReportAndManifest(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	stubStudyInfrastructure(t, root)
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareStudy(StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.cleanupInputs()
	candidates, err := listPlausibleCompletedCachedStudies(root, &prepared)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || !candidates[0].reportClaimedComplete || filepath.Base(candidates[0].stateDir) != first.StudyID {
		t.Fatalf("report+manifest candidates=%#v", candidates)
	}

	blockCachedStudySidecars(t)
	executor := &studyWorkflowExecutor{}
	second, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, executor)
	if err != nil {
		t.Fatal(err)
	}
	if second.StudyID != first.StudyID || second.Completed != 2 || len(executor.requests()) != 0 {
		t.Fatalf("deduplicated regeneration=%#v calls=%#v", second, executor.requests())
	}

	removeStudyReport(t, root, first.StudyID)
	candidates, err = listPlausibleCompletedCachedStudies(root, &prepared)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].reportClaimedComplete || filepath.Base(candidates[0].stateDir) != first.StudyID {
		t.Fatalf("manifest-only candidates=%#v", candidates)
	}
}

func TestRunStudyIgnoresUnrelatedMalformedStateDuringManifestOnlyRecovery(t *testing.T) {
	root := t.TempDir()
	writeParsedBareStudy(t, root, "luna:low")
	preparedCalls := stubStudyInfrastructure(t, root)
	originalAuth := validateBenchmarkAuthentication
	t.Cleanup(func() { validateBenchmarkAuthentication = originalAuth })
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{}, nil
	}
	first, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	removeStudyReport(t, root, first.StudyID)
	unrelated := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", "unrelated")
	if err := os.MkdirAll(filepath.Join(unrelated, "report"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unrelated, "report", "report.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unrelated, "study-manifest.json"), []byte("also-not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	blockCachedStudySidecars(t)
	second, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	if *preparedCalls != 1 {
		t.Fatalf("task preparation ran %d times", *preparedCalls)
	}
	if second.StudyID != first.StudyID || second.Completed != 2 || second.JSONPath == "" {
		t.Fatalf("cached regeneration=%#v", second)
	}
}

func TestStudyReportOmitsAuthenticationPreflightWhenAbsent(t *testing.T) {
	item := StudyExperimentReport{Name: "Bare"}
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "authentication_preflight") {
		t.Fatalf("optional field serialized: %s", data)
	}
}

func TestStudyReportNormalizesCachedAuthenticationTimestampToUTC(t *testing.T) {
	model, effort, err := ParseModelSelection("luna:low")
	if err != nil {
		t.Fatal(err)
	}
	offset := time.FixedZone("offset", 2*3600)
	verified := time.Date(2026, 8, 20, 12, 0, 0, 0, offset)
	item, err := buildStudyExperimentReport(
		preparedStudyExperiment{studyExperiment: studyExperiment{Name: "Bare"}, model: model, effort: effort},
		matrixArm{},
		matrixSelection{},
		matrixPreparation{authentication: map[string]AuthenticationPreflight{
			adapterCodex: {Provider: adapterCodex, Check: codexLoginStatusCheck, AuthenticationMethod: codexAuthMethodChatGPT, VerifiedAt: verified},
		}},
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	if item.AuthenticationPreflight == nil {
		t.Fatal("missing authentication_preflight")
	}
	got := item.AuthenticationPreflight.VerifiedAt
	if got.Location() != time.UTC {
		t.Fatalf("verified_at location=%v", got.Location())
	}
	if !got.Equal(verified) {
		t.Fatalf("verified_at instant changed: got=%v want=%v", got, verified)
	}
	payload, err := json.Marshal(item.AuthenticationPreflight)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "+02:00") {
		t.Fatalf("regenerated report kept offset timestamp: %s", payload)
	}
}

func TestCachedAuthenticationPreflightRejectsExperimentCountMismatch(t *testing.T) {
	stateDir := t.TempDir()
	verified := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	report := cachedStudyReportDeclaration{
		Experiments: []cachedStudyExperimentDeclaration{{
			Name: "Bare",
			AuthenticationPreflight: &AuthenticationPreflight{
				Provider: adapterCodex, Check: codexLoginStatusCheck, AuthenticationMethod: codexAuthMethodChatGPT, VerifiedAt: verified,
			},
		}},
	}
	if err := writeJSON(filepath.Join(stateDir, "report", "report.json"), report); err != nil {
		t.Fatal(err)
	}
	_, err := cachedAuthenticationPreflight(stateDir, &preparedStudy{})
	if err == nil || !strings.Contains(err.Error(), "report declaration does not match its immutable study") {
		t.Fatalf("error=%v", err)
	}
}

func TestRequireJSONCredentialFileRejectsNamedPipeWithoutBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Skipf("named pipes unavailable: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- requireJSONCredentialFile(path, adapterCodex)
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "must be a non-empty JSON file") {
			t.Fatalf("error=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("credential check blocked on a named pipe")
	}
}

func writeParsedTreatmentStudy(t *testing.T, root, selection string) {
	t.Helper()
	model, effort, err := ParseModelSelection(selection)
	if err != nil {
		t.Fatal(err)
	}
	selectionData, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	writeStudyInputFixture(t, root, "selection.json", string(selectionData))
	writeStudyTreatmentConfig(t, root)
	writeStudyInputFixture(t, root, "study.toml", "selection = \"selection.json\"\n[[experiments]]\nname = \"Treatment\"\nmodel = \""+model.Name+"\"\nreasoning = \""+effort+"\"\nconfig = \"config.toml\"\n")
}

func stubFakeStudyTreatmentBundles(t *testing.T) {
	t.Helper()
	original := stageBenchmarkExperimentBundles
	bundle := fakeStudyTreatmentBundle()
	stageBenchmarkExperimentBundles = func(string, *preparedStudy) ([]*TreatmentBundle, error) {
		return []*TreatmentBundle{bundle}, nil
	}
	t.Cleanup(func() { stageBenchmarkExperimentBundles = original })
}

func fakeStudyTreatmentBundle() *TreatmentBundle {
	return &TreatmentBundle{
		ManifestHash:      "treatment-manifest-hash",
		AdapterSHA256:     "adapter-hash",
		LinuxBinarySHA256: "runtime-hash",
		RuntimeSourceKind: treatmentRuntimeSourceRelease,
		RuntimeVersion:    "test",
		Manifest:          TreatmentManifest{Mode: TreatmentInstructionsOnly, AgentTimeoutMultiplier: skillsAgentTimeoutFactor},
	}
}

func removeStudyReport(t *testing.T, root, studyID string) {
	t.Helper()
	path := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", studyID, "report", "report.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func blockCachedStudySidecars(t *testing.T) {
	t.Helper()
	originalPreflight, originalVerify := preflightBenchmark, verifyBenchmarkPier
	originalAuth, originalPrepare := validateBenchmarkAuthentication, prepareBenchmarkTaskSet
	originalTreatment := preflightTreatmentRuntime
	t.Cleanup(func() {
		preflightBenchmark, verifyBenchmarkPier = originalPreflight, originalVerify
		validateBenchmarkAuthentication, prepareBenchmarkTaskSet = originalAuth, originalPrepare
		preflightTreatmentRuntime = originalTreatment
	})
	preflightBenchmark = func([]parsedSelection) error {
		return fmt.Errorf("cached regeneration reached prerequisite discovery")
	}
	verifyBenchmarkPier = func(context.Context) error {
		return fmt.Errorf("cached regeneration reached Pier verification")
	}
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return nil, fmt.Errorf("cached regeneration reached authentication")
	}
	prepareBenchmarkTaskSet = func(context.Context, string, []benchmarkPlanTask) (map[string]string, map[string]string, error) {
		return nil, nil, fmt.Errorf("cached regeneration reached task preparation")
	}
	preflightTreatmentRuntime = func(context.Context, ExecutionRequest) error {
		return fmt.Errorf("cached regeneration reached treatment runtime preflight")
	}
}

func writeMatchingStudyReport(t *testing.T, root, studyID string, prepared preparedStudy, completed, required int) {
	t.Helper()
	report := cachedStudyReportDeclaration{
		SchemaVersion: studyReportSchema,
		StudyID:       studyID,
		SelectionID:   prepared.selectionID,
	}
	for _, experiment := range prepared.experiments {
		report.Experiments = append(report.Experiments, cachedStudyExperimentDeclaration{
			Name: experiment.Name, Identity: experiment.identity,
			Model: experiment.model.PublishedIdentifier, Reasoning: experiment.effort,
			CompletedCells: completed, RequiredCells: required,
		})
	}
	if err := writeJSON(filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", studyID, "report", "report.json"), report); err != nil {
		t.Fatal(err)
	}
}

func writeParsedBareStudy(t *testing.T, root, selection string) {
	t.Helper()
	model, effort, err := ParseModelSelection(selection)
	if err != nil {
		t.Fatal(err)
	}
	writeBareStudy(t, root, model.Name, effort)
}

func writeBareStudy(t *testing.T, root, model, reasoning string) {
	t.Helper()
	selectionData, err := json.Marshal(matrixSelectionFixture())
	if err != nil {
		t.Fatal(err)
	}
	writeStudyInputFixture(t, root, "selection.json", string(selectionData))
	writeStudyInputFixture(t, root, "study.toml", "selection = \"selection.json\"\n[[experiments]]\nname = \"Bare\"\nmodel = \""+model+"\"\nreasoning = \""+reasoning+"\"\n")
}

func TestBareCustomProviderDryRunPreflightsAndRecordsAdapter(t *testing.T) {
	root := t.TempDir()
	writeBareStudy(t, root, modelGrok45, "minimal")
	stubStudyInfrastructure(t, root)

	originalAuth := validateBenchmarkAuthentication
	originalRuntimePreflight := preflightTreatmentRuntime
	t.Cleanup(func() {
		validateBenchmarkAuthentication = originalAuth
		preflightTreatmentRuntime = originalRuntimePreflight
	})
	validateBenchmarkAuthentication = func(context.Context, string, []parsedSelection) (map[string]AuthenticationPreflight, error) {
		return map[string]AuthenticationPreflight{adapterGrok: {
			Provider: adapterGrok, Check: authCheckJSONFilePresence, AuthenticationMethod: authMethodJSONFile, VerifiedAt: time.Now().UTC(),
		}}, nil
	}
	var preflights []ExecutionRequest
	preflightTreatmentRuntime = func(_ context.Context, request ExecutionRequest) error {
		preflights = append(preflights, request)
		return nil
	}

	dryExecutor := &studyWorkflowExecutor{}
	dry, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml"), DryRun: true}, dryExecutor)
	if err != nil {
		t.Fatal(err)
	}
	if len(dryExecutor.requests()) != 0 {
		t.Fatalf("dry run reached inference: %#v", dryExecutor.requests())
	}
	if len(preflights) != 2 {
		t.Fatalf("bare custom runtime preflights = %d, want one per task environment", len(preflights))
	}
	for _, request := range preflights {
		if request.Arm != ArmBaseline || request.Bundle != nil || request.Model.Adapter != adapterGrok || request.Task == "" || request.EnvironmentIdentity == "" {
			t.Fatalf("bare custom runtime preflight = %#v", request)
		}
	}

	wantAdapter, err := embeddedPierAdapterSHA256()
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, ".agent-layer", "state", "benchmarks", "deepswe", "studies", dry.StudyID, "study-manifest.json")
	var manifest immutableStudyManifest
	if err := readStudyJSON(manifestPath, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Arms) != 1 || manifest.Arms[0].Adapter != wantAdapter || manifest.Arms[0].Bundle != "" || manifest.Arms[0].Runtime != "" {
		t.Fatalf("bare custom study manifest = %#v", manifest.Arms)
	}
	var armManifest studyArmManifest
	if err := readStudyJSON(filepath.Join(filepath.Dir(manifestPath), "arms", manifest.Arms[0].ID, "manifest.json"), &armManifest); err != nil {
		t.Fatal(err)
	}
	if armManifest.AdapterSHA256 != wantAdapter || armManifest.TreatmentHash != "" || armManifest.Mode != ArmBaseline {
		t.Fatalf("bare custom arm manifest = %#v", armManifest)
	}

	paid, err := RunStudy(context.Background(), StudyOptions{RepoRoot: root, StudyPath: filepath.Join(root, "study.toml")}, &studyWorkflowExecutor{})
	if err != nil {
		t.Fatal(err)
	}
	var report StudyReport
	if err := readStudyJSON(paid.JSONPath, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Experiments) != 1 || report.Experiments[0].AdapterSHA256 != wantAdapter || report.Experiments[0].BundleManifest != nil || report.Experiments[0].LinuxBinarySHA256 != "" {
		t.Fatalf("bare custom experiment report = %#v", report.Experiments)
	}
}

func stubStudyInfrastructure(t *testing.T, root string) *int {
	t.Helper()
	originalPreflight := preflightBenchmark
	originalVerifyPier := verifyBenchmarkPier
	originalPrepareTasks := prepareBenchmarkTaskSet
	originalArchitecture := dockerHostArchitecture
	preparedCalls := 0
	preflightBenchmark = func([]parsedSelection) error { return nil }
	verifyBenchmarkPier = func(context.Context) error { return nil }
	dockerHostArchitecture = func(context.Context) (string, error) { return "amd64", nil }
	prepareBenchmarkTaskSet = func(_ context.Context, gotRoot string, tasks []benchmarkPlanTask) (map[string]string, map[string]string, error) {
		preparedCalls++
		if gotRoot != root {
			return nil, nil, fmt.Errorf("unexpected task preparation root %q", gotRoot)
		}
		checksums := make(map[string]string, len(tasks))
		environments := make(map[string]string, len(tasks))
		for _, task := range tasks {
			checksums[task.ID] = task.ID + "-checksum"
			environments[task.ID] = task.ID + "-env"
		}
		return checksums, environments, nil
	}
	t.Cleanup(func() {
		preflightBenchmark = originalPreflight
		verifyBenchmarkPier = originalVerifyPier
		prepareBenchmarkTaskSet = originalPrepareTasks
		dockerHostArchitecture = originalArchitecture
	})
	return &preparedCalls
}

func writeJSONCredential(t *testing.T, path string, contents []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func successfulCodexStatusScript() string {
	return "printf 'Logged in using ChatGPT\\n' >&2\nexit 0\n"
}

func installAuthCommandStubs(t *testing.T, codexScript, claudeScript string) string {
	t.Helper()
	logDir := t.TempDir()
	bin := t.TempDir()
	codexBody := "#!/bin/sh\n" +
		"logdir=" + shellQuote(logDir) + "\n" +
		"printf '%s\\n' \"$@\" > \"$logdir/args\"\n" +
		"printf '%s\\n' \"$CODEX_HOME\" > \"$logdir/codex_home\"\n" +
		"echo invoked >> \"$logdir/calls\"\n" +
		codexScript
	writeExecutable(t, filepath.Join(bin, adapterCodex), codexBody)
	if claudeScript == "" {
		claudeScript = "printf 'unexpected claude invocation\\n' >&2\nexit 1\n"
	}
	claudeBody := "#!/bin/sh\necho invoked >> " + shellQuote(filepath.Join(logDir, "claude-calls")) + "\n" + claudeScript
	writeExecutable(t, filepath.Join(bin, providerClaude), claudeBody)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logDir
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil { // #nosec G302 -- executable test stub.
		t.Fatal(err)
	}
}

func readStubFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- test-owned stub log.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
