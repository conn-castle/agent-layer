package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/clients"
	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/projection"
	"github.com/conn-castle/agent-layer/internal/sync"
)

var (
	resolvePromptServerCommandFunc = func(root string) (string, []string, error) {
		return sync.ResolvePromptServerCommand(sync.RealSystem{}, root)
	}
	resolvePromptServerEnvFunc = sync.ResolvePromptServerEnv
)

// CheckPromptServer verifies the internal MCP prompt server can be resolved.
// Args: root is the repo root; cfg is the loaded project config.
// Returns: one Result describing the check outcome.
func CheckPromptServer(root string, cfg *config.ProjectConfig) []Result {
	if cfg == nil {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptServer,
			Message:        messages.DoctorPromptServerConfigMissing,
			Recommendation: messages.DoctorPromptServerConfigRecommend,
		}}
	}

	claudeEnabled := agentEnabled(cfg.Config.Agents.Claude.Enabled) || agentEnabled(cfg.Config.Agents.ClaudeVSCode.Enabled)
	geminiEnabled := agentEnabled(cfg.Config.Agents.Gemini.Enabled)
	if !claudeEnabled && !geminiEnabled {
		return []Result{{
			Status:    StatusOK,
			CheckName: messages.DoctorCheckNamePromptServer,
			Message:   messages.DoctorPromptServerNotRequired,
		}}
	}

	command, args, err := resolvePromptServerCommandFunc(root)
	if err != nil {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptServer,
			Message:        fmt.Sprintf(messages.DoctorPromptServerResolveFailedFmt, err),
			Recommendation: messages.DoctorPromptServerResolveRecommend,
		}}
	}
	if strings.TrimSpace(command) == "" {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptServer,
			Message:        fmt.Sprintf(messages.DoctorPromptServerResolveFailedFmt, "resolved empty command"),
			Recommendation: messages.DoctorPromptServerResolveRecommend,
		}}
	}

	env, err := resolvePromptServerEnvFunc(root)
	if err != nil {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptServer,
			Message:        fmt.Sprintf(messages.DoctorPromptServerEnvFailedFmt, err),
			Recommendation: messages.DoctorPromptServerEnvRecommend,
		}}
	}

	repoRoot := strings.TrimSpace(env[config.BuiltinRepoRootEnvVar])
	if repoRoot == "" {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptServer,
			Message:        messages.DoctorPromptServerMissingRepoRoot,
			Recommendation: messages.DoctorPromptServerEnvRecommend,
		}}
	}

	return []Result{{
		Status:    StatusOK,
		CheckName: messages.DoctorCheckNamePromptServer,
		Message:   fmt.Sprintf(messages.DoctorPromptServerResolvedFmt, formatPromptServerCommand(command, args)),
	}}
}

// CheckPromptServerConfig verifies generated client config files keep the
// internal MCP prompt server entry in sync with the current resolution.
// Args: root is the repo root; cfg is the loaded project config.
// Returns: one or more Results describing each client config check.
func CheckPromptServerConfig(root string, cfg *config.ProjectConfig) []Result {
	if cfg == nil {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptConfig,
			Message:        messages.DoctorPromptServerConfigMissing,
			Recommendation: messages.DoctorPromptServerConfigRecommend,
		}}
	}

	claudeEnabled := agentEnabled(cfg.Config.Agents.Claude.Enabled) || agentEnabled(cfg.Config.Agents.ClaudeVSCode.Enabled)
	geminiEnabled := agentEnabled(cfg.Config.Agents.Gemini.Enabled)
	if !claudeEnabled && !geminiEnabled {
		return []Result{{
			Status:    StatusOK,
			CheckName: messages.DoctorCheckNamePromptConfig,
			Message:   messages.DoctorPromptServerConfigNotRequired,
		}}
	}

	command, args, err := resolvePromptServerCommandFunc(root)
	if err != nil {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptConfig,
			Message:        fmt.Sprintf(messages.DoctorPromptServerConfigResolveFailedFmt, err),
			Recommendation: messages.DoctorPromptServerResolveRecommend,
		}}
	}
	if strings.TrimSpace(command) == "" {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptConfig,
			Message:        fmt.Sprintf(messages.DoctorPromptServerConfigResolveFailedFmt, "resolved empty command"),
			Recommendation: messages.DoctorPromptServerResolveRecommend,
		}}
	}
	env, err := resolvePromptServerEnvFunc(root)
	if err != nil {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptConfig,
			Message:        fmt.Sprintf(messages.DoctorPromptServerConfigResolveFailedFmt, err),
			Recommendation: messages.DoctorPromptServerEnvRecommend,
		}}
	}

	expected := promptServerSpec{
		Transport: config.TransportStdio,
		Command:   command,
		Args:      args,
		Env:       copyOrderedEnv(env),
	}

	approvals := projection.BuildApprovals(cfg.Config, cfg.CommandsAllow)
	geminiTrust := approvals.AllowMCP

	results := make([]Result, 0, 2)
	if claudeEnabled {
		results = append(results, checkPromptServerMCPConfig(root, expected))
	}
	if geminiEnabled {
		expectedGemini := expected
		expectedGemini.Trust = &geminiTrust
		results = append(results, checkPromptServerGeminiConfig(root, expectedGemini))
	}
	return results
}

