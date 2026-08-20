package benchmark

import "fmt"

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

func dispatchTargetConfigured(target TreatmentDispatchTarget) bool {
	return target.Agent != "" && target.Model != "" && target.ReasoningEffort != ""
}

func expectedDispatchSlots(roles []string, config TreatmentDispatchConfig) ([]TreatmentDispatchTarget, error) {
	var slots []TreatmentDispatchTarget
	for _, role := range roles {
		switch role {
		case requiredRolePlanReviewer:
			if len(config.PlanReviewers) == 0 {
				return nil, fmt.Errorf("required dispatch role %q has no configured plan-reviewer target", role)
			}
			for _, target := range config.PlanReviewers {
				if !dispatchTargetConfigured(target) {
					return nil, fmt.Errorf("required dispatch role %q has no configured plan-reviewer target", role)
				}
				slots = append(slots, target)
			}
		case requiredRoleImplementer:
			if !dispatchTargetConfigured(config.Implementer) {
				return nil, fmt.Errorf("required dispatch role %q has no configured implementer target", role)
			}
			slots = append(slots, config.Implementer)
		case requiredRoleCodeReviewer:
			if !dispatchTargetConfigured(config.CodeReviewer) {
				return nil, fmt.Errorf("required dispatch role %q has no configured code-reviewer target", role)
			}
			slots = append(slots, config.CodeReviewer)
		default:
			return nil, fmt.Errorf("unsupported required dispatch role %q", role)
		}
	}
	return slots, nil
}
