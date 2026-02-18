package install

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/envfile"
	"github.com/conn-castle/agent-layer/internal/launchers"
	"github.com/conn-castle/agent-layer/internal/templates"
)

const (
	readinessCheckUnrecognizedConfigKeys        = "unrecognized_config_keys"
	readinessCheckUnresolvedPlaceholders        = "unresolved_config_placeholders"
	readinessCheckProcessEnvOverridesDotenv     = "process_env_overrides_dotenv"
	readinessCheckIgnoredEmptyDotenvAssignments = "ignored_empty_dotenv_assignments"
	readinessCheckPathExpansionAnomalies        = "path_expansion_anomalies"
	readinessCheckVSCodeNoSyncStaleOutput       = "vscode_no_sync_outputs_stale"
	readinessCheckFloatingDependencies          = "floating_external_dependency_specs"
	readinessCheckDisabledArtifacts             = "stale_disabled_agent_artifacts"
	readinessCheckGeneratedSecretRisk           = "generated_secret_risk"
)

const (
	generatedFileMarker = "GENERATED FILE"
	vscodeManagedStart  = "// >>> agent-layer"
	vscodeManagedEnd    = "// <<< agent-layer"
)

var floatingDependencyPattern = regexp.MustCompile(`(?i)@(latest|next|canary)\b`)

// UpgradeReadinessCheck captures a non-fatal pre-upgrade readiness finding for text output.
type UpgradeReadinessCheck struct {
	ID      string   `json:"id"`
	Summary string   `json:"summary"`
	Details []string `json:"details"`
}

func readinessErr(action string, path string, err error) error {
	return fmt.Errorf("readiness check failed to %s %s: %w", action, path, err)
}

func buildUpgradeReadinessChecks(inst *installer) ([]UpgradeReadinessCheck, error) {
	configPath := filepath.Join(inst.root, ".agent-layer", "config.toml")
	configInfo, err := inst.sys.Stat(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []UpgradeReadinessCheck{{
				ID:      readinessCheckUnrecognizedConfigKeys,
				Summary: "Config file is missing; config-based readiness checks were skipped.",
				Details: []string{filepath.ToSlash(inst.relativePath(configPath))},
			}}, nil
		}
		return nil, readinessErr("stat", configPath, err)
	}

	configBytes, err := inst.sys.ReadFile(configPath)
	if err != nil {
		return nil, readinessErr("read", configPath, err)
	}

	checks := make([]UpgradeReadinessCheck, 0, 9)
	if strictErr := decodeConfigStrict(configBytes); strictErr != nil {
		checks = append(checks, UpgradeReadinessCheck{
			ID:      readinessCheckUnrecognizedConfigKeys,
			Summary: "Config contains unrecognized or unsupported keys.",
			Details: []string{strictErr.Error()},
		})
	}

	cfg, parseErrDetail := decodeConfigLoose(configBytes)
	if parseErrDetail != "" {
		checks = append(checks, UpgradeReadinessCheck{
			ID:      readinessCheckUnrecognizedConfigKeys,
			Summary: "Config could not be parsed for readiness checks.",
			Details: []string{parseErrDetail},
		})
		sortReadinessChecks(checks)
		return checks, nil
	}

	envValues, err := readAgentLayerEnvForReadiness(inst)
	if err != nil {
		return nil, err
	}

	if check := detectUnresolvedConfigPlaceholders(&cfg, envValues, inst.root, inst.sys); check != nil {
		checks = append(checks, *check)
	}

	if check := detectProcessEnvOverridesDotenv(&cfg, envValues, inst.sys); check != nil {
		checks = append(checks, *check)
	}

	if check := detectIgnoredEmptyDotenvAssignments(&cfg, envValues, inst.sys); check != nil {
		checks = append(checks, *check)
	}

	if check, err := detectPathExpansionAnomalies(inst, &cfg, envValues); err != nil {
		return nil, err
	} else if check != nil {
		checks = append(checks, *check)
	}

	if check, err := detectVSCodeNoSyncStaleness(inst, &cfg, configPath, configInfo.ModTime()); err != nil {
		return nil, err
	} else if check != nil {
		checks = append(checks, *check)
	}

	if check := detectFloatingDependencies(&cfg); check != nil {
		checks = append(checks, *check)
	}

	if check, err := detectDisabledAgentArtifacts(inst, &cfg); err != nil {
		return nil, err
	} else if check != nil {
		checks = append(checks, *check)
	}

	if check, err := detectGeneratedSecretRisk(inst, string(configBytes)); err != nil {
		return nil, err
	} else if check != nil {
		checks = append(checks, *check)
	}

	sortReadinessChecks(checks)
	return checks, nil
}

