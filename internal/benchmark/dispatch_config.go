package benchmark

import "fmt"

const (
	treatmentDispatchConfigSchema = "agent-layer-benchmark-dispatch-v2"
	dispatchSkillCodeReviewer     = "review-uncommitted-code"
	dispatchSkillImplementer      = "implement-plan"
	dispatchSkillPlanReviewer     = "review-plan"
)

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

type dispatchSlot struct {
	skill  string
	target TreatmentDispatchTarget
}

func dispatchSkillForRole(role string) (string, error) {
	switch role {
	case requiredRolePlanReviewer:
		return dispatchSkillPlanReviewer, nil
	case requiredRoleImplementer:
		return dispatchSkillImplementer, nil
	case requiredRoleCodeReviewer:
		return dispatchSkillCodeReviewer, nil
	default:
		return "", fmt.Errorf("unsupported required dispatch role %q", role)
	}
}

func expectedDispatchSlots(roles []string, config TreatmentDispatchConfig) ([]dispatchSlot, error) {
	var slots []dispatchSlot
	for _, role := range roles {
		skill, err := dispatchSkillForRole(role)
		if err != nil {
			return nil, err
		}
		switch role {
		case requiredRolePlanReviewer:
			if len(config.PlanReviewers) == 0 {
				return nil, fmt.Errorf("required dispatch role %q has no configured plan-reviewer target", role)
			}
			for _, target := range config.PlanReviewers {
				if !dispatchTargetConfigured(target) {
					return nil, fmt.Errorf("required dispatch role %q has no configured plan-reviewer target", role)
				}
				slots = append(slots, dispatchSlot{skill: skill, target: target})
			}
		case requiredRoleImplementer:
			if !dispatchTargetConfigured(config.Implementer) {
				return nil, fmt.Errorf("required dispatch role %q has no configured implementer target", role)
			}
			slots = append(slots, dispatchSlot{skill: skill, target: config.Implementer})
		case requiredRoleCodeReviewer:
			if !dispatchTargetConfigured(config.CodeReviewer) {
				return nil, fmt.Errorf("required dispatch role %q has no configured code-reviewer target", role)
			}
			slots = append(slots, dispatchSlot{skill: skill, target: config.CodeReviewer})
		default:
			return nil, fmt.Errorf("unsupported required dispatch role %q", role)
		}
	}
	return slots, nil
}
