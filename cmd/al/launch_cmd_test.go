package main

import (
	"bytes"
	"io"
	"os"
	"slices"
	"testing"

	"github.com/spf13/cobra"

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
			cmd := newLaunchCmd(launchSpec{
				use: "test", short: "test", agent: "vscode",
				enabled: func(cfg *config.Config) *bool { return cfg.Agents.VSCode.Enabled },
				launch: func(_ *config.ProjectConfig, _ *run.Info, _ []string, args []string) error {
					launchCalls++
					gotArgs = append([]string(nil), args...)
					return nil
				},
				noSync: true,
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
			cmd := newLaunchCmd(launchSpec{
				use: "test", short: "test", agent: "vscode",
				enabled: func(cfg *config.Config) *bool { return cfg.Agents.VSCode.Enabled },
				launch: func(_ *config.ProjectConfig, _ *run.Info, _ []string, args []string) error {
					launchCalls++
					gotArgs = append([]string(nil), args...)
					return nil
				},
				noSync: true,
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

func TestSplitLaunchArgs(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		acceptNoSync bool
		wantNoSync   bool
		wantQuiet    bool
		wantArgs     []string
		wantErr      bool
	}{
		{name: "quiet flag", args: []string{"--quiet", "--foo"}, wantQuiet: true, wantArgs: []string{"--foo"}},
		{name: "quiet shorthand", args: []string{"-q", "--foo"}, wantQuiet: true, wantArgs: []string{"--foo"}},
		{name: "quiet value true", args: []string{"--quiet=true", "--foo"}, wantQuiet: true, wantArgs: []string{"--foo"}},
		{name: "quiet value false consumed", args: []string{"--quiet=false", "--foo"}, wantArgs: []string{"--foo"}},
		{name: "quiet invalid value", args: []string{"--quiet=maybe"}, wantErr: true},
		{name: "quiet after separator", args: []string{"--", "--quiet"}, wantArgs: []string{"--quiet"}},
		{name: "pass-through without separator", args: []string{"--reuse-window"}, wantArgs: []string{"--reuse-window"}},
		{name: "no-sync forwarded when not accepted", args: []string{"--no-sync", "--foo"}, wantArgs: []string{"--no-sync", "--foo"}},
		{name: "no-sync value forwarded when not accepted", args: []string{"--no-sync=maybe"}, wantArgs: []string{"--no-sync=maybe"}},
		{
			name: "no-sync before separator", args: []string{"--no-sync", "--", "--reuse-window"}, acceptNoSync: true,
			wantNoSync: true, wantArgs: []string{"--reuse-window"},
		},
		{
			name: "quiet and no-sync before separator", args: []string{"--quiet", "--no-sync", "--", "--reuse-window"}, acceptNoSync: true,
			wantNoSync: true, wantQuiet: true, wantArgs: []string{"--reuse-window"},
		},
		{name: "no-sync bool false", args: []string{"--no-sync=false", "--reuse-window"}, acceptNoSync: true, wantArgs: []string{"--reuse-window"}},
		{name: "no-sync invalid value", args: []string{"--no-sync=maybe"}, acceptNoSync: true, wantErr: true},
		{name: "no-sync after separator", args: []string{"--", "--no-sync"}, acceptNoSync: true, wantArgs: []string{"--no-sync"}},
		{name: "quiet with no-sync accepted", args: []string{"--quiet=true", "--reuse-window"}, acceptNoSync: true, wantQuiet: true, wantArgs: []string{"--reuse-window"}},
		{name: "quiet invalid value with no-sync accepted", args: []string{"--quiet=maybe"}, acceptNoSync: true, wantErr: true},
		{name: "quiet after separator with no-sync accepted", args: []string{"--", "--quiet"}, acceptNoSync: true, wantArgs: []string{"--quiet"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNoSync, gotQuiet, gotArgs, err := splitLaunchArgs(tt.args, tt.acceptNoSync)
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
			if !slices.Equal(gotArgs, tt.wantArgs) {
				t.Fatalf("expected args %v, got %v", tt.wantArgs, gotArgs)
			}
		})
	}
}

// launchCmdNamed returns the launcher subcommand with the given name.
func launchCmdNamed(t *testing.T, name string) *cobra.Command {
	t.Helper()
	for _, cmd := range newLaunchCmds() {
		if cmd.Name() == name {
			return cmd
		}
	}
	t.Fatalf("no launcher command named %q", name)
	return nil
}

func writeClientLaunchDiagnosticRepo(t *testing.T, quiet bool) string {
	t.Helper()
	root := t.TempDir()
	writeTestRepo(t, root)
	paths := config.DefaultPaths(root)
	configToml := `
[[instructions.local]]
selectors = ["00_rules.md"]
order = 0

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

[agents.grok]
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
	readDone := make(chan struct {
		captured []byte
		err      error
	}, 1)
	go func() {
		captured, readErr := io.ReadAll(reader)
		readDone <- struct {
			captured []byte
			err      error
		}{captured: captured, err: readErr}
	}()

	fn()
	os.Stderr = original
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	result := <-readDone
	if result.err != nil {
		t.Fatalf("read process stderr: %v", result.err)
	}
	return string(result.captured)
}
