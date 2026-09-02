package benchmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// validAttemptResultFixture returns evidence that satisfies every requirement,
// so a table test can vary exactly one field at a time.
func validAttemptResultFixture() AttemptResult {
	cost, duration := .5, 12.0
	started := time.Now().UTC()
	return AttemptResult{
		SchemaVersion: StorageSchemaVersion, EventID: "event", Attempt: 1,
		Task: "example-task", Status: statusSuccess,
		F2PPassed: 3, F2PTotal: 4, F2PScore: .75,
		CostUSD: &cost, CostKind: costKindProviderReported, DurationSeconds: &duration,
		TaskChecksum: strings.Repeat("a", 64), StartedAt: started,
		FinishedAt: started.Add(time.Minute), Provider: adapterCodex,
		PublishedModel: publishedLuna, RuntimeModel: "openai/gpt-5.6-luna",
		ReasoningEffort: effortLow, ProviderClientVersion: CodexClientVersion,
		InvocationCount: 1,
	}
}

func TestAttemptResultValidationKeepsUnusableEvidenceOutOfAnalysis(t *testing.T) {
	if err := validAttemptResultFixture().Validate(); err != nil {
		t.Fatalf("complete evidence rejected: %v", err)
	}

	negativeDuration := -1.0
	negativeCost := -1.0
	inconsistentMinimum := .9
	inconsistentMaximum := .1
	for _, test := range []struct {
		name   string
		mutate func(*AttemptResult)
		wanted string
	}{
		{"unknown schema", func(r *AttemptResult) { r.SchemaVersion = "benchmark-store-v0" }, "missing required normalized fields"},
		{"no event identity", func(r *AttemptResult) { r.EventID = "" }, "missing required normalized fields"},
		{"invalid task name", func(r *AttemptResult) { r.Task = "../escape" }, "missing required normalized fields"},
		{"unknown status", func(r *AttemptResult) { r.Status = "partial" }, "missing required normalized fields"},
		{"no task checksum", func(r *AttemptResult) { r.TaskChecksum = "" }, "missing required normalized fields"},
		{"unsupported effort", func(r *AttemptResult) { r.ReasoningEffort = "turbo" }, "missing required normalized fields"},
		{"no provider client version", func(r *AttemptResult) { r.ProviderClientVersion = "" }, "missing required normalized fields"},
		{"unfinished", func(r *AttemptResult) { r.FinishedAt = time.Time{} }, "missing required normalized fields"},
		{
			"score contradicts the pass counts",
			func(r *AttemptResult) { r.F2PScore = .9 },
			"incomplete score, cost, or duration evidence",
		},
		{
			"more passes than tests",
			func(r *AttemptResult) { r.F2PPassed = 5 },
			"incomplete score, cost, or duration evidence",
		},
		{
			"no duration",
			func(r *AttemptResult) { r.DurationSeconds = nil },
			"incomplete score, cost, or duration evidence",
		},
		{
			"negative duration",
			func(r *AttemptResult) { r.DurationSeconds = &negativeDuration },
			"incomplete score, cost, or duration evidence",
		},
		{
			"unattributed cost",
			func(r *AttemptResult) { r.CostKind = "" },
			"incomplete score, cost, or duration evidence",
		},
		{
			"success carrying an error",
			func(r *AttemptResult) { r.Error = "timed out" },
			"incomplete score, cost, or duration evidence",
		},
		{
			"negative cost",
			func(r *AttemptResult) { r.CostUSD = &negativeCost },
			"invalid cost evidence",
		},
		{
			"inverted cost range",
			func(r *AttemptResult) {
				r.CostMinUSD, r.CostMaxUSD = &inconsistentMinimum, &inconsistentMaximum
			},
			"invalid cost evidence",
		},
		{
			"failure without a reason",
			func(r *AttemptResult) { r.Status, r.Error = statusFailed, "" },
			"failed attempt result must record an error",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := validAttemptResultFixture()
			test.mutate(&result)
			// Every arm's score and cost is derived from these records, so
			// admitting one inconsistent attempt would silently move a published
			// comparison rather than fail.
			err := result.Validate()
			if err == nil || !strings.Contains(err.Error(), test.wanted) {
				t.Fatalf("%s error = %v", test.name, err)
			}
		})
	}

	t.Run("failure records its reason", func(t *testing.T) {
		result := validAttemptResultFixture()
		result.Status, result.Error = statusFailed, "provider returned no patch"
		// A recorded failure is legitimate evidence; only unexplained failures
		// are not.
		if err := result.Validate(); err != nil {
			t.Fatalf("explained failure rejected: %v", err)
		}
	})

	t.Run("dispatch cost components must reconcile", func(t *testing.T) {
		result := validAttemptResultFixture()
		coordinator, child := .3, .2
		minimum, maximum := .4, .6
		result.CostKind = costKindProviderUsage
		result.CostMinUSD, result.CostMaxUSD = &minimum, &maximum
		result.CoordinatorCostUSD, result.CoordinatorCostMinUSD, result.CoordinatorCostMaxUSD = &coordinator, &coordinator, &coordinator
		result.ChildCostUSD, result.ChildCostMinUSD, result.ChildCostMaxUSD = &child, &child, &child
		// The total is what a report charges to an arm; if it does not equal the
		// coordinator plus child parts, one of the two is wrong and the arm's
		// cost cannot be trusted.
		if err := result.Validate(); err == nil ||
			!strings.Contains(err.Error(), "cost components do not reconcile") {
			t.Fatalf("unreconciled dispatch cost error = %v", err)
		}

		result.InvocationCount = 0
		if err := result.Validate(); err == nil ||
			!strings.Contains(err.Error(), "missing invocation evidence") {
			t.Fatalf("missing invocation error = %v", err)
		}
	})
}

