// Package benchmark executes and reports website-planned DeepSWE campaigns.
package benchmark

import (
	"fmt"
	"strings"
)

// Pinned benchmark inputs and artifact schema versions.
const (
	DeepSWECommit          = "e016041a6ccf8da29906afc9a3f5a8df940a1f78"
	PierVersion            = "0.3.0"
	CodexClientVersion     = "0.146.0"
	ClaudeClientVersion    = "2.1.207"
	ReportSchemaVersion    = "benchmark-report-v5"
	StorageSchemaVersion   = "benchmark-store-v1"
	TreatmentSchemaVersion = "benchmark-treatment-v2"
	DeepSWETrialsSourceURL = "https://deepswe.datacurve.ai/artifacts/v1.1/trials.json"
)

func validTreatmentMode(mode string) bool {
	return mode == TreatmentInstructionsOnly || mode == TreatmentInstructionsAndSkills
}

const (
	adapterCodex             = "codex"
	adapterClaudeCode        = "claude-code"
	providerClaude           = "claude"
	commandGit               = "git"
	commandUVX               = "uvx"
	commandDocker            = "docker"
	dispatchEvidenceDir      = "agent-layer-dispatch"
	effortLow                = "low"
	effortMedium             = "medium"
	effortHigh               = "high"
	effortXHigh              = "xhigh"
	effortMax                = "max"
	statusSuccess            = "success"
	statusFailed             = "failed"
	costKindProviderReported = "provider-reported"
	costKindProviderTotal    = "provider-reported-components"
	costKindProviderUsage    = "provider-usage"
	dockerBuildxPlugin       = "docker-buildx"
	dockerComposePlugin      = "docker-compose"
	requiredRoleCodeReviewer = "code-reviewer"
	requiredRoleImplementer  = "implementer"
	requiredRolePlanReviewer = "plan-reviewer"
	verdictInconclusive      = "inconclusive"
	verdictBetter            = "better"
	verdictWorse             = "worse"
	costAxisLogarithmic      = "logarithmic"
	publishedFable           = "claude-fable-5"
	publishedLuna            = "gpt-5-6-luna"
	pierAgentKwarg           = "--agent-kwarg"
	taskInstructionFile      = "instruction.md"
	taskPreArtifactsFile     = "pre_artifacts.sh"
	taskTOMLFile             = "task.toml"
	skillsAgentTimeoutFactor = 4.0
)

// Treatment modes define the files injected into the provider workspace.
const (
	TreatmentInstructionsOnly      = "instructions-only"
	TreatmentInstructionsAndSkills = "instructions-and-skills"
)

// Model defines a published model family and its native Pier adapter.
type Model struct {
	Name                  string `json:"name"`
	PublishedIdentifier   string `json:"published_identifier"`
	RuntimeIdentifier     string `json:"runtime_identifier"`
	Adapter               string `json:"adapter"`
	ProviderClientVersion string `json:"provider_client_version"`
}

var supportedModels = []Model{
	{Name: "luna", PublishedIdentifier: publishedLuna, RuntimeIdentifier: "openai/gpt-5.6-luna", Adapter: adapterCodex, ProviderClientVersion: CodexClientVersion},
	{Name: "terra", PublishedIdentifier: "gpt-5-6-terra", RuntimeIdentifier: "openai/gpt-5.6-terra", Adapter: adapterCodex, ProviderClientVersion: CodexClientVersion},
	{Name: "sol", PublishedIdentifier: "gpt-5-6-sol", RuntimeIdentifier: "openai/gpt-5.6-sol", Adapter: adapterCodex, ProviderClientVersion: CodexClientVersion},
	{Name: "sonnet", PublishedIdentifier: "claude-sonnet-5", RuntimeIdentifier: "claude-sonnet-5", Adapter: adapterClaudeCode, ProviderClientVersion: ClaudeClientVersion},
	{Name: "opus", PublishedIdentifier: "claude-opus-4-8", RuntimeIdentifier: "claude-opus-4-8", Adapter: adapterClaudeCode, ProviderClientVersion: ClaudeClientVersion},
	{Name: "fable", PublishedIdentifier: publishedFable, RuntimeIdentifier: publishedFable, Adapter: adapterClaudeCode, ProviderClientVersion: ClaudeClientVersion},
}

var supportedEfforts = []string{effortLow, effortMedium, effortHigh, effortXHigh, effortMax}

// ParseModelSelection validates the stable family:effort identity.
func ParseModelSelection(value string) (Model, string, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Model{}, "", fmt.Errorf("model %q must be in the form <model>:<effort>", value)
	}
	for _, model := range supportedModels {
		if model.Name != strings.ToLower(parts[0]) {
			continue
		}
		for _, effort := range supportedEfforts {
			if effort == strings.ToLower(parts[1]) {
				return model, effort, nil
			}
		}
		return Model{}, "", fmt.Errorf("unsupported reasoning effort %q (supported: %s)", parts[1], strings.Join(supportedEfforts, ", "))
	}
	return Model{}, "", fmt.Errorf("unsupported model %q (supported: luna, terra, sol, sonnet, opus, fable)", parts[0])
}

func modelNameForPublished(identifier string) string {
	for _, model := range supportedModels {
		if model.PublishedIdentifier == identifier {
			return model.Name
		}
	}
	return identifier
}

func validTaskName(task string) bool {
	if task == "" || len(task) > 160 {
		return false
	}
	for _, character := range task {
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '-' {
			continue
		}
		return false
	}
	return true
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
