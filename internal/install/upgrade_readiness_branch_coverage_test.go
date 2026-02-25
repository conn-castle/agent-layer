package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/templates"
)

func testBool(v bool) *bool { return &v }

func writeReadinessConfig(t *testing.T, root string, content string) {
	t.Helper()
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestBuildUpgradeReadinessChecks_ErrorPropagationBranches(t *testing.T) {
	t.Run("env read error propagates", func(t *testing.T) {
		root := t.TempDir()
		cfgBytes, err := templates.Read("config.toml")
		if err != nil {
			t.Fatalf("read template config: %v", err)
		}
		writeReadinessConfig(t, root, string(cfgBytes))

		fsys := newFaultSystem(RealSystem{})
		envPath := filepath.Join(root, ".agent-layer", ".env")
		fsys.readErrs[normalizePath(envPath)] = errors.New("env read boom")

		inst := &installer{root: root, sys: fsys}
		_, err = buildUpgradeReadinessChecks(inst)
		if err == nil || !strings.Contains(err.Error(), "env read boom") {
			t.Fatalf("expected env read error, got %v", err)
		}
	})

	t.Run("path anomaly checker error propagates", func(t *testing.T) {
		root := t.TempDir()
		configText := `[approvals]
mode = "none"
[agents.gemini]
enabled = false
[agents.claude]
enabled = false
[agents.claude_vscode]
enabled = false
[agents.codex]
enabled = false
[agents.vscode]
enabled = false
[agents.antigravity]
enabled = false
[[mcp.servers]]
id = "srv"
enabled = true
transport = "stdio"
command = "${AL_REPO_ROOT}/bin/tool"
`
		writeReadinessConfig(t, root, configText)

		fsys := newFaultSystem(RealSystem{})
		targetPath := filepath.Join(root, "bin", "tool")
		fsys.statErrs[normalizePath(targetPath)] = errors.New("stat boom")

		inst := &installer{root: root, sys: fsys}
		_, err := buildUpgradeReadinessChecks(inst)
		if err == nil || !strings.Contains(err.Error(), "stat boom") {
			t.Fatalf("expected path-stat error, got %v", err)
		}
	})

	t.Run("disabled artifact checker error propagates", func(t *testing.T) {
		root := t.TempDir()
		cfgBytes, err := templates.Read("config.toml")
		if err != nil {
			t.Fatalf("read template config: %v", err)
		}
		writeReadinessConfig(t, root, string(cfgBytes))

		fsys := newFaultSystem(RealSystem{})
		fsys.statErrs[normalizePath(filepath.Join(root, ".mcp.json"))] = errors.New("artifact stat boom")

		inst := &installer{root: root, sys: fsys}
		_, err = buildUpgradeReadinessChecks(inst)
		if err == nil || !strings.Contains(err.Error(), "artifact stat boom") {
			t.Fatalf("expected disabled artifact error, got %v", err)
		}
	})
}

func TestReadinessDetection_AdditionalBranches(t *testing.T) {
	t.Run("process env override equal value is ignored", func(t *testing.T) {
		cfg := &config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{{
					ID:      "srv",
					Enabled: testBool(true),
					Env: map[string]string{
						"AUTH": "${AL_TOKEN}",
					},
				}},
			},
		}
		sys := newFaultSystem(RealSystem{})
		val := "same-value"
		sys.lookupEnvs["AL_TOKEN"] = &val

		check := detectProcessEnvOverridesDotenv(cfg, map[string]string{"AL_TOKEN": "same-value"}, sys)
		if check != nil {
			t.Fatalf("expected no finding for equal values, got %#v", check)
		}
	})

	t.Run("path anomaly command detail append", func(t *testing.T) {
		root := t.TempDir()
		inst := &installer{root: root, sys: RealSystem{}}
		cfg := &config.Config{
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{{
					ID:        "srv",
					Enabled:   testBool(true),
					Transport: config.TransportStdio,
					Command:   "${AL_REPO_ROOT}/missing/tool",
				}},
			},
		}

		check, err := detectPathExpansionAnomalies(inst, cfg, map[string]string{})
		if err != nil {
			t.Fatalf("detectPathExpansionAnomalies: %v", err)
		}
		if check == nil || len(check.Details) == 0 {
			t.Fatalf("expected anomaly details, got %#v", check)
		}
	})

	t.Run("path expansion failure detail", func(t *testing.T) {
		inst := &installer{root: t.TempDir(), sys: RealSystem{}}
		detail, err := checkPathExpansionValue(inst, map[string]string{}, 0, "srv", "command", "~definitely_missing_user__/tool", true)
		if err != nil {
			t.Fatalf("checkPathExpansionValue: %v", err)
		}
		if !strings.Contains(detail, "failed to expand path") {
			t.Fatalf("expected expand-path detail, got %q", detail)
		}
	})

	t.Run("readinessSubstitutionEnv without placeholders", func(t *testing.T) {
		got := readinessSubstitutionEnv("plain-command", map[string]string{"AL_TOKEN": "x"}, t.TempDir(), RealSystem{})
		if len(got) != 0 {
			t.Fatalf("expected empty substitution env, got %#v", got)
		}
	})
}

