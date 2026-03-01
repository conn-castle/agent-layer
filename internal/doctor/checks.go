package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skillvalidator"
)

var (
	loadConfigLenientFunc = config.LoadConfigLenient
	loadEnvFunc           = config.LoadEnv
)

// CheckStructure verifies that the required project directories exist.
func CheckStructure(root string) []Result {
	var results []Result
	paths := []string{".agent-layer", "docs/agent-layer"}

	for _, p := range paths {
		fullPath := filepath.Join(root, p)
		info, err := os.Stat(fullPath)
		if err != nil {
			results = append(results, Result{
				Status:         StatusFail,
				CheckName:      messages.DoctorCheckNameStructure,
				Message:        fmt.Sprintf(messages.DoctorMissingRequiredDirFmt, p),
				Recommendation: messages.DoctorMissingRequiredDirRecommend,
			})
			continue
		}
		if !info.IsDir() {
			results = append(results, Result{
				Status:         StatusFail,
				CheckName:      messages.DoctorCheckNameStructure,
				Message:        fmt.Sprintf(messages.DoctorPathNotDirFmt, p),
				Recommendation: messages.DoctorPathNotDirRecommend,
			})
			continue
		}
		results = append(results, Result{
			Status:    StatusOK,
			CheckName: messages.DoctorCheckNameStructure,
			Message:   fmt.Sprintf(messages.DoctorDirExistsFmt, p),
		})
	}
	return results
}

// CheckConfig validates that the configuration file can be loaded and parsed.
// When strict loading fails but lenient loading succeeds (e.g., missing required
// fields from a newer version), CheckConfig returns a FAIL result with the
// validation error AND the leniently-loaded config so downstream checks still run.
func CheckConfig(root string) ([]Result, *config.ProjectConfig) {
	var results []Result
	cfg, err := config.LoadProjectConfig(root)
	if err != nil {
		if !errors.Is(err, config.ErrConfigValidation) {
			// Non-validation failure (env, instructions, skills, etc.) —
			// lenient config fallback would not help.
			results = append(results, Result{
				Status:         StatusFail,
				CheckName:      messages.DoctorCheckNameConfig,
				Message:        fmt.Sprintf(messages.DoctorConfigLoadFailedFmt, err),
				Recommendation: messages.DoctorConfigLoadRecommend,
			})
			return results, nil
		}

		// Config has validation errors. Try lenient loading so downstream
		// checks (secrets, agents) can still run.
		configPath := filepath.Join(root, ".agent-layer", "config.toml")
		lenientCfg, lenientErr := loadConfigLenientFunc(configPath)
		if lenientErr != nil {
			// TOML syntax error or file unreadable — can't recover.
			results = append(results, Result{
				Status:         StatusFail,
				CheckName:      messages.DoctorCheckNameConfig,
				Message:        fmt.Sprintf(messages.DoctorConfigLoadFailedFmt, lenientErr),
				Recommendation: messages.DoctorConfigLoadRecommend,
			})
			return results, nil
		}

		// Lenient loading succeeded — report the validation error but return
		// a partial config so downstream checks can still run.
		configRelPath := relPathForDoctor(root, configPath)
		message := fmt.Sprintf(messages.DoctorConfigLoadFailedFmt, err)
		recommendation := messages.DoctorConfigLoadLenientRecommend
		if details, detailsErr := configUnknownKeys(configPath); detailsErr == nil && len(details) > 0 {
			message = fmt.Sprintf(messages.DoctorConfigLoadFailedFmt, summarizeUnknownKeys(details))
			recommendation = formatUnknownKeyRecommendation(configRelPath, details)
		}
		results = append(results, Result{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameConfig,
			Message:        message,
			Recommendation: recommendation,
		})
		partial := &config.ProjectConfig{Config: *lenientCfg, Root: root}

		// Best-effort: load .env so CheckSecrets can check against it.
		// Always inject built-in env vars (e.g., AL_REPO_ROOT) so downstream
		// checks like MCP server resolution don't produce false positives.
		envPath := filepath.Join(root, ".agent-layer", ".env")
		var env map[string]string
		if loaded, envErr := loadEnvFunc(envPath); envErr == nil {
			env = loaded
		}
		partial.Env = config.WithBuiltInEnv(env, root)

		// Best-effort: load skills so CheckSkills does not incorrectly report
		// "No skills configured" when lenient config fallback is active.
		paths := config.DefaultPaths(root)
		skillsRelPath := relPathForDoctor(root, paths.SkillsDir)
		skillsDirInfo, statErr := os.Stat(paths.SkillsDir)
		switch {
		case errors.Is(statErr, os.ErrNotExist):
			// Missing skills directory is valid for repos with no skills.
		case statErr != nil:
			results = append(results, Result{
				Status:         StatusFail,
				CheckName:      messages.DoctorCheckNameSkills,
				Message:        fmt.Sprintf(messages.DoctorSkillsLoadFailedFmt, skillsRelPath, statErr),
				Recommendation: messages.DoctorSkillValidationRecommend,
			})
		case !skillsDirInfo.IsDir():
			results = append(results, Result{
				Status:         StatusFail,
				CheckName:      messages.DoctorCheckNameSkills,
				Message:        fmt.Sprintf(messages.DoctorSkillsLoadFailedFmt, skillsRelPath, fmt.Sprintf(messages.DoctorPathNotDirFmt, skillsRelPath)),
				Recommendation: messages.DoctorSkillValidationRecommend,
			})
		default:
			skills, skillsErr := config.LoadSkills(paths.SkillsDir)
			if skillsErr != nil {
				results = append(results, Result{
					Status:         StatusFail,
					CheckName:      messages.DoctorCheckNameSkills,
					Message:        fmt.Sprintf(messages.DoctorSkillsLoadFailedFmt, skillsRelPath, skillsErr),
					Recommendation: messages.DoctorSkillValidationRecommend,
				})
			} else {
				partial.Skills = skills
			}
		}

		return results, partial
	}

	results = append(results, Result{
		Status:    StatusOK,
		CheckName: messages.DoctorCheckNameConfig,
		Message:   messages.DoctorConfigLoaded,
	})
	return results, cfg
}

