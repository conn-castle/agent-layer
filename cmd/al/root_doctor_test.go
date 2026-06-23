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

func TestDoctorCommand_MissingOptionalDocsDirIsQuiet(t *testing.T) {
	root := t.TempDir()
	writeDoctorTestRepo(t, root)
	if err := os.RemoveAll(filepath.Join(root, "docs", "agent-layer")); err != nil {
		t.Fatalf("remove optional docs dir: %v", err)
	}
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

	origInstructions := checkInstructions
	origMCP := checkMCPServers
	origPolicy := checkPolicy
	t.Cleanup(func() {
		checkInstructions = origInstructions
		checkMCPServers = origMCP
		checkPolicy = origPolicy
	})
	checkInstructions = func(string, *int) ([]warnings.Warning, error) { return nil, nil }
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, warnings.MCPSummary, error) {
		return nil, warnings.MCPSummary{}, nil
	}
	checkPolicy = func(*config.ProjectConfig) []warnings.Warning { return nil }

	var out bytes.Buffer
	testutil.WithWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, nil); err != nil {
			t.Fatalf("doctor failed with only optional docs dir missing: %v", err)
		}
	})

	output := out.String()
	if strings.Contains(output, fmt.Sprintf(messages.DoctorMissingOptionalDirFmt, "docs/agent-layer")) {
		t.Fatalf("missing optional docs dir should be quiet, got:\n%s", output)
	}
	if strings.Contains(output, fmt.Sprintf(messages.DoctorMissingRequiredDirFmt, "docs/agent-layer")) {
		t.Fatalf("expected docs/agent-layer to stop being described as required, got:\n%s", output)
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
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, warnings.MCPSummary, error) {
		return nil, warnings.MCPSummary{}, nil
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
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, warnings.MCPSummary, error) {
		return nil, warnings.MCPSummary{}, nil
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
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, warnings.MCPSummary, error) {
		return nil, warnings.MCPSummary{}, nil
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
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, warnings.MCPSummary, error) {
		return nil, warnings.MCPSummary{}, nil
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
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, warnings.MCPSummary, error) {
		calledMCP = true
		return nil, warnings.MCPSummary{}, nil
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

[agents.antigravity]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = true

[warnings]
noise_mode = "quiet"
instruction_token_threshold = 1
`
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", "config.toml"), []byte(configToml), 0o600); err != nil {
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

func TestDoctorCommand_QuietFlagSuppressesWarningNotifications(t *testing.T) {
	root := t.TempDir()
	writeDoctorTestRepoWithWarnings(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "2.0.0", Outdated: true}, nil)

	skillDir := filepath.Join(root, ".agent-layer", "skills", "alpha")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	skillContent := `---
description: test
---
Body.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillContent), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	var out bytes.Buffer
	testutil.WithWorkingDir(t, root, func() {
		cmd := newRootCmd()
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"--quiet", "doctor"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("doctor --quiet failed: %v\noutput:\n%s", err, out.String())
		}
	})

	output := out.String()
	if strings.Contains(output, "[WARN]") {
		t.Fatalf("expected --quiet to suppress warning results, got:\n%s", output)
	}
	if strings.Contains(output, "WARNING INSTRUCTIONS_TOO_LARGE") {
		t.Fatalf("expected --quiet to suppress warning-system output, got:\n%s", output)
	}
	if strings.Contains(output, messages.DoctorWarningSystemHeader) {
		t.Fatalf("expected --quiet to suppress warning-system header, got:\n%s", output)
	}
	if *calls == 0 {
		t.Fatal("expected update check to run")
	}
}

