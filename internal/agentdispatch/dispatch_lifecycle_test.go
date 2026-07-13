package agentdispatch

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
)

func TestResumeValidatesVersionPromptAndSkillBeforeLaunch(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	session := Session{Name: "short-bright-transistor", Agent: AgentCodex, State: "durable", ProviderSessionID: runtimeSessionID}
	if err := persistSession(root, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}
	err := Resume(ResumeOptions{Root: root, Name: session.Name, PromptArgs: []string{"resume"}, Env: []string{}, LookPath: alwaysFound, VersionLookup: func(string, string) (string, error) { return "0.1.0", nil }})
	requireDispatchExitCode(t, err, ExitUnavailable)
	err = Resume(ResumeOptions{Root: root, Name: session.Name, Env: []string{}, LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }})
	requireDispatchExitCode(t, err, ExitUsage)
	err = Resume(ResumeOptions{Root: root, Name: session.Name, PromptArgs: []string{"resume"}, Skill: "missing", Env: []string{}, LookPath: alwaysFound, VersionLookup: func(_ string, agent string) (string, error) { return supportedProviderVersions[agent], nil }})
	requireDispatchExitCode(t, err, ExitConfig)
}

func TestRunUsesConfiguredDefaultAndHonorsQuietOutput(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	replaceDispatchConfigText(t, root, "instruction_token_threshold = 50000", "instruction_token_threshold = 1")
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "codex.log")
	writeDispatchStub(t, binDir, "codex", `printf '{"type":"agent_message","message":"answer"}\n'`)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := Run(RunOptions{
		Root:       root,
		Agent:      AgentCodex,
		PromptArgs: []string{"Review"},
		Env:        []string{"PATH=" + testPath(binDir), "AL_TEST_LOG=" + logPath},
		Stdout:     &stdout,
		Stderr:     &stderr,
		Quiet:      true,
		LookPath:   mockLookPath(binDir),
	})
	if err != nil {
		t.Fatalf("quiet Run: %v", err)
	}
	if stdout.String() != "answer" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if strings.Contains(stderr.String(), "instructions") {
		t.Fatalf("quiet stderr leaked warnings: %q", stderr.String())
	}
}

func TestExecuteDispatchPreservesFailedFreshRunForRecoveryHistory(t *testing.T) {
	root := t.TempDir()
	run, err := newDispatchRun(root, AgentCodex, supportedProviderVersions[AgentCodex], "fresh")
	if err != nil {
		t.Fatalf("new run: %v", err)
	}
	session, err := reserveSession(root, run)
	if err != nil {
		t.Fatalf("reserve session: %v", err)
	}
	project := &config.ProjectConfig{Root: root, Config: dispatchTestConfig(AgentCodex)}
	err = finishDispatchFailure(dispatchExecution{Root: root, Project: project, Run: run, Session: session, Mode: "fresh"}, exitError(ExitTargetFailure, "failed"))
	requireDispatchExitCode(t, err, ExitTargetFailure)
	retained, err := loadSession(root, session.Name)
	if err != nil {
		t.Fatalf("failed fresh run lost its history mapping: %v", err)
	}
	if retained.ActiveRunID != "" || retained.RunID != run.Record.ID {
		t.Fatalf("failed fresh mapping = %#v", retained)
	}
	record, err := loadRunRecord(root, run.Record.ID)
	if err != nil {
		t.Fatalf("load failed record: %v", err)
	}
	if record.State != dispatchStateFailed || record.RecoveryState != recoveryAcceptanceUnknown || record.TerminalReason != "failed" {
		t.Fatalf("failed record = %#v", record)
	}
}

func TestDispatchInputAndEnvironmentContracts(t *testing.T) {
	root := writeDispatchRepo(t, dispatchRepoConfig{})
	project, stderr, env, depth, err := loadDispatchProject(root, nil, []string{clients.EnvDispatchActive + "=2"})
	if err != nil || stderr != io.Discard || depth != 2 || len(env) != 1 {
		t.Fatalf("loadDispatchProject = %#v, %v, %#v, %d, %v", project, stderr, env, depth, err)
	}
	if err := checkDispatchDepth(project.Config, depth); err != nil {
		t.Fatalf("depth two should be allowed: %v", err)
	}
	if err := checkDispatchDepth(project.Config, 3); err == nil {
		t.Fatal("max depth was accepted")
	} else {
		requireDispatchExitCode(t, err, ExitNested)
	}
	if _, _, _, _, err := loadDispatchProject(root, nil, []string{clients.EnvDispatchActive + "=invalid"}); err == nil {
		t.Fatal("invalid depth was accepted")
	} else {
		requireDispatchExitCode(t, err, ExitNested)
	}
	if err := writeIdentity(failingWriter{}, "tiny-round-capacitor", AgentCodex, "fresh", false); err == nil {
		t.Fatal("writeIdentity hid a writer failure")
	}
}
