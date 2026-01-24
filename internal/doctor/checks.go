package doctor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/messages"
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
func CheckConfig(root string) ([]Result, *config.ProjectConfig) {
	var results []Result
	cfg, err := config.LoadProjectConfig(root)
	if err != nil {
		results = append(results, Result{
			Status:         StatusFail,
			CheckName:      messages.DoctorCheckNameConfig,
			Message:        fmt.Sprintf(messages.DoctorConfigLoadFailedFmt, err),
			Recommendation: messages.DoctorConfigLoadRecommend,
		})
		return results, nil
	}

	results = append(results, Result{
		Status:    StatusOK,
		CheckName: messages.DoctorCheckNameConfig,
		Message:   messages.DoctorConfigLoaded,
	})
	return results, cfg
}

// CheckSecrets scans the configuration for missing environment variables.
func CheckSecrets(cfg *config.ProjectConfig) []Result {
	var results []Result
	required := config.RequiredEnvVarsForMCPServers(cfg.Config.MCP.Servers)

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
