package agentdispatch

import (
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
	benchmarkPolicyAgentEnv  = "AL_BENCHMARK_DISPATCH_AGENT"
	benchmarkPolicyModelEnv  = "AL_BENCHMARK_DISPATCH_MODEL"
	benchmarkPolicyEffortEnv = "AL_BENCHMARK_DISPATCH_REASONING_EFFORT"
)

// benchmarkPolicy is the locked execution target for benchmark dispatches. A
// zero value means no policy is in force and dispatch behaves normally.
type benchmarkPolicy struct {
	active bool
	agent  string
	model  string
	effort string
}

// loadBenchmarkPolicy reads the locked execution target from the environment.
// The three variables are one unit: a partially exported policy is a benchmark
// installation defect and must fail loudly rather than silently permitting
// arbitrary model overrides.
func loadBenchmarkPolicy(env []string) (benchmarkPolicy, error) {
	agent, hasAgent := clients.GetEnv(env, benchmarkPolicyAgentEnv)
	model, hasModel := clients.GetEnv(env, benchmarkPolicyModelEnv)
	effort, hasEffort := clients.GetEnv(env, benchmarkPolicyEffortEnv)
	agent, model, effort = strings.TrimSpace(agent), strings.TrimSpace(model), strings.TrimSpace(effort)
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
		return benchmarkPolicy{active: true, agent: agent, model: model, effort: effort}, nil
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
	options.Agents = []AgentOption{{
		Agent:     p.agent,
		Available: true,
		Model: FieldOption{
			OverrideSupported: true,
			Configured:        p.model,
			Suggestions:       []string{p.model},
		},
		ReasoningEffort: FieldOption{
			OverrideSupported: true,
			Configured:        p.effort,
			Suggestions:       []string{p.effort},
		},
	}}
}

// constrainStart rejects any selection that departs from the locked execution
// target and injects the locked model and effort when they were omitted, so a
// benchmark dispatch records the exact treatment identity.
func (p benchmarkPolicy) constrainStart(agent *string, model *string, effort *string) error {
	if !p.active {
		return nil
	}
	if *agent != p.agent {
		return fmt.Errorf("benchmark dispatch constraint: agent must be %q, got %q", p.agent, *agent)
	}
	if *model != "" && *model != p.model {
		return fmt.Errorf("benchmark dispatch constraint: model must be %q, got %q", p.model, *model)
	}
	if *effort != "" && *effort != p.effort {
		return fmt.Errorf("benchmark dispatch constraint: reasoning_effort must be %q, got %q", p.effort, *effort)
	}
	*model = p.model
	*effort = p.effort
	return nil
}
