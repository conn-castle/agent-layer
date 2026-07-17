package agentdispatch

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFreshCodexThenResumeUsesOnlyDurableMapping(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	writeDispatchStub(t, binDir, "codex", `printf '{"type":"agent_message","message":"first answer"}\n'`)
	env := []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath}

	var freshOut bytes.Buffer
	var freshErr bytes.Buffer
	if err := Run(RunOptions{
		Root:       root,
		Agent:      AgentCodex,
		PromptArgs: []string{"first"},
		Env:        env,
		Stdout:     &freshOut,
		Stderr:     &freshErr,
		LookPath:   mockLookPath(binDir),
	}); err != nil {
		t.Fatalf("fresh Run: %v", err)
	}
	if freshOut.String() != "first answer" {
		t.Fatalf("fresh stdout = %q", freshOut.String())
	}
	name := identityName(t, freshErr.String())
	session, err := loadSession(root, name)
	if err != nil {
		t.Fatalf("load durable session: %v", err)
	}
	if session.Agent != AgentCodex || session.ProviderSessionID != "11111111-1111-4111-8111-111111111111" || session.State != "durable" {
		t.Fatalf("unexpected fresh mapping: %#v", session)
	}

	var resumeOut bytes.Buffer
	var resumeErr bytes.Buffer
	if err := Resume(ResumeOptions{
		Root:       root,
		Name:       name,
		PromptArgs: []string{"revision"},
		Env:        env,
		Stdout:     &resumeOut,
		Stderr:     &resumeErr,
		LookPath:   mockLookPath(binDir),
	}); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumeOut.String() != "first answer" {
		t.Fatalf("resume stdout = %q", resumeOut.String())
	}
	if !strings.Contains(resumeErr.String(), "["+name+"] codex · resumed · durable") {
		t.Fatalf("resume identity = %q", resumeErr.String())
	}
	assertFileContains(t, logPath, "ARG_0=exec")
	assertFileContains(t, logPath, "ARG_1=resume")
	assertFileContains(t, logPath, "ARG_2=--json")
	assertFileContains(t, logPath, "ARG_3=11111111-1111-4111-8111-111111111111")

	var inspected bytes.Buffer
	if err := Inspect(InspectionRequest{Root: root, ID: name, Stdout: &inspected}); err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !strings.Contains(inspected.String(), "Provider conversation: 11111111-1111-4111-8111-111111111111") {
		t.Fatalf("inspection = %q", inspected.String())
	}
	if err := Delete(root, name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := loadSession(root, name); err == nil {
		t.Fatal("deleted mapping remained readable")
	}
}

func TestClaudeResumeKeepsFreshTargetAfterConfigurationChanges(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{ClaudeModel: "configured-model", ClaudeReasoningEffort: "medium"})
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "claude.log")
	writeDispatchStub(t, binDir, "claude", "")
	env := []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath}

	var stderr bytes.Buffer
	if err := Run(RunOptions{
		Root:            root,
		Agent:           AgentClaude,
		Model:           "fable",
		ReasoningEffort: "medium",
		PromptArgs:      []string{"fresh"},
		Env:             env,
		Stderr:          &stderr,
		LookPath:        mockLookPath(binDir),
	}); err != nil {
		t.Fatalf("fresh Claude Run: %v", err)
	}
	name := identityName(t, stderr.String())
	session, err := loadSession(root, name)
	if err != nil {
		t.Fatalf("load Claude session: %v", err)
	}
	if session.Model != "fable" || session.ReasoningEffort != "medium" {
		t.Fatalf("fresh target metadata = %#v", session)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	configData, err := os.ReadFile(configPath) // #nosec G304 -- path is inside the test-owned temporary repository.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	changedConfig := strings.ReplaceAll(string(configData), `model = "configured-model"`, `model = "opus"`)
	changedConfig = strings.ReplaceAll(changedConfig, `reasoning_effort = "medium"`, `reasoning_effort = "high"`)
	if err := os.WriteFile(configPath, []byte(changedConfig), 0o600); err != nil { // #nosec G703 -- path is inside the test-owned temporary repository.
		t.Fatalf("change config: %v", err)
	}
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatalf("reset provider log: %v", err)
	}

	if err := Resume(ResumeOptions{
		Root:       root,
		Name:       name,
		PromptArgs: []string{"resume"},
		Env:        env,
		LookPath:   mockLookPath(binDir),
	}); err != nil {
		t.Fatalf("Resume Claude: %v", err)
	}
	assertFileContains(t, logPath, "ARG_7=--model")
	assertFileContains(t, logPath, "ARG_8=fable")
	assertFileContains(t, logPath, "ARG_9=--effort")
	assertFileContains(t, logPath, "ARG_10=medium")
	assertFileDoesNotContain(t, logPath, "opus")
	assertFileDoesNotContain(t, logPath, "high")
}

func identityName(t *testing.T, stderr string) string {
	t.Helper()
	trimmed := strings.TrimSpace(stderr)
	if !strings.HasPrefix(trimmed, "[") {
		t.Fatalf("missing identity line: %q", stderr)
	}
	end := strings.Index(trimmed, "]")
	if end <= 1 {
		t.Fatalf("invalid identity line: %q", stderr)
	}
	return trimmed[1:end]
}

