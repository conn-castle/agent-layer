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
	"github.com/conn-castle/agent-layer/internal/launchers"
	"github.com/conn-castle/agent-layer/internal/templates"
)

const (
	readinessCheckUnrecognizedConfigKeys  = "unrecognized_config_keys"
	readinessCheckVSCodeNoSyncStaleOutput = "vscode_no_sync_outputs_stale"
	readinessCheckFloatingDependencies    = "floating_external_dependency_specs"
	readinessCheckDisabledArtifacts       = "stale_disabled_agent_artifacts"
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

	checks := make([]UpgradeReadinessCheck, 0, 4)
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

	sortReadinessChecks(checks)
	return checks, nil
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
	if cfg.Agents.VSCode.Enabled == nil || !*cfg.Agents.VSCode.Enabled {
		return nil, nil
	}

	details := make([]string, 0)
	latestGenerated := time.Time{}

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
		{
			agent:   "claude",
			enabled: cfg.Agents.Claude.Enabled,
			files: []disabledArtifactFileSpec{{
				path:     filepath.Join(inst.root, ".mcp.json"),
				evidence: hasAgentLayerMCPSignature,
			}},
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
		{
			agent:   "vscode",
			enabled: cfg.Agents.VSCode.Enabled,
			files: []disabledArtifactFileSpec{
				{path: filepath.Join(inst.root, ".vscode", "settings.json"), evidence: hasVSCodeManagedBlock},
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
