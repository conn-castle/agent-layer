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

func TestBuildUpgradeReadinessChecks_UnresolvedConfigPlaceholders(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(cfg), "id = \"context7\"\nenabled = false", "id = \"context7\"\nenabled = true", 1)
	if updated == string(cfg) {
		t.Fatal("failed to enable context7 in test config")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("AL_CONTEXT7_API_KEY", "")

	inst := &installer{root: root, sys: RealSystem{}}
	checks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		t.Fatalf("buildUpgradeReadinessChecks: %v", err)
	}
	check := findReadinessCheckByID(checks, readinessCheckUnresolvedPlaceholders)
	if check == nil {
		t.Fatalf("expected %s check", readinessCheckUnresolvedPlaceholders)
	}
	if !strings.Contains(strings.Join(check.Details, "\n"), "AL_CONTEXT7_API_KEY") {
		t.Fatalf("expected unresolved placeholder detail, got %q", check.Details)
	}
}

func TestBuildUpgradeReadinessChecks_ProcessEnvOverridesDotenv_UsesSystemLookupEnv(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(cfg), "id = \"context7\"\nenabled = false", "id = \"context7\"\nenabled = true", 1)
	if updated == string(cfg) {
		t.Fatal("failed to enable context7 in test config")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", ".env"), []byte("AL_CONTEXT7_API_KEY=from-file\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	sys := newFaultSystem(RealSystem{})
	processValue := "from-process"
	sys.lookupEnvs["AL_CONTEXT7_API_KEY"] = &processValue

	inst := &installer{root: root, sys: sys}
	checks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		t.Fatalf("buildUpgradeReadinessChecks: %v", err)
	}
	check := findReadinessCheckByID(checks, readinessCheckProcessEnvOverridesDotenv)
	if check == nil {
		t.Fatalf("expected %s check", readinessCheckProcessEnvOverridesDotenv)
	}
	if !strings.Contains(strings.Join(check.Details, "\n"), "AL_CONTEXT7_API_KEY") {
		t.Fatalf("expected process override detail, got %q", check.Details)
	}
}

func TestBuildUpgradeReadinessChecks_IgnoredEmptyDotenvAssignments_UsesSystemLookupEnv(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(cfg), "id = \"context7\"\nenabled = false", "id = \"context7\"\nenabled = true", 1)
	if updated == string(cfg) {
		t.Fatal("failed to enable context7 in test config")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", ".env"), []byte("AL_CONTEXT7_API_KEY=\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	sys := newFaultSystem(RealSystem{})
	processValue := "from-process"
	sys.lookupEnvs["AL_CONTEXT7_API_KEY"] = &processValue

	inst := &installer{root: root, sys: sys}
	checks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		t.Fatalf("buildUpgradeReadinessChecks: %v", err)
	}
	check := findReadinessCheckByID(checks, readinessCheckIgnoredEmptyDotenvAssignments)
	if check == nil {
		t.Fatalf("expected %s check", readinessCheckIgnoredEmptyDotenvAssignments)
	}
	if !strings.Contains(strings.Join(check.Details, "\n"), "AL_CONTEXT7_API_KEY") {
		t.Fatalf("expected empty-assignment detail, got %q", check.Details)
	}
}

func TestBuildUpgradeReadinessChecks_PathExpansionAnomalies(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(cfg), "id = \"filesystem\"\nenabled = false", "id = \"filesystem\"\nenabled = true", 1)
	if updated == string(cfg) {
		t.Fatal("failed to enable filesystem in test config")
	}
	updated = strings.Replace(updated, "${AL_REPO_ROOT}\"]", "${AL_REPO_ROOT}/missing-readiness-dir\"]", 1)
	if !strings.Contains(updated, "missing-readiness-dir") {
		t.Fatal("failed to update filesystem path in test config")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	checks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		t.Fatalf("buildUpgradeReadinessChecks: %v", err)
	}
	check := findReadinessCheckByID(checks, readinessCheckPathExpansionAnomalies)
	if check == nil {
		t.Fatalf("expected %s check", readinessCheckPathExpansionAnomalies)
	}
	if !strings.Contains(strings.Join(check.Details, "\n"), "missing path") {
		t.Fatalf("expected path anomaly detail, got %q", check.Details)
	}
}

func TestBuildUpgradeReadinessChecks_MissingRequiredConfigFields(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	// Write a config that is valid TOML but missing required fields (e.g. claude_vscode).
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	partialConfig := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true
[agents.claude]
enabled = true
[agents.codex]
enabled = false
[agents.vscode]
enabled = true
[agents.antigravity]
enabled = false
`
	if err := os.WriteFile(configPath, []byte(partialConfig), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	inst := &installer{root: root, sys: RealSystem{}}
	checks, err := buildUpgradeReadinessChecks(inst)
	if err != nil {
		t.Fatalf("buildUpgradeReadinessChecks: %v", err)
	}
	check := findReadinessCheckByID(checks, readinessCheckMissingRequiredConfigFields)
	if check == nil {
		t.Fatalf("expected %s check", readinessCheckMissingRequiredConfigFields)
	}
	joined := strings.Join(check.Details, "\n")
	if !strings.Contains(joined, "claude_vscode") {
		t.Fatalf("expected validation error about claude_vscode, got %q", joined)
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
