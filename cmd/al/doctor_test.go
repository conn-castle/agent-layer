package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

func TestEnabledMCPServerIDs(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		name        string
		servers     []config.MCPServer
		enableCodex bool
		enableGrok  bool
		want        []string
	}{
		{
			name:    "empty",
			servers: nil,
			want:    []string{},
		},
		{
			name: "preserves order",
			servers: []config.MCPServer{
				{ID: "server-a", Enabled: &enabled},
				{ID: "server-b", Enabled: &enabled},
				{ID: "server-c", Enabled: &disabled},
			},
			want: []string{"server-a", "server-b"},
		},
		{
			name:        "includes built-in dispatch server",
			enableCodex: true,
			servers: []config.MCPServer{
				{ID: "server-a", Enabled: &enabled},
			},
			want: []string{"server-a", "agent-layer"},
		},
		{
			name:       "includes built-in for another dispatch client",
			enableGrok: true,
			want:       []string{"agent-layer"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{MCP: config.MCPConfig{Servers: tt.servers}}
			if tt.enableCodex {
				cfg.Agents.Codex.Enabled = &enabled
			}
			if tt.enableGrok {
				cfg.Agents.Grok.Enabled = &enabled
			}
			got := enabledMCPServerIDs(cfg)
			if len(got) != len(tt.want) {
				t.Errorf("enabledMCPServerIDs() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("enabledMCPServerIDs()[%d] = %s, want %s", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFormatMCPDiscoveryEvent(t *testing.T) {
	tests := []struct {
		name  string
		event warnings.MCPDiscoveryEvent
		want  string
	}{
		{
			name:  "start",
			event: warnings.MCPDiscoveryEvent{ServerID: "srv", Status: warnings.MCPDiscoveryStatusStart},
			want:  "  - srv: starting",
		},
		{
			name:  "done",
			event: warnings.MCPDiscoveryEvent{ServerID: "srv", Status: warnings.MCPDiscoveryStatusDone},
			want:  "  - srv: done",
		},
		{
			name:  "error without err",
			event: warnings.MCPDiscoveryEvent{ServerID: "srv", Status: warnings.MCPDiscoveryStatusError},
			want:  "  - srv: error",
		},
		{
			name:  "error with err",
			event: warnings.MCPDiscoveryEvent{ServerID: "srv", Status: warnings.MCPDiscoveryStatusError, Err: &testError{msg: "boom"}},
			want:  "  - srv: error (boom)",
		},
		{
			name:  "unknown status",
			event: warnings.MCPDiscoveryEvent{ServerID: "srv", Status: warnings.MCPDiscoveryStatus("custom")},
			want:  "  - srv: custom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatMCPDiscoveryEvent(tt.event)
			if got != tt.want {
				t.Errorf("formatMCPDiscoveryEvent() = %q, want %q", got, tt.want)
			}
		})
	}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }

func TestMCPDiscoveryReporter_Report(t *testing.T) {
	reporter := newMCPDiscoveryReporter([]string{"server1", "server2"}, false, io.Discard)
	reporter.start()

	// Send events
	reporter.report(warnings.MCPDiscoveryEvent{ServerID: "server1", Status: warnings.MCPDiscoveryStatusDone})
	reporter.report(warnings.MCPDiscoveryEvent{ServerID: "server2", Status: warnings.MCPDiscoveryStatusError, Err: &testError{"failed"}})

	// Wait for events to be processed
	var s1, s2 warnings.MCPDiscoveryStatus
	require.Eventually(t, func() bool {
		s1 = reporter.statusFor("server1")
		s2 = reporter.statusFor("server2")
		return s1 == warnings.MCPDiscoveryStatusDone && s2 == warnings.MCPDiscoveryStatusError
	}, time.Second, 10*time.Millisecond)

	reporter.stop()

	// Check final statuses
	if s1 != warnings.MCPDiscoveryStatusDone {
		t.Errorf("server1 status = %v, want done", s1)
	}
	if s2 != warnings.MCPDiscoveryStatusError {
		t.Errorf("server2 status = %v, want error", s2)
	}
}

func TestMCPDiscoveryReporter_ReportAfterStop(t *testing.T) {
	reporter := newMCPDiscoveryReporter([]string{"server1"}, false, io.Discard)
	reporter.start()
	reporter.stop()

	// Reporting after stop should not panic
	reporter.report(warnings.MCPDiscoveryEvent{ServerID: "server1", Status: warnings.MCPDiscoveryStatusDone})
}

func TestStartMCPDiscoveryReporter_NonZero(t *testing.T) {
	origIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = origIsTerminal })

	var output bytes.Buffer
	reporter, stop := startMCPDiscoveryReporter([]string{"srv1"}, &output)
	if reporter == nil {
		t.Fatal("expected reporter")
	}
	reporter(warnings.MCPDiscoveryEvent{ServerID: "srv1", Status: warnings.MCPDiscoveryStatusDone})
	stop()

	if !strings.Contains(output.String(), "srv1") {
		t.Fatalf("expected output to mention server, got %q", output.String())
	}
	if !strings.Contains(output.String(), "done") {
		t.Fatalf("expected output to include done, got %q", output.String())
	}
}