func readAgentLayerEnvForReadiness(inst *installer) (map[string]string, error) {
	envPath := filepath.Join(inst.root, ".agent-layer", ".env")
	data, err := inst.sys.ReadFile(envPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, readinessErr("read", envPath, err)
	}
	parsed, parseErr := envfile.Parse(string(data))
	if parseErr != nil {
		return nil, readinessErr("parse", envPath, parseErr)
	}
	return filterAgentLayerEnvForReadiness(parsed), nil
}

func filterAgentLayerEnvForReadiness(env map[string]string) map[string]string {
	if len(env) == 0 {
		return map[string]string{}
	}
	filtered := make(map[string]string, len(env))
	for key, value := range env {
		if strings.HasPrefix(key, "AL_") {
			filtered[key] = value
		}
	}
	return filtered
}

func detectUnresolvedConfigPlaceholders(cfg *config.Config, env map[string]string, repoRoot string, sys System) *UpgradeReadinessCheck {
	details := make([]string, 0)
	for i, server := range cfg.MCP.Servers {
		if server.Enabled == nil || !*server.Enabled {
			continue
		}
		for _, name := range config.RequiredEnvVarsForMCPServer(server) {
			if _, ok := readinessEnvValue(name, env, repoRoot, sys); ok {
				continue
			}
			details = append(details, fmt.Sprintf("mcp.servers[%d] id=%q missing ${%s}", i, server.ID, name))
		}
	}
	if len(details) == 0 {
		return nil
	}
	sort.Strings(details)
	return &UpgradeReadinessCheck{
		ID:      readinessCheckUnresolvedPlaceholders,
		Summary: "Enabled MCP servers reference placeholders not available from env.",
		Details: details,
	}
}

func detectProcessEnvOverridesDotenv(cfg *config.Config, env map[string]string, sys System) *UpgradeReadinessCheck {
	required := requiredAgentLayerEnvVarsForEnabledServers(cfg)
	details := make([]string, 0)
	for _, key := range required {
		fileValue, exists := env[key]
		if !exists || fileValue == "" {
			continue
		}
		processValue, processSet := processEnvValue(sys, key)
		if !processSet || processValue == fileValue {
			continue
		}
		details = append(details, fmt.Sprintf("%s differs between process env and `.agent-layer/.env`", key))
	}
	if len(details) == 0 {
		return nil
	}
	sort.Strings(details)
	return &UpgradeReadinessCheck{
		ID:      readinessCheckProcessEnvOverridesDotenv,
		Summary: "Process environment values override `.agent-layer/.env` for active placeholders.",
		Details: details,
	}
}

func detectIgnoredEmptyDotenvAssignments(cfg *config.Config, env map[string]string, sys System) *UpgradeReadinessCheck {
	required := requiredAgentLayerEnvVarsForEnabledServers(cfg)
	details := make([]string, 0)
	for _, key := range required {
		fileValue, exists := env[key]
		if !exists || fileValue != "" {
			continue
		}
		_, processSet := processEnvValue(sys, key)
		if !processSet {
			continue
		}
		details = append(details, fmt.Sprintf("%s is empty in `.agent-layer/.env` and falls back to process env", key))
	}
	if len(details) == 0 {
		return nil
	}
	sort.Strings(details)
	return &UpgradeReadinessCheck{
		ID:      readinessCheckIgnoredEmptyDotenvAssignments,
		Summary: "Empty `.env` assignments are masked by process environment values.",
		Details: details,
	}
}

