package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conn-castle/agent-layer/internal/templates"
)

// injectMCPCatalogIntoSeed appends the wizard MCP catalog blocks into a seeded
// .agent-layer/config.toml. The slim install seed deliberately ships zero
// [[mcp.servers]] blocks; readiness tests that need to mutate specific server
// blocks (context7, playwright, etc.) call this helper after Run() to
// recreate the legacy "all defaults present, disabled" shape they were written for.
func injectMCPCatalogIntoSeed(t *testing.T, root string) {
	t.Helper()
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	existing, err := os.ReadFile(configPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read seeded config: %v", err)
	}
	catalog, err := templates.Read("mcp-catalog.toml")
	if err != nil {
		t.Fatalf("read mcp-catalog.toml: %v", err)
	}
	combined := strings.TrimRight(string(existing), "\n") + "\n\n" + string(catalog)
	if err := os.WriteFile(configPath, []byte(combined), 0o600); err != nil {
		t.Fatalf("write seeded config with catalog: %v", err)
	}
}

func TestBuildUpgradeReadinessChecks_UnrecognizedConfigKeys(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	cfg = append(cfg, []byte("\n[unknown_section]\nvalue = true\n")...)
	if err := os.WriteFile(configPath, cfg, 0o600); err != nil {
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
	if err := os.MkdirAll(filepath.Join(vscodeDir, "prompts"), 0o700); err != nil {
		t.Fatalf("mkdir prompts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vscodeDir, "mcp.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write mcp.json: %v", err)
	}
	settings := "{\n  // >>> agent-layer\n  // managed\n  // <<< agent-layer\n}\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatalf("write settings.json: %v", err)
	}
	prompt := "---\nname: alpha\n---\n<!--\n  GENERATED FILE\n-->\n"
	if err := os.WriteFile(filepath.Join(vscodeDir, "prompts", "alpha.prompt.md"), []byte(prompt), 0o600); err != nil {
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
	injectMCPCatalogIntoSeed(t, root)

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(cfg), "id = \"playwright\"\nenabled = false", "id = \"playwright\"\nenabled = true", 1)
	if updated == string(cfg) {
		t.Fatal("failed to enable playwright server in test config")
	}
	updated = strings.Replace(updated, "@playwright/mcp@0.0.68", "@playwright/mcp@latest", 1)
	if !strings.Contains(updated, "@playwright/mcp@latest") {
		t.Fatal("failed to set floating playwright dependency in test config")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
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
	if !strings.Contains(joined, "@playwright/mcp@latest") {
		t.Fatalf("expected floating dependency detail, got %q", joined)
	}
}

func TestBuildUpgradeReadinessChecks_DisabledAgentArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := string(cfg)
	if strings.Contains(updated, "[agents.antigravity]\nenabled = true") {
		updated = strings.Replace(updated, "[agents.antigravity]\nenabled = true", "[agents.antigravity]\nenabled = false", 1)
	} else if !strings.Contains(updated, "[agents.antigravity]\nenabled = false") {
		t.Fatal("seeded config is missing agents.antigravity.enabled")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	settingsPath := filepath.Join(root, ".agy", "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatalf("mkdir antigravity dir: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("{\"permissions\":{\"allow\":[\"command(git)\"]}}\n"), 0o600); err != nil {
		t.Fatalf("write antigravity settings: %v", err)
	}

	mcpPath := filepath.Join(root, ".agy", "antigravity-cli", "mcp_config.json")
	if err := os.WriteFile(mcpPath, []byte("{\"mcpServers\":{}}\n"), 0o600); err != nil {
		t.Fatalf("write antigravity MCP config: %v", err)
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
	for _, expected := range []string{
		".agy/antigravity-cli/settings.json",
		".agy/antigravity-cli/mcp_config.json",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected disabled artifact detail %q, got %q", expected, joined)
		}
	}
}

func TestBuildUpgradeReadinessChecks_UnresolvedConfigPlaceholders(t *testing.T) {
	root := t.TempDir()
	if err := Run(root, Options{System: RealSystem{}}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	injectMCPCatalogIntoSeed(t, root)

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(cfg), "id = \"context7\"\nenabled = false", "id = \"context7\"\nenabled = true", 1)
	if updated == string(cfg) {
		t.Fatal("failed to enable context7 in test config")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
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
	injectMCPCatalogIntoSeed(t, root)

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(cfg), "id = \"context7\"\nenabled = false", "id = \"context7\"\nenabled = true", 1)
	if updated == string(cfg) {
		t.Fatal("failed to enable context7 in test config")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", ".env"), []byte("AL_CONTEXT7_API_KEY=from-file\n"), 0o600); err != nil {
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
	injectMCPCatalogIntoSeed(t, root)

	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := strings.Replace(string(cfg), "id = \"context7\"\nenabled = false", "id = \"context7\"\nenabled = true", 1)
	if updated == string(cfg) {
		t.Fatal("failed to enable context7 in test config")
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agent-layer", ".env"), []byte("AL_CONTEXT7_API_KEY=\n"), 0o600); err != nil {
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

	// The slim seed ships no MCP servers, so append a hand-authored stdio server
	// whose ${AL_REPO_ROOT}-relative path points at a directory that does not
	// exist. This exercises the path-expansion anomaly check without depending on
	// any catalog server (the filesystem server was removed from the catalog).
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	cfg, err := os.ReadFile(configPath) // #nosec G304 -- path is constructed from test-controlled inputs.
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	serverBlock := "\n[[mcp.servers]]\n" +
		"id = \"repo-fs\"\n" +
		"enabled = true\n" +
		"transport = \"stdio\"\n" +
		"command = \"npx\"\n" +
		"args = [\"-y\", \"@modelcontextprotocol/server-filesystem@2026.1.14\", \"${AL_REPO_ROOT}/missing-readiness-dir\"]\n"
	updated := strings.TrimRight(string(cfg), "\n") + "\n" + serverBlock
	if err := os.WriteFile(configPath, []byte(updated), 0o600); err != nil {
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

[agents.antigravity]
enabled = true
[agents.claude]
enabled = true
[agents.codex]
enabled = false
[agents.vscode]
enabled = true
[agents.copilot_cli]
enabled = false
`
	if err := os.WriteFile(configPath, []byte(partialConfig), 0o600); err != nil {
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
