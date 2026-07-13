package agentdispatch

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/config"
)

// OptionsResponse is the stable v0.13 Agent Dispatch capability contract.
type OptionsResponse struct {
	Caller  CallerInfo     `json:"caller"`
	Random  RandomInfo     `json:"random"`
	Targets []TargetOption `json:"targets"`
}

// CallerInfo describes env-only dispatch caller detection.
type CallerInfo struct {
	Known bool   `json:"known"`
	Agent string `json:"agent,omitempty"`
}

// RandomInfo describes the current fresh-dispatch random pool.
type RandomInfo struct {
	Pool           []string `json:"pool"`
	ExcludesCaller bool     `json:"excludes_caller"`
	Empty          bool     `json:"empty"`
}

// CapabilityOption reports a distinct operation capability. Capabilities are
// never collapsed into a misleading single dispatch-capable boolean.
type CapabilityOption struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// TargetCapabilities reports fresh execution, durable continuation, and
// factual inspection separately.
type TargetCapabilities struct {
	Fresh   CapabilityOption `json:"fresh"`
	Resume  CapabilityOption `json:"resume"`
	Inspect CapabilityOption `json:"inspect"`
}

// TargetOption describes one target's installed/configured capability facts.
type TargetOption struct {
	Agent                 string             `json:"agent"`
	Enabled               bool               `json:"enabled"`
	Installed             bool               `json:"installed"`
	InstalledVersion      string             `json:"installed_version,omitempty"`
	SupportedVersion      string             `json:"supported_version"`
	Capabilities          TargetCapabilities `json:"capabilities"`
	RandomEligible        bool               `json:"random_eligible"`
	RandomExclusionReason *string            `json:"random_exclusion_reason,omitempty"`
	Model                 FieldOption        `json:"model"`
	ReasoningEffort       FieldOption        `json:"reasoning_effort"`
	UnavailableReasons    []string           `json:"unavailable_reasons"`
}

// FieldOption describes per-run override metadata.
type FieldOption struct {
	OverrideSupported bool     `json:"override_supported"`
	Configured        string   `json:"configured"`
	Suggestions       []string `json:"suggestions"`
	AllowCustom       bool     `json:"allow_custom"`
}

// BuildOptions loads strict project config and reports each operation's exact
// availability for the installed provider version.
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
	caller, callerKnown := knownCallerFromEnv(env)
	// Options must report the exact installed version even when it is
	// unsupported, so it reads raw provider versions instead of the
	// capability cache, which stores only supported versions.
	versionLookup := req.VersionLookup
	response := &OptionsResponse{Caller: CallerInfo{Known: callerKnown, Agent: caller}, Random: RandomInfo{Pool: []string{}, ExcludesCaller: callerKnown}}
	response.Targets = buildTargetOptions(project.Config, caller, callerKnown, agentoptions.DiscoveryRequest{Env: env, LookPath: lookPath, Live: true}, versionLookup)
	for _, target := range response.Targets {
		if target.RandomEligible {
			response.Random.Pool = append(response.Random.Pool, target.Agent)
		}
	}
	response.Random.Empty = len(response.Random.Pool) == 0
	return response, nil
}

// WriteOptions renders stable JSON or concise human-readable capability facts.
func WriteOptions(req OptionsRequest) error {
	stdout := writerOrDiscard(req.Stdout)
	options, err := BuildOptions(req)
	if err != nil {
		return err
	}
	if req.JSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(options)
	}
	if options.Caller.Known {
		if _, err := fmt.Fprintf(stdout, "Agent Dispatch options (caller: %s)\n", options.Caller.Agent); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(stdout, "Agent Dispatch options (caller: unknown)"); err != nil {
		return err
	}
	for _, target := range options.Targets {
		if _, err := fmt.Fprintf(stdout, "- %s enabled=%t installed=%t version=%s fresh=%t resume=%t inspect=%t\n", target.Agent, target.Enabled, target.Installed, displayVersion(target.InstalledVersion), target.Capabilities.Fresh.Supported, target.Capabilities.Resume.Supported, target.Capabilities.Inspect.Supported); err != nil {
			return err
		}
		if target.Capabilities.Fresh.Reason != "" {
			if _, err := fmt.Fprintf(stdout, "  fresh: %s\n", target.Capabilities.Fresh.Reason); err != nil {
				return err
			}
		}
	}
	return nil
}

func displayVersion(value string) string {
	if value == "" {
		return statusUnknown
	}
	return value
}

func buildTargetOptions(cfg config.Config, caller string, callerKnown bool, discovery agentoptions.DiscoveryRequest, lookups ...func(string, string) (string, error)) []TargetOption {
	var lookup func(string, string) (string, error)
	if len(lookups) > 0 {
		lookup = lookups[0]
	}
	targets := targetRegistry()
	result := make([]TargetOption, 0, len(targets))
	for _, target := range targets {
		_, installedErr := discovery.LookPath(target.Binary)
		installed := installedErr == nil
		enabled := targetEnabled(cfg, target.Name)
		version := ""
		versionOK := false
		if installed {
			path, _ := discovery.LookPath(target.Binary)
			readVersion := lookup
			if readVersion == nil {
				readVersion = providerVersion
			}
			version, installedErr = readVersion(path, target.Name)
			versionOK = installedErr == nil && version == supportedProviderVersions[target.Name]
		}
		fresh := CapabilityOption{Supported: enabled && installed && versionOK}
		if !fresh.Supported {
			switch {
			case !enabled:
				fresh.Reason = "disabled in config"
			case !installed:
				fresh.Reason = "provider binary not found"
			case installedErr != nil:
				fresh.Reason = "provider version could not be verified"
			default:
				fresh.Reason = "unsupported provider version; install " + supportedProviderVersions[target.Name]
			}
		}
		resume := fresh
		inspect := CapabilityOption{Supported: true}
		randomEligible := fresh.Supported && (!callerKnown || target.Name != caller)
		var exclusion *string
		if !randomEligible {
			reason := fresh.Reason
			if callerKnown && target.Name == caller {
				reason = "caller"
			}
			exclusion = &reason
		}
		reasons := []string{}
		if !fresh.Supported {
			reasons = append(reasons, fresh.Reason)
		}
		fieldDiscovery := discovery
		if !fresh.Supported {
			fieldDiscovery.Live = false
		}
		result = append(result, TargetOption{
			Agent:                 target.Name,
			Enabled:               enabled,
			Installed:             installed,
			InstalledVersion:      version,
			SupportedVersion:      supportedProviderVersions[target.Name],
			Capabilities:          TargetCapabilities{Fresh: fresh, Resume: resume, Inspect: inspect},
			RandomEligible:        randomEligible,
			RandomExclusionReason: exclusion,
			Model:                 fieldOptionWithDiscovery(cfg, target, agentoptions.KindModel, fieldDiscovery),
			ReasoningEffort:       fieldOptionWithDiscovery(cfg, target, agentoptions.KindReasoningEffort, fieldDiscovery),
			UnavailableReasons:    reasons,
		})
	}
	return result
}

func fieldOptionWithDiscovery(cfg config.Config, target targetMeta, kind agentoptions.Kind, discovery agentoptions.DiscoveryRequest) FieldOption {
	resolved := agentoptions.Resolve(cfg, target.Name, kind, discovery)
	return FieldOption{OverrideSupported: resolved.OverrideSupported, Configured: resolved.Configured, Suggestions: resolved.Suggestions, AllowCustom: resolved.AllowCustom}
}
