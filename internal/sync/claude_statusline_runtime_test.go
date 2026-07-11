package sync

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

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

// runGit executes git in a test repository with repository-local Git
// environment variables removed. It returns stdout and fails with captured
// output when the command does not complete successfully.
func runGit(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}

	cmd := exec.Command(gitPath, args...) // #nosec G204 -- test-controlled arguments.
	cmd.Dir = cwd
	cmd.Env = gitTestEnvironment(t, gitPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// gitTestEnvironment removes Git's own repository-local environment variables
// so commands against a temporary repository cannot inherit an outer index or
// worktree context.
func gitTestEnvironment(t *testing.T, gitPath string) []string {
	t.Helper()
	cmd := exec.Command(gitPath, "rev-parse", "--local-env-vars") // #nosec G204 -- gitPath came from LookPath.
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("list Git repository-local environment variables: %v", err)
	}

	localNames := make(map[string]struct{})
	for _, name := range strings.Fields(string(output)) {
		localNames[name] = struct{}{}
	}
	if len(localNames) == 0 {
		t.Fatal("Git reported no repository-local environment variables")
	}

	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if _, local := localNames[name]; !local {
			env = append(env, entry)
		}
	}
	return env
}

func TestRunGitDoesNotInheritExternalIndex(t *testing.T) {
	externalRoot := t.TempDir()
	runGit(t, externalRoot, "init")
	if err := os.WriteFile(filepath.Join(externalRoot, "external-sentinel.txt"), []byte("external\n"), 0o600); err != nil {
		t.Fatalf("write external sentinel: %v", err)
	}
	runGit(t, externalRoot, "add", "external-sentinel.txt")
	externalIndexPath := filepath.Join(externalRoot, ".git", "index")
	externalIndexBefore, err := os.ReadFile(externalIndexPath) // #nosec G304 -- test-created index under t.TempDir.
	if err != nil {
		t.Fatalf("read external index before nested setup: %v", err)
	}
	t.Setenv("GIT_INDEX_FILE", externalIndexPath)

	nestedRoot := t.TempDir()
	runGit(t, nestedRoot, "init")
	if err := os.WriteFile(filepath.Join(nestedRoot, "nested.txt"), []byte("nested\n"), 0o600); err != nil {
		t.Fatalf("write nested fixture: %v", err)
	}
	runGit(t, nestedRoot, "add", "nested.txt")
	runGit(t, nestedRoot, "-c", "user.name=Agent Layer", "-c", "user.email=agent-layer@example.invalid", "commit", "-m", "nested baseline")

	externalIndexAfter, err := os.ReadFile(externalIndexPath) // #nosec G304 -- test-created index under t.TempDir.
	if err != nil {
		t.Fatalf("read external index after nested setup: %v", err)
	}
	if !bytes.Equal(externalIndexAfter, externalIndexBefore) {
		t.Fatal("nested Git setup changed the inherited external index")
	}

	tree := runGit(t, nestedRoot, "ls-tree", "--name-only", "-r", "HEAD")
	entries := strings.Fields(tree)
	if !containsString(entries, "nested.txt") {
		t.Fatalf("nested commit tree omitted nested.txt: %q", tree)
	}
	if containsString(entries, "external-sentinel.txt") {
		t.Fatalf("nested commit tree used external index entry: %q", tree)
	}
}

