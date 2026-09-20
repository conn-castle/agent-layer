package muse

import (
	"slices"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBaseArgsOnlyYoloDisablesSafety(t *testing.T) {
	for _, mode := range []string{config.ApprovalModeAll, config.ApprovalModeCommands, config.ApprovalModeMCP, config.ApprovalModeNone} {
		args := BaseArgs(t.TempDir(), config.Config{Approvals: config.ApprovalsConfig{Mode: mode}})
		if slices.Contains(args, "--yolo") || slices.Contains(args, "--disable-approval") {
			t.Fatalf("mode %s broadened safety: %v", mode, args)
		}
		if !slices.Contains(args, "--approval-judge") || !slices.Contains(args, "--trust-workspace") {
			t.Fatalf("mode %s missing policy/trust args: %v", mode, args)
		}
	}
	args := BaseArgs(t.TempDir(), config.Config{Approvals: config.ApprovalsConfig{Mode: config.ApprovalModeYOLO}})
	if !slices.Contains(args, "--yolo") {
		t.Fatalf("yolo args = %v", args)
	}
}
