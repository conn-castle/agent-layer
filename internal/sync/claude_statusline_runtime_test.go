package sync

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/templates"
)

// ansiEscape matches the SGR color codes the status line emits, so assertions
// can target the rendered text rather than escape sequences.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// runClaudeStatusline writes the embedded status line script to a temp file and
// runs it under bash with the given JSON on stdin, returning the ANSI-stripped
// output. It skips the test when bash or jq are unavailable, since the script
// degrades gracefully in those environments and there is nothing to assert.
func runClaudeStatusline(t *testing.T, cwd, stdinJSON string) string {
	t.Helper()
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not available")
	}

	script, err := templates.Read("claude-statusline.sh")
	if err != nil {
		t.Fatalf("read embedded statusline script: %v", err)
	}
	scriptPath := filepath.Join(t.TempDir(), "claude-statusline.sh")
	if err := os.WriteFile(scriptPath, script, 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cmd := exec.Command(bashPath, scriptPath) // #nosec G204 -- test-controlled script path.
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader(stdinJSON)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run statusline script: %v\nstderr: %s", err, stderr.String())
	}
	return ansiEscape.ReplaceAllString(stdout.String(), "")
}

func TestClaudeStatuslineScript_RendersFullPayload(t *testing.T) {
	// cwd is a non-git temp dir so the git segment is deterministically absent.
	cwd := t.TempDir()
	input := `{
		"model": {"display_name": "Opus 4.8"},
		"workspace": {"current_dir": "` + cwd + `"},
		"session_id": "sess123",
		"effort": {"level": "high"},
		"context_window": {"used_percentage": 43, "context_window_size": 200000, "total_input_tokens": 1000},
		"rate_limits": {"seven_day": {"used_percentage": 60}},
		"cost": {"total_cost_usd": 1.5, "total_lines_added": 12, "total_lines_removed": 3},
		"pr": {"number": 123, "review_state": "approved"}
	}`
	out := runClaudeStatusline(t, cwd, input)

	for _, want := range []string{"Opus 4.8", "effort:high", "ctx:43%", "7d:60%", "#sess123", "+12", "-3", "$1.50", "PR#123"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got: %q", want, out)
		}
	}
}

func TestClaudeStatuslineScript_FallsBackToTokenRatioForContext(t *testing.T) {
	// No used_percentage: the script computes ctx% from raw token counts
	// (total_input * 100 / context_window_size) and marks it approximate.
	cwd := t.TempDir()
	input := `{
		"model": {"display_name": "Sonnet"},
		"workspace": {"current_dir": "` + cwd + `"},
		"context_window": {"context_window_size": 200000, "total_input_tokens": 50000}
	}`
	out := runClaudeStatusline(t, cwd, input)

	if !strings.Contains(out, "ctx:~25%") {
		t.Errorf("expected approximate ctx:~25%%, got: %q", out)
	}
	// Absent optional fields must not render their segments.
	for _, absent := range []string{"7d:", "PR#", "effort:"} {
		if strings.Contains(out, absent) {
			t.Errorf("did not expect %q in minimal output, got: %q", absent, out)
		}
	}
}

func TestClaudeStatuslineScript_DefaultsContextToZeroWhenNoData(t *testing.T) {
	// No usage data at all: the segment still renders at 0% rather than hiding.
	cwd := t.TempDir()
	input := `{"model": {"display_name": "Haiku"}, "workspace": {"current_dir": "` + cwd + `"}}`
	out := runClaudeStatusline(t, cwd, input)

	if !strings.Contains(out, "ctx:0%") {
		t.Errorf("expected ctx:0%%, got: %q", out)
	}
}