type promptServerSpec struct {
	Transport string
	Command   string
	Args      []string
	Env       map[string]string
	Trust     *bool
}

type mcpConfigFile struct {
	Servers map[string]mcpServerFile `json:"mcpServers"`
}

type mcpServerFile struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

type geminiSettingsFile struct {
	MCPServers map[string]geminiServerFile `json:"mcpServers"`
}

type geminiServerFile struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Trust   *bool             `json:"trust"`
}

var (
	errPromptServerConfigInvalid      = errors.New("prompt server config invalid")
	errPromptServerConfigMissingEntry = errors.New("prompt server entry missing")
)

// checkPromptServerMCPConfig validates the internal prompt server entry in .mcp.json.
// Args: root is the repo root; expected is the resolved prompt server spec.
// Returns a Result describing the check outcome.
func checkPromptServerMCPConfig(root string, expected promptServerSpec) Result {
	path := filepath.Join(root, ".mcp.json")
	spec, err := readMCPPromptServerSpec(path)
	if err != nil {
		return promptServerConfigFailure(path, err)
	}

	if mismatch := comparePromptServerSpec(spec, expected); mismatch != "" {
		return Result{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptConfig,
			Message:        fmt.Sprintf(messages.DoctorPromptServerConfigMismatchFmt, filepath.ToSlash(path), mismatch),
			Recommendation: messages.DoctorPromptServerConfigFilesRecommend,
		}
	}

	return Result{
		Status:    StatusOK,
		CheckName: messages.DoctorCheckNamePromptConfig,
		Message:   fmt.Sprintf(messages.DoctorPromptServerConfigMatchFmt, filepath.ToSlash(path)),
	}
}

// checkPromptServerGeminiConfig validates the internal prompt server entry in .gemini/settings.json.
// Args: root is the repo root; expected is the resolved prompt server spec.
// Returns a Result describing the check outcome.
func checkPromptServerGeminiConfig(root string, expected promptServerSpec) Result {
	path := filepath.Join(root, ".gemini", "settings.json")
	spec, err := readGeminiPromptServerSpec(path)
	if err != nil {
		return promptServerConfigFailure(path, err)
	}

	expected.Transport = ""
	if mismatch := comparePromptServerSpec(spec, expected); mismatch != "" {
		return Result{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptConfig,
			Message:        fmt.Sprintf(messages.DoctorPromptServerConfigMismatchFmt, filepath.ToSlash(path), mismatch),
			Recommendation: messages.DoctorPromptServerConfigFilesRecommend,
		}
	}

	return Result{
		Status:    StatusOK,
		CheckName: messages.DoctorCheckNamePromptConfig,
		Message:   fmt.Sprintf(messages.DoctorPromptServerConfigMatchFmt, filepath.ToSlash(path)),
	}
}

