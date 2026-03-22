package config

import (
	"testing"
)

func TestIsAgentEnabled_NilPointer(t *testing.T) {
	if IsAgentEnabled(nil) {
		t.Fatal("expected false for nil pointer")
	}
}

func TestIsAgentEnabled_FalsePointer(t *testing.T) {
	v := false
	if IsAgentEnabled(&v) {
		t.Fatal("expected false for false pointer")
	}
}

func TestIsAgentEnabled_TruePointer(t *testing.T) {
	v := true
	if !IsAgentEnabled(&v) {
		t.Fatal("expected true for true pointer")
	}
}
