// Package benchmark executes and reports content-addressed DeepSWE studies.
package benchmark

import (
	"fmt"
	"strings"
)

// Pinned benchmark inputs and artifact schema versions.
const (
	DeepSWECommit       = "e016041a6ccf8da29906afc9a3f5a8df940a1f78"
	PierVersion         = "0.3.0"
	CodexClientVersion  = "0.146.0"
	ClaudeClientVersion = "2.1.207"
	// These are the Linux client releases the benchmark adapters are written
	// and fixture-tested against.  They are part of a new-provider identity;
	// do not change the legacy Codex or Claude identity fields above.
	AntigravityClientVersion = "1.1.21"
	GrokClientVersion        = "1.0.5"
	ReportSchemaVersion      = "benchmark-report-v5"
	StorageSchemaVersion     = "benchmark-store-v1"
	TreatmentSchemaVersion   = "benchmark-treatment-v2"
	DeepSWETrialsSourceURL   = "https://deepswe.datacurve.ai/artifacts/v1.1/trials.json"
)

func validTreatmentMode(mode string) bool {
	return mode == TreatmentInstructionsOnly || mode == TreatmentInstructionsAndSkills
}

const (
	adapterCodex                  = "codex"
	adapterClaudeCode             = "claude-code"
	adapterAntigravity            = "antigravity"
	adapterGrok                   = "grok"
	providerClaude                = "claude"
	commandGit                    = "git"
	commandUVX                    = "uvx"
	commandDocker                 = "docker"
	dispatchEvidenceDir           = "agent-layer-dispatch"
	effortLow                     = "low"
	effortMedium                  = "medium"
	effortHigh                    = "high"
	effortXHigh                   = "xhigh"
	effortMax                     = "max"
	statusSuccess                 = "success"
	statusFailed                  = "failed"
	costKindProviderReported      = "provider-reported"
	costKindProviderTotal         = "provider-reported-components"
	costKindProviderUsage         = "provider-usage"
	authCheckEnvironmentPresence  = "environment-presence"
	authCheckJSONFilePresence     = "json-file-presence"
	authCheckOAuthProfilePresence = "oauth-profile-presence"
	authMethodGoogleOAuth         = "google_oauth"
	authMethodJSONFile            = "auth_json"
	dispatchRunStateCompleted     = "completed"
	dockerBuildxPlugin            = "docker-buildx"
	dockerComposePlugin           = "docker-compose"
	requiredRoleCodeReviewer      = "code-reviewer"
	requiredRoleImplementer       = "implementer"
	requiredRolePlanReviewer      = "plan-reviewer"
	verdictInconclusive           = "inconclusive"
	verdictBetter                 = "better"
	verdictWorse                  = "worse"
	costAxisLogarithmic           = "logarithmic"
	publishedFable                = "claude-fable-5"
	publishedLuna                 = "gpt-5-6-luna"
	modelGeminiFlashLow           = "gemini-3.5-flash-low"
	modelGeminiFlashMedium        = "gemini-3.5-flash-medium"
	modelGeminiFlashHigh          = "gemini-3.5-flash-high"
	modelGrok45                   = "grok-4.5"
	modelGrok46                   = "grok-4.6"
	pierAgentKwarg                = "--agent-kwarg"
	taskInstructionFile           = "instruction.md"
	taskPreArtifactsFile          = "pre_artifacts.sh"
	taskTOMLFile                  = "task.toml"
	studyResourceTimeoutKey       = "agent_timeout_multiplier"
	studyResourceSchemaKey        = "schema"
	studyResourceSchema           = "deepswe-benchmark-resources-v1"
	agentLayerEnvPath             = ".agent-layer/.env"
	antigravityOAuthRelativePath  = ".agy/antigravity-cli/antigravity-oauth-token"
	antigravityOAuthStageFile     = "antigravity-oauth-token"
	studyInputInstructions        = "instructions"
	studyInputSkills              = "skills"
	studyInputEntryPrompt         = "entry_prompt"
	skillsAgentTimeoutFactor      = 4.0
)

// providerDescriptor keeps the benchmark-specific parts of a coordinator in
// one place.  Model remains the serialized study/evidence shape so historical
// Codex and Claude manifests retain their exact bytes.
type providerDescriptor struct {
	Adapter           string
	DispatchAgent     string
	Binary            string
	PierAgent         string
	ClientVersion     string
	CredentialPath    string
	PreflightEvidence string
}

// #nosec G101 -- these are repository-relative credential file locations, not credential values.
var benchmarkProviders = map[string]providerDescriptor{
	adapterCodex:       {Adapter: adapterCodex, DispatchAgent: adapterCodex, Binary: adapterCodex, PierAgent: "AgentLayerCodex", ClientVersion: CodexClientVersion, CredentialPath: ".codex/auth.json", PreflightEvidence: "codex-mcp-preflight.json"},
	adapterClaudeCode:  {Adapter: adapterClaudeCode, DispatchAgent: providerClaude, Binary: providerClaude, PierAgent: "AgentLayerClaudeCode", ClientVersion: ClaudeClientVersion, CredentialPath: ".claude-config/.credentials.json", PreflightEvidence: "claude-mcp-preflight.txt"},
	adapterAntigravity: {Adapter: adapterAntigravity, DispatchAgent: adapterAntigravity, Binary: "agy", PierAgent: "AgentLayerAntigravity", ClientVersion: AntigravityClientVersion, CredentialPath: antigravityOAuthRelativePath, PreflightEvidence: "antigravity-mcp-preflight.json"},
	adapterGrok:        {Adapter: adapterGrok, DispatchAgent: adapterGrok, Binary: adapterGrok, PierAgent: "AgentLayerGrok", ClientVersion: GrokClientVersion, CredentialPath: ".grok-config/auth.json", PreflightEvidence: "grok-mcp-preflight.json"},
}

