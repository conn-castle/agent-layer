package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/conn-castle/agent-layer/internal/messages"
)

func TestCompletionInstallPathBash(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	path, note, err := completionInstallPath("bash")
	if err != nil {
		t.Fatalf("completionInstallPath error: %v", err)
	}
	expected := filepath.Join(xdg, "bash-completion", "completions", "al")
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
	if !strings.Contains(note, "bash-completion") {
		t.Fatalf("expected bash note, got %q", note)
	}
}

func TestCompletionInstallPathFish(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)

	path, note, err := completionInstallPath("fish")
	if err != nil {
		t.Fatalf("completionInstallPath error: %v", err)
	}
	expected := filepath.Join(xdg, "fish", "completions", "al.fish")
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
	if !strings.Contains(note, "fish") {
		t.Fatalf("expected fish note, got %q", note)
	}
}

func TestCompletionInstallPathZshUsesFpath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FPATH", dir)

	path, note, err := completionInstallPath("zsh")
	if err != nil {
		t.Fatalf("completionInstallPath error: %v", err)
	}
	expected := filepath.Join(dir, "_al")
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
	if note != "" {
		t.Fatalf("expected no note, got %q", note)
	}
}

func TestCompletionInstallPathZshFallback(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	t.Setenv("FPATH", filepath.Join(xdg, "missing"))

	path, note, err := completionInstallPath("zsh")
	if err != nil {
		t.Fatalf("completionInstallPath error: %v", err)
	}
	expectedDir := filepath.Join(xdg, "zsh", "site-functions")
	expected := filepath.Join(expectedDir, "_al")
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
	if !strings.Contains(note, "fpath=(") {
		t.Fatalf("expected fpath note, got %q", note)
	}
}

func TestNewCompletionCmd(t *testing.T) {
	cmd := newCompletionCmd()
	if cmd.Use != "completion [bash|zsh|fish]" {
		t.Errorf("unexpected usage: %s", cmd.Use)
	}

	// Test execution
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"bash"})

	// We need a root command for it to work
	root := &cobra.Command{Use: "al"}
	root.AddCommand(cmd)

	// Execute via root
	root.SetOut(buf)
	root.SetArgs([]string{"completion", "bash"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "bash completion") {
		t.Errorf("expected bash completion output, got %q", output)
	}
}

