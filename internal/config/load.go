package config

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/envfile"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/templates"
)

// LoadProjectConfig reads and validates the full Agent Layer config from disk.
func LoadProjectConfig(root string) (*ProjectConfig, error) {
	paths := DefaultPaths(root)

	cfg, err := LoadConfig(paths.ConfigPath)
	if err != nil {
		return nil, err
	}

	env, err := LoadEnv(paths.EnvPath)
	if err != nil {
		return nil, err
	}

	instructions, err := LoadInstructions(paths.InstructionsDir)
	if err != nil {
		return nil, err
	}

	slashCommands, err := LoadSlashCommands(paths.SlashCommandsDir)
	if err != nil {
		return nil, err
	}

	commandsAllow, err := LoadCommandsAllow(paths.CommandsAllow)
	if err != nil {
		return nil, err
	}

	return &ProjectConfig{
		Config:        *cfg,
		Env:           env,
		Instructions:  instructions,
		SlashCommands: slashCommands,
		CommandsAllow: commandsAllow,
		Root:          root,
	}, nil
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
	return env, nil
}

// ParseConfig parses and validates config TOML data from a source identifier.
// data is the TOML content; source is used in error messages.
func ParseConfig(data []byte, source string) (*Config, error) {
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf(messages.ConfigInvalidConfigFmt, source, err)
	}
	if err := cfg.Validate(source); err != nil {
		return nil, err
	}
	return &cfg, nil
}
