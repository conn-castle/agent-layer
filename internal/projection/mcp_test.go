package projection

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestResolveMCPServers(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "http",
			Enabled:   &enabled,
			Clients:   []string{"gemini"},
			Transport: "http",
			URL:       "https://example.com?token=${TOKEN}",
			Headers: map[string]string{
				"Authorization": "Bearer ${TOKEN}",
			},
		},
		{
			ID:        "stdio",
			Enabled:   &enabled,
			Clients:   []string{"gemini"},
			Transport: "stdio",
			Command:   "tool",
			Args:      []string{"--token", "${TOKEN}"},
			Env: map[string]string{
				"TOKEN": "${TOKEN}",
			},
		},
	}
	env := map[string]string{"TOKEN": "abc123"}

	resolved, err := ResolveMCPServers(servers, env, "gemini", nil)
	if err != nil {
		t.Fatalf("resolve mcp servers: %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(resolved))
	}
	if resolved[0].ID != "http" || resolved[1].ID != "stdio" {
		t.Fatalf("unexpected server ordering: %v", resolved)
	}
	if resolved[0].URL != "https://example.com?token=abc123" {
		t.Fatalf("unexpected url: %s", resolved[0].URL)
	}
	if resolved[0].Headers["Authorization"] != "Bearer abc123" {
		t.Fatalf("unexpected header: %s", resolved[0].Headers["Authorization"])
	}
	if resolved[1].Args[1] != "abc123" {
		t.Fatalf("unexpected arg substitution: %s", resolved[1].Args[1])
	}
	if resolved[1].Env["TOKEN"] != "abc123" {
		t.Fatalf("unexpected env substitution: %s", resolved[1].Env["TOKEN"])
	}
}

func TestResolveMCPServersMissingEnv(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "http",
			Enabled:   &enabled,
			Clients:   []string{"gemini"},
			Transport: "http",
			URL:       "https://example.com?token=${TOKEN}",
		},
	}
	_, err := ResolveMCPServers(servers, map[string]string{}, "gemini", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestResolveMCPServersStdioArgMissingEnv(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "stdio",
			Enabled:   &enabled,
			Clients:   []string{"gemini"},
			Transport: "stdio",
			Command:   "tool",
			Args:      []string{"${TOKEN}"},
		},
	}
	_, err := ResolveMCPServers(servers, map[string]string{}, "gemini", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestEnabledServerIDs(t *testing.T) {
	enabled := true
	disabled := false
	servers := []config.MCPServer{
		{ID: "b", Enabled: &enabled, Clients: []string{"gemini"}},
		{ID: "a", Enabled: &enabled, Clients: []string{"gemini"}},
		{ID: "c", Enabled: &disabled, Clients: []string{"gemini"}},
	}
	ids := EnabledServerIDs(servers, "gemini")
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("unexpected ids: %v", ids)
	}
}

func TestResolveMCPServersExpandsRepoRootArg(t *testing.T) {
	enabled := true
	repoRoot := filepath.Join(t.TempDir(), "repo")
	servers := []config.MCPServer{
		{
			ID:        "fs",
			Enabled:   &enabled,
			Clients:   []string{"gemini"},
			Transport: "stdio",
			Command:   "tool",
			Args:      []string{"${" + config.BuiltinRepoRootEnvVar + "}/../data"},
		},
	}
	env := map[string]string{config.BuiltinRepoRootEnvVar: repoRoot}

	resolved, err := ResolveMCPServers(servers, env, "gemini", nil)
	if err != nil {
		t.Fatalf("resolve mcp servers: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected 1 server, got %d", len(resolved))
	}
	want := filepath.Clean(filepath.Join(repoRoot, "..", "data"))
	if resolved[0].Args[0] != want {
		t.Fatalf("unexpected path expansion: %s", resolved[0].Args[0])
	}
}

func TestResolveMCPServersPathExpansionFailsWithoutRepoRoot(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "fs",
			Enabled:   &enabled,
			Clients:   []string{"gemini"},
			Transport: "stdio",
			Command:   "tool",
			// Args reference AL_REPO_ROOT but env doesn't provide it
			Args: []string{"${" + config.BuiltinRepoRootEnvVar + "}/data"},
		},
	}
	// Empty env - no AL_REPO_ROOT
	env := map[string]string{}

	_, err := ResolveMCPServers(servers, env, "gemini", nil)
	if err == nil {
		t.Fatal("expected error when AL_REPO_ROOT is missing for path expansion")
	}
	if !strings.Contains(err.Error(), "mcp server fs") || !strings.Contains(err.Error(), "AL_REPO_ROOT") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
