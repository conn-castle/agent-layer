package config

import "testing"

func TestAppliesToClient(t *testing.T) {
	server := MCPServer{Clients: []string{"antigravity", "codex"}}
	if !server.AppliesToClient("antigravity") {
		t.Fatalf("expected antigravity to apply")
	}
	if server.AppliesToClient("vscode") {
		t.Fatalf("expected vscode not to apply")
	}

	empty := MCPServer{}
	if !empty.AppliesToClient("any") {
		t.Fatalf("expected empty clients to apply")
	}
}
