package doctor

// Status represents the health status of a check (OK, WARN, FAIL).
type Status string

const (
	// StatusOK indicates the check passed.
	StatusOK Status = "OK"
	// StatusWarn indicates a potential issue that doesn't block functionality.
	StatusWarn Status = "WARN"
	// StatusFail indicates a critical issue that must be resolved.
	StatusFail Status = "FAIL"
)

// Result holds the outcome of a single diagnostic check.
type Result struct {
	Status         Status
	CheckName      string
	Message        string
	Recommendation string
}

// HasFailResultForCheck reports whether results contains a StatusFail result for
// checkName. It is used to detect when CheckConfig's lenient fallback already
// emitted a blocking result for a check (Secrets/Skills) so the orchestrator can
// skip the corresponding standalone check and avoid a contradictory cascade.
// Only StatusFail results suppress the standalone check; an OK or WARN result
// for the same check name does not, so non-blocking diagnostics never silence a
// check that could still report meaningful results.
func HasFailResultForCheck(results []Result, checkName string) bool {
	for _, r := range results {
		if r.Status == StatusFail && r.CheckName == checkName {
			return true
		}
	}
	return false
}
