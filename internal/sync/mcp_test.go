package sync

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildMCPConfig(t *testing.T) {
	t.Parallel()
	enabled := true
	root := t.TempDir()
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{Claude: config.ClaudeConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "example",
						Enabled:   &enabled,
						Transport: "http",
						URL:       "https://example.com?token=${TOKEN}",
						Headers: map[string]string{
							"Authorization": "Bearer ${TOKEN}",
						},
					},
				},
			},
		},
		Env:  map[string]string{"TOKEN": "abc"},
		Root: root,
	}

	cfg, err := buildMCPConfig(project)
	if err != nil {
		t.Fatalf("buildMCPConfig error: %v", err)
	}
	if cfg.GeneratedBy != "agent-layer" {
		t.Fatalf("expected _generatedBy agent-layer, got %q", cfg.GeneratedBy)
	}
	if cfg.Servers["example"].Type != "http" {
		t.Fatalf("unexpected server type: %s", cfg.Servers["example"].Type)
	}
	if cfg.Servers["example"].URL != "https://example.com?token=${TOKEN}" {
		t.Fatalf("unexpected url: %s", cfg.Servers["example"].URL)
	}
	if cfg.Servers["example"].Headers["Authorization"] != "Bearer ${TOKEN}" {
		t.Fatalf("unexpected header: %s", cfg.Servers["example"].Headers["Authorization"])
	}
}

func TestWriteMCPConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
	}
	enabled := true
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{Claude: config.ClaudeConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "example",
						Enabled:   &enabled,
						Transport: "http",
						URL:       "https://example.com?token=${TOKEN}",
					},
				},
			},
		},
		Env:  map[string]string{"TOKEN": "abc"},
		Root: root,
	}

	if err := writeMCPConfig(sys, root, project); err != nil {
		t.Fatalf("writeMCPConfig error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".mcp.json")); err != nil {
		t.Fatalf("expected mcp.json: %v", err)
	}
}

func TestWriteMCPConfigError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	project := &config.ProjectConfig{Root: root}
	if err := writeMCPConfig(sys, file, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWriteMCPConfigWriteError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
	}
	if err := os.Mkdir(filepath.Join(root, ".mcp.json"), 0o700); err != nil {
		t.Fatalf("mkdir .mcp.json: %v", err)
	}
	project := &config.ProjectConfig{
		Config: config.Config{
			MCP: config.MCPConfig{Servers: nil},
		},
		Root: root,
	}
	if err := writeMCPConfig(sys, root, project); err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildMCPConfigMissingEnv(t *testing.T) {
	t.Parallel()
	enabled := true
	root := t.TempDir()
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{Claude: config.ClaudeConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "example",
						Enabled:   &enabled,
						Transport: "http",
						URL:       "https://example.com?token=${TOKEN}",
					},
				},
			},
		},
		Env:  map[string]string{},
		Root: root,
	}

	_, err := buildMCPConfig(project)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestBuildMCPConfigStdioServer(t *testing.T) {
	t.Parallel()
	enabled := true
	root := t.TempDir()
	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{Claude: config.ClaudeConfig{Enabled: &enabled}},
			MCP: config.MCPConfig{
				Servers: []config.MCPServer{
					{
						ID:        "stdio",
						Enabled:   &enabled,
						Transport: "stdio",
						Command:   "tool",
						Args:      []string{"--flag", "${TOKEN}"},
						Env: map[string]string{
							"TOKEN": "${TOKEN}",
						},
					},
				},
			},
		},
		Env:  map[string]string{"TOKEN": "abc"},
		Root: root,
	}

	cfg, err := buildMCPConfig(project)
	if err != nil {
		t.Fatalf("buildMCPConfig error: %v", err)
	}
	server, ok := cfg.Servers["stdio"]
	if !ok {
		t.Fatalf("expected stdio server")
	}
	if server.Type != "stdio" {
		t.Fatalf("unexpected type: %s", server.Type)
	}
	if server.Command != "tool" {
		t.Fatalf("unexpected command: %s", server.Command)
	}
	if len(server.Args) != 2 || server.Args[1] != "${TOKEN}" {
		t.Fatalf("unexpected args: %#v", server.Args)
	}
	if server.Env["TOKEN"] != "${TOKEN}" {
		t.Fatalf("unexpected env: %s", server.Env["TOKEN"])
	}
}

