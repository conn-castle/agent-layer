package sync

import (
	"strings"
	"testing"
)

func TestAppendCodexAgentSpecific_EmptyMapIsNoOp(t *testing.T) {
	var builder strings.Builder
	builder.WriteString("base")

	if err := appendCodexAgentSpecific(&builder, map[string]any{}); err != nil {
		t.Fatalf("appendCodexAgentSpecific error: %v", err)
	}
	if got := builder.String(); got != "base" {
		t.Fatalf("expected unchanged builder, got %q", got)
	}
}

func TestAppendCodexAgentSpecific_AppendsEncodedTOML(t *testing.T) {
	var builder strings.Builder
	builder.WriteString("base")

	if err := appendCodexAgentSpecific(&builder, map[string]any{"extra_flag": true}); err != nil {
		t.Fatalf("appendCodexAgentSpecific error: %v", err)
	}

	got := builder.String()
	if !strings.HasPrefix(got, "base\n") {
		t.Fatalf("expected output to append after a newline, got %q", got)
	}
	if !strings.Contains(got, "extra_flag = true") {
		t.Fatalf("expected encoded TOML to contain key/value, got %q", got)
	}
}

func TestAppendCodexAgentSpecific_EncodeError(t *testing.T) {
	var builder strings.Builder
	err := appendCodexAgentSpecific(&builder, map[string]any{"invalid": make(chan int)})
	if err == nil {
		t.Fatal("expected encode error")
	}
}

func TestEncodeAgentSpecificTOML_Success(t *testing.T) {
	encoded, err := encodeAgentSpecificTOML(map[string]any{"temperature": 0.2})
	if err != nil {
		t.Fatalf("encodeAgentSpecificTOML error: %v", err)
	}
	if !strings.Contains(encoded, "temperature = 0.2") {
		t.Fatalf("expected encoded TOML output, got %q", encoded)
	}
}
