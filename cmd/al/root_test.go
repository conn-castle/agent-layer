package main

// NOTE: Tests in this file mutate package-level globals (getwd, isTerminal,
// runWizard, checkForUpdate, checkInstructions, checkMCPServers, runPromptServer).
// Do not use t.Parallel() at the top level. Each test must restore globals via t.Cleanup().

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/doctor"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
	"github.com/conn-castle/agent-layer/internal/update"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

func stubUpdateCheck(t *testing.T, result update.CheckResult, err error) *int {
	t.Helper()

	orig := checkForUpdate
	calls := 0
	checkForUpdate = func(context.Context, string) (update.CheckResult, error) {
		calls++
		return result, err
	}
	t.Cleanup(func() { checkForUpdate = orig })
	return &calls
}

func TestRootVersionFlag(t *testing.T) {
	cmd := newRootCmd()
	cmd.Version = "v1.2.3"
	cmd.SetVersionTemplate("{{.Version}}\n")
	cmd.SetArgs([]string{"--version"})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if strings.TrimSpace(out.String()) != "v1.2.3" {
		t.Fatalf("unexpected version output: %q", out.String())
	}
}

func TestRootHelp(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !strings.Contains(out.String(), "Agent Layer") {
		t.Fatalf("expected help output, got %q", out.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestRootVersionFlagWriteError(t *testing.T) {
	cmd := newRootCmd()
	cmd.Version = "v1.2.3"
	cmd.SetArgs([]string{"--version"})
	cmd.SetOut(failingWriter{})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error when output fails")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStubCmd(t *testing.T) {
	cmd := newStubCmd("doctor")
	err := cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected not implemented error, got %v", err)
	}
}

func TestInitAndSyncCommands(t *testing.T) {
	root := t.TempDir()
	withWorkingDir(t, root, func() {
		cmd := newInitCmd()
		cmd.SetArgs([]string{"--no-wizard"})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(bytes.NewBufferString(""))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("init error: %v", err)
		}
		binDir := t.TempDir()
		writeStub(t, binDir, "al")
		t.Setenv("PATH", binDir)
		t.Setenv(dispatch.EnvNoNetwork, "1")

		syncCmd := newSyncCmd()
		err := syncCmd.RunE(syncCmd, nil)
		// Sync might fail with warnings if templates are large, which is expected behavior now.
		if err != nil && !errors.Is(err, ErrSyncCompletedWithWarnings) {
			t.Fatalf("sync error: %v", err)
		}

		if _, err := os.Stat(filepath.Join(root, ".agent-layer", "config.toml")); err != nil {
			t.Fatalf("expected config.toml to exist: %v", err)
		}
	})
}

func TestInitCommandNoWizardSkipsWizard(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	originalRunWizard := runWizard
	wizardCalled := false
	runWizard = func(_ string, _ string) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizard = originalRunWizard })

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--no-wizard"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("y\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init error: %v", err)
	}
	if wizardCalled {
		t.Fatalf("expected wizard to be skipped with --no-wizard")
	}
}

func TestInitCommandPromptYesRunsWizard(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	originalRunWizard := runWizard
	wizardCalled := false
	runWizard = func(_ string, _ string) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizard = originalRunWizard })

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("y\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init error: %v", err)
	}
	if !wizardCalled {
		t.Fatalf("expected wizard to run after confirmation")
	}
}

func TestInitCommandNonInteractiveSkipsWizard(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	originalRunWizard := runWizard
	wizardCalled := false
	runWizard = func(_ string, _ string) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizard = originalRunWizard })

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init error: %v", err)
	}
	if wizardCalled {
		t.Fatalf("expected wizard to be skipped in non-interactive mode")
	}
}

func TestInitCommandPromptNoDeclinesWizard(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	originalRunWizard := runWizard
	wizardCalled := false
	runWizard = func(_ string, _ string) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizard = originalRunWizard })

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("n\n")) // Decline wizard
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init error: %v", err)
	}
	if wizardCalled {
		t.Fatalf("expected wizard to be skipped when user declines")
	}
}

