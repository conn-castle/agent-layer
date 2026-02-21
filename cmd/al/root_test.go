package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestRootVersionFlag(t *testing.T) {
	cmd := newRootCmd()
	cmd.Version = "v1.2.3"
	cmd.SetVersionTemplate("{{.Version}}\\n")
	cmd.SetArgs([]string{"--version"})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if strings.TrimSpace(out.String()) != "v1.2.3" {
		t.Fatalf("unexpected version output: %q", out.String())
	}
}

func TestRootHelp(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{})

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !strings.Contains(out.String(), "Agent Layer") {
		t.Fatalf("expected help output, got %q", out.String())
	}
}

func TestRootVersionFlagWriteError(t *testing.T) {
	cmd := newRootCmd()
	cmd.Version = "v1.2.3"
	cmd.SetArgs([]string{"--version"})
	cmd.SetOut(failingWriter{})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error when output fails")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStubCmd(t *testing.T) {
	cmd := newStubCmd("doctor")
	err := cmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("expected not implemented error, got %v", err)
	}
}
