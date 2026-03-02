package main

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

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/dispatch"
	"github.com/conn-castle/agent-layer/internal/doctor"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/testutil"
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

func TestDoctorCommand(t *testing.T) {
	root := t.TempDir()
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

	// Test failure (no repo)
	testutil.WithWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected doctor failure in empty dir")
		}
	})

	// Test success
	writeDoctorTestRepo(t, root)
	testutil.WithWorkingDir(t, root, func() {
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

func TestDoctorCommand_MissingPromptServerClientConfigsFails(t *testing.T) {
	root := t.TempDir()
	writeDoctorTestRepo(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

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

	if err := os.Remove(filepath.Join(root, ".mcp.json")); err != nil {
		t.Fatalf("remove .mcp.json: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".gemini", "settings.json")); err != nil {
		t.Fatalf("remove .gemini/settings.json: %v", err)
	}

	var out bytes.Buffer
	testutil.WithWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		cmd.SetOut(&out)
		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected doctor failure when prompt server client configs are missing")
		}
	})
	output := out.String()
	if !strings.Contains(output, messages.DoctorCheckNamePromptServer) {
		t.Fatalf("expected prompt server check in output, got:\n%s", output)
	}
	if !strings.Contains(output, messages.DoctorCheckNamePromptConfig) {
		t.Fatalf("expected prompt config check in output, got:\n%s", output)
	}
	if !strings.Contains(output, ".mcp.json") {
		t.Fatalf("expected missing .mcp.json detail, got:\n%s", output)
	}
	if !strings.Contains(output, ".gemini/settings.json") {
		t.Fatalf("expected missing .gemini/settings.json detail, got:\n%s", output)
	}
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestDoctorCommand_UpdateSkippedNoNetwork(t *testing.T) {
	root := t.TempDir()
	writeDoctorTestRepo(t, root)
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
	testutil.WithWorkingDir(t, root, func() {
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
	writeDoctorTestRepo(t, root)
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

	testutil.WithWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("doctor failed on update check error: %v", err)
		}
	})
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestDoctorCommand_UpdateCheckRateLimitedIsMinimized(t *testing.T) {
	root := t.TempDir()
	writeDoctorTestRepo(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{}, &update.RateLimitError{StatusCode: 429, Status: "429 Too Many Requests"})

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

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = w.Close()
		_ = r.Close()
		os.Stdout = origStdout
	})
	os.Stdout = w

	testutil.WithWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("doctor failed on rate limit: %v", err)
		}
	})

	_ = w.Close()
	outBytes, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	output := string(outBytes)
	if !strings.Contains(output, messages.DoctorUpdateRateLimited) {
		t.Fatalf("expected rate-limit message in output, got:\n%s", output)
	}
	if strings.Contains(output, messages.DoctorUpdateFailedRecommend) {
		t.Fatalf("expected no network-failure recommendation block on rate limit, got:\n%s", output)
	}
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestDoctorCommand_UpdateCheckDevBuild(t *testing.T) {
	root := t.TempDir()
	writeDoctorTestRepo(t, root)
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

	testutil.WithWorkingDir(t, root, func() {
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

	testutil.WithWorkingDir(t, root, func() {
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
	writeDoctorTestRepoWithWarnings(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "2.0.0", Outdated: true}, nil)
	testutil.WithWorkingDir(t, root, func() {
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

func TestDoctorCommand_QuietNoiseModeStillShowsWarnings(t *testing.T) {
	root := t.TempDir()
	writeDoctorTestRepoWithWarnings(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "2.0.0", Outdated: true}, nil)

	configToml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.antigravity]
enabled = true

[warnings]
noise_mode = "quiet"
instruction_token_threshold = 1
`
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "config.toml"), []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var out bytes.Buffer
	testutil.WithWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		cmd.SetOut(&out)
		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected doctor to fail when warnings exist")
		}
	})
	if !strings.Contains(out.String(), "WARNING INSTRUCTIONS_TOO_LARGE") {
		t.Fatalf("expected instructions warning output, got:\n%s", out.String())
	}
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestDoctorCommand_InstructionsError(t *testing.T) {
	root := t.TempDir()
	writeDoctorTestRepo(t, root)
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

	testutil.WithWorkingDir(t, root, func() {
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
	writeDoctorTestRepo(t, root)
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

	testutil.WithWorkingDir(t, root, func() {
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
	var out bytes.Buffer
	// Test all status types to ensure coverage
	results := []doctor.Result{
		{Status: doctor.StatusOK, CheckName: "test-ok", Message: "OK message"},
		{Status: doctor.StatusWarn, CheckName: "test-warn", Message: "Warning message", Recommendation: "Fix it"},
		{Status: doctor.StatusFail, CheckName: "test-fail", Message: "Fail message"},
	}
	for _, r := range results {
		printResult(&out, r)
	}
}

func TestPrintRecommendation_MultiLineIndent(t *testing.T) {
	var out bytes.Buffer
	printRecommendation(&out, "Line one\nLine two\n\nLine four")
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	expected := []string{
		messages.DoctorRecommendationPrefix + "Line one",
		messages.DoctorRecommendationIndent + "Line two",
		messages.DoctorRecommendationIndent,
		messages.DoctorRecommendationIndent + "Line four",
	}
	if len(lines) != len(expected) {
		t.Fatalf("unexpected line count: got %d, want %d\noutput:\n%s", len(lines), len(expected), out.String())
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

func TestDoctorCommand_FlatSkillsDetectedEvenWhenConfigFails(t *testing.T) {
	root := t.TempDir()
	// Set up a repo with invalid config (cfg will be nil) and flat-format skills.
	writeTestRepoInvalidConfig(t, root)
	stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

	// Add a flat-format skill file.
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "my-skill.md"), []byte("# test"), 0o644); err != nil {
		t.Fatalf("write flat skill: %v", err)
	}

	var out bytes.Buffer
	testutil.WithWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		cmd.SetOut(&out)
		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected doctor to fail")
		}
	})

	output := out.String()
	// The FlatSkills check must appear even though config loading failed.
	if !strings.Contains(output, messages.DoctorCheckNameFlatSkills) {
		t.Fatalf("expected FlatSkills check in output when config fails, got:\n%s", output)
	}
	if !strings.Contains(output, "my-skill.md") {
		t.Fatalf("expected flat skill filename in output, got:\n%s", output)
	}
}

func TestStartMCPDiscoveryReporterZero(t *testing.T) {
	var output bytes.Buffer
	reporter, stop := startMCPDiscoveryReporter(nil, &output)
	if reporter != nil {
		t.Fatalf("expected nil reporter when no MCP servers are enabled")
	}
	stop()
	expected := fmt.Sprintf(messages.DoctorMCPCheckStartFmt, 0) + messages.DoctorMCPCheckDone + "\n"
	if output.String() != expected {
		t.Fatalf("unexpected output: got %q, want %q", output.String(), expected)
	}
}
