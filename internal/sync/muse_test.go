package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/projection"
)

func museEnabled(value bool) *bool { return &value }

func TestWriteMuseSettingsPreservesNativeKeysAndProjectsMCP(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".muse-config", "muse", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"theme":"native","mcp_servers":{"old":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	project := &config.ProjectConfig{
		Root: root,
		Env:  map[string]string{config.BuiltinRepoRootEnvVar: root},
		Config: config.Config{
			Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: museEnabled(true)}},
			MCP:    config.MCPConfig{Servers: []config.MCPServer{{ID: "local", Enabled: museEnabled(true), Transport: config.TransportStdio, Command: "tool", Args: []string{"${AL_REPO_ROOT}"}}}},
		},
	}
	if err := writeMuseSettings(RealSystem{}, root, project); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["theme"] != "native" {
		t.Fatalf("native key lost: %s", data)
	}
	if _, exists := got["mcp_servers"]; exists {
		t.Fatalf("legacy alias retained: %s", data)
	}
	servers := got["mcpServers"].(map[string]any)
	if servers["muse-agent-layer"] == nil || servers["muse-local"] == nil {
		t.Fatalf("missing projected servers: %s", data)
	}
	local := servers["muse-local"].(map[string]any)
	if local["type"] != "stdio" || local["mode"] != "optional" {
		t.Fatalf("bad stdio shape: %#v", local)
	}
	if mode := dataMode(t, path); mode != 0o600 {
		t.Fatalf("mode = %o", mode)
	}
}

func TestWriteMuseSettingsRejectsDefaultSSE(t *testing.T) {
	root := t.TempDir()
	project := &config.ProjectConfig{
		Root: root,
		Env:  map[string]string{config.BuiltinRepoRootEnvVar: root},
		Config: config.Config{
			Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: museEnabled(true)}},
			MCP:    config.MCPConfig{Servers: []config.MCPServer{{ID: "legacy", Enabled: museEnabled(true), Transport: config.TransportHTTP, URL: "https://example.test/mcp"}}},
		},
	}
	if err := writeMuseSettings(RealSystem{}, root, project); err == nil {
		t.Fatal("expected unsupported SSE error")
	}
}

func TestWriteMuseSettingsRejectsUnsupportedExistingSchema(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".muse-config", "muse", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":2,"future":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	project := &config.ProjectConfig{Root: root, Config: config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: museEnabled(true)}}}}
	err := writeMuseSettings(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "unsupported Muse settings schema_version 2") {
		t.Fatalf("error = %v, want unsupported schema failure", err)
	}
	data, readErr := os.ReadFile(path) // #nosec G304 -- test-controlled path.
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != `{"schema_version":2,"future":true}` {
		t.Fatalf("unsupported settings were modified: %s", data)
	}
}

func TestWriteMuseSettingsRejectsExistingFileWithoutSchema(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".muse-config", "muse", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"native"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	project := &config.ProjectConfig{Root: root, Config: config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: museEnabled(true)}}}}
	err := writeMuseSettings(RealSystem{}, root, project)
	if err == nil || !strings.Contains(err.Error(), "must declare numeric schema_version 1") {
		t.Fatalf("error = %v, want missing schema failure", err)
	}
}

func dataMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestMuseAndClaudeMCPRemainIndependent(t *testing.T) {
	root := t.TempDir()
	project := &config.ProjectConfig{
		Root: root,
		Env:  map[string]string{config.BuiltinRepoRootEnvVar: root, "AL_TEST_TOKEN": "fixture-value"},
		Config: config.Config{
			Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: museEnabled(true)}, Claude: config.ClaudeConfig{Enabled: museEnabled(true)}},
			MCP: config.MCPConfig{Servers: []config.MCPServer{
				{ID: "shared", Enabled: museEnabled(true), Transport: config.TransportStdio, Command: "fixture", Env: map[string]string{"TOKEN": "${AL_TEST_TOKEN}"}}, // #nosec G101 -- variable reference, not a credential.
				{ID: "claude-only", Enabled: museEnabled(true), Transport: config.TransportStdio, Command: "fixture", Clients: []string{"claude"}},
				{ID: "muse-only", Enabled: museEnabled(true), Transport: config.TransportStdio, Command: "fixture", Clients: []string{"muse"}},
			}},
		},
	}
	if err := writeMCPConfig(RealSystem{}, root, project); err != nil {
		t.Fatal(err)
	}
	claudeBefore, err := os.ReadFile(filepath.Join(root, ".mcp.json")) // #nosec G304 -- test-controlled temporary path.
	if err != nil {
		t.Fatal(err)
	}
	if err := writeMuseSettings(RealSystem{}, root, project); err != nil {
		t.Fatal(err)
	}
	claudeAfter, err := os.ReadFile(filepath.Join(root, ".mcp.json")) // #nosec G304 -- test-controlled temporary path.
	if err != nil {
		t.Fatal(err)
	}
	if string(claudeBefore) != string(claudeAfter) {
		t.Fatal("Muse changed Claude configuration")
	}
	if !strings.Contains(string(claudeAfter), "${AL_TEST_TOKEN}") || strings.Contains(string(claudeAfter), "fixture-value") {
		t.Fatal("Claude placeholder semantics changed")
	}
	data, err := os.ReadFile(museSettingsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{projection.BuiltInDispatchServerID, "shared", "claude-only"} {
		if document.Servers[id][museEnabledKey] != false {
			t.Errorf("project server %s not suppressed in Muse", id)
		}
	}
	if document.Servers["muse-claude-only"] != nil {
		t.Fatal("Claude-only server projected into Muse")
	}
	if document.Servers["muse-muse-only"] == nil {
		t.Fatal("Muse-only server omitted")
	}
	shared := document.Servers["muse-shared"]
	if shared["env"].(map[string]any)["TOKEN"] != "fixture-value" {
		t.Fatal("Muse secret not resolved")
	}
	builtin := document.Servers["muse-agent-layer"]
	if builtin["mode"] != museRequiredMode || builtin["tool_timeout_sec"] != float64(2400) {
		t.Fatalf("Muse dispatch startup/timeout lost: %#v", builtin)
	}
}

func TestMuseAvoidsProjectServerNameCollision(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(`{"mcpServers":{"muse-agent-layer":{"command":"project-only"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	project := &config.ProjectConfig{Root: root, Env: map[string]string{config.BuiltinRepoRootEnvVar: root}, Config: config.Config{Agents: config.AgentsConfig{Muse: config.AgentConfig{Enabled: museEnabled(true)}}}}
	if err := writeMuseSettings(RealSystem{}, root, project); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(museSettingsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Servers["muse-agent-layer"][museEnabledKey] != false || document.Servers["muse-muse-agent-layer"]["mode"] != museRequiredMode {
		t.Fatalf("project collision shadows Muse dispatch: %s", data)
	}
}

func TestDisabledMuseCleanupPreservesNativeSettings(t *testing.T) {
	for _, test := range []struct {
		name, input string
		strip       bool
	}{
		{"newer schema", `{"schema_version":2,"theme":"dark"}`, false},
		{"unversioned", `{"theme":"dark"}`, false},
		{"empty", ``, false},
		{"managed MCP in newer schema", `{"schema_version":2,"theme":"dark","mcpServers":{"muse-agent-layer":{}}}`, true},
		{"legacy MCP", `{"schema_version":1,"theme":"dark","mcp_servers":{"old":{}}}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := museSettingsPath(root)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(test.input), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := cleanMuseSettings(RealSystem{}, root); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
			if err != nil {
				t.Fatal(err)
			}
			if !test.strip {
				if string(data) != test.input {
					t.Fatalf("unmanaged settings changed: %s", data)
				}
				return
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			if got["theme"] != "dark" || got["mcpServers"] != nil || got["mcp_servers"] != nil {
				t.Fatalf("cleanup changed native settings or retained MCP: %s", data)
			}
			var original map[string]any
			if err := json.Unmarshal([]byte(test.input), &original); err != nil {
				t.Fatal(err)
			}
			if got["schema_version"] != original["schema_version"] {
				t.Fatal("cleanup changed native schema")
			}
		})
	}
}

func TestDisabledMuseCleanupRejectsDirectorySymlink(t *testing.T) {
	root, external := t.TempDir(), t.TempDir()
	path := filepath.Join(external, "muse", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte(`{"schema_version":1,"mcpServers":{"external":{}}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, ".muse-config")); err != nil {
		t.Fatal(err)
	}
	if err := cleanMuseSettings(RealSystem{}, root); err == nil {
		t.Fatal("expected symlink rejection")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatal("cleanup modified external configuration")
	}
}
