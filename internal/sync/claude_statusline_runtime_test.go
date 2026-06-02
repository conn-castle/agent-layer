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

// runGit executes git in a test repository and fails with captured output when
// the command does not complete successfully.
func runGit(t *testing.T, cwd string, args ...string) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}

	cmd := exec.Command(gitPath, args...) // #nosec G204 -- test-controlled arguments.
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
}

func TestClaudeStatuslineScript_RendersFullPayload(t *testing.T) {
	cwd := t.TempDir()
	runGit(t, cwd, "init")
	if err := os.WriteFile(filepath.Join(cwd, "tracked.txt"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("write tracked fixture: %v", err)
	}
	runGit(t, cwd, "add", "tracked.txt")
	runGit(t, cwd, "-c", "user.name=Agent Layer", "-c", "user.email=agent-layer@example.invalid", "commit", "-m", "baseline")
	if err := os.WriteFile(filepath.Join(cwd, "tracked.txt"), []byte("one\nTWO\nthree\nfour\nfive\n"), 0o600); err != nil {
		t.Fatalf("modify tracked fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "untracked.txt"), []byte("ignored\nignored\nignored\n"), 0o600); err != nil {
		t.Fatalf("write untracked fixture: %v", err)
	}

	input := `{
		"model": {"display_name": "Opus 4.8"},
		"workspace": {"current_dir": "` + cwd + `"},
		"session_id": "sess123",
		"effort": {"level": "high"},
		"context_window": {"used_percentage": 43, "context_window_size": 200000, "total_input_tokens": 1000},
		"rate_limits": {"seven_day": {"used_percentage": 60}},
		"cost": {"total_cost_usd": 1.5, "total_lines_added": 99, "total_lines_removed": 88},
		"pr": {"number": 123, "review_state": "approved"}
	}`
	out := runClaudeStatusline(t, cwd, input)

	for _, want := range []string{"Opus 4.8", "effort:high", "ctx:43%", "7d:60%", "#sess123", "+3", "-1", "$1.50", "PR#123"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got: %q", want, out)
		}
	}
	for _, absent := range []string{"+99", "-88"} {
		if strings.Contains(out, absent) {
			t.Errorf("did not expect JSON line count %q in output, got: %q", absent, out)
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

func TestClaudeStatuslineScript_DirectoryRelativeToProjectRoot(t *testing.T) {
	// cwd genuinely under project_dir renders "<project>/<relative>".
	cwd := t.TempDir()
	input := `{
		"model": {"display_name": "Opus 4.8"},
		"workspace": {"project_dir": "/home/u/myproj", "current_dir": "/home/u/myproj/src/api"}
	}`
	out := runClaudeStatusline(t, cwd, input)
	if !strings.Contains(out, "myproj/src/api") {
		t.Errorf("expected relative path myproj/src/api, got: %q", out)
	}
}

func TestClaudeStatuslineScript_DirectoryFallsBackWhenNotUnderProjectRoot(t *testing.T) {
	// cwd NOT under project_dir must fall back to the cwd basename, never
	// "<project>/<full-cwd>".
	cwd := t.TempDir()
	input := `{
		"model": {"display_name": "Opus 4.8"},
		"workspace": {"project_dir": "/home/u/myproj", "current_dir": "/var/other/place"}
	}`
	out := runClaudeStatusline(t, cwd, input)
	if !strings.Contains(out, "place") {
		t.Errorf("expected cwd basename 'place', got: %q", out)
	}
	if strings.Contains(out, "myproj/") {
		t.Errorf("did not expect misleading project-prefixed path, got: %q", out)
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
