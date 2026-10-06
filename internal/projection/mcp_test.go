package projection

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

// userServerConfig enables no agent, so no built-in dispatch server is added.
func userServerConfig(servers []config.MCPServer) config.Config {
	return config.Config{MCP: config.MCPConfig{Servers: servers}}
}

func TestEffectiveMCPServersResolvesUserServers(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "http",
			Enabled:   &enabled,
			Clients:   []string{"antigravity"},
			Transport: "http",
			URL:       "https://example.com?token=${TOKEN}",
			Auth:      config.MCPAuthOAuth,
			Headers: map[string]string{
				"Authorization": "Bearer ${TOKEN}",
			},
		},
		{
			ID:        "stdio",
			Enabled:   &enabled,
			Clients:   []string{"antigravity"},
			Transport: "stdio",
			Command:   "tool",
			Args:      []string{"--token", "${TOKEN}"},
			Env: map[string]string{
				"TOKEN": "${TOKEN}",
			},
		},
	}
	env := map[string]string{"TOKEN": "abc123"}

	resolved, err := EffectiveMCPServers(userServerConfig(servers), env, "antigravity", nil)
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
	if resolved[0].Auth != config.MCPAuthOAuth {
		t.Fatalf("unexpected auth mode: %s", resolved[0].Auth)
	}
	if resolved[1].Args[1] != "abc123" {
		t.Fatalf("unexpected arg substitution: %s", resolved[1].Args[1])
	}
	if resolved[1].Env["TOKEN"] != "abc123" {
		t.Fatalf("unexpected env substitution: %s", resolved[1].Env["TOKEN"])
	}
}

func TestEffectiveMCPServersMissingEnv(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "http",
			Enabled:   &enabled,
			Clients:   []string{"antigravity"},
			Transport: "http",
			URL:       "https://example.com?token=${TOKEN}",
		},
	}
	_, err := EffectiveMCPServers(userServerConfig(servers), map[string]string{}, "antigravity", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestEffectiveMCPServersStdioArgMissingEnv(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "stdio",
			Enabled:   &enabled,
			Clients:   []string{"antigravity"},
			Transport: "stdio",
			Command:   "tool",
			Args:      []string{"${TOKEN}"},
		},
	}
	_, err := EffectiveMCPServers(userServerConfig(servers), map[string]string{}, "antigravity", nil)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestEffectiveServerIDsSortsEnabledUserServers(t *testing.T) {
	enabled := true
	disabled := false
	servers := []config.MCPServer{
		{ID: "b", Enabled: &enabled, Clients: []string{"antigravity"}},
		{ID: "a", Enabled: &enabled, Clients: []string{"antigravity"}},
		{ID: "c", Enabled: &disabled, Clients: []string{"antigravity"}},
	}
	ids := EffectiveServerIDs(userServerConfig(servers), "antigravity")
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("unexpected ids: %v", ids)
	}
}

func TestEffectiveMCPServersExpandsRepoRootArg(t *testing.T) {
	enabled := true
	repoRoot := filepath.Join(t.TempDir(), "repo")
	servers := []config.MCPServer{
		{
			ID:        "fs",
			Enabled:   &enabled,
			Clients:   []string{"antigravity"},
			Transport: "stdio",
			Command:   "tool",
			Args:      []string{"${" + config.BuiltinRepoRootEnvVar + "}/../data"},
		},
	}
	env := map[string]string{config.BuiltinRepoRootEnvVar: repoRoot}

	resolved, err := EffectiveMCPServers(userServerConfig(servers), env, "antigravity", nil)
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

func TestEffectiveMCPServersPathExpansionFailsWithoutRepoRoot(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "fs",
			Enabled:   &enabled,
			Clients:   []string{"antigravity"},
			Transport: "stdio",
			Command:   "tool",
			// Args reference AL_REPO_ROOT but env doesn't provide it
			Args: []string{"${" + config.BuiltinRepoRootEnvVar + "}/data"},
		},
	}
	// Empty env - no AL_REPO_ROOT
	env := map[string]string{}

	_, err := EffectiveMCPServers(userServerConfig(servers), env, "antigravity", nil)
	if err == nil {
		t.Fatal("expected error when AL_REPO_ROOT is missing for path expansion")
	}
	if !strings.Contains(err.Error(), "mcp server fs") || !strings.Contains(err.Error(), "AL_REPO_ROOT") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestEffectiveMCPServersReportsFirstFailingServerInConfigOrder proves servers
// resolve in configuration order and are sorted only after all succeed.
func TestEffectiveMCPServersReportsFirstFailingServerInConfigOrder(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{ID: "b-first", Enabled: &enabled, Transport: "stdio", Command: "${MISSING_B}"},
		{ID: "a-second", Enabled: &enabled, Transport: "stdio", Command: "${MISSING_A}"},
	}
	_, err := EffectiveMCPServers(userServerConfig(servers), map[string]string{}, "antigravity", nil)
	var resolveErr *MCPServerResolveError
	if !errors.As(err, &resolveErr) || resolveErr.ServerID != "b-first" {
		t.Fatalf("expected first config-order server to fail, got %v", err)
	}
}

func TestEffectiveServersAreNilWithoutMatchingServers(t *testing.T) {
	enabled := true
	disabled := false
	cfg := userServerConfig([]config.MCPServer{
		{ID: "off", Enabled: &disabled, Transport: "stdio", Command: "tool"},
		{ID: "other", Enabled: &enabled, Clients: []string{"codex"}, Transport: "stdio", Command: "tool"},
	})
	resolved, err := EffectiveMCPServers(cfg, map[string]string{}, "antigravity", nil)
	if err != nil || resolved != nil {
		t.Fatalf("expected nil servers, got %#v, %v", resolved, err)
	}
	if ids := EffectiveServerIDs(cfg, "antigravity"); ids != nil {
		t.Fatalf("expected nil ids, got %#v", ids)
	}
	resolved, err = ResolveEffectiveEnabledMCPServers(cfg, map[string]string{})
	if err != nil || resolved != nil {
		t.Fatalf("expected nil enabled servers, got %#v, %v", resolved, err)
	}
}
