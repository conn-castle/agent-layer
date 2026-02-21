package sync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/testutil"
	"github.com/conn-castle/agent-layer/internal/warnings"
)

// TestRunWithProjectGeminiTrustWarningNoiseControlled verifies that a Gemini
// trust warning produced during RunWithProject is included in the result under
// default noise mode and suppressed under reduce mode.
func TestRunWithProjectGeminiTrustWarningNoiseControlled(t *testing.T) {
	// Stub UserHomeDir to force a warning.
	orig := UserHomeDir
	UserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { UserHomeDir = orig })

	root := t.TempDir()

	// Write minimal gitignore.block so updateGitignore succeeds.
	alDir := filepath.Join(root, ".agent-layer")
	if err := os.MkdirAll(alDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	block := ".agent-layer/\n"
	if err := os.WriteFile(filepath.Join(alDir, "gitignore.block"), []byte(block), 0o644); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}

	sys := &MockSystem{
		Fallback: RealSystem{},
		LookPathFunc: func(file string) (string, error) {
			if file == "al" {
				return "/usr/local/bin/al", nil
			}
			return "", os.ErrNotExist
		},
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Gemini: config.AgentConfig{Enabled: testutil.BoolPtr(true)},
			},
			Warnings: config.WarningsConfig{NoiseMode: "default"},
		},
		Root: root,
	}

	result, err := RunWithProject(sys, root, project)
	if err != nil {
		t.Fatalf("RunWithProject error: %v", err)
	}

	// The trust warning should appear in the result.
	found := false
	for _, w := range result.Warnings {
		if w.Code == warnings.CodeGeminiTrustFolderFailed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected GEMINI_TRUST_FOLDER_FAILED warning in result, got %d warnings", len(result.Warnings))
	}

	// With noise_mode=reduce, the suppressible trust warning should be suppressed.
	project.Config.Warnings.NoiseMode = "reduce"
	result2, err := RunWithProject(sys, root, project)
	if err != nil {
		t.Fatalf("RunWithProject error (reduce): %v", err)
	}
	for _, w := range result2.Warnings {
		if w.Code == warnings.CodeGeminiTrustFolderFailed {
			t.Fatalf("trust warning should be suppressed in reduce mode")
		}
	}
}
