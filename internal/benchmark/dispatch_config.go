package benchmark

const treatmentDispatchConfigSchema = "agent-layer-benchmark-dispatch-v2"

// TreatmentDispatchTarget is one exact Agent Dispatch execution identity.
type TreatmentDispatchTarget struct {
	Agent           string `toml:"agent" json:"agent"`
	Model           string `toml:"model" json:"model"`
	ReasoningEffort string `toml:"reasoning_effort" json:"reasoning_effort"`
}

// TreatmentDispatchConfig assigns exact execution targets to workflow roles.
type TreatmentDispatchConfig struct {
	Schema        string                    `toml:"schema" json:"schema"`
	PlanReviewers []TreatmentDispatchTarget `toml:"plan_reviewers" json:"plan_reviewers"`
	Implementer   TreatmentDispatchTarget   `toml:"implementer" json:"implementer"`
	CodeReviewer  TreatmentDispatchTarget   `toml:"code_reviewer" json:"code_reviewer"`
}

func defaultTreatmentDispatchConfig(model Model, effort string) TreatmentDispatchConfig {
	target := TreatmentDispatchTarget{
		Agent: dispatchAgent(model), Model: dispatchModel(model), ReasoningEffort: effort,
	}
	return TreatmentDispatchConfig{
		Schema: treatmentDispatchConfigSchema, PlanReviewers: []TreatmentDispatchTarget{target},
		Implementer: target, CodeReviewer: target,
	}
}
