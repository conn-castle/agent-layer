package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
	"github.com/conn-castle/agent-layer/internal/skillvalidator"
)

var (
	loadConfigLenientFunc = config.LoadConfigLenient
	loadEnvFunc           = config.LoadEnv
	lookPathFunc          = exec.LookPath
	commandOutputFunc     = func(name string, args ...string) ([]byte, error) {
		return commandOutputWithTimeout(antigravityVersionTimeout, name, args...)
	}
)

const antigravityVersionTimeout = 5 * time.Second
const antigravityVersionWaitDelay = 100 * time.Millisecond

const maxSkillCatalogMetadataChars = 10000

func commandOutputWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// #nosec G204 -- command name is resolved by doctor checks from a fixed executable lookup.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = antigravityVersionWaitDelay
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output, fmt.Errorf("command timed out after %s: %w", timeout, context.DeadlineExceeded)
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		return output, fmt.Errorf("command output wait timed out after %s: %w", antigravityVersionWaitDelay, err)
	}
	return output, err
}

// agyVersionRE matches the version reported on the `agy <version>` line so
// unrelated dotted-numeric noise (build timestamps, go runtime version, etc.)
// in `agy --version` output cannot be silently accepted as the agy version.
// Accepts the common upstream shapes: `agy 1.0.0`, `agy v1.0.0`, and
// `agy version 1.0.0`. The first capture group is the bare X.Y.Z triple.
var agyVersionRE = regexp.MustCompile(`(?m)^agy(?:\s+version)?\s+v?(\d+\.\d+\.\d+)\b`)

// CheckStructure verifies that required and optional project directories are sane.
func CheckStructure(root string) []Result {
	var results []Result
	paths := []struct {
		path     string
		required bool
	}{
		{path: ".agent-layer", required: true},
		{path: "docs/agent-layer", required: false},
	}

	for _, entry := range paths {
		fullPath := filepath.Join(root, entry.path)
		info, err := os.Stat(fullPath)
		if err != nil {
			status := StatusWarn
			message := fmt.Sprintf(messages.DoctorMissingOptionalDirFmt, entry.path)
			recommendation := fmt.Sprintf(messages.DoctorMissingOptionalDirRecommend, entry.path)
			if entry.required {
				status = StatusFail
				message = fmt.Sprintf(messages.DoctorMissingRequiredDirFmt, entry.path)
				recommendation = messages.DoctorMissingRequiredDirRecommend
			}
			results = append(results, Result{
				Status:         status,
				CheckName:      messages.DoctorCheckNameStructure,
				Message:        message,
				Recommendation: recommendation,
			})
			continue
		}
		if !info.IsDir() {
			results = append(results, Result{
				Status:         StatusFail,
				CheckName:      messages.DoctorCheckNameStructure,
				Message:        fmt.Sprintf(messages.DoctorPathNotDirFmt, entry.path),
				Recommendation: messages.DoctorPathNotDirRecommend,
			})
			continue
		}
		results = append(results, Result{
			Status:    StatusOK,
			CheckName: messages.DoctorCheckNameStructure,
			Message:   fmt.Sprintf(messages.DoctorDirExistsFmt, entry.path),
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
		if config.IsAgentEnabled(s.Enabled) {
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
		{"Antigravity", cfg.Config.Agents.Antigravity.Enabled},
		{"Claude", cfg.Config.Agents.Claude.Enabled},
		{"ClaudeVSCode", cfg.Config.Agents.ClaudeVSCode.Enabled},
		{"Codex", cfg.Config.Agents.Codex.Enabled},
		{"VSCode", cfg.Config.Agents.VSCode.Enabled},
	}

	for _, a := range agents {
		if config.IsAgentEnabled(a.Enabled) {
			results = append(results, Result{
				Status:    StatusOK,
				CheckName: messages.DoctorCheckNameAgents,
				Message:   fmt.Sprintf(messages.DoctorAgentEnabledFmt, a.Name),
			})
		} else {
			results = append(results, Result{
				Status:    StatusOK,
				CheckName: messages.DoctorCheckNameAgents,
				Message:   fmt.Sprintf(messages.DoctorAgentDisabledFmt, a.Name),
			})
		}
	}
	if config.IsAgentEnabled(cfg.Config.Agents.Antigravity.Enabled) {
		results = append(results, CheckAntigravityBinary()...)
	}
	return results
}

// CheckAntigravityBinary verifies that agy exists and is at least v1.0.0.
func CheckAntigravityBinary() []Result {
	path, err := lookPathFunc("agy")
	if err != nil {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameAgents,
			Message:        messages.DoctorAntigravityNotFound,
			Recommendation: messages.DoctorAntigravityInstallRecommend,
		}}
	}
	output, err := commandOutputFunc(path, "--version")
	if err != nil {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameAgents,
			Message:        fmt.Sprintf(messages.DoctorAntigravityVersionFailedFmt, err),
			Recommendation: messages.DoctorAntigravityInstallRecommend,
		}}
	}
	versionText := string(output)
	// Use the capture group so an optional `v` prefix is stripped before
	// passing the value to compareDoctorSemver (which expects a bare
	// X.Y.Z triple).
	var versionValue string
	if match := agyVersionRE.FindStringSubmatch(versionText); len(match) >= 2 {
		versionValue = match[1]
	}
	if versionValue == "" {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameAgents,
			Message:        fmt.Sprintf(messages.DoctorAntigravityVersionUnknownFmt, strings.TrimSpace(versionText)),
			Recommendation: messages.DoctorAntigravityInstallRecommend,
		}}
	}
	cmp, err := compareDoctorSemver(versionValue, "1.0.0")
	if err != nil {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameAgents,
			Message:        fmt.Sprintf(messages.DoctorAntigravityVersionUnknownFmt, strings.TrimSpace(versionText)),
			Recommendation: messages.DoctorAntigravityInstallRecommend,
		}}
	}
	if cmp < 0 {
		return []Result{{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameAgents,
			Message:        fmt.Sprintf(messages.DoctorAntigravityVersionTooOldFmt, versionValue),
			Recommendation: messages.DoctorAntigravityInstallRecommend,
		}}
	}
	return []Result{{
		Status:    StatusOK,
		CheckName: messages.DoctorCheckNameAgents,
		Message:   fmt.Sprintf(messages.DoctorAntigravityVersionOKFmt, versionValue),
	}}
}

