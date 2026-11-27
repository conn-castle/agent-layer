package agentdispatch

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/conn-castle/agent-layer/internal/agentoptions"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/quota"
)

const (
	// ExitQuotaPartial means at least one reading succeeded and at least one did not.
	ExitQuotaPartial = 84
	// ExitQuotaNone means there were no valid readings.
	ExitQuotaNone = 85

	// DefaultQuotaTimeout bounds subscription telemetry by default.
	DefaultQuotaTimeout = 15 * time.Second
	// MaxQuotaTimeout is the longest supported telemetry query.
	MaxQuotaTimeout = time.Minute

	// DefaultQuotaRetries counts extra HTTP attempts per quota read.
	DefaultQuotaRetries = 3
	// MaxQuotaRetries bounds additional attempts for each HTTP read.
	MaxQuotaRetries = 5
)

var quotaSelector = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

// QuotaSelection is a validated set of project agents, detached profiles and model/account checks.
type QuotaSelection struct {
	sources  []quota.Source
	expected map[string]string
	models   map[string][]string
	project  bool
}

// ParseQuotaSelection checks the command's selectors before any provider reads.
func ParseQuotaSelection(agents, profiles, expected, models []string, timeout time.Duration, retries int) (QuotaSelection, error) {
	if timeout <= 0 || timeout > MaxQuotaTimeout {
		return QuotaSelection{}, exitError(ExitUsage, "quota timeout must be greater than zero and at most 1m")
	}
	if retries < 0 || retries > MaxQuotaRetries {
		return QuotaSelection{}, exitError(ExitUsage, "quota --retries must be between 0 and 5")
	}
	selection := QuotaSelection{
		expected: make(map[string]string), models: make(map[string][]string),
		project: len(agents) > 0 || len(profiles) == 0,
	}
	for _, name := range agents {
		target, ok := lookupTarget(name)
		if !ok {
			return QuotaSelection{}, exitError(ExitUsage, "unknown dispatch agent in --agent")
		}
		selection.sources = append(selection.sources, quota.Source{Selector: target.Name, Agent: target.Name})
	}
	for _, argument := range profiles {
		label, value, assigned := strings.Cut(argument, "=")
		agent, file, split := strings.Cut(value, ":")
		target, ok := lookupTarget(agent)
		if !assigned || !split || !quotaSelector.MatchString(label) || !ok || strings.TrimSpace(file) == "" || strings.ContainsRune(file, 0) {
			return QuotaSelection{}, exitError(ExitUsage, "quota profile must be LABEL=AGENT:FILE")
		}
		selection.sources = append(selection.sources, quota.Source{Selector: label, Agent: target.Name, File: file})
	}
	selectors := make(map[string]bool)
	agentSelectors := make(map[string]bool)
	for _, source := range selection.sources {
		if selectors[source.Selector] {
			return QuotaSelection{}, exitError(ExitUsage, "quota selectors must be unique")
		}
		selectors[source.Selector] = true
		if source.File == "" {
			agentSelectors[source.Selector] = true
		}
	}
	if len(selection.sources) == 0 {
		for _, target := range targetRegistry() {
			selectors[target.Name] = true
			agentSelectors[target.Name] = true
		}
	}
	for _, argument := range expected {
		key, value, ok := quotaAssignment(selectors, agentSelectors, argument)
		if !ok || selection.expected[key] != "" {
			return QuotaSelection{}, exitError(ExitUsage, "quota --expect-account requires one SELECTOR=ID per selected account")
		}
		selection.expected[key] = value
	}
	for _, argument := range models {
		key, value, ok := quotaAssignment(selectors, agentSelectors, argument)
		if !ok {
			return QuotaSelection{}, exitError(ExitUsage, "quota --model requires SELECTOR=MODEL for a selected account")
		}
		selection.models[key] = append(selection.models[key], value)
	}
	return selection, nil
}

func quotaAssignment(selectors, agentSelectors map[string]bool, argument string) (string, string, bool) {
	key, value, assigned := strings.Cut(argument, "=")
	if !assigned || strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\x00\r\n") {
		return "", "", false
	}
	if !selectors[key] {
		target, ok := lookupTarget(key)
		if !ok || !agentSelectors[target.Name] {
			return "", "", false
		}
		key = target.Name
	}
	return key, value, true
}

// NeedsProject reports whether quota reads need the current Agent Layer project.
func (s QuotaSelection) NeedsProject() bool { return s.project }

// Sources binds project requests to enabled agents and their configured models.
func (s QuotaSelection) Sources(project *config.ProjectConfig) ([]quota.Source, error) {
	sources := append([]quota.Source(nil), s.sources...)
	if len(sources) == 0 && project != nil {
		for _, target := range targetRegistry() {
			if targetEnabled(project.Config, target.Name) {
				sources = append(sources, quota.Source{Selector: target.Name, Agent: target.Name})
			}
		}
	}
	matched := make(map[string]bool)
	for i := range sources {
		source := &sources[i]
		matched[source.Selector] = true
		source.Expected = s.expected[source.Selector]
		source.Models = s.models[source.Selector]
		if source.File == "" && project != nil {
			source.ConfiguredModel = agentoptions.ConfiguredValue(project.Config, source.Agent, agentoptions.KindModel)
			source.Disabled = !targetEnabled(project.Config, source.Agent)
		}
	}
	for selector := range s.expected {
		if !matched[selector] {
			return nil, exitError(ExitUsage, "quota --expect-account names an agent that is not enabled")
		}
	}
	for selector := range s.models {
		if !matched[selector] {
			return nil, exitError(ExitUsage, "quota --model names an agent that is not enabled")
		}
	}
	if project == nil && s.project {
		return nil, errors.New("quota project is required")
	}
	return sources, nil
}
