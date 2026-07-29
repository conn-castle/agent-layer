package benchmark

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"time"
)

const (
	// ArmBaseline is a native provider execution without Agent Layer.
	ArmBaseline = "baseline"
	// ArmTreatment is an execution with an immutable Agent Layer bundle.
	ArmTreatment = "treatment"
)

// AttemptResult is the normalized immutable record used by campaign analysis.
// A score of zero is successful evidence; Status is the success indicator.
type AttemptResult struct {
	SchemaVersion         string    `json:"schema_version"`
	EventID               string    `json:"event_id"`
	Attempt               int       `json:"attempt"`
	Task                  string    `json:"task"`
	Status                string    `json:"status"`
	Error                 string    `json:"error,omitempty"`
	F2PPassed             int       `json:"f2p_passed"`
	F2PTotal              int       `json:"f2p_total"`
	F2PScore              float64   `json:"f2p_score"`
	PartialScore          float64   `json:"partial_score"`
	Reward                float64   `json:"reward"`
	CostUSD               *float64  `json:"cost_usd,omitempty"`
	CostMinUSD            *float64  `json:"cost_min_usd,omitempty"`
	CostMaxUSD            *float64  `json:"cost_max_usd,omitempty"`
	CostKind              string    `json:"cost_kind"`
	DurationSeconds       *float64  `json:"duration_seconds,omitempty"`
	TaskChecksum          string    `json:"task_checksum"`
	StartedAt             time.Time `json:"started_at"`
	FinishedAt            time.Time `json:"finished_at"`
	Provider              string    `json:"provider"`
	PublishedModel        string    `json:"published_model"`
	RuntimeModel          string    `json:"runtime_model"`
	ReasoningEffort       string    `json:"reasoning_effort"`
	ProviderClientVersion string    `json:"provider_client_version"`
	DispatchConformant    bool      `json:"dispatch_conformant"`
	PatchBytes            int64     `json:"patch_bytes"`
	VerifierBuildFailed   bool      `json:"verifier_build_failed"`
	BuildErrorExcerpt     string    `json:"build_error_excerpt,omitempty"`
	CoordinatorCostUSD    *float64  `json:"coordinator_cost_usd,omitempty"`
	CoordinatorCostMinUSD *float64  `json:"coordinator_cost_min_usd,omitempty"`
	CoordinatorCostMaxUSD *float64  `json:"coordinator_cost_max_usd,omitempty"`
	ChildCostUSD          *float64  `json:"child_cost_usd,omitempty"`
	ChildCostMinUSD       *float64  `json:"child_cost_min_usd,omitempty"`
	ChildCostMaxUSD       *float64  `json:"child_cost_max_usd,omitempty"`
	InvocationCount       int       `json:"invocation_count"`
}

// Validate prevents malformed or incomplete evidence from entering analysis.
func (result AttemptResult) Validate() error {
	if result.SchemaVersion != StorageSchemaVersion || result.EventID == "" ||
		result.Attempt < 1 || !validTaskName(result.Task) ||
		(result.Status != statusSuccess && result.Status != statusFailed) ||
		result.TaskChecksum == "" || result.Provider == "" ||
		result.PublishedModel == "" || result.RuntimeModel == "" ||
		!containsString(supportedEfforts, result.ReasoningEffort) ||
		result.ProviderClientVersion == "" || result.StartedAt.IsZero() ||
		result.FinishedAt.IsZero() {
		return fmt.Errorf("attempt result is missing required normalized fields")
	}
	if result.Status == statusFailed {
		if result.Error == "" {
			return fmt.Errorf("failed attempt result must record an error")
		}
		return nil
	}
	if result.Error != "" || result.F2PTotal <= 0 || result.F2PPassed < 0 ||
		result.F2PPassed > result.F2PTotal ||
		math.Abs(result.F2PScore-float64(result.F2PPassed)/float64(result.F2PTotal)) > 1e-12 ||
		result.DurationSeconds == nil || *result.DurationSeconds < 0 ||
		result.CostKind == "" {
		return fmt.Errorf("successful attempt result has incomplete score, cost, or duration evidence")
	}
	if _, _, err := result.CostBounds(); err != nil {
		return fmt.Errorf("successful attempt result has invalid cost evidence: %w", err)
	}
	if result.CostKind == costKindProviderUsage ||
		result.CostKind == costKindProviderUsage+"-range" ||
		result.CostKind == costKindProviderTotal {
		if result.InvocationCount < 1 {
			return fmt.Errorf("provider-usage range is missing invocation evidence")
		}
		for label, values := range map[string][3]*float64{
			"coordinator": {result.CoordinatorCostUSD, result.CoordinatorCostMinUSD, result.CoordinatorCostMaxUSD},
			"child":       {result.ChildCostUSD, result.ChildCostMinUSD, result.ChildCostMaxUSD},
		} {
			if err := validateCostTriple(values[0], values[1], values[2]); err != nil {
				return fmt.Errorf("%s cost evidence is invalid: %w", label, err)
			}
		}
		if math.Abs(*result.CostUSD-(*result.CoordinatorCostUSD+*result.ChildCostUSD)) > 1e-12 ||
			math.Abs(*result.CostMinUSD-(*result.CoordinatorCostMinUSD+*result.ChildCostMinUSD)) > 1e-12 ||
			math.Abs(*result.CostMaxUSD-(*result.CoordinatorCostMaxUSD+*result.ChildCostMaxUSD)) > 1e-12 {
			return fmt.Errorf("provider-usage cost components do not reconcile")
		}
	}
	return nil
}

// CostBounds returns exact cost twice or the explicitly recorded range.
func (result AttemptResult) CostBounds() (float64, float64, error) {
	if result.CostUSD == nil || *result.CostUSD < 0 {
		return 0, 0, fmt.Errorf("cost midpoint is missing")
	}
	if result.CostMinUSD == nil && result.CostMaxUSD == nil {
		return *result.CostUSD, *result.CostUSD, nil
	}
	if result.CostMinUSD == nil || result.CostMaxUSD == nil ||
		*result.CostMinUSD < 0 || *result.CostMaxUSD < *result.CostMinUSD ||
		*result.CostUSD < *result.CostMinUSD || *result.CostUSD > *result.CostMaxUSD {
		return 0, 0, fmt.Errorf("cost range is incomplete or inconsistent")
	}
	return *result.CostMinUSD, *result.CostMaxUSD, nil
}

func validateCostTriple(midpoint, minimum, maximum *float64) error {
	if midpoint == nil || minimum == nil || maximum == nil ||
		*minimum < 0 || *maximum < *minimum ||
		*midpoint < *minimum || *midpoint > *maximum {
		return fmt.Errorf("cost triple is incomplete or inconsistent")
	}
	return nil
}

// NewEventID returns a random, path-safe execution event identity.
func NewEventID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate benchmark event ID: %w", err)
	}
	return hex.EncodeToString(data), nil
}
