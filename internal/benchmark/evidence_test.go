package benchmark

import (
	"testing"
	"time"
)

func floatPointer(value float64) *float64 {
	return &value
}

// validAttemptResult returns a successful provider-usage attempt whose score,
// duration, and component costs all reconcile.
func validAttemptResult() AttemptResult {
	return AttemptResult{
		SchemaVersion:         StorageSchemaVersion,
		EventID:               "0123456789abcdef0123456789abcdef",
		Attempt:               1,
		Task:                  "example-benchmark-task",
		Status:                statusSuccess,
		F2PPassed:             3,
		F2PTotal:              4,
		F2PScore:              .75,
		CostUSD:               floatPointer(1.5),
		CostMinUSD:            floatPointer(1.4),
		CostMaxUSD:            floatPointer(1.6),
		CostKind:              costKindProviderTotal,
		DurationSeconds:       floatPointer(12),
		TaskChecksum:          "checksum",
		StartedAt:             time.Date(2026, 7, 27, 15, 0, 0, 0, time.UTC),
		FinishedAt:            time.Date(2026, 7, 27, 15, 5, 0, 0, time.UTC),
		Provider:              "openai",
		PublishedModel:        "gpt-5-6-luna",
		RuntimeModel:          "gpt-5-6-luna-2026-07-01",
		ReasoningEffort:       effortLow,
		ProviderClientVersion: "1.2.3",
		DispatchConformant:    true,
		InvocationCount:       4,
		CoordinatorCostUSD:    floatPointer(.5),
		CoordinatorCostMinUSD: floatPointer(.4),
		CoordinatorCostMaxUSD: floatPointer(.6),
		ChildCostUSD:          floatPointer(1),
		ChildCostMinUSD:       floatPointer(1),
		ChildCostMaxUSD:       floatPointer(1),
	}
}

func TestValidAttemptResultIsAcceptedAsEvidence(t *testing.T) {
	if err := validAttemptResult().Validate(); err != nil {
		t.Fatalf("complete attempt evidence was rejected: %v", err)
	}
	failed := validAttemptResult()
	failed.Status = statusFailed
	failed.Error = "provider returned a non-zero exit status"
	if err := failed.Validate(); err != nil {
		t.Fatalf("failed attempt evidence with a recorded error was rejected: %v", err)
	}
}

// TestAttemptResultRejectsIncompleteEvidence covers the boundary that keeps a
// benchmark comparison honest: an attempt is written into the immutable store
// and later averaged into a published verdict, so evidence that is incomplete,
// self-contradictory, or silently missing a cost component must never be
// accepted. Each case breaks one requirement a real provider run satisfies.
func TestAttemptResultRejectsIncompleteEvidence(t *testing.T) {
	tests := []struct {
		name   string
		broken func(*AttemptResult)
	}{
		{"stored under a different schema version", func(r *AttemptResult) {
			r.SchemaVersion = "benchmark-store-v0"
		}},
		{"no event identity", func(r *AttemptResult) { r.EventID = "" }},
		{"attempt is not numbered from one", func(r *AttemptResult) { r.Attempt = 0 }},
		{"task name is not a catalog identifier", func(r *AttemptResult) { r.Task = "Example Task" }},
		{"status is neither success nor failure", func(r *AttemptResult) { r.Status = "cancelled" }},
		{"no task checksum ties the run to its task", func(r *AttemptResult) { r.TaskChecksum = "" }},
		{"runtime model is unrecorded", func(r *AttemptResult) { r.RuntimeModel = "" }},
		{"reasoning effort is not a supported level", func(r *AttemptResult) { r.ReasoningEffort = "turbo" }},
		{"provider client version is unrecorded", func(r *AttemptResult) { r.ProviderClientVersion = "" }},
		{"run has no finish time", func(r *AttemptResult) { r.FinishedAt = time.Time{} }},
		{"failure records no error", func(r *AttemptResult) {
			r.Status = statusFailed
			r.Error = ""
		}},
		{"success also records an error", func(r *AttemptResult) { r.Error = "partially failed" }},
		{"score has no tests to pass", func(r *AttemptResult) { r.F2PTotal = 0 }},
		{"more tests passed than exist", func(r *AttemptResult) { r.F2PPassed = 5 }},
		{"score contradicts the pass counts", func(r *AttemptResult) { r.F2PScore = 1 }},
		{"duration is unrecorded", func(r *AttemptResult) { r.DurationSeconds = nil }},
		{"cost midpoint is unrecorded", func(r *AttemptResult) { r.CostUSD = nil }},
		{"cost range is missing its upper bound", func(r *AttemptResult) { r.CostMaxUSD = nil }},
		{"cost midpoint falls outside its range", func(r *AttemptResult) { r.CostUSD = floatPointer(2) }},
		{"component costs carry no invocation count", func(r *AttemptResult) { r.InvocationCount = 0 }},
		{"child cost component is missing", func(r *AttemptResult) { r.ChildCostUSD = nil }},
		{"coordinator cost falls outside its own range", func(r *AttemptResult) {
			r.CoordinatorCostUSD = floatPointer(.9)
		}},
		{"components do not sum to the reported cost", func(r *AttemptResult) {
			r.ChildCostUSD = floatPointer(.9)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := validAttemptResult()
			test.broken(&result)
			if err := result.Validate(); err == nil {
				t.Fatal("incomplete attempt evidence was accepted into benchmark analysis")
			}
		})
	}
}
