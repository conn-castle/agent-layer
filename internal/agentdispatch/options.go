package agentdispatch

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
)

// OptionsResponse is the stable v1 machine-readable Agent Dispatch options shape.
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

// RandomInfo describes the current random-selection pool.
type RandomInfo struct {
	Pool           []string `json:"pool"`
	ExcludesCaller bool     `json:"excludes_caller"`
	Empty          bool     `json:"empty"`
}

// TargetOption describes one dispatch target in the options response.
type TargetOption struct {
	Agent                 string          `json:"agent"`
	Enabled               bool            `json:"enabled"`
	Installed             bool            `json:"installed"`
	DispatchCapable       bool            `json:"dispatch_capable"`
	RandomEligible        bool            `json:"random_eligible"`
	RandomExclusionReason *string         `json:"random_exclusion_reason"`
	Streaming             StreamingOption `json:"streaming"`
	Model                 FieldOption     `json:"model"`
	ReasoningEffort       FieldOption     `json:"reasoning_effort"`
	UnavailableReasons    []string        `json:"unavailable_reasons"`
}

// StreamingOption describes target output streaming capability.
type StreamingOption struct {
	AnswerText string `json:"answer_text"`
	Progress   string `json:"progress"`
}

// FieldOption describes dispatch override metadata for one target field.
type FieldOption struct {
	OverrideSupported bool     `json:"override_supported"`
	Configured        string   `json:"configured"`
	Suggestions       []string `json:"suggestions"`
	AllowCustom       bool     `json:"allow_custom"`
}

// BuildOptions loads strict project config and builds the v1 options response.
func BuildOptions(req OptionsRequest) (*OptionsResponse, error) {
	root := strings.TrimSpace(req.Root)
	if root == "" {
		return nil, exitError(ExitConfig, messages.ConfigRootRequired)
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
	response := &OptionsResponse{
		Caller: CallerInfo{Known: callerKnown, Agent: caller},
	}
	// Initialize Pool to an empty slice (not nil) so JSON consumers always
	// see `"pool": []` instead of `"pool": null` when no targets are
	// eligible. The documented JSON shape promises an array.
	response.Random.Pool = []string{}
	response.Targets = buildTargetOptions(project.Config, caller, callerKnown, lookPath)
	for _, target := range response.Targets {
		if target.RandomEligible {
			response.Random.Pool = append(response.Random.Pool, target.Agent)
		}
	}
	response.Random.ExcludesCaller = callerKnown
	response.Random.Empty = len(response.Random.Pool) == 0
	return response, nil
}

// WriteOptions renders options as JSON or human-readable text.
func WriteOptions(req OptionsRequest) error {
	stdout := req.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	options, err := BuildOptions(req)
	if err != nil {
		return err
	}
	if req.JSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(options)
	}
	return writeOptionsText(stdout, options)
}

func buildTargetOptions(cfg config.Config, caller string, callerKnown bool, lookPath func(string) (string, error)) []TargetOption {
	targets := targetRegistry()
	out := make([]TargetOption, 0, len(targets))
	for _, meta := range targets {
		_, installedErr := lookPath(meta.Binary)
		installed := installedErr == nil
		enabled := targetEnabled(cfg, meta.Name)
		reasons := unavailableReasons(enabled, installed)
		dispatchCapable := enabled && installed
		randomEligible := dispatchCapable
		var exclusion *string
		if !enabled {
			exclusion = stringPtr("disabled")
			randomEligible = false
		} else if !installed {
			exclusion = stringPtr("uninstalled")
			randomEligible = false
		}
		if randomEligible && callerKnown && meta.Name == caller {
			exclusion = stringPtr("caller")
			randomEligible = false
		}
		out = append(out, TargetOption{
			Agent:                 meta.Name,
			Enabled:               enabled,
			Installed:             installed,
			DispatchCapable:       dispatchCapable,
			RandomEligible:        randomEligible,
			RandomExclusionReason: exclusion,
			Streaming: StreamingOption{
				AnswerText: meta.AnswerText,
				Progress:   meta.Progress,
			},
			Model:              fieldOption(cfg, meta, true),
			ReasoningEffort:    fieldOption(cfg, meta, false),
			UnavailableReasons: reasons,
		})
	}
	return out
}

