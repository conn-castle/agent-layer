//go:build tools
// +build tools

package main

import (
	"bytes"
	"math"
	"strings"
	"testing"

	"golang.org/x/tools/cover"
)

func TestSummarizeProfile(t *testing.T) {
	profile := &cover.Profile{
		FileName: "example.go",
		Blocks: []cover.ProfileBlock{
			{StartLine: 1, EndLine: 2, NumStmt: 2, Count: 0},
			{StartLine: 3, EndLine: 3, NumStmt: 1, Count: 1},
		},
	}

	stat := summarizeProfile(profile)
	if stat.totalStmts != 3 {
		t.Fatalf("expected 3 total statements, got %d", stat.totalStmts)
	}
	if stat.coveredStmts != 1 {
		t.Fatalf("expected 1 covered statement, got %d", stat.coveredStmts)
	}
	if stat.linesMissed != 2 {
		t.Fatalf("expected 2 missed lines, got %d", stat.linesMissed)
	}
	if math.Abs(stat.coveragePct-33.3333) > 0.01 {
		t.Fatalf("expected coverage around 33.33, got %.4f", stat.coveragePct)
	}
}

func TestSummarizeTotals(t *testing.T) {
	stats := []fileStats{
		{file: "a.go", totalStmts: 2, coveredStmts: 2, linesMissed: 0},
		{file: "b.go", totalStmts: 3, coveredStmts: 1, linesMissed: 2},
	}

	totalStmts, coveredStmts, linesMissed := summarizeTotals(stats)
	if totalStmts != 5 {
		t.Fatalf("expected 5 total statements, got %d", totalStmts)
	}
	if coveredStmts != 3 {
		t.Fatalf("expected 3 covered statements, got %d", coveredStmts)
	}
	if linesMissed != 2 {
		t.Fatalf("expected 2 missed lines, got %d", linesMissed)
	}
}

func TestWriteSummary(t *testing.T) {
	stats := []fileStats{
		{file: "a.go", totalStmts: 2, coveredStmts: 2, linesMissed: 0},
		{file: "b.go", totalStmts: 2, coveredStmts: 1, linesMissed: 1},
	}

	var buf bytes.Buffer
	if err := writeSummary(&buf, stats); err != nil {
		t.Fatalf("writeSummary failed: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "total coverage: 75.00%") {
		t.Fatalf("unexpected summary output: %q", got)
	}
}
