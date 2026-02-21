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

// LoadProjectConfig reads and validates the full Agent Layer config from disk.
func LoadProjectConfig(root string) (*ProjectConfig, error) {
	return LoadProjectConfigFS(os.DirFS(root), root)
}

// LoadConfig reads .agent-layer/config.toml and validates it.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingFileFmt, path, err)
	}
	return ParseConfig(data, path)
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
		return nil, fmt.Errorf("%w: "+messages.ConfigUnrecognizedKeysFmt+" "+messages.ConfigValidationGuidance, ErrConfigValidation, source, err)
	}
	if err := cfg.Validate(source); err != nil {
		return nil, fmt.Errorf("%w: %w "+messages.ConfigValidationGuidance, ErrConfigValidation, err)
	}
	return &cfg, nil
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
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf(messages.ConfigMissingFileFmt, path, err)
	}
	return ParseConfigLenient(data, path)
}