// readMCPPromptServerSpec loads the internal prompt server entry from .mcp.json.
// Args: path is the .mcp.json file path.
// Returns the parsed spec or an error describing the failure.
func readMCPPromptServerSpec(path string) (promptServerSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return promptServerSpec{}, err
	}

	var cfg mcpConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return promptServerSpec{}, fmt.Errorf("%w: %v", errPromptServerConfigInvalid, err)
	}
	server, ok := cfg.Servers["agent-layer"]
	if !ok {
		return promptServerSpec{}, errPromptServerConfigMissingEntry
	}

	return promptServerSpec{
		Transport: strings.TrimSpace(server.Type),
		Command:   strings.TrimSpace(server.Command),
		Args:      server.Args,
		Env:       server.Env,
	}, nil
}

// readGeminiPromptServerSpec loads the internal prompt server entry from .gemini/settings.json.
// Args: path is the settings.json file path.
// Returns the parsed spec or an error describing the failure.
func readGeminiPromptServerSpec(path string) (promptServerSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return promptServerSpec{}, err
	}

	var cfg geminiSettingsFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return promptServerSpec{}, fmt.Errorf("%w: %v", errPromptServerConfigInvalid, err)
	}
	server, ok := cfg.MCPServers["agent-layer"]
	if !ok {
		return promptServerSpec{}, errPromptServerConfigMissingEntry
	}

	return promptServerSpec{
		Command: strings.TrimSpace(server.Command),
		Args:    server.Args,
		Env:     server.Env,
		Trust:   server.Trust,
	}, nil
}

// promptServerConfigFailure maps prompt server config load errors into doctor Results.
// Args: path is the config path; err is the underlying error.
// Returns a Result describing the failure.
func promptServerConfigFailure(path string, err error) Result {
	if errors.Is(err, os.ErrNotExist) {
		return Result{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptConfig,
			Message:        fmt.Sprintf(messages.DoctorPromptServerConfigMissingFmt, filepath.ToSlash(path)),
			Recommendation: messages.DoctorPromptServerConfigFilesRecommend,
		}
	}

	if errors.Is(err, errPromptServerConfigMissingEntry) {
		return Result{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptConfig,
			Message:        fmt.Sprintf(messages.DoctorPromptServerConfigMissingServerFmt, filepath.ToSlash(path)),
			Recommendation: messages.DoctorPromptServerConfigFilesRecommend,
		}
	}

	if errors.Is(err, errPromptServerConfigInvalid) {
		return Result{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNamePromptConfig,
			Message:        fmt.Sprintf(messages.DoctorPromptServerConfigInvalidFmt, filepath.ToSlash(path), err),
			Recommendation: messages.DoctorPromptServerConfigFilesRecommend,
		}
	}

	return Result{
		Status:         StatusFail,
		CheckName:      messages.DoctorCheckNamePromptConfig,
		Message:        fmt.Sprintf(messages.DoctorPromptServerConfigReadFailedFmt, filepath.ToSlash(path), err),
		Recommendation: messages.DoctorPromptServerConfigFilesRecommend,
	}
}

// comparePromptServerSpec compares actual vs expected prompt server specs.
// Args: actual is the parsed file spec; expected is the resolved spec.
// Returns: a descriptive mismatch string, or empty when they match.
func comparePromptServerSpec(actual promptServerSpec, expected promptServerSpec) string {
	var diffs []string
	if expected.Transport != "" && !strings.EqualFold(strings.TrimSpace(actual.Transport), expected.Transport) {
		diffs = append(diffs, fmt.Sprintf("transport expected %q got %q", expected.Transport, actual.Transport))
	}
	if actual.Command != expected.Command {
		diffs = append(diffs, fmt.Sprintf("command expected %q got %q", expected.Command, actual.Command))
	}
	if !equalStringSlice(actual.Args, expected.Args) {
		diffs = append(diffs, fmt.Sprintf("args expected %v got %v", expected.Args, actual.Args))
	}
	actualEnv := normalizeRepoRootEnv(actual.Env)
	expectedEnv := normalizeRepoRootEnv(expected.Env)
	if !equalStringMap(actualEnv, expectedEnv) {
		if summary := envMismatchSummary(expectedEnv, actualEnv); summary != "" {
			diffs = append(diffs, summary)
		}
	}
	if expected.Trust != nil {
		actualValue := "nil"
		if actual.Trust != nil {
			actualValue = fmt.Sprintf("%t", *actual.Trust)
		}
		if actual.Trust == nil || *actual.Trust != *expected.Trust {
			diffs = append(diffs, fmt.Sprintf("trust expected %t got %s", *expected.Trust, actualValue))
		}
	}
	return strings.Join(diffs, "; ")
}

