package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPromptYesNo_DefaultNoOnEmptyLine(t *testing.T) {
	in := strings.NewReader("\n")
	var out bytes.Buffer

	got, err := promptYesNo(in, &out, "Continue?", false)
	if err != nil {
		t.Fatalf("promptYesNo error: %v", err)
	}
	if got {
		t.Fatal("expected default no on empty response")
	}
	if !strings.Contains(out.String(), "[y/N]:") {
		t.Fatalf("expected [y/N] prompt, got %q", out.String())
	}
}

func TestPromptYesNo_EmptyEOFReturnsFalse(t *testing.T) {
	in := strings.NewReader("")
	var out bytes.Buffer

	got, err := promptYesNo(in, &out, "Continue?", true)
	if err != nil {
		t.Fatalf("promptYesNo error: %v", err)
	}
	if got {
		t.Fatal("expected false on EOF with no response")
	}
}

func TestPromptYesNo_InvalidResponseEOFReturnsError(t *testing.T) {
	in := strings.NewReader("maybe")
	var out bytes.Buffer

	_, err := promptYesNo(in, &out, "Continue?", true)
	if err == nil {
		t.Fatal("expected error for invalid response at EOF")
	}
	if !strings.Contains(err.Error(), "invalid response") {
		t.Fatalf("expected invalid response error, got %v", err)
	}
}

func TestPromptYesNo_InvalidThenNo(t *testing.T) {
	in := strings.NewReader("maybe\nn\n")
	var out bytes.Buffer

	got, err := promptYesNo(in, &out, "Continue?", true)
	if err != nil {
		t.Fatalf("promptYesNo error: %v", err)
	}
	if got {
		t.Fatal("expected no after responding n")
	}
	if !strings.Contains(out.String(), "Please enter y or n.") {
		t.Fatalf("expected invalid-response hint, got %q", out.String())
	}
}
