package projection

import (
	"errors"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestClientPlaceholderResolver(t *testing.T) {
	resolver := ClientPlaceholderResolver("${%s}")

	// Regular env var should be converted to placeholder
	result := resolver("MY_TOKEN", "secret123")
	if result != "${MY_TOKEN}" {
		t.Fatalf("expected ${MY_TOKEN}, got %s", result)
	}

	// Built-in env var should pass through actual value
	result = resolver(config.BuiltinRepoRootEnvVar, "/path/to/repo")
	if result != "/path/to/repo" {
		t.Fatalf("expected /path/to/repo, got %s", result)
	}
}

func TestClientPlaceholderResolverVSCode(t *testing.T) {
	resolver := ClientPlaceholderResolver("${env:%s}")

	result := resolver("MY_TOKEN", "secret123")
	if result != "${env:MY_TOKEN}" {
		t.Fatalf("expected ${env:MY_TOKEN}, got %s", result)
	}
}

func TestFullValueResolver(t *testing.T) {
	env := map[string]string{"TOKEN": "from-env"}

	resolver := FullValueResolver(env)

	// Value from env map
	result := resolver("TOKEN", "from-env")
	if result != "from-env" {
		t.Fatalf("expected from-env, got %s", result)
	}
}

func TestResolveEnabledMCPServers(t *testing.T) {
	enabled := true
	disabled := false
	servers := []config.MCPServer{
		{
			ID:        "server1",
			Enabled:   &enabled,
			Transport: "stdio",
			Command:   "cmd1",
			Args:      []string{"--token", "${TOKEN}"},
		},
		{
			ID:        "server2",
			Enabled:   &disabled,
			Transport: "stdio",
			Command:   "cmd2",
		},
		{
			ID:        "server3",
			Enabled:   &enabled,
			Transport: "http",
			URL:       "https://example.com?key=${API_KEY}",
		},
	}
	env := map[string]string{
		"TOKEN":   "secret",
		"API_KEY": "key123",
	}

	resolved, err := ResolveEnabledMCPServers(servers, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only have 2 servers (disabled one excluded)
	if len(resolved) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(resolved))
	}

	// Check values are fully resolved
	if resolved[0].Args[1] != "secret" {
		t.Fatalf("expected secret, got %s", resolved[0].Args[1])
	}
	if resolved[1].URL != "https://example.com?key=key123" {
		t.Fatalf("expected resolved URL, got %s", resolved[1].URL)
	}
}

func TestResolveEnabledMCPServers_DefaultHTTPTransport(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "http-default",
			Enabled:   &enabled,
			Transport: "http",
			URL:       "https://example.com",
		},
		{
			ID:            "http-streamable",
			Enabled:       &enabled,
			Transport:     "http",
			HTTPTransport: "streamable",
			URL:           "https://example.com/streamable",
		},
	}

	resolved, err := ResolveEnabledMCPServers(servers, map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved[0].HTTPTransport != "sse" {
		t.Fatalf("expected default http transport sse, got %s", resolved[0].HTTPTransport)
	}
	if resolved[1].HTTPTransport != "streamable" {
		t.Fatalf("expected streamable http transport, got %s", resolved[1].HTTPTransport)
	}
}

func TestMCPServerResolveError_Unwrap(t *testing.T) {
	root := errors.New("boom")
	err := &MCPServerResolveError{ServerID: "server-1", Err: root}

	if err.Error() != "mcp server server-1: boom" {
		t.Fatalf("unexpected error string: %q", err.Error())
	}
	if !errors.Is(err, root) {
		t.Fatalf("expected errors.Is to match root error")
	}
	if err.Unwrap() != root {
		t.Fatalf("expected Unwrap() to return root error")
	}
}

func TestResolveEnabledMCPServers_UnknownTransportFails(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "bad-transport",
			Enabled:   &enabled,
			Transport: "ftp",
		},
	}
	_, err := ResolveEnabledMCPServers(servers, map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unsupported transport") {
		t.Fatalf("expected unsupported transport error, got %v", err)
	}
}
