package projection

import (
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestEnabledServerIDs_AppliesToClient(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{ID: "gemini-only", Enabled: &enabled, Clients: []string{"gemini"}},
		{ID: "claude-only", Enabled: &enabled, Clients: []string{"claude"}},
		{ID: "all", Enabled: &enabled, Clients: []string{}},
	}
	ids := EnabledServerIDs(servers, "gemini")
	// Should include "gemini-only" and "all". "claude-only" excluded.
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %d", len(ids))
	}
	if ids[0] != "all" || ids[1] != "gemini-only" {
		t.Fatalf("unexpected ids: %v", ids)
	}
}

func TestEnabledServerIDs_NilEnabled(t *testing.T) {
	// If Enabled is nil, it should be treated as disabled (skipped)
	servers := []config.MCPServer{
		{ID: "nil-enabled", Enabled: nil, Clients: []string{"gemini"}},
	}
	ids := EnabledServerIDs(servers, "gemini")
	if len(ids) != 0 {
		t.Fatalf("expected 0 ids, got %d", len(ids))
	}
}

func TestResolveMCPServers_HeaderError(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "http",
			Enabled:   &enabled,
			Clients:   []string{"gemini"},
			Transport: "http",
			URL:       "https://example.com",
			Headers: map[string]string{
				"Authorization": "${MISSING}",
			},
		},
	}
	_, err := ResolveMCPServers(servers, map[string]string{}, "gemini", nil)
	if err == nil {
		t.Fatalf("expected error for missing env in header")
	}
	if !strings.Contains(err.Error(), "missing environment variables: MISSING") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveMCPServers_CommandError(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "stdio",
			Enabled:   &enabled,
			Clients:   []string{"gemini"},
			Transport: "stdio",
			Command:   "${MISSING}",
		},
	}
	_, err := ResolveMCPServers(servers, map[string]string{}, "gemini", nil)
	if !strings.Contains(err.Error(), "missing environment variables: MISSING") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveMCPServers_EnvError(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "stdio",
			Enabled:   &enabled,
			Clients:   []string{"gemini"},
			Transport: "stdio",
			Command:   "tool",
			Env: map[string]string{
				"KEY": "${MISSING}",
			},
		},
	}
	_, err := ResolveMCPServers(servers, map[string]string{}, "gemini", nil)
	if err == nil {
		t.Fatalf("expected error for missing env in env map")
	}
	if !strings.Contains(err.Error(), "missing environment variables: MISSING") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveMCPServers_UnsupportedTransport(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "unknown",
			Enabled:   &enabled,
			Clients:   []string{"gemini"},
			Transport: "pigeon",
		},
	}
	_, err := ResolveMCPServers(servers, map[string]string{}, "gemini", nil)
	if err == nil {
		t.Fatalf("expected error for unsupported transport")
	}
	if !strings.Contains(err.Error(), "unsupported transport pigeon") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveMCPServers_NilEnabled(t *testing.T) {
	servers := []config.MCPServer{
		{
			ID:      "skipped",
			Enabled: nil,
		},
	}
	resolved, err := ResolveMCPServers(servers, map[string]string{}, "gemini", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 0 {
		t.Fatalf("expected 0 servers, got %d", len(resolved))
	}
}

func TestResolveMCPServers_AppliesToClient(t *testing.T) {
	enabled := true
	servers := []config.MCPServer{
		{
			ID:        "wrong-client",
			Enabled:   &enabled,
			Clients:   []string{"claude"},
			Transport: "http",
			URL:       "http://example.com",
		},
	}
	resolved, err := ResolveMCPServers(servers, map[string]string{}, "gemini", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resolved) != 0 {
		t.Fatalf("expected 0 servers, got %d", len(resolved))
	}
}
