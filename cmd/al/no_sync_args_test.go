package main

import (
	"bytes"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/run"
	"github.com/conn-castle/agent-layer/internal/testutil"
)

func TestClientLaunchDiagnosticsUseCommandStderr(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "synchronized", args: []string{"--client-arg"}},
		{name: "no sync", args: []string{"--no-sync", "--client-arg"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeClientLaunchDiagnosticRepo(t, false)
			var gotArgs []string
			launchCalls := 0
			cmd := newNoSyncLaunchCmd("test", "test", "vscode", func(cfg *config.Config) *bool {
				return cfg.Agents.VSCode.Enabled
			}, func(_ *config.ProjectConfig, _ *run.Info, _ []string, args []string) error {
				launchCalls++
				gotArgs = append([]string(nil), args...)
				return nil
			})
			var commandStderr bytes.Buffer
			cmd.SetErr(&commandStderr)

			var runErr error
			processStderr := captureProcessStderr(t, func() {
				testutil.WithWorkingDir(t, root, func() {
					runErr = cmd.RunE(cmd, tt.args)
				})
			})
			if runErr != nil {
				t.Fatalf("run client command: %v", runErr)
			}
			if commandStderr.Len() == 0 {
				t.Fatal("expected launch diagnostic on the command stderr writer")
			}
			if processStderr != "" {
				t.Fatalf("expected no launch diagnostic on process stderr, got %q", processStderr)
			}
			if launchCalls != 1 {
				t.Fatalf("launch calls = %d, want 1", launchCalls)
			}
			if want := []string{"--client-arg"}; !slices.Equal(gotArgs, want) {
				t.Fatalf("launch args = %#v, want %#v", gotArgs, want)
			}
		})
	}
}

func TestClientLaunchQuietSuppressesCommandDiagnostics(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		configQuiet bool
	}{
		{name: "synchronized flag", args: []string{"--quiet", "--client-arg"}},
		{name: "synchronized config", args: []string{"--client-arg"}, configQuiet: true},
		{name: "no sync flag", args: []string{"--no-sync", "--quiet", "--client-arg"}},
		{name: "no sync config", args: []string{"--no-sync", "--client-arg"}, configQuiet: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := writeClientLaunchDiagnosticRepo(t, tt.configQuiet)
			var gotArgs []string
			launchCalls := 0
			cmd := newNoSyncLaunchCmd("test", "test", "vscode", func(cfg *config.Config) *bool {
				return cfg.Agents.VSCode.Enabled
			}, func(_ *config.ProjectConfig, _ *run.Info, _ []string, args []string) error {
				launchCalls++
				gotArgs = append([]string(nil), args...)
				return nil
			})
			var commandStderr bytes.Buffer
			cmd.SetErr(&commandStderr)

			var runErr error
			processStderr := captureProcessStderr(t, func() {
				testutil.WithWorkingDir(t, root, func() {
					runErr = cmd.RunE(cmd, tt.args)
				})
			})
			if runErr != nil {
				t.Fatalf("run client command: %v", runErr)
			}
			if commandStderr.Len() != 0 {
				t.Fatalf("expected quiet launch to suppress command diagnostics, got %q", commandStderr.String())
			}
			if processStderr != "" {
				t.Fatalf("expected quiet launch to suppress process diagnostics, got %q", processStderr)
			}
			if launchCalls != 1 {
				t.Fatalf("launch calls = %d, want 1", launchCalls)
			}
			if want := []string{"--client-arg"}; !slices.Equal(gotArgs, want) {
				t.Fatalf("launch args = %#v, want %#v", gotArgs, want)
			}
		})
	}
}

func TestSplitNoSyncArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantNoSync bool
		wantQuiet  bool
		wantArgs   []string
		wantErr    bool
	}{
		{
			name:       "no-sync before separator",
			args:       []string{"--no-sync", "--", "--reuse-window"},
			wantNoSync: true,
			wantQuiet:  false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "quiet before separator",
			args:       []string{"--quiet", "--no-sync", "--", "--reuse-window"},
			wantNoSync: true,
			wantQuiet:  true,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "quiet bool true",
			args:       []string{"--quiet=true", "--reuse-window"},
			wantNoSync: false,
			wantQuiet:  true,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "no-sync bool false",
			args:       []string{"--no-sync=false", "--reuse-window"},
			wantNoSync: false,
			wantQuiet:  false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "quiet bool false consumed",
			args:       []string{"--quiet=false", "--reuse-window"},
			wantNoSync: false,
			wantQuiet:  false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:    "no-sync invalid value",
			args:    []string{"--no-sync=maybe"},
			wantErr: true,
		},
		{
			name:    "quiet invalid value",
			args:    []string{"--quiet=maybe"},
			wantErr: true,
		},
		{
			name:       "pass-through without separator",
			args:       []string{"--reuse-window"},
			wantNoSync: false,
			wantQuiet:  false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "no-sync after separator",
			args:       []string{"--", "--no-sync"},
			wantNoSync: false,
			wantQuiet:  false,
			wantArgs:   []string{"--no-sync"},
		},
		{
			name:       "quiet after separator",
			args:       []string{"--", "--quiet"},
			wantNoSync: false,
			wantQuiet:  false,
			wantArgs:   []string{"--quiet"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNoSync, gotQuiet, gotArgs, err := splitNoSyncArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotNoSync != tt.wantNoSync {
				t.Fatalf("expected noSync=%v, got %v", tt.wantNoSync, gotNoSync)
			}
			if gotQuiet != tt.wantQuiet {
				t.Fatalf("expected quiet=%v, got %v", tt.wantQuiet, gotQuiet)
			}
			if strings.Join(gotArgs, ",") != strings.Join(tt.wantArgs, ",") {
				t.Fatalf("expected args %v, got %v", tt.wantArgs, gotArgs)
			}
		})
	}
}

func writeClientLaunchDiagnosticRepo(t *testing.T, quiet bool) string {
	t.Helper()
	root := t.TempDir()
	writeTestRepo(t, root)
	paths := config.DefaultPaths(root)
	configToml := `
[approvals]
mode = "yolo"

[agents.antigravity]
enabled = true

[agents.claude]
enabled = true

[agents.claude_vscode]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = true

[agents.copilot_cli]
enabled = true
`
	if quiet {
		configToml += "\n[warnings]\nnoise_mode = \"quiet\"\n"
	}
	if err := os.WriteFile(paths.ConfigPath, []byte(configToml), 0o600); err != nil {
		t.Fatalf("write diagnostic config fixture: %v", err)
	}

	binDir := t.TempDir()
	testutil.WriteStub(t, binDir, "al")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return root
}

func captureProcessStderr(t *testing.T, fn func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	original := os.Stderr
	os.Stderr = writer
	defer func() {
		os.Stderr = original
		_ = reader.Close()
		_ = writer.Close()
	}()

	fn()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	os.Stderr = original
	captured, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read process stderr: %v", err)
	}
	return string(captured)
}
