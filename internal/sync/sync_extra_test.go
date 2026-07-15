package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestEnsureEnabled(t *testing.T) {
	name := "antigravity"
	if err := EnsureEnabled(name, nil); err == nil {
		t.Fatalf("expected error for nil enabled")
	}

	disabled := false
	if err := EnsureEnabled(name, &disabled); err == nil {
		t.Fatalf("expected error for disabled")
	}

	enabled := true
	if err := EnsureEnabled(name, &enabled); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMissingConfig(t *testing.T) {
	_, err := Run(t.TempDir())
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunWithProjectError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Antigravity: config.AntigravityConfig{Enabled: testutil.BoolPtr(true)},
			},
		},
		Instructions: []config.InstructionFile{{Name: "00_base.md", Content: "base"}},
		Skills: []config.Skill{
			{Name: "alpha", Description: "desc", Body: "body"},
		},
		Root: file,
	}

	_, err := RunWithProject(RealSystem{}, file, project)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunStepsError(t *testing.T) {
	err := runSteps([]func() error{
		func() error { return fmt.Errorf("boom") },
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestCollectWarningsInstructionsError(t *testing.T) {
	root := t.TempDir()
	agentsPath := filepath.Join(root, "AGENTS.md")
	if err := os.MkdirAll(agentsPath, 0o700); err != nil {
		t.Fatalf("mkdir AGENTS.md dir: %v", err)
	}
	threshold := 1

	project := &config.ProjectConfig{
		Config: config.Config{
			Warnings: config.WarningsConfig{InstructionTokenThreshold: &threshold},
		},
		Root: root,
	}

	if _, err := collectWarnings(project, nil); err == nil {
		t.Fatal("expected collectWarnings to fail when AGENTS.md cannot be read as a file")
	}
}

// TestRunWithProject_AppliesWarningNoiseControl pins F-C-6: the noise-control
// pipeline (warnings.ApplyNoiseControl) must run inside RunWithProject for
// every successful sync. Without this test, a refactor that removes the call
// would silently regress quiet/reduce behavior for every client. Uses the
// invalid-noise-mode path because it adds a deterministic warning that does
// not require coercing other check sites.
func TestRunWithProject_AppliesWarningNoiseControl(t *testing.T) {
	t.Parallel()

	setupFixture := func(t *testing.T) (string, *config.ProjectConfig) {
		t.Helper()
		return loadSyncFixtureProject(t)
	}

	t.Run("invalid mode surfaces critical warning under default", func(t *testing.T) {
		root, project := setupFixture(t)
		// Invalid mode passed to ApplyNoiseControl falls through to the
		// "unknown" branch which appends a critical WARNING_NOISE_MODE_INVALID
		// to the filtered output. If ApplyNoiseControl is not wired in
		// RunWithProject, this warning never appears in result.Warnings.
		project.Config.Warnings.NoiseMode = "invalid-mode-xyz"
		result, err := RunWithProject(RealSystem{}, root, project)
		if err != nil {
			t.Fatalf("RunWithProject: %v", err)
		}
		foundCritical := false
		for _, w := range result.Warnings {
			if w.Code == "WARNING_NOISE_MODE_INVALID" {
				foundCritical = true
				break
			}
		}
		if !foundCritical {
			t.Fatalf("expected ApplyNoiseControl to inject WARNING_NOISE_MODE_INVALID; got Warnings=%+v AllWarnings=%+v", result.Warnings, result.AllWarnings)
		}
	})

	t.Run("quiet mode filters a deterministically-generated warning", func(t *testing.T) {
		root, project := setupFixture(t)
		// Force at least one collected warning by setting the instruction-token
		// threshold to 1 — the fixture instructions are guaranteed to be
		// larger than that, so CheckInstructions emits a warning. With this
		// warning present, the test can distinguish "ApplyNoiseControl is
		// wired" (Warnings empty under quiet mode) from "fixture produced no
		// warnings" (which would have masked a missing wiring fix).
		threshold := 1
		project.Config.Warnings.InstructionTokenThreshold = &threshold
		project.Config.Warnings.NoiseMode = "quiet"
		result, err := RunWithProject(RealSystem{}, root, project)
		if err != nil {
			t.Fatalf("RunWithProject: %v", err)
		}
		if len(result.AllWarnings) == 0 {
			t.Fatal("test setup error: expected the instruction-threshold warning to be raised in AllWarnings")
		}
		if len(result.Warnings) != 0 {
			t.Fatalf("expected quiet mode to suppress all warnings, got: %+v", result.Warnings)
		}
	})
}

func TestRunWithProject_ProjectsNotificationsChimeForEnabledProviders(t *testing.T) {
	root, project := loadSyncFixtureProject(t)
	enabled := true
	project.Config.Notifications.Chime = &enabled

	if _, err := RunWithProject(RealSystem{}, root, project); err != nil {
		t.Fatalf("RunWithProject: %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, ".claude", "settings.json"),
		filepath.Join(root, ".codex", "config.toml"),
		filepath.Join(root, ".agents", "plugins", "agent-layer-chime", "hooks.json"),
	} {
		content := readFileForTest(t, path)
		if !strings.Contains(content, agentLayerChimeMarker) {
			t.Fatalf("expected %s to contain chime marker, got:\n%s", path, content)
		}
	}
}

func TestRunWithProject_ProjectsCodexRuntimeFeaturesForVSCodeOnly(t *testing.T) {
	root, project := loadSyncFixtureProject(t)
	enabled := true
	disabled := false
	project.Config.Agents.Codex.Enabled = &disabled
	project.Config.Agents.Codex.Model = "cli-only-model"
	project.Config.Agents.Codex.ReasoningEffort = "high"
	project.Config.Agents.Codex.Statusline = &enabled
	project.Config.Agents.Codex.AgentSpecific = map[string]any{
		"features": map[string]any{
			"apps":           true,
			"plugins":        false,
			"browser_use":    false,
			"in_app_browser": false,
			"computer_use":   false,
		},
	}
	project.Config.Agents.VSCode.Enabled = &enabled

	if err := os.Remove(filepath.Join(root, ".agent-layer", codexStatuslineSourceName)); err != nil {
		t.Fatalf("remove CLI-only statusline source: %v", err)
	}
	if _, err := RunWithProject(RealSystem{}, root, project); err != nil {
		t.Fatalf("RunWithProject: %v", err)
	}

	parsed := parseCodexConfig(t, readFileForTest(t, filepath.Join(root, ".codex", "config.toml")))
	features, ok := parsed["features"].(map[string]any)
	if !ok {
		t.Fatalf("expected generated Codex features table, got %#v", parsed["features"])
	}
	for key, want := range map[string]bool{
		"apps":           true,
		"plugins":        false,
		"browser_use":    false,
		"in_app_browser": false,
		"computer_use":   false,
	} {
		if got, exists := features[key]; !exists || got != want {
			t.Fatalf("expected features.%s = %v, got %#v", key, want, got)
		}
	}
	if tui, exists := parsed["tui"]; exists {
		if tuiMap, table := tui.(map[string]any); !table || tuiMap["status_line"] != nil {
			t.Fatalf("did not expect CLI-only statusline in VS Code Codex config: %#v", tui)
		}
	}
	for _, key := range []string{config.CodexModelKey, config.CodexReasoningEffortKey} {
		if value, exists := parsed[key]; exists {
			t.Fatalf("did not expect CLI-only %s in VS Code Codex config: %#v", key, value)
		}
	}
}

func TestRunWithProject_CleansNotificationsChimeWhenProvidersDisabled(t *testing.T) {
	root, project := loadSyncFixtureProject(t)
	disabled := false
	project.Config.Notifications.Chime = &disabled
	project.Config.Agents.Antigravity.Enabled = &disabled
	project.Config.Agents.Claude.Enabled = &disabled
	project.Config.Agents.ClaudeVSCode.Enabled = &disabled
	project.Config.Agents.Codex.Enabled = &disabled

	claudeSettingsPath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(claudeSettingsPath), 0o700); err != nil {
		t.Fatalf("mkdir claude settings dir: %v", err)
	}
	if err := os.WriteFile(claudeSettingsPath, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/usr/bin/afplay /System/Library/Sounds/Blow.aiff >/dev/null 2>&1 & # agent-layer-chime","timeout":5}]}]}}`), 0o600); err != nil {
		t.Fatalf("seed Claude chime: %v", err)
	}
	writeExistingCodexConfig(t, root, codexPartialHeader+codexChimeBlockForTest())
	enabled := true
	chimeProject := &config.ProjectConfig{Config: config.Config{Notifications: config.NotificationsConfig{Chime: &enabled}}}
	if err := writeAntigravityChimePlugin(RealSystem{}, root, chimeProject); err != nil {
		t.Fatalf("seed Antigravity chime plugin: %v", err)
	}

	if _, err := RunWithProject(RealSystem{}, root, project); err != nil {
		t.Fatalf("RunWithProject: %v", err)
	}
	for _, path := range []string{
		claudeSettingsPath,
		filepath.Join(root, ".codex", "config.toml"),
	} {
		content := readFileForTest(t, path)
		if strings.Contains(content, agentLayerChimeMarker) {
			t.Fatalf("expected stale chime marker removed from %s, got:\n%s", path, content)
		}
	}
	if _, err := os.Lstat(antigravityChimePluginDir(root)); !os.IsNotExist(err) {
		t.Fatalf("expected Antigravity chime plugin removed, lstat err=%v", err)
	}
}

func TestRunWithProject_CleansNotificationsChimeWhenProvidersStayEnabled(t *testing.T) {
	root, project := loadSyncFixtureProject(t)
	enabled := true
	project.Config.Notifications.Chime = &enabled

	if _, err := RunWithProject(RealSystem{}, root, project); err != nil {
		t.Fatalf("RunWithProject chime enabled: %v", err)
	}

	paths := []string{
		filepath.Join(root, ".claude", "settings.json"),
		filepath.Join(root, ".codex", "config.toml"),
		filepath.Join(root, ".agents", "plugins", "agent-layer-chime", "hooks.json"),
	}
	for _, path := range paths {
		content := readFileForTest(t, path)
		if !strings.Contains(content, agentLayerChimeMarker) {
			t.Fatalf("test setup error: expected %s to contain chime marker, got:\n%s", path, content)
		}
	}

	disabled := false
	project.Config.Notifications.Chime = &disabled
	if _, err := RunWithProject(RealSystem{}, root, project); err != nil {
		t.Fatalf("RunWithProject chime disabled: %v", err)
	}
	for _, path := range paths[:2] {
		content := readFileForTest(t, path)
		if strings.Contains(content, agentLayerChimeMarker) {
			t.Fatalf("expected stale chime marker removed from %s, got:\n%s", path, content)
		}
	}
	if _, err := os.Lstat(antigravityChimePluginDir(root)); !os.IsNotExist(err) {
		t.Fatalf("expected Antigravity chime plugin removed while provider stays enabled, lstat err=%v", err)
	}
}

func loadSyncFixtureProject(t *testing.T) (string, *config.ProjectConfig) {
	t.Helper()
	fixtureRoot := filepath.Join("testdata", "fixture-repo")
	root := t.TempDir()
	if err := copyFixtureRepo(fixtureRoot, root); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	envPath := filepath.Join(root, ".agent-layer", ".env")
	if err := os.WriteFile(envPath, []byte("AL_EXAMPLE_TOKEN=token123\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	writeTemplateToFixtureSource(t, root, "claude-statusline.sh", filepath.Join(".agent-layer", "claude-statusline.sh"), 0o755)
	writeTemplateToFixtureSource(t, root, "codex-statusline.toml", filepath.Join(".agent-layer", "codex-statusline.toml"), 0o644)
	project, err := config.LoadProjectConfig(root)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	return root, project
}

func TestUpdateGitignoreValidationError(t *testing.T) {
	root := t.TempDir()
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(blockPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	invalid := "# >>> agent-layer\n.agent-layer/\n"
	if err := os.WriteFile(blockPath, []byte(invalid), 0o600); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}

	if err := updateGitignore(RealSystem{}, root); err == nil {
		t.Fatal("expected updateGitignore validation error")
	}
}
