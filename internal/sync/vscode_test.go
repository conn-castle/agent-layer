package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestBuildVSCodeSettings(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	settings, err := buildVSCodeSettings(project)
	if err != nil {
		t.Fatalf("buildVSCodeSettings error: %v", err)
	}
	if len(settings.ChatToolsTerminalAutoApprove) != 1 {
		t.Fatalf("expected 1 auto-approve entry")
	}
	if settings.ChatAgentSkillsLocations[".agents/skills"] != true {
		t.Fatalf("expected shared project skills to be enabled")
	}
	if settings.ChatAgentSkillsLocations[".github/skills"] != false {
		t.Fatalf("expected duplicate GitHub project skills to be disabled")
	}
	if settings.ChatAgentSkillsLocations[".claude/skills"] != false {
		t.Fatalf("expected duplicate Claude project skills to be disabled")
	}
	if settings.ChatAgentSkillsLocations["~/.copilot/skills"] != true ||
		settings.ChatAgentSkillsLocations["~/.claude/skills"] != true ||
		settings.ChatAgentSkillsLocations["~/.agents/skills"] != true {
		t.Fatalf("expected personal skill locations to remain enabled")
	}
}

func TestBuildVSCodeSettingsOmitsSkillLocationsWhenVSCodeDisabled(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
			Agents: config.AgentsConfig{
				VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)},
			},
		},
	}

	settings, err := buildVSCodeSettings(project)
	if err != nil {
		t.Fatalf("buildVSCodeSettings error: %v", err)
	}
	if len(settings.ChatAgentSkillsLocations) != 0 {
		t.Fatalf("did not expect Copilot skill locations when agents.vscode is disabled")
	}
}

func TestBuildVSCodeSettingsEscapesSlash(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"scripts/dev.sh"},
	}

	settings, err := buildVSCodeSettings(project)
	if err != nil {
		t.Fatalf("buildVSCodeSettings error: %v", err)
	}

	expected := "/^scripts\\/dev\\.sh(\\b.*)?$/"
	if _, ok := settings.ChatToolsTerminalAutoApprove[expected]; !ok {
		t.Fatalf("expected escaped pattern %q", expected)
	}
}

func TestWriteVSCodeSettings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := writeVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeVSCodeSettings error: %v", err)
	}
	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	data, err := os.ReadFile(settingsPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	got := string(data)
	for _, key := range []string{
		`"chat.agentSkillsLocations"`,
		`".agents/skills": true`,
		`".github/skills": false`,
		`".claude/skills": false`,
		`"~/.agents/skills": true`,
		`"~/.claude/skills": true`,
		`"~/.copilot/skills": true`,
	} {
		if !strings.Contains(got, key) {
			t.Fatalf("expected %s in settings.json, got:\n%s", key, got)
		}
	}
}

func TestWriteVSCodeSettingsAgentSkillsLocationsIdempotent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := writeVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("first writeVSCodeSettings error: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(root, ".vscode", "settings.json")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read first: %v", err)
	}

	if err := writeVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("second writeVSCodeSettings error: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(root, ".vscode", "settings.json")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read second: %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("expected idempotent re-sync output; first vs second differ:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestBuildVSCodeSettingsYOLO(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	settings, err := buildVSCodeSettings(project)
	if err != nil {
		t.Fatalf("buildVSCodeSettings error: %v", err)
	}
	if len(settings.ChatToolsTerminalAutoApprove) != 1 {
		t.Fatalf("expected 1 terminal auto-approve entry for yolo mode")
	}

	root := t.TempDir()
	if err := writeVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeVSCodeSettings error: %v", err)
	}
	updatedBytes, err := os.ReadFile(filepath.Join(root, ".vscode", "settings.json")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	if strings.Contains(string(updatedBytes), "chat.tools.global.autoApprove") {
		t.Fatalf("expected yolo mode not to emit deprecated chat.tools.global.autoApprove setting")
	}
}

func TestBuildVSCodeSettingsClaudeVSCodeYOLO(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
			Agents: config.AgentsConfig{
				VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)},
			},
		},
	}

	settings, err := buildVSCodeSettings(project)
	if err != nil {
		t.Fatalf("buildVSCodeSettings error: %v", err)
	}
	if settings.ClaudeCodeAllowDangerouslySkipPerms == nil || !*settings.ClaudeCodeAllowDangerouslySkipPerms {
		t.Fatal("expected ClaudeCodeAllowDangerouslySkipPerms=true when claude_vscode enabled + yolo")
	}
}

func TestBuildVSCodeSettingsClaudeVSCodeNonYOLO(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeAll},
			Agents: config.AgentsConfig{
				VSCode:       config.EnableOnlyConfig{Enabled: testutil.BoolPtr(false)},
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)},
			},
		},
	}

	settings, err := buildVSCodeSettings(project)
	if err != nil {
		t.Fatalf("buildVSCodeSettings error: %v", err)
	}
	if settings.ClaudeCodeAllowDangerouslySkipPerms != nil {
		t.Fatal("expected ClaudeCodeAllowDangerouslySkipPerms to be nil when mode is not yolo")
	}
}

