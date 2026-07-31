package benchmark

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTreatmentDispatchConfigPreservesExactRoleTargets(t *testing.T) {
	model, _, err := ParseModelSelection("sol:medium")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "dispatch.toml")
	data := []byte(`schema = "agent-layer-benchmark-dispatch-v1"
[[plan_reviewers]]
agent = "codex"
model = "gpt-5.6-terra"
reasoning_effort = "high"
[implementer]
agent = "codex"
model = "gpt-5.6-luna"
reasoning_effort = "high"
[code_reviewer]
agent = "codex"
model = "gpt-5.6-terra"
reasoning_effort = "high"
[fixer]
agent = "codex"
model = "gpt-5.6-luna"
reasoning_effort = "high"
`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := loadTreatmentDispatchConfig(path, model)
	if err != nil {
		t.Fatal(err)
	}
	if len(config.PlanReviewers) != 1 || config.PlanReviewers[0].Model != "gpt-5.6-terra" ||
		config.Implementer.Model != "gpt-5.6-luna" || config.CodeReviewer.Model != "gpt-5.6-terra" ||
		config.Fixer.Model != "gpt-5.6-luna" {
		t.Fatalf("loaded role targets = %#v", config)
	}
}

func TestTreatmentDispatchConfigRejectsAProviderUnavailableToCoordinator(t *testing.T) {
	model, _, err := ParseModelSelection("sol:medium")
	if err != nil {
		t.Fatal(err)
	}
	config := defaultTreatmentDispatchConfig(model, "medium")
	config.Fixer = TreatmentDispatchTarget{Agent: "claude", Model: "claude-fable-5", ReasoningEffort: "high"}
	if err := validateTreatmentDispatchConfig(config, model); err == nil {
		t.Fatal("cross-provider role target was accepted without its credentials")
	}
}
