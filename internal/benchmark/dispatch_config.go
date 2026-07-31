package benchmark

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const treatmentDispatchConfigSchema = "agent-layer-benchmark-dispatch-v1"

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
	Fixer         TreatmentDispatchTarget   `toml:"fixer" json:"fixer"`
}

func defaultTreatmentDispatchConfig(model Model, effort string) TreatmentDispatchConfig {
	target := TreatmentDispatchTarget{
		Agent: dispatchAgent(model), Model: dispatchModel(model), ReasoningEffort: effort,
	}
	return TreatmentDispatchConfig{
		Schema: treatmentDispatchConfigSchema, PlanReviewers: []TreatmentDispatchTarget{target},
		Implementer: target, CodeReviewer: target, Fixer: target,
	}
}

func loadTreatmentDispatchConfig(path string, coordinator Model) (TreatmentDispatchConfig, error) {
	if strings.TrimSpace(path) == "" {
		return TreatmentDispatchConfig{}, fmt.Errorf("benchmark treatment dispatch config path is empty")
	}
	data, err := os.ReadFile(path) // #nosec G304 -- the user explicitly selects the configuration artifact.
	if err != nil {
		return TreatmentDispatchConfig{}, fmt.Errorf("read benchmark treatment dispatch config: %w", err)
	}
	var config TreatmentDispatchConfig
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return TreatmentDispatchConfig{}, fmt.Errorf("decode benchmark treatment dispatch config: %w", err)
	}
	if err := validateTreatmentDispatchConfig(config, coordinator); err != nil {
		return TreatmentDispatchConfig{}, err
	}
	return config, nil
}

func validateTreatmentDispatchConfig(config TreatmentDispatchConfig, coordinator Model) error {
	if config.Schema != treatmentDispatchConfigSchema || len(config.PlanReviewers) == 0 {
		return fmt.Errorf("benchmark treatment dispatch config has an invalid schema or no plan reviewers")
	}
	targets := append([]TreatmentDispatchTarget(nil), config.PlanReviewers...)
	targets = append(targets, config.Implementer, config.CodeReviewer, config.Fixer)
	for _, target := range targets {
		model, effort, err := modelForDispatchTarget(target)
		if err != nil {
			return err
		}
		if model.Adapter != coordinator.Adapter || effort != target.ReasoningEffort {
			return fmt.Errorf("benchmark dispatch target %s/%s/%s must use the coordinator provider %s", target.Agent, target.Model, target.ReasoningEffort, dispatchAgent(coordinator))
		}
	}
	return nil
}

func modelForDispatchTarget(target TreatmentDispatchTarget) (Model, string, error) {
	for _, model := range supportedModels {
		if dispatchAgent(model) != target.Agent || dispatchModel(model) != target.Model {
			continue
		}
		parsed, effort, err := ParseModelSelection(model.Name + ":" + target.ReasoningEffort)
		if err != nil || parsed != model {
			return Model{}, "", fmt.Errorf("invalid benchmark dispatch target %s/%s/%s", target.Agent, target.Model, target.ReasoningEffort)
		}
		return model, effort, nil
	}
	return Model{}, "", fmt.Errorf("unsupported benchmark dispatch target %s/%s/%s", target.Agent, target.Model, target.ReasoningEffort)
}

func treatmentDispatchConfigJSON(config TreatmentDispatchConfig) ([]byte, error) {
	data, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("encode benchmark treatment dispatch config: %w", err)
	}
	return append(data, '\n'), nil
}