// normalizeRepoRootEnv returns a copy of env with AL_REPO_ROOT canonicalized.
// Args: env is the env map to normalize.
// Returns: a copy of env with AL_REPO_ROOT resolved, or nil when env is nil.
func normalizeRepoRootEnv(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		if key == config.BuiltinRepoRootEnvVar {
			if strings.TrimSpace(value) == "" {
				out[key] = value
				continue
			}
			out[key] = clients.ResolvePath(value)
			continue
		}
		out[key] = value
	}
	return out
}

// envMismatchSummary describes env mismatches without exposing values.
// Args: expected and actual are the env maps to compare.
// Returns: a descriptive summary string when mismatches exist, otherwise empty.
func envMismatchSummary(expected map[string]string, actual map[string]string) string {
	expected = normalizeNilMap(expected)
	actual = normalizeNilMap(actual)

	var missing []string
	var extra []string
	var different []string
	for key, expectedValue := range expected {
		actualValue, ok := actual[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		if actualValue != expectedValue {
			different = append(different, key)
		}
	}
	for key := range actual {
		if _, ok := expected[key]; !ok {
			extra = append(extra, key)
		}
	}

	if len(missing) == 0 && len(extra) == 0 && len(different) == 0 {
		return ""
	}

	sort.Strings(missing)
	sort.Strings(extra)
	sort.Strings(different)

	parts := make([]string, 0, 3)
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf("missing keys %s", strings.Join(missing, ", ")))
	}
	if len(extra) > 0 {
		parts = append(parts, fmt.Sprintf("extra keys %s", strings.Join(extra, ", ")))
	}
	if len(different) > 0 {
		parts = append(parts, fmt.Sprintf("different values for %s", strings.Join(different, ", ")))
	}
	return fmt.Sprintf("env %s", strings.Join(parts, "; "))
}

// normalizeNilMap returns a non-nil env map for comparison.
// Args: env is the map to normalize.
// Returns: an empty map when env is nil, otherwise the original map.
func normalizeNilMap(env map[string]string) map[string]string {
	if env == nil {
		return map[string]string{}
	}
	return env
}

// equalStringSlice returns true when both slices have identical values in order.
// Args: left and right are the slices to compare.
// Returns: true when they match element-for-element.
func equalStringSlice(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i, value := range left {
		if value != right[i] {
			return false
		}
	}
	return true
}

// equalStringMap returns true when both maps contain identical key/value pairs.
// Args: left and right are the maps to compare.
// Returns: true when they match key-for-key and value-for-value.
func equalStringMap(left map[string]string, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, leftValue := range left {
		rightValue, ok := right[key]
		if !ok || rightValue != leftValue {
			return false
		}
	}
	return true
}

// copyOrderedEnv clones an OrderedMap into a standard string map.
// Args: env is an ordered map of environment values.
// Returns: a standard map copy, or nil when env is empty.
func copyOrderedEnv(env sync.OrderedMap[string]) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for key, value := range env {
		out[key] = value
	}
	return out
}

// agentEnabled returns true when a bool pointer is non-nil and set to true.
// Args: enabled is the optional flag pointer.
// Returns: true when enabled is set and true.
func agentEnabled(enabled *bool) bool {
	return enabled != nil && *enabled
}

// formatPromptServerCommand joins command + args into a human-readable string.
// Args: command is the resolved executable; args are its arguments.
// Returns: a space-joined command string with empty segments removed.
func formatPromptServerCommand(command string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	if trimmed := strings.TrimSpace(command); trimmed != "" {
		parts = append(parts, trimmed)
	}
	for _, arg := range args {
		if strings.TrimSpace(arg) == "" {
			continue
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}
