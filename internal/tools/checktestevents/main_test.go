//go:build tools
// +build tools

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEvents(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "go-test.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunAcceptsCompletePackages(t *testing.T) {
	path := writeEvents(t,
		`{"Action":"start","Package":"example/ok"}`,
		`{"Action":"run","Package":"example/ok","Test":"TestA"}`,
		`{"Action":"pass","Package":"example/ok","Test":"TestA"}`,
		`{"Action":"pass","Package":"example/ok","Elapsed":0.1}`,
		`{"Action":"start","Package":"example/empty"}`,
		`{"Action":"skip","Package":"example/empty","Elapsed":0}`,
	)
	var errOut bytes.Buffer
	if code := run([]string{"checktestevents", path}, &errOut); code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, errOut.String())
	}
}

// A package whose binary exited mid-run has test-level results but no
// package-level result; test-level passes must not count as completion.
func TestRunReportsTruncatedPackages(t *testing.T) {
	path := writeEvents(t,
		`{"Action":"start","Package":"example/truncated"}`,
		`{"Action":"run","Package":"example/truncated","Test":"TestA"}`,
		`{"Action":"pass","Package":"example/truncated","Test":"TestA"}`,
		`{"Action":"start","Package":"example/ok"}`,
		`{"Action":"pass","Package":"example/ok","Elapsed":0.1}`,
		`{"Action":"start","Package":"example/also-truncated"}`,
	)
	var errOut bytes.Buffer
	if code := run([]string{"checktestevents", path}, &errOut); code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	got := errOut.String()
	for _, pkg := range []string{"example/also-truncated", "example/truncated"} {
		if !strings.Contains(got, "  "+pkg+"\n") {
			t.Fatalf("expected %s to be reported, got:\n%s", pkg, got)
		}
	}
	if strings.Contains(got, "example/ok") {
		t.Fatalf("completed package reported as unfinished:\n%s", got)
	}
	if strings.Index(got, "  example/also-truncated\n") > strings.Index(got, "  example/truncated\n") {
		t.Fatalf("expected unfinished packages in sorted order, got:\n%s", got)
	}
}

func TestRunRejectsMalformedEvents(t *testing.T) {
	path := writeEvents(t, `{"Action":"start","Package":"example/ok"}`, `not json`)
	var errOut bytes.Buffer
	if code := run([]string{"checktestevents", path}, &errOut); code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "line 2") {
		t.Fatalf("expected malformed line number in error, got: %s", errOut.String())
	}
}

func TestRunRejectsMissingFile(t *testing.T) {
	var errOut bytes.Buffer
	missing := filepath.Join(t.TempDir(), "missing.jsonl")
	if code := run([]string{"checktestevents", missing}, &errOut); code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
}

type failingReader struct {
	read bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.ErrUnexpectedEOF
	}
	r.read = true
	return copy(p, `{"Action":"start","Package":"example/unfinished"}`+"\n"), nil
}

func TestUnfinishedPackagesReportsReadErrorLine(t *testing.T) {
	_, err := unfinishedPackages(&failingReader{})
	if err == nil || !errors.Is(err, io.ErrUnexpectedEOF) || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("expected line 2 read error, got %v", err)
	}
}

func TestRunRejectsUsageError(t *testing.T) {
	var errOut bytes.Buffer
	if code := run([]string{"checktestevents"}, &errOut); code != 2 {
		t.Fatalf("expected exit 2, got %d", code)
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("expected usage message, got %q", errOut.String())
	}
}