func unavailableReasons(enabled bool, installed bool) []string {
	if !enabled {
		return []string{"disabled"}
	}
	if !installed {
		return []string{"binary_not_found"}
	}
	return []string{}
}

func fieldOption(cfg config.Config, target targetMeta, model bool) FieldOption {
	var key string
	var supported bool
	var configured string
	if model {
		key = target.ModelKey
		supported = target.SupportsModel
		configured = configuredModel(cfg, target.Name)
	} else {
		key = target.ReasoningKey
		supported = target.SupportsReasoning
		configured = configuredReasoning(cfg, target.Name)
	}
	option := FieldOption{
		OverrideSupported: supported,
		Configured:        configured,
		Suggestions:       []string{},
	}
	if !supported {
		return option
	}
	field, ok := config.LookupField(key)
	if !ok {
		return option
	}
	option.AllowCustom = field.AllowCustom
	option.Suggestions = make([]string, 0, len(field.Options))
	for _, value := range field.Options {
		option.Suggestions = append(option.Suggestions, value.Value)
	}
	return option
}

func writeOptionsText(stdout io.Writer, options *OptionsResponse) error {
	if _, err := fmt.Fprintln(stdout, messages.DispatchOptionsHeader); err != nil {
		return err
	}
	if options.Caller.Known {
		if _, err := fmt.Fprintf(stdout, messages.DispatchOptionsCallerKnownFmt+"\n", options.Caller.Agent); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(stdout, messages.DispatchOptionsCallerUnknown); err != nil {
		return err
	}
	pool := strings.Join(options.Random.Pool, ", ")
	if pool == "" {
		pool = messages.DispatchOptionsNoRandomTargets
	}
	if _, err := fmt.Fprintf(stdout, messages.DispatchOptionsRandomPoolFmt+"\n", pool); err != nil {
		return err
	}
	for _, target := range options.Targets {
		if _, err := fmt.Fprintf(stdout, messages.DispatchOptionsTargetFmt+"\n", target.Agent, target.Enabled, target.Installed, target.DispatchCapable); err != nil {
			return err
		}
		if err := writeTargetDetails(stdout, target); err != nil {
			return err
		}
	}
	return nil
}

// writeTargetDetails emits the indented per-target detail block required by
// the spec § CLI: random eligibility plus exclusion reason, streaming
// capability, model + reasoning_effort metadata (configured value, override
// support, allow_custom, suggestions), and unavailable_reasons.
func writeTargetDetails(stdout io.Writer, target TargetOption) error {
	exclusion := ""
	if target.RandomExclusionReason != nil {
		exclusion = ", reason: " + *target.RandomExclusionReason
	}
	if _, err := fmt.Fprintf(stdout, "  random_eligible: %t%s\n", target.RandomEligible, exclusion); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "  streaming: answer_text=%s progress=%s\n", target.Streaming.AnswerText, target.Streaming.Progress); err != nil {
		return err
	}
	if err := writeFieldOption(stdout, "model", target.Model); err != nil {
		return err
	}
	if err := writeFieldOption(stdout, "reasoning_effort", target.ReasoningEffort); err != nil {
		return err
	}
	reasons := strings.Join(target.UnavailableReasons, ", ")
	if reasons == "" {
		reasons = messages.DispatchOptionsNoUnavailableReasons
	}
	if _, err := fmt.Fprintf(stdout, "  unavailable_reasons: [%s]\n", reasons); err != nil {
		return err
	}
	return nil
}

func writeFieldOption(stdout io.Writer, label string, field FieldOption) error {
	configured := field.Configured
	if configured == "" {
		configured = "none"
	}
	suggestions := strings.Join(field.Suggestions, ", ")
	_, err := fmt.Fprintf(
		stdout,
		"  %s: configured=%s override=%t allow_custom=%t suggestions=[%s]\n",
		label, configured, field.OverrideSupported, field.AllowCustom, suggestions,
	)
	return err
}

func stringPtr(value string) *string {
	return &value
}