func TestDoctorCommand_QuietFlagPreservesFailures(t *testing.T) {
	root := t.TempDir()
	writeTestRepoInvalidConfig(t, root)
	calls := stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

	var out bytes.Buffer
	var execErr error
	testutil.WithWorkingDir(t, root, func() {
		cmd := newRootCmd()
		cmd.SetOut(&out)
		cmd.SetArgs([]string{"--quiet", "doctor"})
		execErr = cmd.Execute()
	})

	if execErr == nil {
		t.Fatalf("expected --quiet doctor to return non-nil error on FAIL; output:\n%s", out.String())
	}
	if !strings.Contains(out.String(), messages.DoctorStatusFailLabel) {
		t.Fatalf("expected --quiet to preserve %q in output, got:\n%s", messages.DoctorStatusFailLabel, out.String())
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
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, warnings.MCPSummary, error) {
		return nil, warnings.MCPSummary{}, nil
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
	checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, warnings.MCPSummary, error) {
		return nil, warnings.MCPSummary{}, errors.New("mcp failed")
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

	if got := len(enabledMCPServerIDs(servers)); got != 2 {
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
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "my-skill.md"), []byte("# test"), 0o600); err != nil {
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

// writeTestRepoLenientConfig writes a repo whose config fails STRICT validation
// (an unknown key) but still parses leniently, so doctor's lenient fallback runs
// and the orchestrator's CheckSecrets/CheckSkills calls execute against the
// partial config. body is appended verbatim under the valid sections.
func writeTestRepoLenientConfig(t *testing.T, root string) {
	t.Helper()
	configDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir .agent-layer: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs", "agent-layer"), 0o700); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "instructions"), 0o700); err != nil {
		t.Fatalf("mkdir instructions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "instructions", "00_rules.md"), []byte("# Base"), 0o600); err != nil {
		t.Fatalf("write rules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "commands.allow"), []byte(""), 0o600); err != nil {
		t.Fatalf("write commands.allow: %v", err)
	}
	// `definitely_unknown_key` fails strict decoding but parses leniently.
	configToml := `
definitely_unknown_key = true

[approvals]
mode = "all"

[agents.antigravity]
enabled = true
[agents.claude]
enabled = true
[agents.codex]
enabled = false
[agents.vscode]
enabled = true
`
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configToml), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestDoctorCommand_MalformedSkill_NoContradictorySkillsOK(t *testing.T) {
	root := t.TempDir()
	writeTestRepoLenientConfig(t, root)
	stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

	// A directory-format skill whose SKILL.md fails to load, so CheckConfig's
	// lenient fallback emits a Skills FAIL and leaves cfg.Skills empty.
	skillDir := filepath.Join(root, ".agent-layer", "skills", "broken")
	if err := os.MkdirAll(skillDir, 0o700); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("no frontmatter here"), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	var out bytes.Buffer
	testutil.WithWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, nil); err == nil {
			t.Fatal("expected doctor to fail")
		}
	})

	output := out.String()
	// The Skills load failure must be reported.
	if !strings.Contains(output, "Failed to load skills") {
		t.Fatalf("expected Skills load failure, got:\n%s", output)
	}
	// The contradictory "No skills configured" OK line must NOT appear.
	if strings.Contains(output, messages.DoctorSkillsNoneConfigured) {
		t.Fatalf("did not expect contradictory %q line, got:\n%s", messages.DoctorSkillsNoneConfigured, output)
	}
}