func detectPathExpansionAnomalies(inst *installer, cfg *config.Config, env map[string]string) (*UpgradeReadinessCheck, error) {
	details := make([]string, 0)
	for i, server := range cfg.MCP.Servers {
		if server.Enabled == nil || !*server.Enabled || server.Transport != config.TransportStdio {
			continue
		}
		commandDetail, err := checkPathExpansionValue(inst, env, i, server.ID, "command", server.Command, true)
		if err != nil {
			return nil, err
		}
		if commandDetail != "" {
			details = append(details, commandDetail)
		}
		for argIdx, arg := range server.Args {
			detail, err := checkPathExpansionValue(inst, env, i, server.ID, fmt.Sprintf("args[%d]", argIdx), arg, false)
			if err != nil {
				return nil, err
			}
			if detail != "" {
				details = append(details, detail)
			}
		}
	}
	if len(details) == 0 {
		return nil, nil
	}
	sort.Strings(details)
	return &UpgradeReadinessCheck{
		ID:      readinessCheckPathExpansionAnomalies,
		Summary: "Path-like MCP command values contain expansion anomalies.",
		Details: details,
	}, nil
}

func checkPathExpansionValue(inst *installer, env map[string]string, serverIndex int, serverID string, field string, rawValue string, commandField bool) (string, error) {
	if !config.ShouldExpandPath(rawValue) {
		return "", nil
	}
	substEnv := readinessSubstitutionEnv(rawValue, env, inst.root, inst.sys)
	resolved, err := config.SubstituteEnvVars(rawValue, substEnv)
	if err != nil {
		return fmt.Sprintf("mcp.servers[%d] id=%q %s has unresolved path placeholder: %v", serverIndex, serverID, field, err), nil
	}
	expanded, err := config.ExpandPathIfNeeded(rawValue, resolved, inst.root)
	if err != nil {
		return fmt.Sprintf("mcp.servers[%d] id=%q %s failed to expand path %q: %v", serverIndex, serverID, field, rawValue, err), nil
	}
	if !filepath.IsAbs(expanded) || strings.Contains(expanded, "${") || strings.HasPrefix(strings.TrimSpace(expanded), "~") {
		return fmt.Sprintf("mcp.servers[%d] id=%q %s did not expand cleanly: raw=%q expanded=%q", serverIndex, serverID, field, rawValue, expanded), nil
	}
	info, statErr := inst.sys.Stat(expanded)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return fmt.Sprintf("mcp.servers[%d] id=%q %s expands to missing path %q", serverIndex, serverID, field, expanded), nil
		}
		return "", readinessErr("stat", expanded, statErr)
	}
	if commandField && info.IsDir() {
		return fmt.Sprintf("mcp.servers[%d] id=%q %s expands to a directory, not an executable path: %q", serverIndex, serverID, field, expanded), nil
	}
	return "", nil
}

