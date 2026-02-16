package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVSCodeNoSync(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	binDir := t.TempDir()
	writeStub(t, binDir, "code")

	t.Setenv("PATH", binDir)
	if err := runVSCodeNoSync(root, nil); err != nil {
		t.Fatalf("runVSCodeNoSync error: %v", err)
	}
}

func TestRunVSCodeNoSyncDisabled(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	paths := filepath.Join(root, ".agent-layer", "config.toml")
	configToml := `
[approvals]
mode = "all"

[agents.gemini]
enabled = true

[agents.claude]
enabled = true

[agents.codex]
enabled = true

[agents.vscode]
enabled = false

[agents.antigravity]
enabled = true
`
	if err := os.WriteFile(paths, []byte(configToml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := runVSCodeNoSync(root, nil); err == nil {
		t.Fatal("expected error when VS Code is disabled")
	}
}

func TestRunVSCodeNoSyncManagedBlockConflict(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	settingsPath := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatalf("mkdir .vscode: %v", err)
	}
	if err := os.WriteFile(settingsPath, []byte("{\n// >>> agent-layer\n}\n"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	binDir := t.TempDir()
	writeStub(t, binDir, "code")
	t.Setenv("PATH", binDir)

	err := runVSCodeNoSync(root, nil)
	if err == nil {
		t.Fatal("expected managed-block conflict error")
	}
	if !strings.Contains(err.Error(), "managed settings block conflict") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSplitVSCodeArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantNoSync bool
		wantArgs   []string
		wantErr    bool
	}{
		{
			name:       "no-sync before separator",
			args:       []string{"--no-sync", "--", "--reuse-window"},
			wantNoSync: true,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "no-sync bool false",
			args:       []string{"--no-sync=false", "--reuse-window"},
			wantNoSync: false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:    "no-sync invalid value",
			args:    []string{"--no-sync=maybe"},
			wantErr: true,
		},
		{
			name:       "pass-through without separator",
			args:       []string{"--reuse-window"},
			wantNoSync: false,
			wantArgs:   []string{"--reuse-window"},
		},
		{
			name:       "no-sync after separator",
			args:       []string{"--", "--no-sync"},
			wantNoSync: false,
			wantArgs:   []string{"--no-sync"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNoSync, gotArgs, err := splitVSCodeArgs(tt.args)
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
			if strings.Join(gotArgs, ",") != strings.Join(tt.wantArgs, ",") {
				t.Fatalf("expected args %v, got %v", tt.wantArgs, gotArgs)
			}
		})
	}
}