func TestClientCommandsMissingConfig(t *testing.T) {
	root := t.TempDir()
	withWorkingDir(t, root, func() {
		commands := []*cobra.Command{
			newGeminiCmd(),
			newClaudeCmd(),
			newCodexCmd(),
			newVSCodeCmd(),
			newAntigravityCmd(),
			newMcpPromptsCmd(),
		}
		for _, cmd := range commands {
			err := cmd.RunE(cmd, nil)
			if err == nil {
				t.Fatalf("expected error for %s", cmd.Use)
			}
		}
	})
}

func TestClientCommandsSuccess(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	binDir := t.TempDir()
	writeStub(t, binDir, "gemini")
	writeStub(t, binDir, "claude")
	writeStub(t, binDir, "codex")
	writeStub(t, binDir, "code")
	writeStub(t, binDir, "antigravity")
	writeStub(t, binDir, "al")

	t.Setenv("PATH", binDir)

	original := runPromptServer
	t.Cleanup(func() { runPromptServer = original })
	runPromptServer = func(ctx context.Context, version string, commands []config.SlashCommand) error {
		return nil
	}

	withWorkingDir(t, root, func() {
		commands := []*cobra.Command{
			newGeminiCmd(),
			newClaudeCmd(),
			newCodexCmd(),
			newVSCodeCmd(),
			newAntigravityCmd(),
			newMcpPromptsCmd(),
		}
		for _, cmd := range commands {
			if err := cmd.RunE(cmd, nil); err != nil {
				t.Fatalf("command %s failed: %v", cmd.Use, err)
			}
		}
	})
}

func TestDoctorCommand(t *testing.T) {
	root := t.TempDir()
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

	// Test failure (no repo)
	withWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected doctor failure in empty dir")
		}
	})

	// Test success
	writeTestRepo(t, root)
	withWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		// Capture output? doctor prints to stdout.
		// We just care about return code for now.
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("doctor failed in valid repo: %v", err)
		}
	})
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestDoctorCommand_UpdateSkippedNoNetwork(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "2.0.0", Outdated: true}, nil)

	origInstructions := checkInstructions
	origMCP := checkMCPServers
	t.Cleanup(func() {
		checkInstructions = origInstructions
		checkMCPServers = origMCP
	})
	checkInstructions = func(string, *int) ([]warnings.Warning, error) { return nil, nil }
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, error) {
		return nil, nil
	}

	t.Setenv(dispatch.EnvNoNetwork, "1")
	withWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("doctor failed when updates are skipped: %v", err)
		}
	})
	if *calls != 0 {
		t.Fatalf("expected update check to be skipped, got %d calls", *calls)
	}
}

