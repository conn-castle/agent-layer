//go:build tools
// +build tools

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"

	"golang.org/x/tools/cover"

	"github.com/conn-castle/agent-layer/internal/messages"
)

type fileStats struct {
	file         string
	totalStmts   int64
	coveredStmts int64
	linesMissed  int
	coveragePct  float64
}

func main() {
	profilePath := flag.String("profile", "", messages.CoverReportProfileFlagUsage)
	threshold := flag.Float64("threshold", -1, messages.CoverReportThresholdFlagUsage)
	flag.Parse()

	if *profilePath == "" {
		fmt.Fprintln(os.Stderr, messages.CoverReportMissingProfileFlag)
		flag.Usage()
		os.Exit(2)
	}

	profiles, err := cover.ParseProfiles(*profilePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, messages.CoverReportParseFailedFmt, err)
		os.Exit(1)
	}

	stats := buildStats(profiles)
	sortStats(stats)
	if err := writeTable(os.Stdout, stats); err != nil {
		fmt.Fprintf(os.Stderr, messages.CoverReportWriteTableFailedFmt, err)
		os.Exit(1)
	}
	if err := writeSummary(os.Stdout, stats, *threshold); err != nil {
		fmt.Fprintf(os.Stderr, messages.CoverReportWriteSummaryFailedFmt, err)
		os.Exit(1)
	}
}

// buildStats converts coverage profiles into per-file statistics.
// profiles are parsed coverage profiles; returns per-file coverage totals and line misses.
func buildStats(profiles []*cover.Profile) []fileStats {
	stats := make([]fileStats, 0, len(profiles))
	for _, profile := range profiles {
		stats = append(stats, summarizeProfile(profile))
	}
	return stats
}

// summarizeProfile computes coverage metrics for a single file profile.
// profile is the parsed coverage profile; returns aggregated statistics for that file.
func summarizeProfile(profile *cover.Profile) fileStats {
	lineState := make(map[int]int)
	var totalStmts int64
	var coveredStmts int64

	for _, block := range profile.Blocks {
		totalStmts += int64(block.NumStmt)
		if block.Count > 0 {
			coveredStmts += int64(block.NumStmt)
		}
		for line := block.StartLine; line <= block.EndLine; line++ {
			if block.Count > 0 {
				lineState[line] = 1
				continue
			}
			if lineState[line] != 1 {
				lineState[line] = -1
			}
		}
	}

	linesMissed := 0
	for _, state := range lineState {
		if state == -1 {
			linesMissed++
		}
	}

	coveragePct := 100.0
	if totalStmts > 0 {
		coveragePct = 100 * float64(coveredStmts) / float64(totalStmts)
	}

	return fileStats{
		file:         profile.FileName,
		totalStmts:   totalStmts,
		coveredStmts: coveredStmts,
		linesMissed:  linesMissed,
		coveragePct:  coveragePct,
	}
}

// sortStats orders files by ascending coverage, then descending missed lines, then path.
// stats is the list of file statistics to sort in place.
func sortStats(stats []fileStats) {
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].coveragePct != stats[j].coveragePct {
			return stats[i].coveragePct < stats[j].coveragePct
		}
		if stats[i].linesMissed != stats[j].linesMissed {
			return stats[i].linesMissed > stats[j].linesMissed
		}
		return stats[i].file < stats[j].file
	})
}

// writeTable writes the coverage summary table to the provided writer.
// out is the destination for the report; stats are the per-file coverage results.
func writeTable(out io.Writer, stats []fileStats) error {
	writer := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, messages.CoverReportTableHeader); err != nil {
		return err
	}
	for _, stat := range stats {
		if _, err := fmt.Fprintf(writer, messages.CoverReportTableRowFmt, stat.file, stat.coveragePct, stat.linesMissed); err != nil {
			return err
		}
	}
	return writer.Flush()
}

// summarizeTotals aggregates totals across all file stats.
// stats are per-file coverage results; returns total statements, covered statements, and missed lines.
func summarizeTotals(stats []fileStats) (int64, int64, int) {
	var totalStmts int64
	var coveredStmts int64
	linesMissed := 0
	for _, stat := range stats {
		totalStmts += stat.totalStmts
		coveredStmts += stat.coveredStmts
		linesMissed += stat.linesMissed
	}
	return totalStmts, coveredStmts, linesMissed
}

// totalCoverage calculates a total coverage percentage from statement counts.
// totalStmts/coveredStmts are statement totals; returns coverage percentage in [0,100].
func totalCoverage(totalStmts int64, coveredStmts int64) float64 {
	if totalStmts == 0 {
		return 100.0
	}
	return 100 * float64(coveredStmts) / float64(totalStmts)
}

// writeSummary writes a total coverage line and optional threshold status.
// out is the destination for the summary; stats are per-file results; threshold enables pass/fail output.
func writeSummary(out io.Writer, stats []fileStats, threshold float64) error {
	totalStmts, coveredStmts, _ := summarizeTotals(stats)
	total := totalCoverage(totalStmts, coveredStmts)

	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}

	if threshold >= 0 {
		status := messages.CoverReportStatusPass
		if total < threshold {
			status = messages.CoverReportStatusFail
		}
		_, err := fmt.Fprintf(out, messages.CoverReportTotalWithThresholdFmt, total, threshold, status)
		return err
	}

	_, err := fmt.Fprintf(out, messages.CoverReportTotalFmt, total)
	return err
}
