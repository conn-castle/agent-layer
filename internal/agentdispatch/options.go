package agentdispatch

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/config"
)

// OptionsResponse describes the agents that may be selected by dispatch start.
type OptionsResponse struct {
	Agents []AgentOption `json:"agents"`
}

// AgentOption describes one selectable provider and its optional overrides.
type AgentOption struct {
	Agent             string      `json:"agent"`
	Available         bool        `json:"available"`
	UnavailableReason string      `json:"unavailable_reason,omitempty"`
	Model             FieldOption `json:"model"`
	ReasoningEffort   FieldOption `json:"reasoning_effort"`
}

// FieldOption describes an optional start override.
type FieldOption struct {
	OverrideSupported bool     `json:"supported"`
	Configured        string   `json:"configured"`
	Suggestions       []string `json:"suggestions"`
	AllowCustom       bool     `json:"allow_custom"`
	Source            string   `json:"source,omitempty"`
	DiscoveryError    string   `json:"discovery_error,omitempty"`
}

// BuildOptions loads strict project config and reports valid start selections.
func BuildOptions(req OptionsRequest) (*OptionsResponse, error) {
	root := strings.TrimSpace(req.Root)
	if root == "" {
		return nil, exitError(ExitConfig, "repository root is required")
	}
	env := req.Env
	if env == nil {
		env = os.Environ()
	}
	lookPath := req.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	project, err := config.LoadProjectConfig(root)
	if err != nil {
		return nil, exitError(ExitConfig, err.Error())
	}
	versionLookup := req.VersionLookup
	ctx := req.Context
	if ctx == nil {
		ctx = context.Background()
	}
	options := &OptionsResponse{Agents: buildTargetOptions(
		project.Config,
		agentoptions.DiscoveryRequest{Context: ctx, Project: project, Env: env, LookPath: lookPath, Live: true, Cleanup: req.Cleanup},
		versionLookup,
	)}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return options, nil
}

// WriteOptions renders the discovery contract as one JSON object.
func WriteOptions(req OptionsRequest) error {
	stdout := writerOrDiscard(req.Stdout)
	options, err := BuildOptions(req)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(options)
}

func buildTargetOptions(cfg config.Config, discovery agentoptions.DiscoveryRequest, lookup func(string, string) (string, error)) []AgentOption {
	targets := targetRegistry()
	result := make([]AgentOption, len(targets))
	var workers sync.WaitGroup
	for i, target := range targets {
		workers.Go(func() {
			resolvedPath, available, reason := "", false, "disabled in config"
			if targetEnabled(cfg, target.Name) {
				resolvedPath, available, reason = discoverTarget(discovery.Context, target, discovery.LookPath, lookup)
			}
			fieldDiscovery := discovery
			if !available {
				fieldDiscovery.Live = false
			} else {
				fieldDiscovery.LookPath = func(binary string) (string, error) {
					if binary == target.Binary {
						return resolvedPath, nil
					}
					return discovery.LookPath(binary)
				}
			}
			result[i] = AgentOption{
				Agent:             target.Name,
				Available:         available,
				UnavailableReason: reason,
				Model:             fieldOptionWithDiscovery(cfg, target, agentoptions.KindModel, fieldDiscovery),
				ReasoningEffort:   fieldOptionWithDiscovery(cfg, target, agentoptions.KindReasoningEffort, fieldDiscovery),
			}
		})
	}
	workers.Wait()
	return result
}

// discoverTarget reports an enabled target's resolved binary path, availability,
// and unavailable reason.
func discoverTarget(ctx context.Context, target targetMeta, lookPath func(string) (string, error), lookup func(string, string) (string, error)) (string, bool, string) {
	path, err := lookPath(target.Binary)
	if err != nil {
		return "", false, "provider binary not found"
	}
	if lookup == nil {
		if ctx == nil {
			ctx = context.Background()
		}
		lookup = func(path, agent string) (string, error) {
			return providerVersionWithContext(ctx, path, agent)
		}
	}
	installed, err := lookup(path, target.Name)
	if err == nil {
		if _, err = providerVersionCompatibility(target.Name, installed); err == nil {
			return path, true, ""
		}
		if installed != "" {
			return path, false, "unsupported provider version; install " + supportedProviderVersions[target.Name]
		}
	}
	return path, false, "provider version could not be verified"
}

func fieldOptionWithDiscovery(cfg config.Config, target targetMeta, kind agentoptions.Kind, discovery agentoptions.DiscoveryRequest) FieldOption {
	resolved := agentoptions.Resolve(cfg, target.Name, kind, discovery)
	return FieldOption{OverrideSupported: resolved.OverrideSupported, Configured: resolved.Configured, Suggestions: resolved.Suggestions, AllowCustom: resolved.AllowCustom, Source: resolved.Source, DiscoveryError: resolved.DiscoveryError}
}
