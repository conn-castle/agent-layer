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
		name    string
		servers []config.MCPServer
		want    []string
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := enabledMCPServerIDs(tt.servers)
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

func TestMCPDiscoveryReporter_Spinner(t *testing.T) {
	reporter := newMCPDiscoveryReporter([]string{"server1"}, true, io.Discard)
	reporter.start()

	// Allow spinner to tick a few times
	time.Sleep(300 * time.Millisecond)

	reporter.report(warnings.MCPDiscoveryEvent{ServerID: "server1", Status: warnings.MCPDiscoveryStatusDone})

	// Wait for event to be processed
	require.Eventually(t, func() bool {
		return reporter.statusFor("server1") == warnings.MCPDiscoveryStatusDone
	}, time.Second, 10*time.Millisecond)

	reporter.stop()
}

func TestMCPDiscoveryReporter_FormatLine(t *testing.T) {
	reporter := newMCPDiscoveryReporter([]string{"srv"}, false, io.Discard)

	// Initial state - no status recorded
	line := reporter.formatLine("srv")
	if line != "  - srv: starting" {
		t.Errorf("formatLine() = %q, want starting", line)
	}

	// Done status
	reporter.mu.Lock()
	reporter.statuses["srv"] = warnings.MCPDiscoveryStatusDone
	reporter.mu.Unlock()
	line = reporter.formatLine("srv")
	if line != "  - srv: done" {
		t.Errorf("formatLine() = %q, want done", line)
	}

	// Error status without error
	reporter.mu.Lock()
	reporter.statuses["srv"] = warnings.MCPDiscoveryStatusError
	reporter.mu.Unlock()
	line = reporter.formatLine("srv")
	if line != "  - srv: error" {
		t.Errorf("formatLine() = %q, want error", line)
	}

	// Error status with error
	reporter.mu.Lock()
	reporter.errors["srv"] = &testError{"boom"}
	reporter.mu.Unlock()
	line = reporter.formatLine("srv")
	if line != "  - srv: error (boom)" {
		t.Errorf("formatLine() = %q, want error (boom)", line)
	}
}

func TestMCPDiscoveryReporter_StatusFor(t *testing.T) {
	reporter := newMCPDiscoveryReporter([]string{"srv"}, false, io.Discard)

	// Unknown server returns start status
	status := reporter.statusFor("unknown")
	if status != warnings.MCPDiscoveryStatusStart {
		t.Errorf("statusFor(unknown) = %v, want start", status)
	}

	// Known server returns its status
	reporter.mu.Lock()
	reporter.statuses["srv"] = warnings.MCPDiscoveryStatusDone
	reporter.mu.Unlock()
	status = reporter.statusFor("srv")
	if status != warnings.MCPDiscoveryStatusDone {
		t.Errorf("statusFor(srv) = %v, want done", status)
	}
}

func TestMCPDiscoveryReporter_AdvanceSpinner(t *testing.T) {
	reporter := newMCPDiscoveryReporter([]string{"srv"}, true, io.Discard)

	initial := reporter.spinnerIndex
	reporter.advanceSpinner()
	if reporter.spinnerIndex != (initial+1)%len(reporter.spinnerFrames) {
		t.Errorf("spinner did not advance")
	}
}

func TestMCPDiscoveryReporter_AdvanceSpinnerEmpty(t *testing.T) {
	reporter := &mcpDiscoveryReporter{spinnerFrames: nil}
	// Should not panic
	reporter.advanceSpinner()
}

func TestMCPDiscoveryReporter_FinalizePending(t *testing.T) {
	reporter := newMCPDiscoveryReporter([]string{"srv1", "srv2"}, false, io.Discard)

	// srv1 has no status, srv2 is still starting
	reporter.mu.Lock()
	reporter.statuses["srv2"] = warnings.MCPDiscoveryStatusStart
	reporter.mu.Unlock()

	reporter.finalizePending()

	// Both should now be done
	if s := reporter.statusFor("srv1"); s != warnings.MCPDiscoveryStatusDone {
		t.Errorf("srv1 status = %v, want done", s)
	}
	if s := reporter.statusFor("srv2"); s != warnings.MCPDiscoveryStatusDone {
		t.Errorf("srv2 status = %v, want done", s)
	}
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

func TestMCPDiscoveryReporter_DrainEvents(t *testing.T) {
	var output bytes.Buffer
	reporter := newMCPDiscoveryReporter([]string{"srv"}, false, &output)
	reporter.events <- warnings.MCPDiscoveryEvent{ServerID: "srv", Status: warnings.MCPDiscoveryStatusDone}

	reporter.drainEvents()

	if reporter.statusFor("srv") != warnings.MCPDiscoveryStatusDone {
		t.Fatalf("expected status done")
	}
	if !strings.Contains(output.String(), "srv: done") {
		t.Fatalf("expected drain output, got %q", output.String())
	}
}

func TestMCPDiscoveryReporter_RenderMoveCursor(t *testing.T) {
	reporter := newMCPDiscoveryReporter([]string{"srv"}, true, io.Discard)
	reporter.rendered = true
	reporter.render(true)
}
