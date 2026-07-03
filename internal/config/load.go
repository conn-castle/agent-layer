package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/envfile"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// ErrConfigValidation is a sentinel that wraps config validation failures
// (as opposed to TOML syntax, filesystem, or other loading errors).
// Callers can use errors.Is(err, ErrConfigValidation) to distinguish
// validation problems from other LoadProjectConfig failure modes.
var ErrConfigValidation = errors.New("config validation failed")

// ErrConfigNeedsUpgrade is a sentinel wrapped alongside ErrConfigValidation
// when a config fails validation because it contains a legacy key that only
// `al upgrade` can migrate (e.g. a removed agent table). Repair tools such as
// the wizard cannot rewrite these keys in place, so they use
// errors.Is(err, ErrConfigNeedsUpgrade) to redirect the user to `al upgrade`
// instead of attempting a fix that would dead-end at sync.
var ErrConfigNeedsUpgrade = errors.New("config requires migration")

// LoadProjectConfig reads and validates the full Agent Layer config from disk.
func LoadProjectConfig(root string) (*ProjectConfig, error) {
	return LoadProjectConfigFS(os.DirFS(root), root)
}

// LoadTemplateConfig returns the embedded default config template as a validated Config.
func LoadTemplateConfig() (*Config, error) {
	data, err := templates.Read("config.toml")
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigFailedReadTemplateFmt, err)
	}
	return ParseConfig(data, "template config.toml")
}

// LoadEnv reads .agent-layer/.env into a key-value map.
func LoadEnv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingEnvFileFmt, path, err)
	}

	env, err := envfile.Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigInvalidEnvFileFmt, path, err)
	}
	return filterAgentLayerEnv(env), nil
}

// filterAgentLayerEnv restricts .env values to the AL_ namespace.
func filterAgentLayerEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return env
	}
	filtered := make(map[string]string, len(env))
	for key, value := range env {
		if strings.HasPrefix(key, "AL_") {
			filtered[key] = value
		}
	}
	return filtered
}

// ParseConfig parses and validates config TOML data from a source identifier.
// data is the TOML content; source is used in error messages.
func ParseConfig(data []byte, source string) (*Config, error) {
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf(messages.ConfigInvalidConfigFmt, source, err)
	}
	if err := decodeStrict(data); err != nil {
		if HasLegacyGeminiConfig(data) {
			// Wrap ErrConfigNeedsUpgrade so repair tools redirect to `al upgrade`.
			// The message already embeds the `al upgrade` guidance, so the generic
			// ConfigValidationGuidance suffix (which suggests `al wizard` can fix
			// it) is omitted here — the wizard cannot rewrite this legacy key.
			return nil, fmt.Errorf("%w: %w: "+messages.ConfigLegacyGeminiUnsupportedFmt, ErrConfigValidation, ErrConfigNeedsUpgrade, source)
		}
		return nil, fmt.Errorf("%w: "+messages.ConfigUnrecognizedKeysFmt+" "+messages.ConfigValidationGuidance, ErrConfigValidation, source, err)
	}
	if err := cfg.Validate(source); err != nil {
		if errors.Is(err, ErrConfigNeedsUpgrade) {
			return nil, fmt.Errorf("%w: %w", ErrConfigValidation, err)
		}
		return nil, fmt.Errorf("%w: %w "+messages.ConfigValidationGuidance, ErrConfigValidation, err)
	}
	return &cfg, nil
}

// HasLegacyGeminiConfig reports whether `data` contains a legacy `[agents.gemini]`
// table or any subkey under it. Parses the raw TOML map so whitespace, tabs,
// comments, and TOML inline-table forms are all handled correctly — the previous
// space-strip substring scan missed tab-indented and comment-style configs.
func HasLegacyGeminiConfig(data []byte) bool {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return false
	}
	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = agents["gemini"]
	return ok
}

// HasLegacyAntigravityAgentSpecificModel reports whether `data` contains the
// pre-v0.12.0 Antigravity provider passthrough model key. That key is migrated
// to the typed `agents.antigravity.model` field by `al upgrade`; repair tools
// must not preserve it through lenient config rewrites.
func HasLegacyAntigravityAgentSpecificModel(data []byte) bool {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return false
	}
	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		return false
	}
	antigravity, ok := agents["antigravity"].(map[string]any)
	if !ok {
		return false
	}
	agentSpecific, ok := antigravity["agent_specific"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = agentSpecific["model"]
	return ok
}

// decodeStrict re-decodes the TOML data with strict unknown-field rejection.
// This catches keys that toml.Unmarshal silently ignores (e.g. model on
// enable-only agents whose struct has no Model field).
func decodeStrict(data []byte) error {
	var cfg Config
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(&cfg)
}

// ParseConfigLenient parses config TOML data without validation.
// Returns an error only on TOML syntax errors. Missing or invalid fields
// are not checked, making this suitable for repair tools (wizard, doctor)
// that need to read partially valid configs.
func ParseConfigLenient(data []byte, source string) (*Config, error) {
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf(messages.ConfigInvalidConfigFmt, source, err)
	}
	applyLegacyConfigAliases(data, &cfg)
	return &cfg, nil
}

// applyLegacyConfigAliases carries forward values from legacy config keys
// that were renamed in migration manifests but not yet migrated on disk.
// This ensures repair tools (wizard, doctor) see the correct enabled state
// even when the migration has not run yet.
func applyLegacyConfigAliases(data []byte, cfg *Config) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return
	}
	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		return
	}
	// Legacy: [agents.claude-vscode] → [agents.claude_vscode]
	// (v0.8.8 migration config_rename_key)
	if legacy, ok := agents["claude-vscode"].(map[string]any); ok {
		if cfg.Agents.ClaudeVSCode.Enabled == nil {
			if val, ok := legacy["enabled"].(bool); ok {
				cfg.Agents.ClaudeVSCode.Enabled = &val
			}
		}
	}
	// Legacy: [agents.gemini] → [agents.antigravity]
	// (v0.10.2 migration config_rename_key b-rename-agents-gemini-enabled).
	// Repair tools must see the user's intended enablement state on
	// pre-migration repos so doctor's CheckAntigravityBinary gate fires.
	if legacy, ok := agents["gemini"].(map[string]any); ok {
		if cfg.Agents.Antigravity.Enabled == nil {
			if val, ok := legacy["enabled"].(bool); ok {
				cfg.Agents.Antigravity.Enabled = &val
			}
		}
	}
}

// LoadConfigLenient reads .agent-layer/config.toml without validation.
// Returns an error only on filesystem or TOML syntax errors.
func LoadConfigLenient(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingFileFmt, path, err)
	}
	return ParseConfigLenient(data, path)
}