func requiredAgentLayerEnvVarsForEnabledServers(cfg *config.Config) []string {
	seen := make(map[string]struct{})
	for _, server := range cfg.MCP.Servers {
		if server.Enabled == nil || !*server.Enabled {
			continue
		}
		for _, key := range config.RequiredEnvVarsForMCPServer(server) {
			if strings.HasPrefix(key, "AL_") {
				seen[key] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func readinessSubstitutionEnv(raw string, env map[string]string, repoRoot string, sys System) map[string]string {
	names := config.ExtractEnvVarNames(raw)
	if len(names) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(names))
	for _, name := range names {
		if value, ok := readinessEnvValue(name, env, repoRoot, sys); ok {
			out[name] = value
		}
	}
	return out
}

func readinessEnvValue(name string, env map[string]string, repoRoot string, sys System) (string, bool) {
	if config.IsBuiltInEnvVar(name) {
		if repoRoot == "" {
			return "", false
		}
		return repoRoot, true
	}
	if value, ok := processEnvValue(sys, name); ok && value != "" {
		return value, true
	}
	if value, ok := env[name]; ok && value != "" {
		return value, true
	}
	return "", false
}

func processEnvValue(sys System, key string) (string, bool) {
	if sys == nil {
		return "", false
	}
	value, ok := sys.LookupEnv(key)
	if !ok || value == "" {
		return "", false
	}
	return value, true
}

func decodeConfigStrict(data []byte) error {
	var cfg config.Config
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(&cfg)
}

func decodeConfigLoose(data []byte) (config.Config, string) {
	var cfg config.Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return config.Config{}, err.Error()
	}
	return cfg, ""
}

func detectVSCodeNoSyncStaleness(inst *installer, cfg *config.Config, configPath string, configMTime time.Time) (*UpgradeReadinessCheck, error) {
	vscodeEnabled := cfg.Agents.VSCode.Enabled != nil && *cfg.Agents.VSCode.Enabled
	claudeVSCodeEnabled := cfg.Agents.ClaudeVSCode.Enabled != nil && *cfg.Agents.ClaudeVSCode.Enabled
	if !vscodeEnabled && !claudeVSCodeEnabled {
		return nil, nil
	}

	details := make([]string, 0)
	latestGenerated := time.Time{}

	// .vscode/mcp.json is only generated when agents.vscode is enabled (Codex MCP config).
	if vscodeEnabled {
		mcpPath := filepath.Join(inst.root, ".vscode", "mcp.json")
		mcpInfo, err := inst.sys.Stat(mcpPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				details = append(details, fmt.Sprintf("missing %s", filepath.ToSlash(inst.relativePath(mcpPath))))
			} else {
				return nil, readinessErr("stat", mcpPath, err)
			}
		} else if !mcpInfo.IsDir() {
			latestGenerated = maxModTime(latestGenerated, mcpInfo.ModTime())
		}
	}

	// .vscode/settings.json managed block is generated when either agent is enabled.
	settingsPath := filepath.Join(inst.root, ".vscode", "settings.json")
	settingsInfo, err := inst.sys.Stat(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			details = append(details, fmt.Sprintf("missing %s", filepath.ToSlash(inst.relativePath(settingsPath))))
		} else {
			return nil, readinessErr("stat", settingsPath, err)
		}
	} else if !settingsInfo.IsDir() {
		settingsData, readErr := inst.sys.ReadFile(settingsPath)
		if readErr != nil {
			return nil, readinessErr("read", settingsPath, readErr)
		}
		if strings.Contains(string(settingsData), vscodeManagedStart) && strings.Contains(string(settingsData), vscodeManagedEnd) {
			latestGenerated = maxModTime(latestGenerated, settingsInfo.ModTime())
		} else {
			details = append(details, fmt.Sprintf("missing Agent Layer managed block in %s", filepath.ToSlash(inst.relativePath(settingsPath))))
		}
	}

	// .vscode/prompts/ is only generated when agents.vscode is enabled.
	if vscodeEnabled {
		slashCount, err := countMarkdownFiles(inst, filepath.Join(inst.root, ".agent-layer", "slash-commands"))
		if err != nil {
			return nil, err
		}
		if slashCount > 0 {
			promptDir := filepath.Join(inst.root, ".vscode", "prompts")
			promptFiles, newestPrompt, promptErr := listGeneratedFilesWithSuffix(inst, promptDir, ".prompt.md")
			if promptErr != nil {
				return nil, promptErr
			}
			if len(promptFiles) == 0 {
				details = append(details, "missing generated VS Code prompt files under .vscode/prompts")
			} else {
				latestGenerated = maxModTime(latestGenerated, newestPrompt)
			}
		}
	}

	// .mcp.json and .claude/settings.json are generated when claude OR claude-vscode is enabled.
	// The Claude extension in VS Code depends on these, so they must be fresh for --no-sync.
	claudeEnabled := cfg.Agents.Claude.Enabled != nil && *cfg.Agents.Claude.Enabled
	if claudeEnabled || claudeVSCodeEnabled {
		claudeMCPPath := filepath.Join(inst.root, ".mcp.json")
		claudeMCPInfo, err := inst.sys.Stat(claudeMCPPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				details = append(details, fmt.Sprintf("missing %s", filepath.ToSlash(inst.relativePath(claudeMCPPath))))
			} else {
				return nil, readinessErr("stat", claudeMCPPath, err)
			}
		} else if !claudeMCPInfo.IsDir() {
			latestGenerated = maxModTime(latestGenerated, claudeMCPInfo.ModTime())
		}

		claudeSettingsPath := filepath.Join(inst.root, ".claude", "settings.json")
		claudeSettingsInfo, err := inst.sys.Stat(claudeSettingsPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				details = append(details, fmt.Sprintf("missing %s", filepath.ToSlash(inst.relativePath(claudeSettingsPath))))
			} else {
				return nil, readinessErr("stat", claudeSettingsPath, err)
			}
		} else if !claudeSettingsInfo.IsDir() {
			latestGenerated = maxModTime(latestGenerated, claudeSettingsInfo.ModTime())
		}
	}

	if !latestGenerated.IsZero() && configMTime.After(latestGenerated) {
		details = append(details, fmt.Sprintf(
			"%s is newer than generated VS Code outputs (config=%s, outputs=%s)",
			filepath.ToSlash(inst.relativePath(configPath)),
			configMTime.UTC().Format(time.RFC3339),
			latestGenerated.UTC().Format(time.RFC3339),
		))
	}

	if len(details) == 0 {
		return nil, nil
	}
	sort.Strings(details)
	return &UpgradeReadinessCheck{
		ID:      readinessCheckVSCodeNoSyncStaleOutput,
		Summary: "VS Code `--no-sync` launch path may use stale generated outputs.",
		Details: details,
	}, nil
}

