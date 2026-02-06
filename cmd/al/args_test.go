package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestClientArgsPassThrough(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "claude-args.txt")
	writeArgsStub(t, binDir, "claude", argsFile)
	writeStub(t, binDir, "al")

	t.Setenv("PATH", binDir)

	withWorkingDir(t, root, func() {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"claude", "--foo", "bar", "--baz=qux"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute error: %v", err)
		}
	})

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"--foo", "bar", "--baz=qux"}
	if strings.Join(lines, ",") != strings.Join(want, ",") {
		t.Fatalf("expected args %v, got %v", want, lines)
	}
}

func TestClientArgsPassThroughWithSeparator(t *testing.T) {
	root := t.TempDir()
	writeTestRepo(t, root)

	binDir := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "claude-args-sep.txt")
	writeArgsStub(t, binDir, "claude", argsFile)
	writeStub(t, binDir, "al")

	t.Setenv("PATH", binDir)

	withWorkingDir(t, root, func() {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"claude", "--", "--help"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("execute error: %v", err)
		}
	})

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"--help"}
	if strings.Join(lines, ",") != strings.Join(want, ",") {
		t.Fatalf("expected args %v, got %v", want, lines)
	}
}

func TestStripArgsSeparator(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "empty args",
			args: nil,
			want: []string{},
		},
		{
			name: "separator at start",
			args: []string{"--", "--help"},
			want: []string{"--help"},
		},
		{
			name: "separator in middle",
			args: []string{"--foo", "--", "--bar"},
			want: []string{"--foo", "--bar"},
		},
		{
			name: "multiple separators",
			args: []string{"--foo", "--", "--bar", "--", "--baz"},
			want: []string{"--foo", "--bar", "--", "--baz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripArgsSeparator(tt.args)
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func writeArgsStub(t *testing.T, dir string, name string, outputPath string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := []byte(fmt.Sprintf("#!/bin/sh\nprintf '%s\\n' \"$@\" > %s\n", "%s", strconv.Quote(outputPath)))
	if err := os.WriteFile(path, content, 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}
