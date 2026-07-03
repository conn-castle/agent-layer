package agentoptions

import (
	"os"
	"os/exec"
	"strings"
	"time"

	antigravityclient "github.com/conn-castle/agent-layer/internal/clients/antigravity"
	"github.com/conn-castle/agent-layer/internal/config"
)

// Kind identifies a configurable agent option surface.
type Kind string

const (
	// KindModel selects an agent model field.
	KindModel Kind = "model"
	// KindReasoningEffort selects an agent reasoning-effort field.
	KindReasoningEffort Kind = "reasoning_effort"
)

// DiscoveryRequest controls live option discovery.
type DiscoveryRequest struct {
	Env      []string
	LookPath func(string) (string, error)
	Live     bool
	// Timeout bounds live discovery commands. Zero uses the client default.
	Timeout time.Duration
}

// OptionSet is the shared option metadata used by wizard and dispatch.
type OptionSet struct {
	OverrideSupported bool
	Configured        string
	Suggestions       []string
	AllowCustom       bool
}

type fieldProvider struct {
	key        string
	configured func(config.Config) string
	live       func(DiscoveryRequest) []string
}

var providers = map[string]map[Kind]fieldProvider{
	"antigravity": {
		KindModel: {
			key: config.AntigravityModelFieldKey,
			configured: func(cfg config.Config) string {
				return cfg.Agents.Antigravity.Model
			},
			live: antigravityModelOptions,
		},
	},
	"claude": {
		KindModel: {
			key: config.ClaudeModelFieldKey,
			configured: func(cfg config.Config) string {
				return cfg.Agents.Claude.Model
			},
		},
		KindReasoningEffort: {
			key: config.ClaudeReasoningEffortFieldKey,
			configured: func(cfg config.Config) string {
				return cfg.Agents.Claude.ReasoningEffort
			},
		},
	},
	"codex": {
		KindModel: {
			key: config.CodexModelFieldKey,
			configured: func(cfg config.Config) string {
				return cfg.Agents.Codex.Model
			},
		},
		KindReasoningEffort: {
			key: config.CodexReasoningEffortFieldKey,
			configured: func(cfg config.Config) string {
				return cfg.Agents.Codex.ReasoningEffort
			},
		},
	},
	"copilot_cli": {
		KindModel: {
			key: config.CopilotCLIModelFieldKey,
			configured: func(cfg config.Config) string {
				return cfg.Agents.CopilotCLI.Model
			},
		},
	},
}

// DefaultDiscoveryRequest enables live option discovery with the current
// process environment and PATH lookup.
func DefaultDiscoveryRequest() DiscoveryRequest {
	return DiscoveryRequest{
		Env:      os.Environ(),
		LookPath: exec.LookPath,
		Live:     true,
	}
}

// Resolve returns the option metadata for an agent field, including live
// suggestions when available and catalog fallback otherwise.
func Resolve(cfg config.Config, agent string, kind Kind, req DiscoveryRequest) OptionSet {
	provider, ok := lookup(agent, kind)
	option := OptionSet{
		OverrideSupported: ok,
		Suggestions:       []string{},
	}
	if !ok {
		return option
	}
	option.Configured = strings.TrimSpace(provider.configured(cfg))
	field, _ := config.LookupField(provider.key)
	option.AllowCustom = field.AllowCustom
	option.Suggestions = values(provider, field, req)
	return option
}

// Values returns suggested values for an agent field.
func Values(agent string, kind Kind, req DiscoveryRequest) []string {
	provider, ok := lookup(agent, kind)
	if !ok {
		return nil
	}
	field, _ := config.LookupField(provider.key)
	return values(provider, field, req)
}

// Supports reports whether an agent exposes the requested option surface.
func Supports(agent string, kind Kind) bool {
	_, ok := lookup(agent, kind)
	return ok
}

// ConfiguredValue returns the configured value for an agent field.
func ConfiguredValue(cfg config.Config, agent string, kind Kind) string {
	provider, ok := lookup(agent, kind)
	if !ok {
		return ""
	}
	return strings.TrimSpace(provider.configured(cfg))
}

func lookup(agent string, kind Kind) (fieldProvider, bool) {
	fields, ok := providers[agent]
	if !ok {
		return fieldProvider{}, false
	}
	provider, ok := fields[kind]
	return provider, ok
}

func values(provider fieldProvider, field config.FieldDef, req DiscoveryRequest) []string {
	if req.Live && provider.live != nil {
		if live := provider.live(req); len(live) > 0 {
			return live
		}
	}
	values := make([]string, 0, len(field.Options))
	for _, option := range field.Options {
		values = append(values, option.Value)
	}
	return values
}

func antigravityModelOptions(req DiscoveryRequest) []string {
	return antigravityclient.ModelOptions(antigravityclient.ModelOptionsRequest{
		Env:      req.Env,
		LookPath: req.LookPath,
		Timeout:  req.Timeout,
	})
}
