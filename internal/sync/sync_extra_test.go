package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestEnsureEnabled(t *testing.T) {
	name := "antigravity"
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
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	project := &config.ProjectConfig{
		Config: config.Config{
			Agents: config.AgentsConfig{
				Antigravity: config.AntigravityConfig{Enabled: testutil.BoolPtr(true)},
			},
		},
		Instructions: []config.InstructionFile{{Name: "00_base.md", Content: "base"}},
		Skills: []config.Skill{
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
	if err := os.MkdirAll(agentsPath, 0o700); err != nil {
		t.Fatalf("mkdir AGENTS.md dir: %v", err)
	}
	threshold := 1

	project := &config.ProjectConfig{
		Config: config.Config{
			Warnings: config.WarningsConfig{InstructionTokenThreshold: &threshold},
		},
		Root: root,
	}

	if _, err := collectWarnings(project, nil); err == nil {
		t.Fatal("expected collectWarnings to fail when AGENTS.md cannot be read as a file")
	}
}

// TestRunWithProject_AppliesWarningNoiseControl pins F-C-6: the noise-control
// pipeline (warnings.ApplyNoiseControl) must run inside RunWithProject for
// every successful sync. Without this test, a refactor that removes the call
// would silently regress quiet/reduce behavior for every client. Uses the
// invalid-noise-mode path because it adds a deterministic warning that does
// not require coercing other check sites.
func TestRunWithProject_AppliesWarningNoiseControl(t *testing.T) {
	t.Parallel()

	setupFixture := func(t *testing.T) (string, *config.ProjectConfig) {
		t.Helper()
		fixtureRoot := filepath.Join("testdata", "fixture-repo")
		root := t.TempDir()
		if err := copyFixtureRepo(fixtureRoot, root); err != nil {
			t.Fatalf("copy fixture: %v", err)
		}
		envPath := filepath.Join(root, ".agent-layer", ".env")
		if err := os.WriteFile(envPath, []byte("AL_EXAMPLE_TOKEN=token123\n"), 0o600); err != nil {
			t.Fatalf("write env: %v", err)
		}
		project, err := config.LoadProjectConfig(root)
		if err != nil {
			t.Fatalf("load project: %v", err)
		}
		return root, project
	}

	t.Run("invalid mode surfaces critical warning under default", func(t *testing.T) {
		root, project := setupFixture(t)
		// Invalid mode passed to ApplyNoiseControl falls through to the
		// "unknown" branch which appends a critical WARNING_NOISE_MODE_INVALID
		// to the filtered output. If ApplyNoiseControl is not wired in
		// RunWithProject, this warning never appears in result.Warnings.
		project.Config.Warnings.NoiseMode = "invalid-mode-xyz"
		result, err := RunWithProject(RealSystem{}, root, project)
		if err != nil {
			t.Fatalf("RunWithProject: %v", err)
		}
		foundCritical := false
		for _, w := range result.Warnings {
			if w.Code == "WARNING_NOISE_MODE_INVALID" {
				foundCritical = true
				break
			}
		}
		if !foundCritical {
			t.Fatalf("expected ApplyNoiseControl to inject WARNING_NOISE_MODE_INVALID; got Warnings=%+v AllWarnings=%+v", result.Warnings, result.AllWarnings)
		}
	})

	t.Run("quiet mode filters a deterministically-generated warning", func(t *testing.T) {
		root, project := setupFixture(t)
		// Force at least one collected warning by setting the instruction-token
		// threshold to 1 — the fixture instructions are guaranteed to be
		// larger than that, so CheckInstructions emits a warning. With this
		// warning present, the test can distinguish "ApplyNoiseControl is
		// wired" (Warnings empty under quiet mode) from "fixture produced no
		// warnings" (which would have masked a missing wiring fix).
		threshold := 1
		project.Config.Warnings.InstructionTokenThreshold = &threshold
		project.Config.Warnings.NoiseMode = "quiet"
		result, err := RunWithProject(RealSystem{}, root, project)
		if err != nil {
			t.Fatalf("RunWithProject: %v", err)
		}
		if len(result.AllWarnings) == 0 {
			t.Fatal("test setup error: expected the instruction-threshold warning to be raised in AllWarnings")
		}
		if len(result.Warnings) != 0 {
			t.Fatalf("expected quiet mode to suppress all warnings, got: %+v", result.Warnings)
		}
	})
}

func TestUpdateGitignoreValidationError(t *testing.T) {
	root := t.TempDir()
	blockPath := filepath.Join(root, ".agent-layer", "gitignore.block")
	if err := os.MkdirAll(filepath.Dir(blockPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	invalid := "# >>> agent-layer\n.agent-layer/\n"
	if err := os.WriteFile(blockPath, []byte(invalid), 0o600); err != nil {
		t.Fatalf("write gitignore.block: %v", err)
	}

	if err := updateGitignore(RealSystem{}, root); err == nil {
		t.Fatal("expected updateGitignore validation error")
	}
}
