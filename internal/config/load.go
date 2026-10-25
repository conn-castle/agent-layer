package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/envfile"
	"github.com/conn-castle/agent-layer/internal/fsutil"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// ErrConfigValidation is a sentinel that wraps config validation failures
// (as opposed to TOML syntax, filesystem, or other loading errors).
// Callers can use errors.Is(err, ErrConfigValidation) to distinguish
// validation problems from other LoadProjectConfig failure modes.
var ErrConfigValidation = errors.New("config validation failed")

// ErrConfigNeedsUpgrade is a sentinel wrapped alongside ErrConfigValidation
// when config contains a retired key requiring Agent Layer v1.0.0. Repair tools
// use errors.Is to direct users to that release before attempting edits.
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

// LoadEnv reads .agent-layer/.env into a key-value map. A missing file is
// equivalent to an empty environment; malformed or unreadable files fail.
func LoadEnv(path string) (map[string]string, error) {
	data, err := fsutil.ReadRegularFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf(messages.ConfigFailedReadEnvFileFmt, path, err)
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
	if HasRetiredConfigAliases(data) {
		return nil, fmt.Errorf("%w: %w: "+messages.TransitionConfigFmt, ErrConfigValidation, ErrConfigNeedsUpgrade, source)
	}
	if err := decodeStrict(data); err != nil {
		if HasLegacyGeminiConfig(data) {
			// Wrap ErrConfigNeedsUpgrade so repair tools redirect to `al upgrade`.
			// The message already embeds the `al upgrade` guidance, so the generic
			// ConfigValidationGuidance suffix (which suggests `al wizard` can fix
			// it) is omitted here — the wizard cannot rewrite this legacy key.
			return nil, fmt.Errorf("%w: %w: "+messages.ConfigLegacyGeminiUnsupportedFmt, ErrConfigValidation, ErrConfigNeedsUpgrade, source)
		}
		if HasLegacyDispatchConfig(data) {
			return nil, fmt.Errorf("%w: %w: "+messages.ConfigLegacyDispatchUnsupportedFmt, ErrConfigValidation, ErrConfigNeedsUpgrade, source)
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

// HasLegacyDispatchConfig reports whether data contains retired per-agent
// dispatch default tables that must be removed by al upgrade.
func HasLegacyDispatchConfig(data []byte) bool {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		return false
	}
	agents, ok := raw["agents"].(map[string]any)
	if !ok {
		return false
	}
	for _, agent := range []string{agentAntigravity, agentClaude, agentCodex} {
		config, ok := agents[agent].(map[string]any)
		if !ok {
			continue
		}
		if _, ok := config["dispatch"]; ok {
			return true
		}
	}
	return false
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
	return &cfg, nil
}

// LoadConfigLenient reads .agent-layer/config.toml without validation.
// Returns an error only on filesystem or TOML syntax errors.
func LoadConfigLenient(path string) (*Config, error) {
	data, err := fsutil.ReadRegularFile(path)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingFileFmt, path, err)
	}
	return ParseConfigLenient(data, path)
}

// HasRetiredConfigAliases recognizes aliases no longer converted by this CLI.
func HasRetiredConfigAliases(data []byte) bool {
	var raw map[string]any
	if toml.Unmarshal(data, &raw) != nil {
		return false
	}
	agents, _ := raw["agents"].(map[string]any)
	if _, ok := agents["claude-vscode"]; ok {
		return true
	}
	mcp, _ := raw["mcp"].(map[string]any)
	servers, _ := mcp["servers"].([]any)
	for _, server := range servers {
		table, _ := server.(map[string]any)
		clients, _ := table["clients"].([]any)
		for _, client := range clients {
			if client == "gemini" {
				return true
			}
		}
	}
	return false
}
