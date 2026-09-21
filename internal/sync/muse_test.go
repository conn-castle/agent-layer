package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDisabledMuseCleanupPreservesNativeSettings(t *testing.T) {
	for _, test := range []struct {
		name, input string
		strip       bool
	}{
		{"newer schema", `{"schema_version":2,"theme":"dark"}`, false},
		{"unversioned", `{"theme":"dark"}`, false},
		{"empty", ``, false},
		{"managed MCP in newer schema", `{"schema_version":2,"theme":"dark","mcpServers":{"muse-agent-layer":{}},"agentLayerManagedMcpServers":["muse-agent-layer"]}`, true},
		{"untracked MCP preserved", `{"schema_version":2,"theme":"dark","mcpServers":{"muse-agent-layer":{}}}`, false},
		{"legacy MCP", `{"schema_version":1,"theme":"dark","mcp_servers":{"old":{}}}`, false},
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

func TestDisabledMuseCleanupRemovesOnlyTrackedEntries(t *testing.T) {
	root := t.TempDir()
	path := museSettingsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	input := `{"schema_version":1,"theme":"dark","mcpServers":{"muse-local":{"type":"stdio","command":"tool"},"user-srv":{"type":"stdio","command":"user-tool"}},"agentLayerManagedMcpServers":["muse-local","muse-stale"]}`
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cleanMuseSettings(RealSystem{}, root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	servers, ok := got["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("user-owned mcpServers lost: %s", data)
	}
	if len(servers) != 1 {
		t.Fatalf("cleanup kept unexpected servers: %s", data)
	}
	user, ok := servers["user-srv"].(map[string]any)
	if !ok || user["command"] != "user-tool" {
		t.Fatalf("user-owned server lost or altered: %s", data)
	}
	if got["theme"] != "dark" {
		t.Fatalf("native key lost: %s", data)
	}
	if _, exists := got[agentLayerManagedMcpKey]; exists {
		t.Fatalf("tracking record retained: %s", data)
	}
}

func TestDisabledMuseCleanupPreservesUnownedDirectorySymlink(t *testing.T) {
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
	if err := cleanMuseSettings(RealSystem{}, root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- test-controlled temporary path.
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(original) {
		t.Fatal("cleanup modified external configuration")
	}
}
