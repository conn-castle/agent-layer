package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
)

func TestEnsureEnabled(t *testing.T) {
	name := "gemini"
	if err := EnsureEnabled(name, nil); err == nil {
		t.Fatalf("expected error for nil enabled")
	}

	disabled := false
	if err := EnsureEnabled(name, &disabled); err == nil {
		t.Fatalf("expected error for disabled")
	}

	enabled := true
	if err := EnsureEnabled(name, &enabled); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMissingConfig(t *testing.T) {
	_, err := Run(t.TempDir())
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunWithProjectError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Enabled: boolPtr(true)},
			},
		},
		Instructions: []config.InstructionFile{{Name: "00_base.md", Content: "base"}},
		SlashCommands: []config.SlashCommand{
			{Name: "alpha", Description: "desc", Body: "body"},
		},
		Root: file,
	}

	_, err := RunWithProject(RealSystem{}, file, project)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestRunStepsError(t *testing.T) {
	err := runSteps([]func() error{
		func() error { return fmt.Errorf("boom") },
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestCollectWarningsInstructionsError(t *testing.T) {
	root := t.TempDir()
	agentsPath := filepath.Join(root, "AGENTS.md")
	if err := os.MkdirAll(agentsPath, 0o755); err != nil {
		t.Fatalf("mkdir AGENTS.md dir: %v", err)
	}
	threshold := 1

	project := &config.ProjectConfig{
		Config: config.Config{
			Warnings: config.WarningsConfig{InstructionTokenThreshold: &threshold},
		},
		Root: root,
	}

	if _, err := collectWarnings(project); err == nil {
		t.Fatal("expected collectWarnings to fail when AGENTS.md cannot be read as a file")
	}
}

func TestUpdateGitignoreValidationError(t *testing.T) {
	root := t.TempDir()
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(blockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	invalid := "# >>> agent-layer\n.agent-layer/\n"
	if err := os.WriteFile(blockPath, []byte(invalid), 0o644); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}

	if err := updateGitignore(RealSystem{}, root); err == nil {
		t.Fatal("expected updateGitignore validation error")
	}
}

func boolPtr(v bool) *bool {
	return &v
}
