package projection

import (
	"slices"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildApprovals(t *testing.T) {
	cfg := config.Config{
		Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeCommands},
	}
	result := BuildApprovals(cfg, []string{"git status"})
	if !result.AllowCommands || result.AllowMCP {
		t.Fatalf("unexpected approvals flags: %+v", result)
	}
	if len(result.Commands) != 1 || result.Commands[0] != "git status" {
		t.Fatalf("unexpected commands: %+v", result.Commands)
	}
}

func TestBuildApprovalsYOLO(t *testing.T) {
	cfg := config.Config{
		Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO},
	}
	result := BuildApprovals(cfg, []string{"make test"})
	if !result.AllowCommands {
		t.Fatal("expected AllowCommands=true for yolo mode")
	}
	if !result.AllowMCP {
		t.Fatal("expected AllowMCP=true for yolo mode")
	}
	if len(result.Commands) != 1 || result.Commands[0] != "make test" {
		t.Fatalf("unexpected commands: %+v", result.Commands)
	}
}

// TestClaudeAllowRulesGrantOnlyWhatTheModeAllows pins the pre-approval boundary
// that headless dispatch passes to Claude on the command line. Every rule
// returned here bypasses a permission prompt, so a mode must never leak a grant
// for the feature it does not cover.
func TestClaudeAllowRulesGrantOnlyWhatTheModeAllows(t *testing.T) {
	commands := []string{"git status", "make test"}
	servers := []string{"context7", "agent-layer"}

	tests := []struct {
		mode string
		want []string
	}{
		{config.ApprovalModeNone, nil},
		{config.ApprovalModeCommands, []string{"Bash(git status:*)", "Bash(make test:*)"}},
		{config.ApprovalModeMCP, []string{"mcp__agent-layer__*", "mcp__context7__*"}},
		{config.ApprovalModeAll, []string{"Bash(git status:*)", "Bash(make test:*)", "mcp__agent-layer__*", "mcp__context7__*"}},
		{config.ApprovalModeYOLO, []string{"Bash(git status:*)", "Bash(make test:*)", "mcp__agent-layer__*", "mcp__context7__*"}},
	}

	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			cfg := config.Config{Approvals: config.ApprovalsConfig{Mode: test.mode}}
			got := ClaudeAllowRules(cfg, commands, servers)
			if !slices.Equal(got, test.want) {
				t.Fatalf("allow rules for mode %q = %#v, want %#v", test.mode, got, test.want)
			}
		})
	}
}