func TestInspectDoesNotInferProviderHealth(t *testing.T) {
	record := RunRecord{ID: "11111111-1111-4111-8111-111111111111", Name: "tiny-round-capacitor", Agent: AgentClaude, State: dispatchStateRunning, PID: 0}
	inspection := inspectionFromRecord(record)
	if inspection.Process != statusUnknown {
		t.Fatalf("process = %q, want unknown", inspection.Process)
	}
	if inspection.State != dispatchStateRunning {
		t.Fatalf("state = %q", inspection.State)
	}
}

func TestAntigravitySuccessfulAnswerWithoutIDIsNotResumable(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	binDir := t.TempDir()
	path := filepath.Join(binDir, "agy")
	stub := `#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  printf '1.1.1\n'
  exit 0
fi
printf 'answer without a provider id'
`
	if err := os.WriteFile(path, []byte(stub), 0o700); err != nil { // #nosec G306 -- test-controlled provider stub.
		t.Fatalf("write agy stub: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := Run(RunOptions{
		Root:       root,
		Agent:      AgentAntigravity,
		PromptArgs: []string{"fresh"},
		Env:        []string{"PATH=" + testPath(binDir)},
		Stdout:     &stdout,
		Stderr:     &stderr,
		LookPath:   mockLookPath(binDir),
	}); err != nil {
		t.Fatalf("Run Antigravity: %v", err)
	}
	if stdout.String() != "answer without a provider id" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "not resumable · agy 1.1.1 · diagnostics:") {
		t.Fatalf("missing not-resumable warning: %q", stderr.String())
	}
	sessions, err := listSessions(root)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].State != "pending" || sessions[0].ActiveRunID != "" {
		t.Fatalf("non-resumable history mapping = %#v", sessions)
	}
}

func TestAntigravityResumeWithoutParsedIDRetainsDurableMapping(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	run, err := newDispatchRun(root, AgentAntigravity, supportedProviderVersions[AgentAntigravity], dispatchModeFresh)
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve session: %v", err)
	}
	session.ProviderSessionID = runtimeSessionID
	session.State = "durable"
	session.ActiveRunID = ""
	session.ActiveClaimKnown = false
	completed := time.Now().UTC()
	run.Record.State = dispatchStateCompleted
	run.Record.RecoveryState = recoveryResumeRequired
	run.Record.CompletedAt = &completed
	if err := writeRunRecord(run.Dir, &run.Record); err != nil {
		t.Fatalf("complete prior run: %v", err)
	}
	if err := persistSession(root, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	binDir := t.TempDir()
	path := filepath.Join(binDir, "agy")
	stub := `#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  printf '1.1.1\n'
  exit 0
fi
printf 'resumed answer without a provider id'
`
	if err := os.WriteFile(path, []byte(stub), 0o700); err != nil { // #nosec G306 -- test-controlled provider stub.
		t.Fatalf("write agy stub: %v", err)
	}
	if err := Resume(ResumeOptions{
		Root:       root,
		Name:       session.Name,
		PromptArgs: []string{"resume"},
		Env:        []string{"PATH=" + testPath(binDir)},
		LookPath:   mockLookPath(binDir),
	}); err != nil {
		t.Fatalf("Resume Antigravity: %v", err)
	}
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatalf("load retained session: %v", err)
	}
	if retained.ProviderSessionID != runtimeSessionID || retained.State != "durable" {
		t.Fatalf("retained session = %#v", retained)
	}
}

func TestSupportedVersionFixturesReduceOnlyRequiredEvents(t *testing.T) {
	claudeData, err := os.ReadFile(filepath.Join("testdata", "claude", "v0.13-2.1.207.jsonl"))
	if err != nil {
		t.Fatalf("read Claude fixture: %v", err)
	}
	var claudeRaw bytes.Buffer
	var claudeEvents []providerEvent
	if err := readStructuredEvents(bytes.NewReader(claudeData), &claudeRaw, AgentClaude, "11111111-1111-4111-8111-111111111111", func(event providerEvent) error {
		claudeEvents = append(claudeEvents, event)
		return nil
	}); err != nil {
		t.Fatalf("reduce Claude fixture: %v", err)
	}
	if len(claudeEvents) != 4 || claudeEvents[0].Kind != eventProgress || claudeEvents[1].Kind != eventSession || claudeEvents[2].Answer != "Claude final answer." || claudeEvents[3].Kind != eventComplete {
		t.Fatalf("Claude events = %#v", claudeEvents)
	}

	codexData, err := os.ReadFile(filepath.Join("testdata", "codex", "v0.13-0.144.1.jsonl"))
	if err != nil {
		t.Fatalf("read Codex fixture: %v", err)
	}
	var codexRaw bytes.Buffer
	var codexEvents []providerEvent
	if err := readStructuredEvents(bytes.NewReader(codexData), &codexRaw, AgentCodex, "", func(event providerEvent) error {
		codexEvents = append(codexEvents, event)
		return nil
	}); err != nil {
		t.Fatalf("reduce Codex fixture: %v", err)
	}
	if len(codexEvents) != 3 || codexEvents[0].SessionID != "22222222-2222-4222-8222-222222222222" || codexEvents[1].Answer != "Codex final answer." || codexEvents[2].Kind != eventComplete {
		t.Fatalf("Codex events = %#v", codexEvents)
	}

	logPath := filepath.Join("testdata", "antigravity", "v0.13-1.1.1.log")
	id, err := antigravitySessionID(logPath)
	if err != nil {
		t.Fatalf("extract Antigravity fixture ID: %v", err)
	}
	if id != "33333333-3333-4333-8333-333333333333" {
		t.Fatalf("Antigravity ID = %q", id)
	}
}
