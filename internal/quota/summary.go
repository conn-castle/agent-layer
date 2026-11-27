package quota

import (
	"slices"
	"time"
)

// SummaryResponse presents the quota results with less detail for agent callers.
type SummaryResponse struct {
	SchemaVersion int              `json:"schema_version"`
	Format        string           `json:"format"`
	Status        string           `json:"status"`
	Missing       []MissingReading `json:"missing_readings"`
	Results       []summaryResult  `json:"results"`
}

type summaryResult struct {
	Selector     string     `json:"selector"`
	Agent        string     `json:"agent"`
	Status       string     `json:"status"`
	Reason       string     `json:"reason,omitempty"`
	ObservedAt   *time.Time `json:"observed_at"`
	Reauth       *bool      `json:"reauth_required"`
	Retryable    bool       `json:"retryable"`
	Retries      int        `json:"retries,omitempty"`
	RetryAt      *time.Time `json:"retry_at,omitempty"`
	AccountCheck string     `json:"account_check,omitempty"`
	Admission
	BlockReason  string         `json:"block_reason,omitempty"`
	HighestUsage *summaryWindow `json:"highest_usage,omitempty"`
	Models       []summaryModel `json:"models,omitempty"`
}

type summaryWindow struct {
	Pool        string     `json:"pool"`
	Scope       string     `json:"scope"`
	Window      string     `json:"window"`
	UsedPercent float64    `json:"used_percent"`
	ResetsAt    *time.Time `json:"resets_at"`
}

type summaryModel struct {
	ID         string `json:"id"`
	Coverage   string `json:"coverage"`
	AuthStatus string `json:"auth_status,omitempty"`
	Admission
}

// Summary presents the highest observed utilization and admission for models
// configured or requested by the caller. It uses the same underlying readings.
func (r Response) Summary(sources []Source) SummaryResponse {
	report := SummaryResponse{
		SchemaVersion: r.SchemaVersion, Format: "summary", Status: r.Status,
		Missing: r.Missing, Results: make([]summaryResult, 0, len(r.Results)),
	}
	selected := make(map[string]Source, len(sources))
	for _, source := range sources {
		selected[source.Selector] = source
	}
	for _, result := range r.Results {
		row := summaryResult{
			Selector: result.Selector, Agent: result.Agent, Status: result.Status, Reason: result.Reason,
			ObservedAt: result.ObservedAt, Reauth: result.Reauth, Retryable: result.Retryable,
			Retries: result.Retries, RetryAt: result.RetryAt,
		}
		if result.AccountCheck != "not_requested" {
			row.AccountCheck = result.AccountCheck
		}
		if result.Status != statusMismatch {
			row.Admission, row.BlockReason = result.Admission, result.BlockReason
			row.Models = summarizeModels(result.Models, selected[result.Selector])
		}
		if result.Status == statusOK {
			row.HighestUsage = summarizeUsage(result.Pools)
		}
		report.Results = append(report.Results, row)
	}
	return report
}

func summarizeModels(models []Model, source Source) []summaryModel {
	var result []summaryModel
	for _, model := range models {
		if model.ID != "" && (model.ID == source.ConfiguredModel || slices.Contains(source.Models, model.ID)) {
			result = append(result, summaryModel{
				ID: model.ID, Coverage: model.Coverage, AuthStatus: model.AuthStatus, Admission: model.Admission,
			})
		}
	}
	return result
}

func summarizeUsage(pools []Pool) *summaryWindow {
	var highest *summaryWindow
	for _, pool := range pools {
		if pool.Scope == scopeOther {
			continue
		}
		for _, window := range pool.Windows {
			if window.UsedPercent == nil || window.Disabled != nil && *window.Disabled ||
				highest != nil && *window.UsedPercent <= highest.UsedPercent {
				continue
			}
			highest = &summaryWindow{
				Pool: pool.ID, Scope: pool.Scope, Window: window.ID,
				UsedPercent: *window.UsedPercent, ResetsAt: window.ResetsAt,
			}
		}
	}
	return highest
}