func TestWriteVSCodeSettingsPreservesExistingContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "{\n  // user setting\n  \"editor.formatOnSave\": true\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := writeVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeVSCodeSettings error: %v", err)
	}

	updatedBytes, err := os.ReadFile(filepath.Join(vscodeDir, "settings.json")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	updated := string(updatedBytes)

	if !strings.Contains(updated, "// >>> agent-layer") {
		t.Fatalf("expected managed block start")
	}
	if !strings.Contains(updated, "Managed by Agent Layer") {
		t.Fatalf("expected managed block header")
	}
	if !strings.Contains(updated, "\"chat.tools.terminal.autoApprove\"") {
		t.Fatalf("expected managed settings content")
	}
	if !strings.Contains(updated, "},\n  // <<< agent-layer") {
		t.Fatalf("expected managed block to include trailing comma")
	}
	if !strings.Contains(updated, "\"editor.formatOnSave\": true") {
		t.Fatalf("expected user setting to be preserved")
	}
	if !strings.Contains(updated, "// user setting") {
		t.Fatalf("expected user comment to be preserved")
	}
}

func TestWriteVSCodeSettingsReplacesManagedBlock(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "{\n  // >>> agent-layer\n  // Managed by Agent Layer. To customize, edit .agent-layer/config.toml\n  // and .agent-layer/commands.allow, then re-run `al sync`.\n  //\n  \"chat.tools.terminal.autoApprove\": {\n    \"/^old(\\\\b.*)?$/\": true\n  },\n  // <<< agent-layer\n  \"editor.tabSize\": 2\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := writeVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeVSCodeSettings error: %v", err)
	}

	updatedBytes, err := os.ReadFile(filepath.Join(vscodeDir, "settings.json")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	updated := string(updatedBytes)

	if strings.Contains(updated, "/^old(\\\\b.*)?$/") {
		t.Fatalf("expected old managed entry to be replaced")
	}
	if !strings.Contains(updated, "/^git status(\\\\b.*)?$/") {
		t.Fatalf("expected new managed entry to be present")
	}
	if !strings.Contains(updated, "},\n  // <<< agent-layer") {
		t.Fatalf("expected managed block to include trailing comma")
	}
	if !strings.Contains(updated, "\"editor.tabSize\": 2") {
		t.Fatalf("expected user setting to be preserved")
	}
}

func TestWriteVSCodeSettingsNoTrailingCommaWhenManagedBlockIsLast(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "{\n  // >>> agent-layer\n  // Managed by Agent Layer. To customize, edit .agent-layer/config.toml\n  // and .agent-layer/commands.allow, then re-run `al sync`.\n  //\n  \"chat.tools.terminal.autoApprove\": {\n    \"/^old(\\\\b.*)?$/\": true\n  },\n  // <<< agent-layer\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := writeVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeVSCodeSettings error: %v", err)
	}

	updatedBytes, err := os.ReadFile(filepath.Join(vscodeDir, "settings.json")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	updated := string(updatedBytes)

	if strings.Contains(updated, "},\n  // <<< agent-layer") {
		t.Fatalf("expected managed block to omit trailing comma")
	}
	if !strings.Contains(updated, "}\n  // <<< agent-layer") {
		t.Fatalf("expected managed block to end without trailing comma")
	}
}

func TestWriteVSCodeSettingsInsertsManagedBlockWithExistingFields(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "{\n  \"files.eol\": \"\\\\n\",\n  \"editor.tabSize\": 2\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := writeVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("writeVSCodeSettings error: %v", err)
	}

	updatedBytes, err := os.ReadFile(filepath.Join(vscodeDir, "settings.json")) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	updated := string(updatedBytes)

	if !strings.Contains(updated, "\"files.eol\": \"\\\\n\"") {
		t.Fatalf("expected existing setting to be preserved")
	}
	if !strings.Contains(updated, "\"editor.tabSize\": 2") {
		t.Fatalf("expected existing setting to be preserved")
	}
	if !strings.Contains(updated, "},\n  // <<< agent-layer") {
		t.Fatalf("expected managed block to include trailing comma")
	}
	if strings.Index(updated, "// >>> agent-layer") > strings.Index(updated, "\"files.eol\":") {
		t.Fatalf("expected managed block to be inserted before existing fields")
	}
}

func TestWriteVSCodeSettingsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	project := &config.ProjectConfig{}
	if err := writeVSCodeSettings(RealSystem{}, file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteVSCodeSettingsInvalidJSONC(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "{\n  \"editor.tabSize\": 2,\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
		},
	}

	if err := writeVSCodeSettings(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteVSCodeSettingsInvalidJSONCExtraTokensBeforeRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "}\n{\n  \"editor.tabSize\": 2\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeNone},
		},
	}

	if err := writeVSCodeSettings(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}
