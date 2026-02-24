package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/dispatch"
	alsync "github.com/conn-castle/agent-layer/internal/sync"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestInitAndSyncCommands(t *testing.T) {
	home := t.TempDir()
	origHome := alsync.UserHomeDir
	alsync.UserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { alsync.UserHomeDir = origHome })

	root := t.TempDir()
	testutil.WithWorkingDir(t, root, func() {
		cmd := newInitCmd()
		cmd.SetArgs([]string{"--no-wizard"})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetIn(bytes.NewBufferString(""))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("init error: %v", err)
		}
		binDir := t.TempDir()
		testutil.WriteStub(t, binDir, "al")
		t.Setenv("PATH", binDir)
		t.Setenv(dispatch.EnvNoNetwork, "1")

		syncCmd := newSyncCmd()
		err := syncCmd.RunE(syncCmd, nil)
		// Sync might fail with warnings if templates are large, which is expected behavior now.
		if err != nil && !errors.Is(err, ErrSyncCompletedWithWarnings) {
			t.Fatalf("sync error: %v", err)
		}

		if _, err := os.Stat(filepath.Join(root, ".agent-layer", "config.toml")); err != nil {
			t.Fatalf("expected config.toml to exist: %v", err)
		}
	})
}

func TestInitCommandNoWizardSkipsWizard(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	originalRunWizard := runWizard
	wizardCalled := false
	runWizard = func(_ string, _ string) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizard = originalRunWizard })

	cmd := newInitCmd()
	cmd.SetArgs([]string{"--no-wizard"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("y\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init error: %v", err)
	}
	if wizardCalled {
		t.Fatalf("expected wizard to be skipped with --no-wizard")
	}
}

func TestInitCommandPromptYesRunsWizard(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	originalRunWizard := runWizard
	wizardCalled := false
	runWizard = func(_ string, _ string) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizard = originalRunWizard })

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("y\n"))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init error: %v", err)
	}
	if !wizardCalled {
		t.Fatalf("expected wizard to run after confirmation")
	}
}

func TestInitCommandNonInteractiveSkipsWizard(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	originalRunWizard := runWizard
	wizardCalled := false
	runWizard = func(_ string, _ string) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizard = originalRunWizard })

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init error: %v", err)
	}
	if wizardCalled {
		t.Fatalf("expected wizard to be skipped in non-interactive mode")
	}
}

func TestInitCommandPromptNoDeclinesWizard(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	originalRunWizard := runWizard
	wizardCalled := false
	runWizard = func(_ string, _ string) error {
		wizardCalled = true
		return nil
	}
	t.Cleanup(func() { runWizard = originalRunWizard })

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString("n\n")) // Decline wizard
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init error: %v", err)
	}
	if wizardCalled {
		t.Fatalf("expected wizard to be skipped when user declines")
	}
}

func TestClientCommandsMissingConfig(t *testing.T) {
	t.Setenv(config.BuiltinRepoRootEnvVar, "")
	originalPromptServer := runPromptServer
	runPromptServer = func(ctx context.Context, version string, commands []config.SlashCommand) error {
		t.Fatalf("runPromptServer should not be called when .agent-layer is missing")
		return nil
	}
	t.Cleanup(func() { runPromptServer = originalPromptServer })

	root := t.TempDir()
	testutil.WithWorkingDir(t, root, func() {
		commands := []*cobra.Command{
			newGeminiCmd(),
			newClaudeCmd(),
			newCodexCmd(),
			newVSCodeCmd(),
			newAntigravityCmd(),
			newMcpPromptsCmd(),
		}
		for _, cmd := range commands {
			cmd.SetContext(context.Background())
			err := cmd.RunE(cmd, nil)
			if err == nil {
				t.Fatalf("expected error for %s", cmd.Use)
			}
		}
	})
}

func TestClientCommandsSuccess(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "gemini")
	testutil.WriteStub(t, binDir, "claude")
	testutil.WriteStub(t, binDir, "codex")
	testutil.WriteStub(t, binDir, "code")
	testutil.WriteStub(t, binDir, "antigravity")
	testutil.WriteStub(t, binDir, "al")

	t.Setenv("PATH", binDir)

	original := runPromptServer
	t.Cleanup(func() { runPromptServer = original })
	runPromptServer = func(ctx context.Context, version string, commands []config.SlashCommand) error {
		return nil
	}

	testutil.WithWorkingDir(t, root, func() {
		commands := []*cobra.Command{
			newGeminiCmd(),
			newClaudeCmd(),
			newCodexCmd(),
			newVSCodeCmd(),
			newAntigravityCmd(),
			newMcpPromptsCmd(),
		}
		for _, cmd := range commands {
			if err := cmd.RunE(cmd, nil); err != nil {
				t.Fatalf("command %s failed: %v", cmd.Use, err)
			}
		}
	})
}