func detectFloatingDependencies(cfg *config.Config) *UpgradeReadinessCheck {
	details := make([]string, 0)
	for i, server := range cfg.MCP.Servers {
		// Floating versions only matter for enabled servers because disabled servers are not launched.
		if server.Enabled == nil || !*server.Enabled {
			continue
		}
		details = append(details, floatingDetails(i, server.ID, "command", server.Command)...)
		details = append(details, floatingDetails(i, server.ID, "url", server.URL)...)
		for argIdx, arg := range server.Args {
			details = append(details, floatingDetails(i, server.ID, fmt.Sprintf("args[%d]", argIdx), arg)...)
		}
		envKeys := sortedMapKeys(server.Env)
		for _, envKey := range envKeys {
			details = append(details, floatingDetails(i, server.ID, fmt.Sprintf("env.%s", envKey), server.Env[envKey])...)
		}
	}

	if len(details) == 0 {
		return nil
	}
	sort.Strings(details)
	return &UpgradeReadinessCheck{
		ID:      readinessCheckFloatingDependencies,
		Summary: "Enabled MCP servers include floating dependency specs.",
		Details: details,
	}
}

func detectGeneratedSecretRisk(inst *installer, sourceConfigContent string) (*UpgradeReadinessCheck, error) {
	// If the source config (.agent-layer/config.toml) does not contain secret
	// literals, any secrets in generated files came from env var resolution
	// (e.g., ${AL_*} placeholders resolved by al sync). This is correct usage
	// and not actionable, so suppress the check.
	if !config.ContainsPotentialSecretLiteral(sourceConfigContent) {
		return nil, nil
	}
	paths := []string{
		filepath.Join(inst.root, ".codex", "config.toml"),
		filepath.Join(inst.root, ".mcp.json"),
		filepath.Join(inst.root, ".claude", "settings.json"),
		filepath.Join(inst.root, ".gemini", "settings.json"),
		filepath.Join(inst.root, ".vscode", "mcp.json"),
	}

	details := make([]string, 0)
	for _, path := range paths {
		info, err := inst.sys.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, readinessErr("stat", path, err)
		}
		if info.IsDir() {
			continue
		}
		data, err := inst.sys.ReadFile(path)
		if err != nil {
			return nil, readinessErr("read", path, err)
		}
		if !config.ContainsPotentialSecretLiteral(string(data)) {
			continue
		}
		details = append(details, filepath.ToSlash(inst.relativePath(path)))
	}

	if len(details) == 0 {
		return nil, nil
	}
	sort.Strings(details)
	return &UpgradeReadinessCheck{
		ID:      readinessCheckGeneratedSecretRisk,
		Summary: "Generated files appear to contain secret-like literals.",
		Details: details,
	}, nil
}

func floatingDetails(serverIndex int, serverID string, field string, value string) []string {
	if value == "" || !floatingDependencyPattern.MatchString(value) {
		return nil
	}
	return []string{fmt.Sprintf("mcp.servers[%d] id=%q %s=%q", serverIndex, serverID, field, value)}
}

type disabledArtifactFileSpec struct {
	path     string
	evidence func([]byte) (bool, error)
}

type disabledArtifactDirSpec struct {
	root   string
	suffix string
}

type disabledAgentArtifactRule struct {
	agent   string
	enabled *bool
	files   []disabledArtifactFileSpec
	dirs    []disabledArtifactDirSpec
}

