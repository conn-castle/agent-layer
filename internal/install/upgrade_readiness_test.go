package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildUpgradeReadinessChecks_UnrecognizedConfigKeys(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg = append(cfg, []byte("\n[unknown_section]\nvalue = true\n")...)
	if err := os.WriteFile(configPath, cfg, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	checks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		t.Fatalf("buildUpgradeReadinessChecks: %v", err)
	}
	check := findReadinessCheckByID(checks, readinessCheckUnrecognizedConfigKeys)
	if check == nil {
		t.Fatalf("expected %s check", readinessCheckUnrecognizedConfigKeys)
	}
}

func TestBuildUpgradeReadinessChecks_VSCodeNoSyncStaleByMTime(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	vscodeDir := filepath.Join(root, ".vscode")
	if err := os.MkdirAll(filepath.Join(vscodeDir, "prompts"), 0o755); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vscodeDir, "mcp.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}
	settings := "{\n  // >>> agent-layer\n  // managed\n  // <<< agent-layer\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(settings), 0o644); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
	prompt := "---\nname: alpha\n---\n<!--\n  GENERATED FILE\n-->\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "prompts", "alpha.prompt.md"), []byte(prompt), 0o644); err != nil {
		t.Fatalf("write prompt: %v", err)
	}

	base := time.Now().Add(-2 * time.Hour)
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	if err := os.Chtimes(filepath.Join(vscodeDir, "mcp.json"), base, base); err != nil {
		t.Fatalf("chtime mcp: %v", err)
	}
	if err := os.Chtimes(filepath.Join(vscodeDir, "settings.json"), base, base); err != nil {
		t.Fatalf("chtime settings: %v", err)
	}
	if err := os.Chtimes(filepath.Join(vscodeDir, "prompts", "alpha.prompt.md"), base, base); err != nil {
		t.Fatalf("chtime prompt: %v", err)
	}
	configTime := base.Add(90 * time.Minute)
	if err := os.Chtimes(configPath, configTime, configTime); err != nil {
		t.Fatalf("chtime config: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	checks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		t.Fatalf("buildUpgradeReadinessChecks: %v", err)
	}
	check := findReadinessCheckByID(checks, readinessCheckVSCodeNoSyncStaleOutput)
	if check == nil {
		t.Fatalf("expected %s check", readinessCheckVSCodeNoSyncStaleOutput)
	}
	joined := strings.Join(check.Details, "\n")
	if !strings.Contains(joined, "config.toml is newer than generated VS Code outputs") {
		t.Fatalf("expected config mtime detail, got %q", joined)
	}
}

func TestBuildUpgradeReadinessChecks_FloatingDependenciesEnabledOnly(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(cfg), "id = \"ripgrep\"\nenabled = false", "id = \"ripgrep\"\nenabled = true", 1)
	if updated == string(cfg) {
		t.Fatal("failed to enable ripgrep server in test config")
	}
	updated = strings.Replace(updated, "mcp-ripgrep@0.4.0", "mcp-ripgrep@latest", 1)
	if !strings.Contains(updated, "mcp-ripgrep@latest") {
		t.Fatal("failed to set floating ripgrep dependency in test config")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	checks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		t.Fatalf("buildUpgradeReadinessChecks: %v", err)
	}
	check := findReadinessCheckByID(checks, readinessCheckFloatingDependencies)
	if check == nil {
		t.Fatalf("expected %s check", readinessCheckFloatingDependencies)
	}
	joined := strings.Join(check.Details, "\n")
	if !strings.Contains(joined, "mcp-ripgrep@latest") {
		t.Fatalf("expected floating dependency detail, got %q", joined)
	}
}

func TestBuildUpgradeReadinessChecks_DisabledAgentArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(cfg), "[agents.gemini]\nenabled = true", "[agents.gemini]\nenabled = false", 1)
	if updated == string(cfg) {
		t.Fatal("failed to disable gemini in test config")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	geminiPath := filepath.Join(root, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(geminiPath), 0o755); err != nil {
		t.Fatalf("mkdir gemini dir: %v", err)
	}
	generatedGemini := "{\n  \"mcpServers\": {\n    \"agent-layer\": {\n      \"command\": \"al\",\n      \"args\": [\"mcp-prompts\"]\n    }\n  }\n}\n"
	if err := os.WriteFile(geminiPath, []byte(generatedGemini), 0o644); err != nil {
		t.Fatalf("write gemini settings: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	checks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		t.Fatalf("buildUpgradeReadinessChecks: %v", err)
	}
	check := findReadinessCheckByID(checks, readinessCheckDisabledArtifacts)
	if check == nil {
		t.Fatalf("expected %s check", readinessCheckDisabledArtifacts)
	}
	joined := strings.Join(check.Details, "\n")
	if !strings.Contains(joined, ".gemini/settings.json") {
		t.Fatalf("expected disabled artifact detail, got %q", joined)
	}
}

func findReadinessCheckByID(checks []UpgradeReadinessCheck, id string) *UpgradeReadinessCheck {
	for _, check := range checks {
		if check.ID == id {
			c := check
			return &c
		}
	}
	return nil
}