func TestDetectVSCodeNoSyncStaleness_ClaudeBranches(t *testing.T) {
	t.Run("claude generated files present and up to date => no finding", func(t *testing.T) {
		root := t.TempDir()
		vscodeSettingsPath := filepath.Join(root, ".vscode", "settings.json")
		mcpPath := filepath.Join(root, ".mcp.json")
		claudeSettingsPath := filepath.Join(root, ".claude", "settings.json")
		if err := os.MkdirAll(filepath.Dir(vscodeSettingsPath), 0o755); err != nil {
			t.Fatalf("mkdir vscode dir: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(claudeSettingsPath), 0o755); err != nil {
			t.Fatalf("mkdir claude dir: %v", err)
		}
		if err := os.WriteFile(vscodeSettingsPath, []byte(vscodeManagedStart+"\n{}\n"+vscodeManagedEnd), 0o644); err != nil {
			t.Fatalf("write vscode settings: %v", err)
		}
		if err := os.WriteFile(mcpPath, []byte(`{"mcpServers":{"agent-layer":{},"mcp-prompts":{}}}`), 0o644); err != nil {
			t.Fatalf("write .mcp.json: %v", err)
		}
		if err := os.WriteFile(claudeSettingsPath, []byte(`{}`), 0o644); err != nil {
			t.Fatalf("write claude settings: %v", err)
		}

		cfg := &config.Config{
			Agents: config.AgentsConfig{
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: testBool(true)},
			},
		}
		inst := &installer{root: root, sys: RealSystem{}}
		check, err := detectVSCodeNoSyncStaleness(inst, cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now().Add(-1*time.Hour))
		if err != nil {
			t.Fatalf("detectVSCodeNoSyncStaleness: %v", err)
		}
		if check != nil {
			t.Fatalf("expected no stale-output finding, got %#v", check)
		}
	})

	t.Run("claude settings stat error propagates", func(t *testing.T) {
		root := t.TempDir()
		vscodeSettingsPath := filepath.Join(root, ".vscode", "settings.json")
		if err := os.MkdirAll(filepath.Dir(vscodeSettingsPath), 0o755); err != nil {
			t.Fatalf("mkdir vscode dir: %v", err)
		}
		if err := os.WriteFile(vscodeSettingsPath, []byte(vscodeManagedStart+"\n{}\n"+vscodeManagedEnd), 0o644); err != nil {
			t.Fatalf("write vscode settings: %v", err)
		}
		cfg := &config.Config{
			Agents: config.AgentsConfig{
				ClaudeVSCode: config.EnableOnlyConfig{Enabled: testBool(true)},
			},
		}
		fsys := newFaultSystem(RealSystem{})
		settingsPath := filepath.Join(root, ".claude", "settings.json")
		fsys.statErrs[normalizePath(settingsPath)] = errors.New("settings stat boom")

		inst := &installer{root: root, sys: fsys}
		_, err := detectVSCodeNoSyncStaleness(inst, cfg, filepath.Join(root, ".agent-layer", "config.toml"), time.Now())
		if err == nil || !strings.Contains(err.Error(), "settings stat boom") {
			t.Fatalf("expected claude settings stat error, got %v", err)
		}
	})
}

func TestReadinessFilesystemWalkErrCallbackBranches(t *testing.T) {
	root := t.TempDir()
	mdRoot := filepath.Join(root, ".agent-layer", "slash-commands")
	promptRoot := filepath.Join(root, ".vscode", "prompts")
	if err := os.MkdirAll(mdRoot, 0o755); err != nil {
		t.Fatalf("mkdir markdown root: %v", err)
	}
	if err := os.MkdirAll(promptRoot, 0o755); err != nil {
		t.Fatalf("mkdir prompt root: %v", err)
	}

	inst := &installer{root: root, sys: walkCallbackErrSystem{base: RealSystem{}}}

	_, err := countMarkdownFiles(inst, mdRoot)
	if err == nil || !strings.Contains(err.Error(), "walk callback boom") {
		t.Fatalf("expected markdown walk callback error, got %v", err)
	}

	_, _, err = listGeneratedFilesWithSuffix(inst, promptRoot, ".prompt.md")
	if err == nil || !strings.Contains(err.Error(), "walk callback boom") {
		t.Fatalf("expected generated-files walk callback error, got %v", err)
	}
}

func TestHasVSCodeManagedBlock_Branch(t *testing.T) {
	matched, err := hasVSCodeManagedBlock([]byte(vscodeManagedStart + "\n{}\n" + vscodeManagedEnd))
	if err != nil {
		t.Fatalf("hasVSCodeManagedBlock: %v", err)
	}
	if !matched {
		t.Fatal("expected managed block detection")
	}
}