func detectDisabledAgentArtifacts(inst *installer, cfg *config.Config) (*UpgradeReadinessCheck, error) {
	launcherPaths := launchers.VSCodePaths(inst.root)
	rules := []disabledAgentArtifactRule{
		{
			agent:   "gemini",
			enabled: cfg.Agents.Gemini.Enabled,
			files: []disabledArtifactFileSpec{{
				path:     filepath.Join(inst.root, ".gemini", "settings.json"),
				evidence: hasAgentLayerMCPSignature,
			}},
		},
		// .mcp.json and .claude/settings.json are generated when either agents.claude
		// or agents.claude-vscode is enabled.
		{
			agent:   "claude",
			enabled: combinedBoolOr(cfg.Agents.Claude.Enabled, cfg.Agents.ClaudeVSCode.Enabled),
			files: []disabledArtifactFileSpec{
				{path: filepath.Join(inst.root, ".mcp.json"), evidence: hasAgentLayerMCPSignature},
				{path: filepath.Join(inst.root, ".claude", "settings.json"), evidence: isJSONObject},
			},
		},
		{
			agent:   "codex",
			enabled: cfg.Agents.Codex.Enabled,
			files: []disabledArtifactFileSpec{
				{path: filepath.Join(inst.root, ".codex", "AGENTS.md"), evidence: hasGeneratedFileMarker},
				{path: filepath.Join(inst.root, ".codex", "config.toml"), evidence: hasGeneratedFileMarker},
				{path: filepath.Join(inst.root, ".codex", "rules", "default.rules"), evidence: hasGeneratedFileMarker},
			},
			dirs: []disabledArtifactDirSpec{
				{root: filepath.Join(inst.root, ".codex", "skills"), suffix: "SKILL.md"},
			},
		},
		{
			agent:   "antigravity",
			enabled: cfg.Agents.Antigravity.Enabled,
			dirs: []disabledArtifactDirSpec{
				{root: filepath.Join(inst.root, ".agent", "skills"), suffix: "SKILL.md"},
			},
		},
		// .vscode/settings.json is generated when either agents.vscode or agents.claude-vscode is enabled.
		{
			agent:   "vscode",
			enabled: combinedBoolOr(cfg.Agents.VSCode.Enabled, cfg.Agents.ClaudeVSCode.Enabled),
			files: []disabledArtifactFileSpec{
				{path: filepath.Join(inst.root, ".vscode", "settings.json"), evidence: hasVSCodeManagedBlock},
			},
		},
		// Prompts and launchers are only generated when agents.vscode is enabled.
		{
			agent:   "vscode",
			enabled: cfg.Agents.VSCode.Enabled,
			files: []disabledArtifactFileSpec{
				{path: launcherPaths.Command, evidence: exactTemplateMatcher("launchers/open-vscode.command")},
				{path: launcherPaths.Shell, evidence: exactTemplateMatcher("launchers/open-vscode.sh")},
				{path: launcherPaths.Desktop, evidence: exactTemplateMatcher("launchers/open-vscode.desktop")},
				{path: launcherPaths.AppInfoPlist, evidence: exactTemplateMatcher("launchers/open-vscode.app/Contents/Info.plist")},
				{path: launcherPaths.AppExec, evidence: exactTemplateMatcher("launchers/open-vscode.app/Contents/MacOS/open-vscode")},
			},
			dirs: []disabledArtifactDirSpec{
				{root: filepath.Join(inst.root, ".vscode", "prompts"), suffix: ".prompt.md"},
			},
		},
	}

	details := make([]string, 0)
	for _, rule := range rules {
		if rule.enabled != nil && *rule.enabled {
			continue
		}
		for _, file := range rule.files {
			if err := appendDisabledArtifactDetail(inst, &details, rule.agent, file.path, file.evidence); err != nil {
				return nil, err
			}
		}
		for _, dir := range rule.dirs {
			paths, _, err := listGeneratedFilesWithSuffix(inst, dir.root, dir.suffix)
			if err != nil {
				return nil, err
			}
			for _, path := range paths {
				details = append(details, fmt.Sprintf("%s: %s", rule.agent, filepath.ToSlash(inst.relativePath(path))))
			}
		}
	}

	if len(details) == 0 {
		return nil, nil
	}
	sort.Strings(details)
	return &UpgradeReadinessCheck{
		ID:      readinessCheckDisabledArtifacts,
		Summary: "Disabled agents still have generated artifacts on disk.",
		Details: details,
	}, nil
}