func TestDoctorCommand_UpdateCheckError(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{}, errors.New("update failed"))

	origInstructions := checkInstructions
	origMCP := checkMCPServers
	t.Cleanup(func() {
		checkInstructions = origInstructions
		checkMCPServers = origMCP
	})
	checkInstructions = func(string, *int) ([]warnings.Warning, error) { return nil, nil }
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, error) {
		return nil, nil
	}

	withWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("doctor failed on update check error: %v", err)
		}
	})
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestDoctorCommand_UpdateCheckDevBuild(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{
		Current:      "1.0.0-dev",
		Latest:       "1.0.0",
		CurrentIsDev: true,
	}, nil)

	origInstructions := checkInstructions
	origMCP := checkMCPServers
	t.Cleanup(func() {
		checkInstructions = origInstructions
		checkMCPServers = origMCP
	})
	checkInstructions = func(string, *int) ([]warnings.Warning, error) { return nil, nil }
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, error) {
		return nil, nil
	}

	withWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("doctor failed on dev build: %v", err)
		}
	})
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestDoctorCommand_ConfigErrorSkipsWarningSystem(t *testing.T) {
	root := t.TempDir()
	writeTestRepoInvalidConfig(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

	origInstructions := checkInstructions
	origMCP := checkMCPServers
	calledInstructions := false
	calledMCP := false
	t.Cleanup(func() {
		checkInstructions = origInstructions
		checkMCPServers = origMCP
	})
	checkInstructions = func(string, *int) ([]warnings.Warning, error) {
		calledInstructions = true
		return nil, nil
	}
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, error) {
		calledMCP = true
		return nil, nil
	}

	withWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		if err := cmd.RunE(cmd, nil); err == nil {
			t.Fatal("expected doctor error for invalid config")
		}
	})
	if calledInstructions || calledMCP {
		t.Fatal("expected warning checks to be skipped when config is invalid")
	}
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestDoctorCommand_WithWarnings(t *testing.T) {
	root := t.TempDir()
	writeTestRepoWithWarnings(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "2.0.0", Outdated: true}, nil)
	withWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		err := cmd.RunE(cmd, nil)
		// Doctor should fail when warnings exist
		if err == nil {
			t.Fatal("expected doctor to fail when warnings exist")
		}
		if !strings.Contains(err.Error(), "doctor checks failed") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestDoctorCommand_InstructionsError(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

	origInstructions := checkInstructions
	origMCP := checkMCPServers
	t.Cleanup(func() {
		checkInstructions = origInstructions
		checkMCPServers = origMCP
	})

	checkInstructions = func(string, *int) ([]warnings.Warning, error) {
		return nil, errors.New("instructions failed")
	}
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, error) {
		return nil, nil
	}

	withWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected doctor error")
		}
	})
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestDoctorCommand_MCPError(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

	origInstructions := checkInstructions
	origMCP := checkMCPServers
	t.Cleanup(func() {
		checkInstructions = origInstructions
		checkMCPServers = origMCP
	})

	checkInstructions = func(string, *int) ([]warnings.Warning, error) {
		return nil, nil
	}
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, error) {
		return nil, errors.New("mcp failed")
	}

	withWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected doctor error")
		}
	})
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestPrintResult_AllStatuses(t *testing.T) {
	// Test all status types to ensure coverage
	results := []doctor.Result{
		{Status: doctor.StatusOK, CheckName: "test-ok", Message: "OK message"},
		{Status: doctor.StatusWarn, CheckName: "test-warn", Message: "Warning message", Recommendation: "Fix it"},
		{Status: doctor.StatusFail, CheckName: "test-fail", Message: "Fail message"},
	}
	for _, r := range results {
		// printResult prints to stdout, just verify it doesn't panic
		printResult(r)
	}
}

func TestPrintRecommendation_MultiLineIndent(t *testing.T) {
	output := captureStdout(t, func() {
		printRecommendation("Line one\nLine two\n\nLine four")
	})
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	expected := []string{
		messages.DoctorRecommendationPrefix + "Line one",
		messages.DoctorRecommendationIndent + "Line two",
		messages.DoctorRecommendationIndent,
		messages.DoctorRecommendationIndent + "Line four",
	}
	if len(lines) != len(expected) {
		t.Fatalf("unexpected line count: got %d, want %d\noutput:\n%s", len(lines), len(expected), output)
	}
	for i, want := range expected {
		if lines[i] != want {
			t.Fatalf("line %d mismatch: got %q, want %q", i, lines[i], want)
		}
	}
}

func TestCountEnabledMCPServers(t *testing.T) {
	enabled := true
	disabled := false
	servers := []config.MCPServer{
		{ID: "a", Enabled: &enabled},
		{ID: "b", Enabled: &disabled},
		{ID: "c", Enabled: &enabled},
		{ID: "d", Enabled: nil},
	}

	if got := countEnabledMCPServers(servers); got != 2 {
		t.Fatalf("expected 2 enabled servers, got %d", got)
	}
}

func TestStartMCPDiscoveryReporterZero(t *testing.T) {
	output := captureStdout(t, func() {
		reporter, stop := startMCPDiscoveryReporter(nil)
		if reporter != nil {
			t.Fatalf("expected nil reporter when no MCP servers are enabled")
		}
		stop()
	})
	expected := fmt.Sprintf(messages.DoctorMCPCheckStartFmt, 0) + messages.DoctorMCPCheckDone + "\n"
	if output != expected {
		t.Fatalf("unexpected output: got %q, want %q", output, expected)
	}
}