func TestDoctorCommand_MalformedEnv_NoMissingSecretCascade(t *testing.T) {
	root := t.TempDir()
	writeTestRepoLenientConfig(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".agent-layer", "skills"), 0o700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

	// A malformed .env (line without `=`).
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", ".env"), []byte("AL_NO_EQUALS_HERE\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	var out bytes.Buffer
	testutil.WithWorkingDir(t, root, func() {
		cmd := newDoctorCmd()
		cmd.SetOut(&out)
		if err := cmd.RunE(cmd, nil); err == nil {
			t.Fatal("expected doctor to fail")
		}
	})

	output := out.String()
	// The .env parse failure must be surfaced with an actionable next step.
	if !strings.Contains(output, "Could not read .agent-layer/.env") {
		t.Fatalf("expected .env unreadable diagnostic, got:\n%s", output)
	}
	if !strings.Contains(output, messages.DoctorEnvFileUnreadableRecommend) {
		t.Fatalf("expected actionable recommendation, got:\n%s", output)
	}
	// The misleading "Missing secret" cascade must NOT be layered on top.
	if strings.Contains(output, "Missing secret") {
		t.Fatalf("did not expect misleading Missing secret cascade, got:\n%s", output)
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

func TestRenderSizeSummary(t *testing.T) {
	intp := func(v int) *int { return &v }

	t.Run("values against thresholds", func(t *testing.T) {
		var out bytes.Buffer
		w := config.WarningsConfig{
			InstructionTokenThreshold:     intp(10000),
			MCPServerThreshold:            intp(15),
			MCPToolsTotalThreshold:        intp(60),
			MCPSchemaTokensTotalThreshold: intp(30000),
		}
		mcp := warnings.MCPSummary{Available: true, EnabledServers: 4, ReachableServers: 4, TotalTools: 38, TotalSchemaTokens: 18400}
		renderSizeSummary(&out, w, 3240, "AGENTS.md", nil, 1820, true, mcp)
		s := out.String()
		for _, want := range []string{
			"📊 Context size summary",
			"Instructions (AGENTS.md): 3240 / 10000 tokens",
			"Skills (always-loaded descriptions): 1820 / 4000 tokens",
			"MCP servers enabled: 4 / 15",
			"MCP tools (total): 38 / 60",
			"MCP tool schemas (total): 18400 / 30000 tokens",
			"Total always-loaded (estimated): ~23460 tokens",
		} {
			if !strings.Contains(s, want) {
				t.Fatalf("expected %q in summary, got:\n%s", want, s)
			}
		}
		if strings.Contains(s, "no limit set") {
			t.Fatalf("did not expect no-limit text, got:\n%s", s)
		}
		if strings.Contains(s, "unreachable") {
			t.Fatalf("did not expect partial note when all reachable, got:\n%s", s)
		}
	})

	t.Run("nil thresholds show no limit set", func(t *testing.T) {
		var out bytes.Buffer
		mcp := warnings.MCPSummary{Available: true, EnabledServers: 2, ReachableServers: 2, TotalTools: 5, TotalSchemaTokens: 1000}
		renderSizeSummary(&out, config.WarningsConfig{}, 100, "AGENTS.md", nil, 12, true, mcp)
		s := out.String()
		for _, want := range []string{
			"Instructions (AGENTS.md): 100 tokens (no limit set)",
			"Skills (always-loaded descriptions): 12 / 4000 tokens",
			"MCP servers enabled: 2 (no limit set)",
			"MCP tools (total): 5 (no limit set)",
			"MCP tool schemas (total): 1000 tokens (no limit set)",
			"Total always-loaded (estimated): ~1112 tokens",
		} {
			if !strings.Contains(s, want) {
				t.Fatalf("expected %q, got:\n%s", want, s)
			}
		}
	})

	t.Run("instruction measure error is surfaced", func(t *testing.T) {
		var out bytes.Buffer
		renderSizeSummary(&out, config.WarningsConfig{}, 0, "", errors.New("boom"), 0, true, warnings.MCPSummary{Available: true})
		s := out.String()
		if !strings.Contains(s, "Instructions: size unavailable (boom)") {
			t.Fatalf("expected instruction error surfaced, got:\n%s", s)
		}
		if !strings.Contains(s, "Total always-loaded (estimated): ~0 tokens (excludes instructions)") {
			t.Fatalf("expected total to exclude instructions, got:\n%s", s)
		}
	})

	t.Run("mcp unavailable hides totals", func(t *testing.T) {
		var out bytes.Buffer
		renderSizeSummary(&out, config.WarningsConfig{}, 100, "AGENTS.md", nil, 0, true, warnings.MCPSummary{Available: false})
		s := out.String()
		if !strings.Contains(s, "MCP servers: size unavailable") {
			t.Fatalf("expected mcp unavailable, got:\n%s", s)
		}
		if strings.Contains(s, "MCP servers enabled:") {
			t.Fatalf("did not expect enabled-servers line when unavailable, got:\n%s", s)
		}
		if !strings.Contains(s, "Total always-loaded (estimated): ~100 tokens (excludes MCP tool schemas)") {
			t.Fatalf("expected total to exclude MCP tool schemas and still print, got:\n%s", s)
		}
	})

	t.Run("partial note when some servers unreachable", func(t *testing.T) {
		var out bytes.Buffer
		mcp := warnings.MCPSummary{Available: true, EnabledServers: 3, ReachableServers: 1, TotalTools: 2, TotalSchemaTokens: 500}
		renderSizeSummary(&out, config.WarningsConfig{}, 0, "AGENTS.md", nil, 0, true, mcp)
		if !strings.Contains(out.String(), "2 of 3 enabled MCP server(s) unreachable") {
			t.Fatalf("expected partial note, got:\n%s", out.String())
		}
	})

	t.Run("skills unavailable excluded from total and named", func(t *testing.T) {
		var out bytes.Buffer
		mcp := warnings.MCPSummary{Available: true, EnabledServers: 1, ReachableServers: 1, TotalTools: 2, TotalSchemaTokens: 300}
		// skillTokens is non-zero but must NOT be counted because skills are unavailable.
		renderSizeSummary(&out, config.WarningsConfig{}, 100, "AGENTS.md", nil, 999, false, mcp)
		s := out.String()
		if !strings.Contains(s, "Skills: size unavailable") {
			t.Fatalf("expected skills unavailable line, got:\n%s", s)
		}
		if strings.Contains(s, "Skills (always-loaded descriptions):") {
			t.Fatalf("did not expect skills token line when unavailable, got:\n%s", s)
		}
		// Total excludes the 999 skill tokens: 100 (instructions) + 300 (MCP schema) = 400.
		if !strings.Contains(s, "Total always-loaded (estimated): ~400 tokens (excludes skills)") {
			t.Fatalf("expected total to exclude skills, got:\n%s", s)
		}
	})
}

func TestDoctorCommand_SizeSummaryAlwaysPrinted(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		name := "default"
		if quiet {
			name = "quiet"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeDoctorTestRepo(t, root)
			stubUpdateCheck(t, update.CheckResult{Current: "1.0.0", Latest: "1.0.0"}, nil)

			origInstructions := checkInstructions
			origMeasure := measureInstructions
			origMCP := checkMCPServers
			origPolicy := checkPolicy
			t.Cleanup(func() {
				checkInstructions = origInstructions
				measureInstructions = origMeasure
				checkMCPServers = origMCP
				checkPolicy = origPolicy
			})
			checkInstructions = func(string, *int) ([]warnings.Warning, error) { return nil, nil }
			measureInstructions = func(string) (int, string, error) { return 1234, "AGENTS.md", nil }
			checkMCPServers = func(context.Context, *config.ProjectConfig, warnings.Connector, warnings.MCPDiscoveryStatusFunc) ([]warnings.Warning, warnings.MCPSummary, error) {
				return nil, warnings.MCPSummary{Available: true, EnabledServers: 3, ReachableServers: 3, TotalTools: 7, TotalSchemaTokens: 4200}, nil
			}
			checkPolicy = func(*config.ProjectConfig) []warnings.Warning { return nil }

			var out bytes.Buffer
			testutil.WithWorkingDir(t, root, func() {
				cmd := newRootCmd()
				cmd.SetOut(&out)
				if quiet {
					cmd.SetArgs([]string{"--quiet", "doctor"})
				} else {
					cmd.SetArgs([]string{"doctor"})
				}
				if err := cmd.Execute(); err != nil {
					t.Fatalf("doctor command failed (quiet=%v): %v\noutput:\n%s", quiet, err, out.String())
				}
			})

			s := out.String()
			for _, want := range []string{
				messages.DoctorSizeSummaryHeader,
				"Instructions (AGENTS.md): 1234",
				"MCP servers enabled: 3",
				"MCP tools (total): 7",
				"MCP tool schemas (total): 4200",
			} {
				if !strings.Contains(s, want) {
					t.Fatalf("expected %q in doctor output (quiet=%v), got:\n%s", want, quiet, s)
				}
			}
		})
	}
}
