package agentdispatch

import (
	"encoding/json"
	"fmt"
	"io"
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
// CompatibilityWarning is set when the installed version is newer than the
// tested version: the target stays supported and dispatchable, and the
// warning names both versions.
type TargetOption struct {
	Agent                 string             `json:"agent"`
	Enabled               bool               `json:"enabled"`
	Installed             bool               `json:"installed"`
	InstalledVersion      string             `json:"installed_version,omitempty"`
	SupportedVersion      string             `json:"supported_version"`
	CompatibilityWarning  string             `json:"compatibility_warning,omitempty"`
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

type targetDiscovery struct {
	Target                targetMeta
	Enabled               bool
	Installed             bool
	InstalledVersion      string
	CompatibilityWarning  string
	Fresh                 CapabilityOption
	RandomEligible        bool
	RandomExclusionReason string
}

type targetVersionDiscovery func(string, targetMeta) (string, string, error)

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
		if _, err := fmt.Fprintf(stdout, "- %s enabled=%t installed=%t installed_version=%s supported_version=%s fresh=%t resume=%t inspect=%t random_eligible=%t\n", target.Agent, target.Enabled, target.Installed, displayVersion(target.InstalledVersion), target.SupportedVersion, target.Capabilities.Fresh.Supported, target.Capabilities.Resume.Supported, target.Capabilities.Inspect.Supported, target.RandomEligible); err != nil {
			return err
		}
		if target.Capabilities.Fresh.Reason != "" {
			if _, err := fmt.Fprintf(stdout, "  fresh: %s\n", target.Capabilities.Fresh.Reason); err != nil {
				return err
			}
		}
		if target.CompatibilityWarning != "" {
			if _, err := fmt.Fprintf(stdout, "  %s\n", target.CompatibilityWarning); err != nil {
				return err
			}
		}
		if target.RandomExclusionReason != nil {
			if _, err := fmt.Fprintf(stdout, "  random: excluded (%s)\n", *target.RandomExclusionReason); err != nil {
				return err
			}
		}
		if err := writeFieldOption(stdout, "model", target.Model); err != nil {
			return err
		}
		if err := writeFieldOption(stdout, "reasoning_effort", target.ReasoningEffort); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(stdout, "Random pool: %s (excludes_caller=%t empty=%t)\n", strings.Join(options.Random.Pool, ","), options.Random.ExcludesCaller, options.Random.Empty); err != nil {
		return err
	}
	return nil
}

func writeFieldOption(stdout io.Writer, name string, option FieldOption) error {
	_, err := fmt.Fprintf(stdout, "  %s: override_supported=%t configured=%q allow_custom=%t suggestions=%q\n", name, option.OverrideSupported, option.Configured, option.AllowCustom, option.Suggestions)
	return err
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
		facts := discoverTarget(cfg, target, caller, callerKnown, discovery.LookPath, rawTargetVersionDiscovery(lookup))
		resume := facts.Fresh
		inspect := CapabilityOption{Supported: true}
		var exclusion *string
		if facts.RandomExclusionReason != "" {
			exclusion = &facts.RandomExclusionReason
		}
		reasons := []string{}
		if !facts.Fresh.Supported {
			reasons = append(reasons, facts.Fresh.Reason)
		}
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
		result = append(result, TargetOption{
			Agent:                 target.Name,
			Enabled:               facts.Enabled,
			Installed:             facts.Installed,
			InstalledVersion:      facts.InstalledVersion,
			SupportedVersion:      supportedProviderVersions[target.Name],
			CompatibilityWarning:  facts.CompatibilityWarning,
			Capabilities:          TargetCapabilities{Fresh: facts.Fresh, Resume: resume, Inspect: inspect},
			RandomEligible:        facts.RandomEligible,
			RandomExclusionReason: exclusion,
			Model:                 fieldOptionWithDiscovery(cfg, target, agentoptions.KindModel, fieldDiscovery),
			ReasoningEffort:       fieldOptionWithDiscovery(cfg, target, agentoptions.KindReasoningEffort, fieldDiscovery),
			UnavailableReasons:    reasons,
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

func discoverTarget(cfg config.Config, target targetMeta, caller string, callerKnown bool, lookPath func(string) (string, error), discoverVersion targetVersionDiscovery) targetDiscovery {
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
	facts.RandomEligible = facts.Fresh.Supported && (!callerKnown || target.Name != caller)
	if !facts.RandomEligible {
		facts.RandomExclusionReason = facts.Fresh.Reason
		if callerKnown && target.Name == caller {
			facts.RandomExclusionReason = "caller"
		}
	}
	return facts
}

func fieldOptionWithDiscovery(cfg config.Config, target targetMeta, kind agentoptions.Kind, discovery agentoptions.DiscoveryRequest) FieldOption {
	resolved := agentoptions.Resolve(cfg, target.Name, kind, discovery)
	return FieldOption{OverrideSupported: resolved.OverrideSupported, Configured: resolved.Configured, Suggestions: resolved.Suggestions, AllowCustom: resolved.AllowCustom}
}
