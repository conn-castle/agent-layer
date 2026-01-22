package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nicholasjconn/agent-layer/internal/config"
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

	_, err := RunWithProject(file, project)
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

func boolPtr(v bool) *bool {
	return &v
}