// CheckSecrets scans the configuration for missing environment variables.
// Only enabled MCP servers are considered; disabled servers are skipped.
func CheckSecrets(cfg *config.ProjectConfig) []Result {
	var results []Result
	var enabled []config.MCPServer
	for _, s := range cfg.Config.MCP.Servers {
		if s.Enabled != nil && *s.Enabled {
			enabled = append(enabled, s)
		}
	}
	required := config.RequiredEnvVarsForMCPServers(enabled)

	// Scan .env for missing values
	for _, secret := range required {
		val, ok := cfg.Env[secret]
		if !ok || val == "" {
			// Check if it's in the actual environment
			if os.Getenv(secret) == "" {
				results = append(results, Result{
					Status:         StatusFail,
					CheckName:      messages.DoctorCheckNameSecrets,
					Message:        fmt.Sprintf(messages.DoctorMissingSecretFmt, secret),
					Recommendation: fmt.Sprintf(messages.DoctorMissingSecretRecommendFmt, secret),
				})
			} else {
				results = append(results, Result{
					Status:    StatusOK,
					CheckName: messages.DoctorCheckNameSecrets,
					Message:   fmt.Sprintf(messages.DoctorSecretFoundEnvFmt, secret),
				})
			}
		} else {
			results = append(results, Result{
				Status:    StatusOK,
				CheckName: messages.DoctorCheckNameSecrets,
				Message:   fmt.Sprintf(messages.DoctorSecretFoundEnvFileFmt, secret),
			})
		}
	}

	if len(required) == 0 {
		results = append(results, Result{
			Status:    StatusOK,
			CheckName: messages.DoctorCheckNameSecrets,
			Message:   messages.DoctorNoRequiredSecrets,
		})
	}

	return results
}