func TestSyncCommand_WithWarnings(t *testing.T) {
	root := t.TempDir()
	writeTestRepoWithWarnings(t, root)
	binDir := t.TempDir()
	writeStub(t, binDir, "al")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	withWorkingDir(t, root, func() {
		cmd := newSyncCmd()
		err := cmd.RunE(cmd, nil)
		// Sync should fail when warnings exist
		if err == nil {
			t.Fatal("expected sync to fail when warnings exist")
		}
		if !errors.Is(err, ErrSyncCompletedWithWarnings) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	original := os.Stdout
	os.Stdout = writer

	fn()

	_ = writer.Close()
	os.Stdout = original

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	_ = reader.Close()

	return buf.String()
}

func TestWizardCommand(t *testing.T) {
	originalIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	root := t.TempDir()
	withWorkingDir(t, root, func() {
		// Force the non-interactive path to keep tests deterministic.
		cmd := newWizardCmd()
		err := cmd.RunE(cmd, nil)
		// Should fail because not interactive
		if err == nil {
			t.Fatal("expected wizard to fail in non-interactive test")
		}
		if !strings.Contains(err.Error(), "interactive terminal") {
			t.Logf("got error: %v", err)
		}
	})
}

func TestCommandsGetwdError(t *testing.T) {
	original := getwd
	getwd = func() (string, error) {
		return "", errors.New("boom")
	}
	t.Cleanup(func() { getwd = original })

	commands := []*cobra.Command{
		newInitCmd(),
		newSyncCmd(),
		newMcpPromptsCmd(),
		newGeminiCmd(),
		newClaudeCmd(),
		newCodexCmd(),
		newVSCodeCmd(),
		newAntigravityCmd(),
		newDoctorCmd(),
	}
	for _, cmd := range commands {
		if err := cmd.RunE(cmd, nil); err == nil {
			t.Fatalf("expected error for %s", cmd.Use)
		}
	}
}

func TestInitCommandInstallRunError(t *testing.T) {
	// Point to a file instead of directory to cause install.Run to fail
	root := t.TempDir()
	blockingFile := filepath.Join(root, ".agent-layer")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when install.Run fails")
	}
}

func TestInitCommandPromptError(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(errorReader{}) // Use errorReader to cause promptYesNo to fail

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when promptYesNo fails")
	}
	if !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeTestRepo(t *testing.T, root string) {
	t.Helper()
	paths := config.DefaultPaths(root)
	if err := os.MkdirAll(paths.InstructionsDir, 0o755); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SlashCommandsDir, 0o755); err != nil {
		t.Fatalf("mkdir slash commands: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	configToml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true
`
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_base.md"), []byte("base"), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	command := `---
name: alpha
description: test
---

Do it.`
	if err := os.WriteFile(filepath.Join(paths.SlashCommandsDir, "alpha.md"), []byte(command), 0o644); err != nil {
		t.Fatalf("write slash command: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o644); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}
	writeGitignoreBlock(t, root)
}

func writeTestRepoInvalidConfig(t *testing.T, root string) {
	t.Helper()
	agentLayerDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(agentLayerDir, 0o755); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentLayerDir, "config.toml"), []byte("invalid = "), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
}

func writeTestRepoWithWarnings(t *testing.T, root string) {
	t.Helper()
	paths := config.DefaultPaths(root)
	if err := os.MkdirAll(paths.InstructionsDir, 0o755); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.MkdirAll(paths.SlashCommandsDir, 0o755); err != nil {
		t.Fatalf("mkdir slash commands: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}

	// Config with very low instruction token threshold to trigger a warning
	configToml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true

[warnings]
instruction_token_threshold = 1
`
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(paths.EnvPath, []byte(""), 0o644); err != nil {
		t.Fatalf("write env: %v", err)
	}
	// Write large instructions to exceed the threshold
	largeContent := strings.Repeat("This is a test instruction that will exceed the low token threshold. ", 50)
	if err := os.WriteFile(filepath.Join(paths.InstructionsDir, "00_base.md"), []byte(largeContent), 0o644); err != nil {
		t.Fatalf("write instructions: %v", err)
	}
	if err := os.WriteFile(paths.CommandsAllow, []byte("git status"), 0o644); err != nil {
		t.Fatalf("write commands allow: %v", err)
	}
	writeGitignoreBlock(t, root)
}

func writeGitignoreBlock(t *testing.T, root string) {
	t.Helper()
	templateBytes, err := templates.Read("gitignore.block")
	if err != nil {
		t.Fatalf("read gitignore.block template: %v", err)
	}
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.WriteFile(blockPath, templateBytes, 0o644); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}
}

func writeStub(t *testing.T, dir string, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore chdir: %v", err)
		}
	}()
	fn()
}
