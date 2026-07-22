package agentdispatch

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/config"
)

// OptionsResponse describes the agents that may be selected by dispatch start.
type OptionsResponse struct {
	Agents []AgentOption `json:"agents"`
}

// CapabilityOption is an internal provider-availability result.
type CapabilityOption struct {
	Supported bool
	Reason    string
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
}

type targetDiscovery struct {
	Target               targetMeta
	Enabled              bool
	Installed            bool
	InstalledVersion     string
	CompatibilityWarning string
	Fresh                CapabilityOption
}

type targetVersionDiscovery func(string, targetMeta) (string, string, error)

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
	return &OptionsResponse{Agents: buildTargetOptions(
		project.Config,
		agentoptions.DiscoveryRequest{Env: env, LookPath: lookPath, Live: true},
		versionLookup,
	)}, nil
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

func buildTargetOptions(cfg config.Config, discovery agentoptions.DiscoveryRequest, lookups ...func(string, string) (string, error)) []AgentOption {
	var lookup func(string, string) (string, error)
	if len(lookups) > 0 {
		lookup = lookups[0]
	}
	targets := targetRegistry()
	result := make([]AgentOption, 0, len(targets))
	for _, target := range targets {
		facts := discoverTarget(cfg, target, discovery.LookPath, rawTargetVersionDiscovery(lookup))
		fieldDiscovery := discovery
		if !facts.Fresh.Supported {
			fieldDiscovery.Live = false
		} else {
			resolvedPath := facts.Target.Binary
			fieldDiscovery.LookPath = func(binary string) (string, error) {
				if binary == target.Binary {
					return resolvedPath, nil
				}
				return discovery.LookPath(binary)
			}
		}
		result = append(result, AgentOption{
			Agent:             target.Name,
			Available:         facts.Fresh.Supported,
			UnavailableReason: facts.Fresh.Reason,
			Model:             fieldOptionWithDiscovery(cfg, target, agentoptions.KindModel, fieldDiscovery),
			ReasoningEffort:   fieldOptionWithDiscovery(cfg, target, agentoptions.KindReasoningEffort, fieldDiscovery),
		})
	}
	return result
}

func rawTargetVersionDiscovery(lookup func(string, string) (string, error)) targetVersionDiscovery {
	return func(path string, target targetMeta) (string, string, error) {
		readVersion := lookup
		if readVersion == nil {
			readVersion = providerVersion
		}
		installed, err := readVersion(path, target.Name)
		if err != nil {
			return "", "", err
		}
		warning, err := providerVersionCompatibility(target.Name, installed)
		return installed, warning, err
	}
}

func discoverTarget(cfg config.Config, target targetMeta, lookPath func(string) (string, error), discoverVersion targetVersionDiscovery) targetDiscovery {
	facts := targetDiscovery{Target: target, Enabled: targetEnabled(cfg, target.Name)}
	path, pathErr := lookPath(target.Binary)
	var versionErr error
	if pathErr == nil {
		facts.Installed = true
		facts.Target.Binary = path
		version, warning, err := discoverVersion(path, target)
		facts.InstalledVersion = version
		facts.CompatibilityWarning = warning
		versionErr = err
	}
	facts.Fresh.Supported = facts.Enabled && facts.Installed && versionErr == nil
	if !facts.Fresh.Supported {
		switch {
		case !facts.Enabled:
			facts.Fresh.Reason = "disabled in config"
		case !facts.Installed:
			facts.Fresh.Reason = "provider binary not found"
		case facts.InstalledVersion == "":
			facts.Fresh.Reason = "provider version could not be verified"
		default:
			facts.Fresh.Reason = "unsupported provider version; install " + supportedProviderVersions[target.Name]
		}
	}
	return facts
}

func fieldOptionWithDiscovery(cfg config.Config, target targetMeta, kind agentoptions.Kind, discovery agentoptions.DiscoveryRequest) FieldOption {
	resolved := agentoptions.Resolve(cfg, target.Name, kind, discovery)
	return FieldOption{OverrideSupported: resolved.OverrideSupported, Configured: resolved.Configured, Suggestions: resolved.Suggestions, AllowCustom: resolved.AllowCustom}
}
