package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestMcpPromptsDeprecated(t *testing.T) {
	cmd := newMcpPromptsCmd()

	var stderr bytes.Buffer
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !strings.Contains(stderr.String(), "deprecated") {
		t.Fatalf("expected deprecation message on stderr, got %q", stderr.String())
	}
}