func TestWriteMCPConfigMarshalError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sys := &MockSystem{
		Fallback: RealSystem{},
		MarshalIndentFunc: func(v any, prefix, indent string) ([]byte, error) {
			return nil, errors.New("marshal failed")
		},
	}
	project := &config.ProjectConfig{
		Config: config.Config{},
		Root:   root,
	}

	if err := writeMCPConfig(sys, root, project); err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestSharedMuseClaudeMCP(t *testing.T) {
	root := t.TempDir()
	enabled := true
	project := &config.ProjectConfig{Root: root, Env: map[string]string{"TOKEN": "private-value"}, Config: config.Config{
		Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}, Claude: config.ClaudeConfig{Enabled: &enabled}},
		MCP: config.MCPConfig{Servers: []config.MCPServer{
			{ID: "shared", Enabled: &enabled, Transport: config.TransportHTTP, HTTPTransport: config.HTTPTransportStreamable, URL: "https://example.test/mcp", Headers: map[string]string{"Authorization": "Bearer ${TOKEN}"}},
			{ID: "claude-only", Enabled: &enabled, Clients: []string{"claude"}, Transport: config.TransportStdio, Command: "claude-tool"},
			{ID: "muse-only", Enabled: &enabled, Clients: []string{"muse"}, Transport: config.TransportStdio, Command: "muse-tool"},
		}}}}
	if err := writeMCPConfig(RealSystem{}, root, project); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".mcp.json")) // #nosec G304 -- test-owned project.
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Servers["shared"]["type"] != "streamable-http" || document.Servers["shared"]["headers"].(map[string]any)["Authorization"] != "Bearer private-value" {
		t.Fatalf("bad HTTP definition: %s", data)
	}
	if document.Servers["claude-only"]["enabled"] != false || document.Servers["muse-only"]["enabled"] != true {
		t.Fatalf("Muse filters lost: %s", data)
	}
	if document.Servers["agent-layer"]["tool_timeout_sec"] == nil {
		t.Fatalf("dispatch timeout lost: %s", data)
	}
	info, err := os.Stat(filepath.Join(root, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatal("resolved secrets must be private")
	}
	settings, err := buildClaudeSettings(root, project)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(settings["disabledMcpjsonServers"].([]string), "muse-only") {
		t.Fatal("Claude filter lost")
	}
	project.Config.Agents.Claude.Enabled = nil
	project.Config.MCP.Servers[1].Args = []string{"${MISSING_CLAUDE_ONLY_TOKEN}"}
	cfg, err := buildMCPConfig(project)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Servers["claude-only"]; ok {
		t.Fatal("Muse-only installation acquired Claude server")
	}
	project.Config.MCP.Servers[0].HTTPTransport = config.HTTPTransportSSE
	if _, err := buildMCPConfig(project); err == nil {
		t.Fatal("Muse SSE should fail explicitly")
	}
}

func TestMuseOnlySyncMigratesOwnedSettingsWithoutMovingNativeState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	root, _ := loadSyncFixtureProject(t)
	settings := museSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"schema_version":1,"theme":"dark","mcpServers":{"muse-owned":{"command":"old"},"user-owned":{"command":"keep"}},"agentLayerManagedMcpServers":["muse-owned"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	preserved := []string{filepath.Join(root, ".muse-config", "muse", "auth.json"), filepath.Join(root, ".muse-data", "muse", "session.json")}
	for _, path := range preserved {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("native-state"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	enabled := true
	project := &config.ProjectConfig{Root: root, Env: map[string]string{config.BuiltinRepoRootEnvVar: root}, Config: config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}}}}
	if _, err := RunWithProject(RealSystem{}, root, project); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".mcp.json")) // #nosec G304 -- test-owned project.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"agent-layer"`) {
		t.Fatalf("Muse-only sync omitted built-in MCP: %s", data)
	}
	data, err = os.ReadFile(settings) // #nosec G304 -- test-owned project.
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "muse-owned") || !strings.Contains(string(data), "user-owned") || !strings.Contains(string(data), "dark") {
		t.Fatalf("incorrect migration: %s", data)
	}
	for _, path := range preserved {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != "native-state" {
			t.Fatalf("native state changed at %s: %v", path, err)
		}
	} // #nosec G304 -- test-owned project.
}

func TestMuseMCPRefusesTrackedOrUnignoredSecrets(t *testing.T) {
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "init.templateDir="}, args...)...)
		cmd.Dir = root
		// Commit hooks export repository selectors; fixture setup must never
		// initialize or stage files in that inherited repository.
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			switch key {
			case "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR",
				"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE":
				continue
			}
			cmd.Env = append(cmd.Env, entry)
		}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v: %s", err, output)
		}
	}
	runGit("init", "--quiet")
	enabled := true
	const secret = "synthetic-private-header-value"
	project := &config.ProjectConfig{Root: root, Env: map[string]string{"TOKEN": secret}, Config: config.Config{
		Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: &enabled}},
		MCP:    config.MCPConfig{Servers: []config.MCPServer{{ID: "private", Enabled: &enabled, Transport: config.TransportHTTP, HTTPTransport: config.HTTPTransportStreamable, URL: "https://example.test/mcp", Headers: map[string]string{"Authorization": "Bearer ${TOKEN}"}}}},
	}}
	path := filepath.Join(root, ".mcp.json")
	original := []byte(`{"mcpServers":{}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeMCPConfig(RealSystem{}, root, project); err == nil || !strings.Contains(err.Error(), "untracked and gitignored") {
		t.Fatalf("unignored file: %v", err)
	}
	runGit("add", ".mcp.json")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/.mcp.json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeMCPConfig(RealSystem{}, root, project); err == nil {
		t.Fatal("tracked ignored file was overwritten")
	} else if strings.Contains(err.Error(), secret) {
		t.Fatal("secret leaked through the protective write error")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-owned project.
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("unsafe output changed: %s %v", data, err)
	}
	runGit("rm", "--cached", ".mcp.json")
	if err := writeMCPConfig(RealSystem{}, root, project); err != nil {
		t.Fatalf("safe ignored output rejected: %v", err)
	}
}

func TestNoMCPConsumersRemoveOnlyGeneratedOutput(t *testing.T) {
	root, project := loadSyncFixtureProject(t)
	project.Config.Agents = config.AgentsConfig{}
	path := filepath.Join(root, ".mcp.json")
	for _, test := range []struct {
		body        string
		wantRemoved bool
	}{
		{`{"_generatedBy":"agent-layer","mcpServers":{"old":{"env":{"TOKEN":"resolved-secret"}}}}`, true},
		{`{"mcpServers":{"user-owned":{"command":"keep"}}}`, false},
		{"", false},
		{"{invalid JSON", false},
	} {
		if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := RunWithProject(RealSystem{}, root, project); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path) // #nosec G304 -- test-owned project.
		if test.wantRemoved {
			if !os.IsNotExist(err) {
				t.Fatalf("generated secret file retained: %v", err)
			}
		} else if err != nil || string(data) != test.body {
			t.Fatalf("user file changed: %s %v", data, err)
		}
	}
}