func benchmarkProvider(adapter string) (providerDescriptor, error) {
	provider, ok := benchmarkProviders[adapter]
	if !ok {
		return providerDescriptor{}, fmt.Errorf("unsupported benchmark provider adapter %q", adapter)
	}
	return provider, nil
}

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
	// Antigravity models are exact `agy models` slugs. The benchmark exports
	// only the CLI's OAuth profile from the same repo-local/keyring boundary
	// used by Agent Layer; conversation and account caches never cross into Pier.
	{Name: modelGeminiFlashLow, PublishedIdentifier: modelGeminiFlashLow, RuntimeIdentifier: modelGeminiFlashLow, Adapter: adapterAntigravity, ProviderClientVersion: AntigravityClientVersion},
	{Name: modelGeminiFlashMedium, PublishedIdentifier: modelGeminiFlashMedium, RuntimeIdentifier: modelGeminiFlashMedium, Adapter: adapterAntigravity, ProviderClientVersion: AntigravityClientVersion},
	{Name: modelGeminiFlashHigh, PublishedIdentifier: modelGeminiFlashHigh, RuntimeIdentifier: modelGeminiFlashHigh, Adapter: adapterAntigravity, ProviderClientVersion: AntigravityClientVersion},
	{Name: modelGrok45, PublishedIdentifier: modelGrok45, RuntimeIdentifier: modelGrok45, Adapter: adapterGrok, ProviderClientVersion: GrokClientVersion},
	{Name: modelGrok46, PublishedIdentifier: modelGrok46, RuntimeIdentifier: modelGrok46, Adapter: adapterGrok, ProviderClientVersion: GrokClientVersion},
}

// supportedEfforts is the evidence-level vocabulary.  Legacy coordinator
// parsing intentionally remains narrower in legacyEfforts below.
var supportedEfforts = []string{"none", "minimal", effortLow, effortMedium, effortHigh, effortXHigh, effortMax}
var legacyEfforts = []string{effortLow, effortMedium, effortHigh, effortXHigh, effortMax}

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
		effort := strings.ToLower(parts[1])
		if model.Adapter == adapterAntigravity {
			if slugEffort, ok := antigravitySlugEffort(model.RuntimeIdentifier); !ok {
				return Model{}, "", fmt.Errorf("antigravity model %q must be an exact Gemini slug ending in -low, -medium, or -high", model.RuntimeIdentifier)
			} else if effort != slugEffort {
				return Model{}, "", fmt.Errorf("antigravity model %q requires reasoning effort %q, got %q", model.RuntimeIdentifier, slugEffort, parts[1])
			}
			return model, effort, nil
		}
		if model.Adapter == adapterGrok {
			if containsString(grokEfforts, effort) {
				return model, effort, nil
			}
			return Model{}, "", fmt.Errorf("unsupported Grok reasoning effort %q (supported: %s)", parts[1], strings.Join(grokEfforts, ", "))
		}
		for _, supported := range legacyEfforts {
			if supported == effort {
				return model, effort, nil
			}
		}
		return Model{}, "", fmt.Errorf("unsupported reasoning effort %q (supported: %s)", parts[1], strings.Join(legacyEfforts, ", "))
	}
	return Model{}, "", fmt.Errorf("unsupported model %q (supported: %s)", parts[0], strings.Join(supportedModelNames(), ", "))
}

var grokEfforts = []string{"none", "minimal", effortLow, effortMedium, effortHigh, effortXHigh, effortMax}

func antigravitySlugEffort(slug string) (string, bool) {
	for _, effort := range []string{effortLow, effortMedium, effortHigh} {
		if strings.HasSuffix(slug, "-"+effort) {
			return effort, true
		}
	}
	return "", false
}

func modelNameForPublished(identifier string) string {
	for _, model := range supportedModels {
		if model.PublishedIdentifier == identifier {
			return model.Name
		}
	}
	return identifier
}

func supportedPublishedModelIdentifiers() []string {
	identifiers := make([]string, 0, len(supportedModels))
	for _, model := range supportedModels {
		identifiers = append(identifiers, model.PublishedIdentifier)
	}
	return identifiers
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

func supportedModelNames() []string {
	values := make([]string, 0, len(supportedModels))
	for _, model := range supportedModels {
		values = append(values, model.Name)
	}
	return values
}

func validRequiredDispatchRole(role string) bool {
	switch role {
	case requiredRoleCodeReviewer, requiredRoleImplementer, requiredRolePlanReviewer:
		return true
	default:
		return false
	}
}