func compareDoctorSemver(a string, b string) (int, error) {
	av, err := parseDoctorSemver(a)
	if err != nil {
		return 0, err
	}
	bv, err := parseDoctorSemver(b)
	if err != nil {
		return 0, err
	}
	if av[0] != bv[0] {
		return compareDoctorInt(av[0], bv[0]), nil
	}
	if av[1] != bv[1] {
		return compareDoctorInt(av[1], bv[1]), nil
	}
	return compareDoctorInt(av[2], bv[2]), nil
}

func compareDoctorInt(a int, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func parseDoctorSemver(raw string) ([3]int, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return [3]int{}, fmt.Errorf("invalid semantic version %q", raw)
	}
	var parsed [3]int
	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, err
		}
		parsed[i] = value
	}
	return parsed, nil
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

	catalogChars := skillCatalogMetadataChars(skills)
	if catalogChars > maxSkillCatalogMetadataChars {
		results = append(results, Result{
			Status:         StatusWarn,
			CheckName:      messages.DoctorCheckNameSkills,
			Message:        fmt.Sprintf(messages.DoctorSkillCatalogTooLargeFmt, maxSkillCatalogMetadataChars, catalogChars, len(skills)),
			Recommendation: messages.DoctorSkillValidationRecommend,
		})
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

func skillCatalogMetadataChars(skills []config.Skill) int {
	total := 0
	for _, skill := range skills {
		total += utf8.RuneCountInString(strings.TrimSpace(skill.Name))
		total += utf8.RuneCountInString(strings.TrimSpace(skill.Description))
	}
	return total
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