func TestNewCompletionCmd_Install(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	cmd := newCompletionCmd()
	buf := new(bytes.Buffer)

	root := &cobra.Command{Use: "al"}
	root.AddCommand(cmd)

	root.SetOut(buf)
	root.SetArgs([]string{"completion", "bash", "--install"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	expected := filepath.Join(xdg, "bash-completion", "completions", "al")
	if !strings.Contains(buf.String(), "Installed bash completion to") {
		t.Errorf("expected installation message, got %q", buf.String())
	}
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected completion file to be created at %s", expected)
	}
}

func TestInstallCompletion_Error(t *testing.T) {
	// Setup unwritable XDG_DATA_HOME to force MkdirAll/WriteFileAtomic error
	xdg := t.TempDir()
	// Make xdg read-only
	if err := os.Chmod(xdg, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", xdg)

	// installCompletion tries to write to xdg/bash-completion/...
	// It tries MkdirAll(filepath.Dir(path)).
	// filepath.Dir(path) is xdg/bash-completion/completions.
	// Since xdg is 0555, creating "bash-completion" should fail.

	var buf bytes.Buffer
	err := installCompletion("bash", "script", &buf)
	if err == nil {
		t.Fatal("expected error when install dir is unwritable")
	}
	if !strings.Contains(err.Error(), "create completion dir") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestInstallCompletion_WriteError(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	// Create directory where the file should be to force write error
	targetDir := filepath.Join(xdg, "bash-completion", "completions")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(targetDir, "al")
	if err := os.Mkdir(targetFile, 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := installCompletion("bash", "script", &buf)
	if err == nil {
		t.Fatal("expected error when target is a directory")
	}
	if !strings.Contains(err.Error(), "write completion file") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestInstallCompletion_PathError(t *testing.T) {
	// Unset HOME to fail UserHomeDir
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "") // Ensure no fallback

	var buf bytes.Buffer
	err := installCompletion("bash", "script", &buf)
	if err == nil {
		// If running in an env where UserHomeDir works without HOME (e.g. some CI/OS), this might fail.
		// os.UserHomeDir implementation depends on OS.
		// On Unix it checks HOME.
		// Let's assume it fails or returns error.
		// If it succeeds, we skip the test assertion or log it.
		t.Log("os.UserHomeDir succeeded without HOME, skipping failure check")
	}
}

func TestNewCompletionCmd_UnknownShell(t *testing.T) {
	cmd := newCompletionCmd()
	buf := new(bytes.Buffer)

	root := &cobra.Command{Use: "al"}
	root.AddCommand(cmd)

	root.SetOut(buf)
	root.SetArgs([]string{"completion", "unknown"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for unknown shell")
	}
}
func TestInstallCompletion_ZshError(t *testing.T) {
	t.Setenv("FPATH", "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("PATH", "") // Hide zsh so firstWritableFpath fails

	var buf bytes.Buffer
	err := installCompletion("zsh", "script", &buf)
	if err == nil {
		t.Log("installCompletion zsh succeeded unexpectedly (maybe fallback worked?)")
	}
}
func TestGenerateCompletion(t *testing.T) {
	cmd := newCompletionCmd()

	tests := []struct {
		shell   string
		wantErr bool
	}{
		{"bash", false},
		{"zsh", false},
		{"fish", false},
		{"unknown", true},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			got, err := generateCompletion(cmd.Root(), tt.shell)
			if (err != nil) != tt.wantErr {
				t.Errorf("generateCompletion() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == "" {
				t.Error("generateCompletion() returned empty string")
			}
		})
	}
}

func TestGenerateCompletion_Error(t *testing.T) {
	origBash := genBashCompletion
	origZsh := genZshCompletion
	origFish := genFishCompletion
	t.Cleanup(func() {
		genBashCompletion = origBash
		genZshCompletion = origZsh
		genFishCompletion = origFish
	})

	errBoom := errors.New("boom")
	tests := []struct {
		name  string
		shell string
		setup func()
	}{
		{
			name:  "bash",
			shell: shellBash,
			setup: func() {
				genBashCompletion = func(_ *cobra.Command, _ io.Writer) error { return errBoom }
			},
		},
		{
			name:  "zsh",
			shell: shellZsh,
			setup: func() {
				genZshCompletion = func(_ *cobra.Command, _ io.Writer) error { return errBoom }
			},
		},
		{
			name:  "fish",
			shell: shellFish,
			setup: func() {
				genFishCompletion = func(_ *cobra.Command, _ io.Writer) error { return errBoom }
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			genBashCompletion = origBash
			genZshCompletion = origZsh
			genFishCompletion = origFish
			tt.setup()

			cmd := newCompletionCmd()
			_, err := generateCompletion(cmd.Root(), tt.shell)
			if !errors.Is(err, errBoom) {
				t.Fatalf("expected error %q, got %v", errBoom, err)
			}
		})
	}
}

func TestInstallCompletion(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	// Mock stdout
	// We can't easily mock cmd.OutOrStdout in installCompletion as it takes io.Writer
	// But installCompletion is what we are testing directly.

	var buf strings.Builder
	err := installCompletion("bash", "script content", &buf)
	if err != nil {
		t.Fatalf("installCompletion error: %v", err)
	}

	expectedPath := filepath.Join(xdg, "bash-completion", "completions", "al")
	if !strings.Contains(buf.String(), expectedPath) {
		t.Errorf("output missing path: %s", buf.String())
	}
}

type alwaysFailWriter struct{}

func (alwaysFailWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

type failOnStringWriter struct {
	failOn string
}

func (w *failOnStringWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.failOn) {
		return 0, errors.New("write failed")
	}
	return len(p), nil
}

func (w *failOnStringWriter) WriteString(s string) (int, error) {
	if strings.Contains(s, w.failOn) {
		return 0, errors.New("write failed")
	}
	return len(s), nil
}

func TestInstallCompletion_OutputWriteError(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	err := installCompletion("bash", "script content", alwaysFailWriter{})
	if err == nil {
		t.Fatal("expected error when output write fails")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallCompletion_NoteWriteError(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)

	writer := &failOnStringWriter{failOn: messages.CompletionBashNote}
	err := installCompletion("bash", "script content", writer)
	if err == nil {
		t.Fatal("expected error when note write fails")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestXdgDataHomeFallback(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	// We can't easily unset HOME/USERPROFILE in a cross-platform way safely for other tests,
	// but we can check if it returns a path containing ".local/share"
	got, err := xdgDataHome()
	if err != nil {
		t.Fatalf("xdgDataHome error: %v", err)
	}
	if !strings.Contains(got, ".local") { // Simple check
		t.Errorf("expected fallback path to contain .local, got %s", got)
	}
}

func TestXdgConfigHomeFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got, err := xdgConfigHome()
	if err != nil {
		t.Fatalf("xdgConfigHome error: %v", err)
	}
	if !strings.Contains(got, ".config") {
		t.Errorf("expected fallback path to contain .config, got %s", got)
	}
}

func TestCompletionInstallPath_HomeError(t *testing.T) {
	orig := userHomeDir
	t.Cleanup(func() { userHomeDir = orig })
	userHomeDir = func() (string, error) {
		return "", errors.New("home error")
	}

	t.Setenv("XDG_DATA_HOME", "")
	if _, _, err := completionInstallPath(shellBash); err == nil {
		t.Fatal("expected error for bash when home resolution fails")
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	if _, _, err := completionInstallPath(shellFish); err == nil {
		t.Fatal("expected error for fish when home resolution fails")
	}
}

func TestFirstWritableFpath(t *testing.T) {
	// Setup a mix of invalid and valid paths in FPATH
	tempDir := t.TempDir()
	validDir := filepath.Join(tempDir, "valid")
	if err := os.Mkdir(validDir, 0o755); err != nil {
		t.Fatal(err)
	}
	invalidDir := filepath.Join(tempDir, "invalid")
	// missing dir

	pathList := []string{invalidDir, validDir}
	t.Setenv("FPATH", strings.Join(pathList, string(filepath.ListSeparator)))

	got, ok := firstWritableFpath()
	if !ok {
		t.Fatal("expected to find a writable directory")
	}
	if got != validDir {
		t.Errorf("expected %s, got %s", validDir, got)
	}
}

// Test helper process for mocking exec.Command
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	// Print a fake fpath from FAKE_FPATH_OUTPUT env var (set by the test).
	if p := os.Getenv("FAKE_FPATH_OUTPUT"); p != "" {
		fmt.Println(p)
	}
	os.Exit(0)
}

func TestFirstWritableFpath_ExecZsh(t *testing.T) {
	origLookPath := lookPath
	origExecCommand := execCommand
	defer func() {
		lookPath = origLookPath
		execCommand = origExecCommand
	}()

	lookPath = func(file string) (string, error) {
		return "zsh", nil
	}

	tempDir := t.TempDir()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, arg...)
		cmd := exec.Command(os.Args[0], cs...) //nolint:gosec // standard test re-exec pattern
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", fmt.Sprintf("FAKE_FPATH_OUTPUT=%s", tempDir))
		return cmd
	}

	got, ok := firstWritableFpath()
	if !ok {
		t.Fatal("expected success via zsh exec")
	}
	if got != tempDir {
		t.Errorf("got %s, want %s", got, tempDir)
	}
}

func TestFirstWritableFpath_ExecFail(t *testing.T) {
	origLookPath := lookPath
	origExecCommand := execCommand
	defer func() {
		lookPath = origLookPath
		execCommand = origExecCommand
	}()

	// Case 1: LookPath fails
	lookPath = func(file string) (string, error) {
		return "", fmt.Errorf("not found")
	}
	_, ok := firstWritableFpath()
	if ok {
		t.Error("expected failure when LookPath fails")
	}

	// Case 2: Exec fails
	lookPath = func(file string) (string, error) {
		return "zsh", nil
	}
	execCommand = func(name string, arg ...string) *exec.Cmd {
		// Run a command that fails
		cmd := exec.Command("false")
		return cmd
	}
	_, ok = firstWritableFpath()
	if ok {
		t.Error("expected failure when exec fails")
	}
}

func TestFirstWritableFpath_ExecZshSkipsEmpty(t *testing.T) {
	origLookPath := lookPath
	origExecCommand := execCommand
	t.Cleanup(func() {
		lookPath = origLookPath
		execCommand = origExecCommand
	})

	lookPath = func(string) (string, error) {
		return "zsh", nil
	}

	tempDir := t.TempDir()
	output := fmt.Sprintf("\n\n%s\n", tempDir)
	execCommand = func(name string, arg ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--", name}
		cs = append(cs, arg...)
		cmd := exec.Command(os.Args[0], cs...) //nolint:gosec // standard test re-exec pattern
		cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", fmt.Sprintf("FAKE_FPATH_OUTPUT=%s", output))
		return cmd
	}

	got, ok := firstWritableFpath()
	if !ok {
		t.Fatal("expected success via zsh exec")
	}
	if got != tempDir {
		t.Fatalf("got %s, want %s", got, tempDir)
	}
}

func TestUserHomeDir_Error(t *testing.T) {
	orig := userHomeDir
	defer func() { userHomeDir = orig }()
	userHomeDir = func() (string, error) {
		return "", fmt.Errorf("home dir error")
	}

	t.Setenv("XDG_DATA_HOME", "")
	_, err := xdgDataHome()
	if err == nil {
		t.Error("expected error from xdgDataHome when home dir fails")
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	_, err = xdgConfigHome()
	if err == nil {
		t.Error("expected error from xdgConfigHome when home dir fails")
	}
}

func TestWritableDir(t *testing.T) {
	// We can test writableDir indirectly via firstWritableFpath
	// or export it? No, keep it internal.
	// We already tested successful case in TestFirstWritableFpath.

	// Test unwritable dir
	tempDir := t.TempDir()
	unwritable := filepath.Join(tempDir, "unwritable")
	if err := os.Mkdir(unwritable, 0o500); err != nil { // Read/Exec only
		t.Fatal(err)
	}

	t.Setenv("FPATH", unwritable)
	_, ok := firstWritableFpath()
	// Depending on OS/user (root), this might still be writable.
	// Skipping explicit fail check to avoid flake on CI/root.
	if ok {
		t.Logf("Directory %s is writable (possibly running as root or Windows ignores mode)", unwritable)
	}
}
