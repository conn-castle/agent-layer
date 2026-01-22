package main

import (
	"bytes"
	"errors"
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

// errorWriter fails after a configurable number of writes
type errorWriter struct {
	failAfter int
	writes    int
}

func (e *errorWriter) Write(p []byte) (int, error) {
	e.writes++
	if e.writes > e.failAfter {
		return 0, errors.New("write failed")
	}
	return len(p), nil
}

func TestPromptYesNo_PromptWriteError_DefaultYes(t *testing.T) {
	in := strings.NewReader("y\n")
	out := &errorWriter{failAfter: 0}

	_, err := promptYesNo(in, out, "Continue?", true)
	if err == nil {
		t.Fatal("expected error when prompt write fails")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestPromptYesNo_PromptWriteError_DefaultNo(t *testing.T) {
	in := strings.NewReader("n\n")
	out := &errorWriter{failAfter: 0}

	_, err := promptYesNo(in, out, "Continue?", false)
	if err == nil {
		t.Fatal("expected error when prompt write fails")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestPromptYesNo_HintWriteError(t *testing.T) {
	in := strings.NewReader("maybe\nn\n")
	out := &errorWriter{failAfter: 1} // Allow first prompt, fail on hint

	_, err := promptYesNo(in, out, "Continue?", true)
	if err == nil {
		t.Fatal("expected error when hint write fails")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected write error, got %v", err)
	}
}

// errorReader returns an error on read
type errorReader struct{}

func (errorReader) Read(p []byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestPromptYesNo_ReadError(t *testing.T) {
	in := errorReader{}
	var out bytes.Buffer

	_, err := promptYesNo(in, &out, "Continue?", true)
	if err == nil {
		t.Fatal("expected error on read failure")
	}
	if !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("expected read error, got %v", err)
	}
}
