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
			Approvals: config.ApprovalsConfig{Mode: "commands"},
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
}

func TestBuildVSCodeSettingsEscapesSlash(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "commands"},
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
			Approvals: config.ApprovalsConfig{Mode: "commands"},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := WriteVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteVSCodeSettings error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".vscode", "settings.json")); err != nil {
		t.Fatalf("expected settings.json: %v", err)
	}
}

func TestBuildVSCodeSettingsYOLO(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "yolo"},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	settings, err := buildVSCodeSettings(project)
	if err != nil {
		t.Fatalf("buildVSCodeSettings error: %v", err)
	}
	if settings.ChatToolsGlobalAutoApprove == nil || !*settings.ChatToolsGlobalAutoApprove {
		t.Fatalf("expected ChatToolsGlobalAutoApprove=true for yolo mode")
	}
	if len(settings.ChatToolsTerminalAutoApprove) != 1 {
		t.Fatalf("expected 1 terminal auto-approve entry for yolo mode")
	}
}

func TestBuildVSCodeSettingsClaudeVSCodeYOLO(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "yolo"},
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
		t.Fatal("expected ClaudeCodeAllowDangerouslySkipPerms=true when claude-vscode enabled + yolo")
	}
}

func TestBuildVSCodeSettingsClaudeVSCodeNonYOLO(t *testing.T) {
	t.Parallel()
	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "all"},
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
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "{\n  // user setting\n  \"editor.formatOnSave\": true\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "commands"},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := WriteVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteVSCodeSettings error: %v", err)
	}

	updatedBytes, err := os.ReadFile(filepath.Join(vscodeDir, "settings.json"))
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
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "{\n  // >>> agent-layer\n  // Managed by Agent Layer. To customize, edit .agent-layer/config.toml\n  // and .agent-layer/commands.allow, then re-run `al sync`.\n  //\n  \"chat.tools.terminal.autoApprove\": {\n    \"/^old(\\\\b.*)?$/\": true\n  },\n  // <<< agent-layer\n  \"editor.tabSize\": 2\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "commands"},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := WriteVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteVSCodeSettings error: %v", err)
	}

	updatedBytes, err := os.ReadFile(filepath.Join(vscodeDir, "settings.json"))
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
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "{\n  // >>> agent-layer\n  // Managed by Agent Layer. To customize, edit .agent-layer/config.toml\n  // and .agent-layer/commands.allow, then re-run `al sync`.\n  //\n  \"chat.tools.terminal.autoApprove\": {\n    \"/^old(\\\\b.*)?$/\": true\n  },\n  // <<< agent-layer\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "commands"},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := WriteVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteVSCodeSettings error: %v", err)
	}

	updatedBytes, err := os.ReadFile(filepath.Join(vscodeDir, "settings.json"))
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
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "{\n  \"files.eol\": \"\\\\n\",\n  \"editor.tabSize\": 2\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "commands"},
			Agents:    config.AgentsConfig{VSCode: config.EnableOnlyConfig{Enabled: testutil.BoolPtr(true)}},
		},
		CommandsAllow: []string{"git status"},
	}

	if err := WriteVSCodeSettings(RealSystem{}, root, project); err != nil {
		t.Fatalf("WriteVSCodeSettings error: %v", err)
	}

	updatedBytes, err := os.ReadFile(filepath.Join(vscodeDir, "settings.json"))
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
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	project := &config.ProjectConfig{}
	if err := WriteVSCodeSettings(RealSystem{}, file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteVSCodeSettingsInvalidJSONC(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "{\n  \"editor.tabSize\": 2,\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
	}

	if err := WriteVSCodeSettings(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteVSCodeSettingsInvalidJSONCExtraTokensBeforeRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existing := "}\n{\n  \"editor.tabSize\": 2\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(existing), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Approvals: config.ApprovalsConfig{Mode: "none"},
		},
	}

	if err := WriteVSCodeSettings(RealSystem{}, root, project); err == nil {
		t.Fatalf("expected error")
	}
}