// CheckAgents reports which agents are enabled or disabled.
func CheckAgents(cfg *config.ProjectConfig) []Result {
	var results []Result
	agents := []struct {
		Name    string
		Enabled *bool
	}{
		{"Gemini", cfg.Config.Agents.Gemini.Enabled},
		{"Claude", cfg.Config.Agents.Claude.Enabled},
		{"ClaudeVSCode", cfg.Config.Agents.ClaudeVSCode.Enabled},
		{"Codex", cfg.Config.Agents.Codex.Enabled},
		{"VSCode", cfg.Config.Agents.VSCode.Enabled},
		{"Antigravity", cfg.Config.Agents.Antigravity.Enabled},
	}

	for _, a := range agents {
		if a.Enabled != nil && *a.Enabled {
			results = append(results, Result{
				Status:    StatusOK,
				CheckName: messages.DoctorCheckNameAgents,
				Message:   fmt.Sprintf(messages.DoctorAgentEnabledFmt, a.Name),
			})
		} else {
			results = append(results, Result{
				Status:    StatusWarn,
				CheckName: messages.DoctorCheckNameAgents,
				Message:   fmt.Sprintf(messages.DoctorAgentDisabledFmt, a.Name),
			})
		}
	}
	return results
}

// CheckFlatFormatSkills scans .agent-layer/skills/ for stale flat-format .md files
// at the root level. Returns a FAIL result for each, recommending `al upgrade`.
func CheckFlatFormatSkills(root string) []Result {
	skillsDir := filepath.Join(root, ".agent-layer", "skills")
	info, err := os.Stat(skillsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // no skills directory — nothing to check
		}
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameFlatSkills,
			Message:        fmt.Sprintf(messages.DoctorSkillFlatFormatScanFailedFmt, skillsDir, err),
			Recommendation: messages.DoctorSkillFlatFormatScanRecommend,
		}}
	}
	if !info.IsDir() {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameFlatSkills,
			Message:        fmt.Sprintf(messages.DoctorPathNotDirFmt, ".agent-layer/skills"),
			Recommendation: messages.DoctorPathNotDirRecommend,
		}}
	}

	entries, readErr := os.ReadDir(skillsDir)
	if readErr != nil {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameFlatSkills,
			Message:        fmt.Sprintf(messages.DoctorSkillFlatFormatScanFailedFmt, skillsDir, readErr),
			Recommendation: messages.DoctorSkillFlatFormatScanRecommend,
		}}
	}

	var results []Result
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		results = append(results, Result{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameFlatSkills,
			Message:        fmt.Sprintf(messages.DoctorSkillFlatFormatDetectedFmt, entry.Name()),
			Recommendation: messages.DoctorSkillFlatFormatRecommend,
		})
	}
	return results
}

// CheckSkills validates configured skills against agentskills-aligned conventions.
func CheckSkills(cfg *config.ProjectConfig) []Result {
	if cfg == nil {
		return nil
	}
	if len(cfg.Skills) == 0 {
		return []Result{{
			Status:    StatusOK,
			CheckName: messages.DoctorCheckNameSkills,
			Message:   messages.DoctorSkillsNoneConfigured,
		}}
	}

	skills := append([]config.Skill(nil), cfg.Skills...)
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].SourcePath == skills[j].SourcePath {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].SourcePath < skills[j].SourcePath
	})

	results := make([]Result, 0)
	for _, skill := range skills {
		parsed, err := skillvalidator.ParseSkillSource(skill.SourcePath)
		if err != nil {
			results = append(results, Result{
				Status:         StatusFail,
				CheckName:      messages.DoctorCheckNameSkills,
				Message:        fmt.Sprintf(messages.DoctorSkillValidationFailedFmt, relPathForDoctor(cfg.Root, skill.SourcePath), err),
				Recommendation: messages.DoctorSkillValidationRecommend,
			})
			continue
		}
		findings := skillvalidator.ValidateParsedSkill(parsed)
		if len(findings) == 0 {
			continue
		}
		for _, finding := range findings {
			results = append(results, Result{
				Status:         StatusWarn,
				CheckName:      messages.DoctorCheckNameSkills,
				Message:        fmt.Sprintf(messages.DoctorSkillValidationWarnFmt, relPathForDoctor(cfg.Root, finding.Path), finding.Message),
				Recommendation: messages.DoctorSkillValidationRecommend,
			})
		}
	}

	if len(results) > 0 {
		return results
	}
	return []Result{{
		Status:    StatusOK,
		CheckName: messages.DoctorCheckNameSkills,
		Message:   fmt.Sprintf(messages.DoctorSkillsValidatedFmt, len(skills)),
	}}
}

func relPathForDoctor(root string, path string) string {
	if strings.TrimSpace(root) == "" {
		return filepath.ToSlash(path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