func TestTaskTreeChecksumIdentifiesContentRatherThanLayoutAccidents(t *testing.T) {
	build := func(t *testing.T, files map[string]string) string {
		t.Helper()
		root := t.TempDir()
		for name, content := range files {
			path := filepath.Join(root, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	files := map[string]string{
		"instruction.md":    "solve it\n",
		"tests/test.sh":     "run tests\n",
		"nested/deep/a.txt": "a\n",
	}

	first, err := TaskTreeChecksum(build(t, files))
	if err != nil {
		t.Fatal(err)
	}
	// The checksum is the arm's reuse key: two checkouts of the same pinned task
	// must agree, or previously purchased evidence would be discarded.
	second, err := TaskTreeChecksum(build(t, files))
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) != 64 {
		t.Fatalf("checksum = %q and %q", first, second)
	}

	changedContent := map[string]string{}
	for name, content := range files {
		changedContent[name] = content
	}
	changedContent["tests/test.sh"] = "run different tests\n"
	changed, err := TaskTreeChecksum(build(t, changedContent))
	if err != nil {
		t.Fatal(err)
	}
	if changed == first {
		t.Fatal("changed verifier produced the same task checksum")
	}

	// An empty directory contributes nothing, so adding one must not invalidate
	// an arm that is already paid for.
	withEmptyDirectory := build(t, files)
	if err := os.MkdirAll(filepath.Join(withEmptyDirectory, "empty", "deeper"), 0o700); err != nil {
		t.Fatal(err)
	}
	stable, err := TaskTreeChecksum(withEmptyDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if stable != first {
		t.Fatalf("empty directory changed checksum: %q != %q", stable, first)
	}
}

func TestTaskTreeChecksumRefusesRootsItCannotIdentify(t *testing.T) {
	root := t.TempDir()
	if _, err := TaskTreeChecksum(filepath.Join(root, "absent")); err == nil ||
		!strings.Contains(err.Error(), "inspect task checksum root") {
		t.Fatalf("missing root error = %v", err)
	}

	file := filepath.Join(root, "task.toml")
	if err := os.WriteFile(file, []byte("task"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskTreeChecksum(file); err == nil ||
		!strings.Contains(err.Error(), "must be a directory") {
		t.Fatalf("file root error = %v", err)
	}

	if _, err := TaskTreeChecksum(t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "contains no files") {
		t.Fatalf("empty root error = %v", err)
	}

	// A symlink can point outside the pinned checkout, so its target would not
	// be part of the identity the checksum claims to cover.
	linked := t.TempDir()
	if err := os.WriteFile(filepath.Join(linked, "real.txt"), []byte("real"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, filepath.Join(linked, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := TaskTreeChecksum(linked); err == nil ||
		!strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink error = %v", err)
	}
}