func appendDisabledArtifactDetail(inst *installer, details *[]string, agent string, absPath string, evidence func([]byte) (bool, error)) error {
	info, err := inst.sys.Stat(absPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return readinessErr("stat", absPath, err)
	}
	if info.IsDir() {
		return nil
	}
	if evidence != nil {
		data, readErr := inst.sys.ReadFile(absPath)
		if readErr != nil {
			return readinessErr("read", absPath, readErr)
		}
		matched, evidenceErr := evidence(data)
		if evidenceErr != nil {
			return evidenceErr
		}
		if !matched {
			return nil
		}
	}
	*details = append(*details, fmt.Sprintf("%s: %s", agent, filepath.ToSlash(inst.relativePath(absPath))))
	return nil
}

func hasGeneratedFileMarker(data []byte) (bool, error) {
	return strings.Contains(string(data), generatedFileMarker), nil
}

func hasVSCodeManagedBlock(data []byte) (bool, error) {
	content := string(data)
	return strings.Contains(content, vscodeManagedStart) && strings.Contains(content, vscodeManagedEnd), nil
}

func hasAgentLayerMCPSignature(data []byte) (bool, error) {
	content := string(data)
	return strings.Contains(content, "\"mcpServers\"") &&
		strings.Contains(content, "\"agent-layer\"") &&
		strings.Contains(content, "\"mcp-prompts\""), nil
}

// isJSONObject reports whether data looks like a JSON object.
// .claude/settings.json is wholly generated by al sync and always contains a
// JSON object (with or without a "permissions" key). This is sufficient provenance
// because the check only fires when both Claude agents are disabled.
func isJSONObject(data []byte) (bool, error) {
	trimmed := bytes.TrimSpace(data)
	return len(trimmed) > 0 && trimmed[0] == '{', nil
}

func exactTemplateMatcher(templatePath string) func([]byte) (bool, error) {
	return func(content []byte) (bool, error) {
		templateData, err := templates.Read(templatePath)
		if err != nil {
			return false, readinessErr("read embedded template", templatePath, err)
		}
		return bytes.Equal(content, templateData), nil
	}
}

func sortReadinessChecks(checks []UpgradeReadinessCheck) {
	sort.Slice(checks, func(i, j int) bool {
		return checks[i].ID < checks[j].ID
	})
}

func countMarkdownFiles(inst *installer, root string) (int, error) {
	if _, err := inst.sys.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, readinessErr("stat", root, err)
	}

	count := 0
	err := inst.sys.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), ".md") {
			count++
		}
		return nil
	})
	if err != nil {
		return 0, readinessErr("walk", root, err)
	}
	return count, nil
}

func listGeneratedFilesWithSuffix(inst *installer, root string, suffix string) ([]string, time.Time, error) {
	if _, err := inst.sys.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, time.Time{}, nil
		}
		return nil, time.Time{}, readinessErr("stat", root, err)
	}

	paths := make([]string, 0)
	latest := time.Time{}
	err := inst.sys.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(entry.Name(), suffix) {
			return nil
		}
		data, err := inst.sys.ReadFile(path)
		if err != nil {
			return readinessErr("read", path, err)
		}
		if !strings.Contains(string(data), generatedFileMarker) {
			return nil
		}
		info, err := inst.sys.Stat(path)
		if err != nil {
			return readinessErr("stat", path, err)
		}
		paths = append(paths, path)
		latest = maxModTime(latest, info.ModTime())
		return nil
	})
	if err != nil {
		return nil, time.Time{}, readinessErr("walk", root, err)
	}
	sort.Strings(paths)
	return paths, latest, nil
}

func maxModTime(current time.Time, candidate time.Time) time.Time {
	if current.IsZero() || candidate.After(current) {
		return candidate
	}
	return current
}

// combinedBoolOr returns a pointer to true if either a or b is non-nil and true.
func combinedBoolOr(a, b *bool) *bool {
	v := (a != nil && *a) || (b != nil && *b)
	return &v
}

func sortedMapKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
