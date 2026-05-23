package agentdispatch

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestBuildChildPromptExactBytes(t *testing.T) {
	project := &config.ProjectConfig{
		Skills: []config.Skill{{Name: "review-plan"}},
	}
	tests := []struct {
		name   string
		target string
		prompt string
		skill  string
		want   string
	}{
		{name: "codex prompt only", target: AgentCodex, prompt: "Review this.", want: "Review this."},
		{name: "codex skill only", target: AgentCodex, skill: "review-plan", want: "$review-plan"},
		{name: "codex prompt and skill", target: AgentCodex, prompt: "Review this.", skill: "review-plan", want: "$review-plan\nReview this."},
		{name: "codex multiline prompt", target: AgentCodex, prompt: "Line one\nLine two", skill: "review-plan", want: "$review-plan\nLine one\nLine two"},
		{name: "codex unicode prompt", target: AgentCodex, prompt: "Review café.", skill: "review-plan", want: "$review-plan\nReview café."},
		{name: "claude prompt only", target: AgentClaude, prompt: "Review this.", want: "Review this."},
		{name: "claude skill only", target: AgentClaude, skill: "review-plan", want: "/review-plan"},
		{name: "claude prompt and skill", target: AgentClaude, prompt: "Review this.", skill: "review-plan", want: "/review-plan\nReview this."},
		{name: "claude multiline prompt", target: AgentClaude, prompt: "Line one\nLine two", skill: "review-plan", want: "/review-plan\nLine one\nLine two"},
		{name: "claude unicode prompt", target: AgentClaude, prompt: "Review café.", skill: "review-plan", want: "/review-plan\nReview café."},
		{name: "antigravity prompt only", target: AgentAntigravity, prompt: "Review this.", want: "Review this."},
		{name: "antigravity skill only", target: AgentAntigravity, skill: "review-plan", want: "/review-plan"},
		{name: "antigravity prompt and skill", target: AgentAntigravity, prompt: "Review this.", skill: "review-plan", want: "/review-plan\nReview this."},
		{name: "antigravity multiline prompt", target: AgentAntigravity, prompt: "Line one\nLine two", skill: "review-plan", want: "/review-plan\nLine one\nLine two"},
		{name: "antigravity unicode prompt", target: AgentAntigravity, prompt: "Review café.", skill: "review-plan", want: "/review-plan\nReview café."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildChildPrompt(project, tc.target, tc.prompt, tc.skill)
			if err != nil {
				t.Fatalf("BuildChildPrompt error: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("prompt bytes = %q, want %q", string(got), tc.want)
			}
		})
	}
}

func TestBuildChildPromptRequiresPromptOrSkill(t *testing.T) {
	_, err := BuildChildPrompt(&config.ProjectConfig{}, AgentCodex, "", "")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitUsage {
		t.Fatalf("expected ExitUsage, got %T: %v", err, err)
	}
}

func TestBuildChildPromptMissingSkill(t *testing.T) {
	_, err := BuildChildPrompt(&config.ProjectConfig{}, AgentCodex, "Review", "missing")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != ExitConfig {
		t.Fatalf("expected ExitConfig, got %T: %v", err, err)
	}
}

func TestResolvePromptPositionalWinsOverStdin(t *testing.T) {
	got, err := ResolvePrompt([]string{"hello", "world"}, errorReader{}, true)
	if err != nil {
		t.Fatalf("ResolvePrompt error: %v", err)
	}
	if got != "hello world" {
		t.Fatalf("prompt = %q, want positional prompt", got)
	}
}

func TestResolvePromptReadsStdin(t *testing.T) {
	got, err := ResolvePrompt(nil, strings.NewReader("from stdin"), true)
	if err != nil {
		t.Fatalf("ResolvePrompt error: %v", err)
	}
	if got != "from stdin" {
		t.Fatalf("prompt = %q, want stdin prompt", got)
	}
}

type errorReader struct{}

func (errorReader) Read(_ []byte) (int, error) {
	return 0, errors.New("read failed")
}

// TestValidateSkillProjectionRejectsSymlink exercises F6: a synced
// projection that is a symlink (which Stat would have silently followed)
// is rejected as a non-regular file with ExitConfig.
func TestValidateSkillProjectionRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
	root := t.TempDir()
	target := targetMeta{Name: AgentCodex, SharedSkillProject: true}
	skill := "evil"
	projection := skillProjectionPath(root, target, skill)
	if err := os.MkdirAll(filepath.Dir(projection), 0o700); err != nil {
		t.Fatalf("mkdir projection dir: %v", err)
	}
	// Point the projection at a real regular file we own so the test is
	// portable (no reliance on system files like /etc/hostname). Lstat
	// must still see the symlink mode and refuse to follow it.
	targetFile := filepath.Join(root, "symlink-target.txt")
	if err := os.WriteFile(targetFile, []byte("ok"), 0o600); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	if err := os.Symlink(targetFile, projection); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	err := validateSkillProjection(root, target, skill)
	exitErr := requireDispatchExitCode(t, err, ExitConfig)
	if !strings.Contains(exitErr.Error(), "not a regular file") {
		t.Fatalf("expected not-a-regular-file message, got %q", exitErr.Error())
	}
}
