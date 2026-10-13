package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conn-castle/agent-layer/internal/warnings"
)

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
			name:  "auth not validated",
			event: warnings.MCPDiscoveryEvent{ServerID: "figma", Status: warnings.MCPDiscoveryStatusAuthNotValidated},
			want:  "  - figma: OAuth authentication not validated by doctor",
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
	reporter := newMCPDiscoveryReporter([]string{"server1", "server2", "server3"}, false, io.Discard)
	reporter.start()

	// Send events
	reporter.report(warnings.MCPDiscoveryEvent{ServerID: "server1", Status: warnings.MCPDiscoveryStatusDone})
	reporter.report(warnings.MCPDiscoveryEvent{ServerID: "server2", Status: warnings.MCPDiscoveryStatusError, Err: &testError{"failed"}})
	reporter.report(warnings.MCPDiscoveryEvent{ServerID: "server3", Status: warnings.MCPDiscoveryStatusAuthNotValidated})

	// Wait for events to be processed
	var s1, s2, s3 warnings.MCPDiscoveryStatus
	require.Eventually(t, func() bool {
		reporter.mu.RLock()
		defer reporter.mu.RUnlock()
		s1 = reporter.statusForLocked("server1")
		s2 = reporter.statusForLocked("server2")
		s3 = reporter.statusForLocked("server3")
		return s1 == warnings.MCPDiscoveryStatusDone && s2 == warnings.MCPDiscoveryStatusError && s3 == warnings.MCPDiscoveryStatusAuthNotValidated
	}, time.Second, 10*time.Millisecond)

	reporter.stop()

	// Check final statuses
	if s1 != warnings.MCPDiscoveryStatusDone {
		t.Errorf("server1 status = %v, want done", s1)
	}
	if s2 != warnings.MCPDiscoveryStatusError {
		t.Errorf("server2 status = %v, want error", s2)
	}
	if s3 != warnings.MCPDiscoveryStatusAuthNotValidated {
		t.Errorf("server3 status = %v, want auth_not_validated", s3)
	}
	if line := reporter.formatLine("server3"); !strings.Contains(line, "OAuth authentication not validated by doctor") {
		t.Errorf("expected formatLine to include OAuth not validated, got %q", line)
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
