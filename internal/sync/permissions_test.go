package sync

import (
	"reflect"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

// TestBuildPermissionsBlock covers the contract of buildPermissionsBlock
// directly (not via the Claude settings marshaller). The previous test was a
// renamed claude-settings golden and missed every edge case the function is
// supposed to handle. Addresses F-C-3 and F-C-7.
func TestBuildPermissionsBlock(t *testing.T) {
	t.Parallel()
	enabled := true
	disabled := false
	allMode := config.ApprovalsConfig{Mode: config.ApprovalModeAll}
	mcpMode := config.ApprovalsConfig{Mode: config.ApprovalModeMCP}
	cmdMode := config.ApprovalsConfig{Mode: config.ApprovalModeCommands}
	noneMode := config.ApprovalsConfig{Mode: config.ApprovalModeNone}

	makeServer := func(id string, enabled *bool) config.MCPServer {
		return config.MCPServer{ID: id, Enabled: enabled, Transport: config.TransportHTTP, URL: "https://example.com"}
	}

	cases := []struct {
		name              string
		cfg               config.Config
		commandsAllow     []string
		enabledServerIDs  []string
		renderer          permissionRenderer
		wantAllow         []string
		wantNilWhenEmpty  bool
		wantDistinctOrder bool
	}{
		{
			name:             "approvals none returns nil block",
			cfg:              config.Config{Approvals: noneMode},
			commandsAllow:    []string{"git status"},
			enabledServerIDs: []string{"example"},
			renderer:         claudeRenderer{},
			wantNilWhenEmpty: true,
		},
		{
			name: "approvals mcp emits only mcp entries (sorted)",
			cfg: config.Config{
				Approvals: mcpMode,
				MCP: config.MCPConfig{Servers: []config.MCPServer{
					makeServer("zeta", &enabled),
					makeServer("alpha", &enabled),
				}},
			},
			commandsAllow:    []string{"git status", "ls"},
			enabledServerIDs: []string{"zeta", "alpha"},
			renderer:         claudeRenderer{},
			// Commands must NOT appear in mcp-only mode.
			wantAllow: []string{"mcp__alpha__*", "mcp__zeta__*"},
		},
		{
			name: "approvals commands emits only commands (no mcp)",
			cfg: config.Config{
				Approvals: cmdMode,
				MCP: config.MCPConfig{Servers: []config.MCPServer{
					makeServer("example", &enabled),
				}},
			},
			commandsAllow:    []string{"git status", "ls"},
			enabledServerIDs: []string{"example"},
			renderer:         claudeRenderer{},
			wantAllow:        []string{"Bash(git status:*)", "Bash(ls:*)"},
		},
		{
			name: "approvals all emits commands then mcp, mcp sorted",
			cfg: config.Config{
				Approvals: allMode,
				MCP: config.MCPConfig{Servers: []config.MCPServer{
					makeServer("zeta", &enabled),
					makeServer("alpha", &enabled),
				}},
			},
			commandsAllow:    []string{"git status", "ls"},
			enabledServerIDs: []string{"zeta", "alpha"},
			renderer:         claudeRenderer{},
			wantAllow: []string{
				"Bash(git status:*)", "Bash(ls:*)",
				"mcp__alpha__*", "mcp__zeta__*",
			},
		},
		{
			name: "antigravity renderer produces command(...)/mcp(.../) shape",
			cfg: config.Config{
				Approvals: allMode,
				MCP: config.MCPConfig{Servers: []config.MCPServer{
					makeServer("example", &enabled),
				}},
			},
			commandsAllow:    []string{"git status"},
			enabledServerIDs: []string{"example"},
			renderer:         antigravityRenderer{},
			wantAllow:        []string{"command(git status)", "mcp(example/)"},
		},
		{
			name: "empty inputs return nil block",
			cfg: config.Config{
				Approvals: allMode,
			},
			commandsAllow:    nil,
			enabledServerIDs: nil,
			renderer:         claudeRenderer{},
			wantNilWhenEmpty: true,
		},
		{
			// Contract: buildPermissionsBlock treats enabledServerIDs as
			// authoritative. Even when MCP.Servers lists a disabled server,
			// if the caller passes the ID in enabledServerIDs the function
			// emits the mcp entry (the caller is responsible for filtering).
			// This pins the contract so a future "filter by Enabled flag"
			// refactor would fail the test.
			name: "enabledServerIDs is authoritative regardless of MCP.Servers Enabled flag",
			cfg: config.Config{
				Approvals: mcpMode,
				MCP: config.MCPConfig{Servers: []config.MCPServer{
					makeServer("disabled-id", &disabled),
				}},
			},
			commandsAllow:    nil,
			enabledServerIDs: []string{"disabled-id"},
			renderer:         claudeRenderer{},
			wantAllow:        []string{"mcp__disabled-id__*"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildPermissionsBlock(tc.cfg, tc.commandsAllow, tc.enabledServerIDs, tc.renderer)
			if tc.wantNilWhenEmpty {
				if got != nil {
					t.Fatalf("expected nil block, got %v", got)
				}
				return
			}
			allow, ok := got["allow"].([]string)
			if !ok {
				t.Fatalf("expected allow to be []string, got %T", got["allow"])
			}
			if !reflect.DeepEqual(allow, tc.wantAllow) {
				t.Fatalf("allow mismatch\nwant: %#v\n got: %#v", tc.wantAllow, allow)
			}
		})
	}
}

// TestBuildPermissionsBlock_StableMCPOrder pins the sort guarantee on MCP
// IDs so callers can rely on deterministic output. Documented in
// permissions.go via the explicit sort.Strings call; the test makes the
// guarantee enforceable.
func TestBuildPermissionsBlock_StableMCPOrder(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeMCP}}
	got := buildPermissionsBlock(cfg, nil, []string{"zeta", "alpha", "middle"}, claudeRenderer{})
	want := []string{"mcp__alpha__*", "mcp__middle__*", "mcp__zeta__*"}
	if !reflect.DeepEqual(got["allow"], want) {
		t.Fatalf("expected sorted mcp ids; want %v, got %v", want, got["allow"])
	}
}