func TestSyncCommand_WithWarnings(t *testing.T) {
	root := t.TempDir()
	writeTestRepoWithWarnings(t, root)
	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "al")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	testutil.WithWorkingDir(t, root, func() {
		cmd := newSyncCmd()
		err := cmd.RunE(cmd, nil)
		// Sync should fail when warnings exist
		if err == nil {
			t.Fatal("expected sync to fail when warnings exist")
		}
		if !errors.Is(err, ErrSyncCompletedWithWarnings) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestSyncCommand_QuietSuppressesWarnings(t *testing.T) {
	root := t.TempDir()
	writeTestRepoWithWarnings(t, root)
	testutil.WithWorkingDir(t, root, func() {
		cmd := newSyncCmd()
		cmd.Flags().Bool("quiet", false, "")
		if err := cmd.Flags().Set("quiet", "true"); err != nil {
			t.Fatalf("set quiet flag: %v", err)
		}
		var stderr bytes.Buffer
		cmd.SetErr(&stderr)
		err := cmd.RunE(cmd, nil)
		if err == nil {
			t.Fatal("expected sync to fail with warnings in quiet mode")
		}
		var silent *SilentExitError
		if !errors.As(err, &silent) {
			t.Fatalf("expected SilentExitError{Code:1}, got %T: %v", err, err)
		}
		if silent.Code != 1 {
			t.Fatalf("expected SilentExitError{Code:1}, got Code:%d", silent.Code)
		}
		if stderr.String() != "" {
			t.Fatalf("expected no stderr output, got %q", stderr.String())
		}
	})
}

func TestWizardCommand(t *testing.T) {
	originalIsTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	root := t.TempDir()
	testutil.WithWorkingDir(t, root, func() {
		// Force the non-interactive path to keep tests deterministic.
		cmd := newWizardCmd()
		err := cmd.RunE(cmd, nil)
		// Should fail because not interactive
		if err == nil {
			t.Fatal("expected wizard to fail in non-interactive test")
		}
		if !strings.Contains(err.Error(), "interactive terminal") {
			t.Logf("got error: %v", err)
		}
	})
}

func TestCommandsGetwdError(t *testing.T) {
	t.Setenv(config.BuiltinRepoRootEnvVar, "")
	originalPromptServer := runPromptServer
	runPromptServer = func(ctx context.Context, version string, commands []config.SlashCommand) error {
		t.Fatalf("runPromptServer should not be called when getwd fails")
		return nil
	}
	t.Cleanup(func() { runPromptServer = originalPromptServer })

	original := getwd
	getwd = func() (string, error) {
		return "", errors.New("boom")
	}
	t.Cleanup(func() { getwd = original })

	commands := []*cobra.Command{
		newInitCmd(),
		newSyncCmd(),
		newMcpPromptsCmd(),
		newGeminiCmd(),
		newClaudeCmd(),
		newCodexCmd(),
		newVSCodeCmd(),
		newAntigravityCmd(),
		newDoctorCmd(),
	}
	for _, cmd := range commands {
		cmd.SetContext(context.Background())
		if err := cmd.RunE(cmd, nil); err == nil {
			t.Fatalf("expected error for %s", cmd.Use)
		}
	}
}

func TestInitCommandInstallRunError(t *testing.T) {
	// Point to a file instead of directory to cause install.Run to fail
	root := t.TempDir()
	blockingFile := filepath.Join(root, ".agent-layer")
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}

	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(bytes.NewBufferString(""))

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when install.Run fails")
	}
}

func TestInitCommandPromptError(t *testing.T) {
	root := t.TempDir()
	originalGetwd := getwd
	getwd = func() (string, error) { return root, nil }
	t.Cleanup(func() { getwd = originalGetwd })

	originalIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = originalIsTerminal })

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(errorReader{}) // Use errorReader to cause promptYesNo to fail

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when promptYesNo fails")
	}
	if !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
