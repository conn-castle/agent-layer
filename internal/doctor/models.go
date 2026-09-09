package doctor

import (
	"fmt"
	"slices"
	"sync"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/config"
)

// CheckModels queries enabled harnesses with explicit model overrides
// concurrently, without sync. Client defaults need no model lookup. A model
// absent from a picker is not necessarily invalid (aliases and custom endpoints
// remain supported); report it as a warning rather than rejecting config.
// The caller must supply a non-nil, loaded project configuration.
func CheckModels(project *config.ProjectConfig, req agentoptions.DiscoveryRequest) []Result {
	agents := []struct {
		name    string
		enabled *bool
	}{
		{"claude", project.Config.Agents.Claude.Enabled},
		{"codex", project.Config.Agents.Codex.Enabled},
		{"grok", project.Config.Agents.Grok.Enabled},
		{"antigravity", project.Config.Agents.Antigravity.Enabled},
		{"copilot_cli", project.Config.Agents.CopilotCLI.Enabled},
	}
	req.Project = project
	req.Live = true
	results := make([]Result, len(agents))
	var workers sync.WaitGroup
	for i, agent := range agents {
		if agent.enabled == nil || !*agent.enabled || agentoptions.ConfiguredValue(project.Config, agent.name, agentoptions.KindModel) == "" {
			continue
		}
		workers.Go(func() {
			option := agentoptions.Resolve(project.Config, agent.name, agentoptions.KindModel, req)
			result := Result{CheckName: agent.name + " models", Status: StatusOK,
				Message: fmt.Sprintf("Harness lists %d selectable models", len(option.Suggestions))}
			switch {
			case option.DiscoveryError != "":
				result.Status = StatusWarn
				result.Message = option.DiscoveryError
				result.Recommendation = "Check the harness installation, authentication, and connectivity; no model list is available."
			case option.Configured != "" && !slices.Contains(option.Suggestions, option.Configured):
				result.Status = StatusWarn
				result.Message = fmt.Sprintf("Configured model %q is not in the harness model list", option.Configured)
				result.Recommendation = "Check the model with the harness. Custom model IDs and aliases may still be valid."
			}
			results[i] = result
		})
	}
	workers.Wait()
	return slices.DeleteFunc(results, func(result Result) bool { return result.CheckName == "" })
}