func TestClaudeStatuslineScript_RendersFullPayload(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("write tracked fixture: %v", err)
	}
	runGit(t, root, "add", "tracked.txt")
	runGit(t, root, "-c", "user.name=Agent Layer", "-c", "user.email=agent-layer@example.invalid", "commit", "-m", "baseline")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("one\nTWO\nthree\nfour\nfive\n"), 0o600); err != nil {
		t.Fatalf("modify tracked fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("ignored\nignored\nignored\n"), 0o600); err != nil {
		t.Fatalf("write untracked fixture: %v", err)
	}
	cwd := filepath.Join(root, "subdir")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatalf("make subdir: %v", err)
	}

	// ~6.1 days out verifies the statusline rounds up partial days.
	resetEpoch := time.Now().Unix() + 6*86400 + 3*3600
	input := `{
		"model": {"display_name": "Opus 4.8"},
		"workspace": {"current_dir": "` + cwd + `"},
		"session_id": "sess123",
		"effort": {"level": "high"},
		"context_window": {"used_percentage": 43, "context_window_size": 200000, "total_input_tokens": 1000},
		"rate_limits": {"seven_day": {"used_percentage": 60, "resets_at": ` + strconv.FormatInt(resetEpoch, 10) + `}},
		"cost": {"total_cost_usd": 1.5, "total_lines_added": 99, "total_lines_removed": 88}
	}`
	out := runClaudeStatusline(t, cwd, input)

	// used_percentage 60 → 40% headroom remaining; reset ~6.1 days out.
	for _, want := range []string{"Opus 4.8 (high)", "ctx:43%", "lim:7d/40% left", "sess123", "+6", "-1", "Δ2", "$1.50"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got: %q", want, out)
		}
	}
	for _, absent := range []string{"#sess123", "+99", "-88"} {
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
	// Absent optional fields must not render their segments. "(" guards against
	// a stray reasoning-effort parenthetical on the model (e.g. "Sonnet (high)").
	for _, absent := range []string{"lim:", "("} {
		if strings.Contains(out, absent) {
			t.Errorf("did not expect %q in minimal output, got: %q", absent, out)
		}
	}
}

func TestClaudeStatuslineScript_WeeklyLimitShowsRoundedUpHoursWhenUnderADay(t *testing.T) {
	// Under 24h to reset: the time segment switches from days to hours, rounded
	// up so partial remaining hours are not hidden.
	cwd := t.TempDir()
	// ~5.5 hours out, which rounds up to 6h.
	resetEpoch := time.Now().Unix() + 5*3600 + 1800
	input := `{
		"model": {"display_name": "Opus 4.8"},
		"workspace": {"current_dir": "` + cwd + `"},
		"rate_limits": {"seven_day": {"used_percentage": 75, "resets_at": ` + strconv.FormatInt(resetEpoch, 10) + `}}
	}`
	out := runClaudeStatusline(t, cwd, input)
	// used_percentage 75 → 25% headroom remaining.
	if !strings.Contains(out, "lim:6h/25% left") {
		t.Errorf("expected lim:6h/25%% left, got: %q", out)
	}
}

func TestClaudeStatuslineScript_WeeklyLimitShowsSubHourReset(t *testing.T) {
	// Under an hour to reset: render "<1h" rather than the misleading "0h".
	cwd := t.TempDir()
	// ~30 min out; well under 3600s even after clock drift, so it stays "<1h".
	resetEpoch := time.Now().Unix() + 1800
	input := `{
		"model": {"display_name": "Opus 4.8"},
		"workspace": {"current_dir": "` + cwd + `"},
		"rate_limits": {"seven_day": {"used_percentage": 90, "resets_at": ` + strconv.FormatInt(resetEpoch, 10) + `}}
	}`
	out := runClaudeStatusline(t, cwd, input)
	// used_percentage 90 → 10% headroom remaining.
	if !strings.Contains(out, "lim:<1h/10% left") {
		t.Errorf("expected lim:<1h/10%% left, got: %q", out)
	}
}

func TestClaudeStatuslineScript_WeeklyLimitOmitsTimeWhenNoReset(t *testing.T) {
	// rate_limits present but resets_at absent (e.g. before the first API
	// response): show remaining headroom without fabricating a time-to-reset.
	cwd := t.TempDir()
	input := `{
		"model": {"display_name": "Opus 4.8"},
		"workspace": {"current_dir": "` + cwd + `"},
		"rate_limits": {"seven_day": {"used_percentage": 10}}
	}`
	out := runClaudeStatusline(t, cwd, input)
	// used_percentage 10 → 90% headroom; no "Nd/" or "Nh/" prefix.
	if !strings.Contains(out, "lim:90% left") {
		t.Errorf("expected lim:90%% left with no time segment, got: %q", out)
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
