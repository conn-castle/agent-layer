package agentdispatch

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/conn-castle/agent-layer/internal/clients"
)

// Benchmark containers pin every dispatch to one immutable execution target so
// treatment arms stay comparable. The root-owned CLI gate enforces that policy
// for `al dispatch options` and `al dispatch start`; it exports the same values
// to the MCP server through these variables, because an MCP start never passes
// through the gate's argument inspection.
const (
	benchmarkPolicyAgentEnv   = "AL_BENCHMARK_DISPATCH_AGENT"
	benchmarkPolicyModelEnv   = "AL_BENCHMARK_DISPATCH_MODEL"
	benchmarkPolicyEffortEnv  = "AL_BENCHMARK_DISPATCH_REASONING_EFFORT"
	benchmarkPolicyTargetsEnv = "AL_BENCHMARK_DISPATCH_TARGETS"
)

// benchmarkPolicy is the locked execution target for benchmark dispatches. A
// zero value means no policy is in force and dispatch behaves normally.
type benchmarkPolicy struct {
	active  bool
	targets []benchmarkPolicyTarget
}

type benchmarkPolicyTarget struct {
	Agent  string `json:"agent"`
	Model  string `json:"model"`
	Effort string `json:"reasoning_effort"`
}

// loadBenchmarkPolicy reads the locked execution target from the environment.
// The three variables are one unit: a partially exported policy is a benchmark
// installation defect and must fail loudly rather than silently permitting
// arbitrary model overrides.
func loadBenchmarkPolicy(env []string) (benchmarkPolicy, error) {
	encodedTargets, hasTargets := clients.GetEnv(env, benchmarkPolicyTargetsEnv)
	agent, hasAgent := clients.GetEnv(env, benchmarkPolicyAgentEnv)
	model, hasModel := clients.GetEnv(env, benchmarkPolicyModelEnv)
	effort, hasEffort := clients.GetEnv(env, benchmarkPolicyEffortEnv)
	agent, model, effort = strings.TrimSpace(agent), strings.TrimSpace(model), strings.TrimSpace(effort)
	encodedTargets = strings.TrimSpace(encodedTargets)
	if hasTargets && encodedTargets != "" {
		if (hasAgent && agent != "") || (hasModel && model != "") || (hasEffort && effort != "") {
			return benchmarkPolicy{}, exitError(ExitConfig, "benchmark dispatch policy mixes target-list and legacy variables")
		}
		var targets []benchmarkPolicyTarget
		if err := json.Unmarshal([]byte(encodedTargets), &targets); err != nil || len(targets) == 0 {
			return benchmarkPolicy{}, exitError(ExitConfig, "benchmark dispatch target policy must be a non-empty JSON array")
		}
		seen := make(map[benchmarkPolicyTarget]bool, len(targets))
		for _, target := range targets {
			target.Agent = strings.TrimSpace(target.Agent)
			target.Model = strings.TrimSpace(target.Model)
			target.Effort = strings.TrimSpace(target.Effort)
			if target.Agent == "" || target.Model == "" || target.Effort == "" || seen[target] {
				return benchmarkPolicy{}, exitError(ExitConfig, "benchmark dispatch target policy contains an incomplete or duplicate target")
			}
			seen[target] = true
		}
		return benchmarkPolicy{active: true, targets: targets}, nil
	}
	present := 0
	for _, set := range []bool{hasAgent && agent != "", hasModel && model != "", hasEffort && effort != ""} {
		if set {
			present++
		}
	}
	switch present {
	case 0:
		return benchmarkPolicy{}, nil
	case 3:
		return benchmarkPolicy{active: true, targets: []benchmarkPolicyTarget{{Agent: agent, Model: model, Effort: effort}}}, nil
	default:
		return benchmarkPolicy{}, exitError(ExitConfig, fmt.Sprintf(
			"benchmark dispatch policy is incomplete: %s, %s, and %s must all be set",
			benchmarkPolicyAgentEnv, benchmarkPolicyModelEnv, benchmarkPolicyEffortEnv))
	}
}

// constrainOptions narrows a live discovery response to the locked execution
// target so the coordinator can only select what the benchmark permits.
func (p benchmarkPolicy) constrainOptions(options *OptionsResponse) {
	if !p.active || options == nil {
		return
	}
	var agents []string
	for _, target := range p.targets {
		if !containsPolicyValue(agents, target.Agent) {
			agents = append(agents, target.Agent)
		}
	}
	options.Agents = make([]AgentOption, 0, len(agents))
	for _, agent := range agents {
		var models, efforts []string
		for _, target := range p.targets {
			if target.Agent != agent {
				continue
			}
			if !containsPolicyValue(models, target.Model) {
				models = append(models, target.Model)
			}
			if !containsPolicyValue(efforts, target.Effort) {
				efforts = append(efforts, target.Effort)
			}
		}
		options.Agents = append(options.Agents, AgentOption{
			Agent: agent, Available: true,
			Model:           FieldOption{OverrideSupported: true, Configured: models[0], Suggestions: models},
			ReasoningEffort: FieldOption{OverrideSupported: true, Configured: efforts[0], Suggestions: efforts},
		})
	}
}

// constrainStart rejects any selection that departs from the locked execution
// target and injects the locked model and effort when they were omitted, so a
// benchmark dispatch records the exact treatment identity.
func (p benchmarkPolicy) constrainStart(agent *string, model *string, effort *string) error {
	if !p.active {
		return nil
	}
	var matches []benchmarkPolicyTarget
	for _, target := range p.targets {
		if target.Agent == *agent && (*model == "" || target.Model == *model) && (*effort == "" || target.Effort == *effort) {
			matches = append(matches, target)
		}
	}
	if len(matches) != 1 {
		return fmt.Errorf("benchmark dispatch constraint: selection must resolve to exactly one configured target")
	}
	*model = matches[0].Model
	*effort = matches[0].Effort
	return nil
}

func containsPolicyValue(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
