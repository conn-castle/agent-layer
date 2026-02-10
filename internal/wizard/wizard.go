package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"

	"github.com/conn-castle/agent-layer/internal/config"
	"github.com/conn-castle/agent-layer/internal/envfile"
	"github.com/conn-castle/agent-layer/internal/install"
	"github.com/conn-castle/agent-layer/internal/messages"
)

var (
	loadDefaultMCPServersFunc = loadDefaultMCPServers
	loadWarningDefaultsFunc   = loadWarningDefaults
	loadProjectConfigFunc     = config.LoadProjectConfig
)

// Run starts the interactive wizard.
// pinVersion is written to .agent-layer/al.version when install is needed.
func Run(root string, ui UI, runSync syncer, pinVersion string) error {
	configPath := filepath.Join(root, ".agent-layer", "config.toml")

	// 2. Install gating
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		confirm := true
		err := ui.Confirm(messages.WizardInstallPrompt, &confirm)
		if err != nil {
			return err
		}
		if !confirm {
			fmt.Println(messages.WizardExitWithoutChanges)
			return nil
		}

		// Run install
		if err := install.Run(root, install.Options{Overwrite: false, PinVersion: pinVersion, System: install.RealSystem{}}); err != nil {
			return fmt.Errorf(messages.WizardInstallFailedFmt, err)
		}
		fmt.Println(messages.WizardInstallComplete)
	}

	// 3. Load config
	cfg, err := loadProjectConfigFunc(root)
	if err != nil {
		return fmt.Errorf(messages.WizardLoadConfigFailedFmt, err)
	}

	// 4. Initialize choices from config
	choices := NewChoices()

	defaultServers, err := loadDefaultMCPServersFunc()
	if err != nil {
		return fmt.Errorf(messages.WizardLoadDefaultMCPServersFailedFmt, err)
	}
	choices.DefaultMCPServers = defaultServers
	warningDefaults, err := loadWarningDefaultsFunc()
	if err != nil {
		return fmt.Errorf(messages.WizardLoadWarningDefaultsFailedFmt, err)
	}
	choices.InstructionTokenThreshold = warningDefaults.InstructionTokenThreshold
	choices.MCPServerThreshold = warningDefaults.MCPServerThreshold
	choices.MCPToolsTotalThreshold = warningDefaults.MCPToolsTotalThreshold
	choices.MCPServerToolsThreshold = warningDefaults.MCPServerToolsThreshold
	choices.MCPSchemaTokensTotalThreshold = warningDefaults.MCPSchemaTokensTotalThreshold
	choices.MCPSchemaTokensServerThreshold = warningDefaults.MCPSchemaTokensServerThreshold

	// Approvals
	choices.ApprovalMode = cfg.Config.Approvals.Mode
	if choices.ApprovalMode == "" {
		choices.ApprovalMode = ApprovalAll
	}

	// Agents
	agentConfigs := []agentEnabledConfig{
		{id: AgentGemini, enabled: cfg.Config.Agents.Gemini.Enabled},
		{id: AgentClaude, enabled: cfg.Config.Agents.Claude.Enabled},
		{id: AgentCodex, enabled: cfg.Config.Agents.Codex.Enabled},
		{id: AgentVSCode, enabled: cfg.Config.Agents.VSCode.Enabled},
		{id: AgentAntigravity, enabled: cfg.Config.Agents.Antigravity.Enabled},
	}
	setEnabledAgentsFromConfig(choices.EnabledAgents, agentConfigs)

	// Models
	choices.GeminiModel = cfg.Config.Agents.Gemini.Model
	choices.ClaudeModel = cfg.Config.Agents.Claude.Model
	choices.CodexModel = cfg.Config.Agents.Codex.Model
	choices.CodexReasoning = cfg.Config.Agents.Codex.ReasoningEffort

	// MCP Servers
	for _, srv := range cfg.Config.MCP.Servers {
		if srv.Enabled != nil && *srv.Enabled {
			choices.EnabledMCPServers[srv.ID] = true
		}
	}

	// Warnings
	choices.WarningsEnabled = cfg.Config.Warnings.InstructionTokenThreshold != nil ||
		cfg.Config.Warnings.MCPServerThreshold != nil ||
		cfg.Config.Warnings.MCPToolsTotalThreshold != nil ||
		cfg.Config.Warnings.MCPServerToolsThreshold != nil ||
		cfg.Config.Warnings.MCPSchemaTokensTotalThreshold != nil ||
		cfg.Config.Warnings.MCPSchemaTokensServerThreshold != nil
	if cfg.Config.Warnings.InstructionTokenThreshold != nil {
		choices.InstructionTokenThreshold = *cfg.Config.Warnings.InstructionTokenThreshold
	}
	if cfg.Config.Warnings.MCPServerThreshold != nil {
		choices.MCPServerThreshold = *cfg.Config.Warnings.MCPServerThreshold
	}
	if cfg.Config.Warnings.MCPToolsTotalThreshold != nil {
		choices.MCPToolsTotalThreshold = *cfg.Config.Warnings.MCPToolsTotalThreshold
	}
	if cfg.Config.Warnings.MCPServerToolsThreshold != nil {
		choices.MCPServerToolsThreshold = *cfg.Config.Warnings.MCPServerToolsThreshold
	}
	if cfg.Config.Warnings.MCPSchemaTokensTotalThreshold != nil {
		choices.MCPSchemaTokensTotalThreshold = *cfg.Config.Warnings.MCPSchemaTokensTotalThreshold
	}
	if cfg.Config.Warnings.MCPSchemaTokensServerThreshold != nil {
		choices.MCPSchemaTokensServerThreshold = *cfg.Config.Warnings.MCPSchemaTokensServerThreshold
	}

	// 5. UI Flow

	// Approvals
	approvalModeLabel, ok := approvalModeLabelForValue(choices.ApprovalMode)
	if !ok {
		return fmt.Errorf(messages.WizardUnknownApprovalModeFmt, choices.ApprovalMode)
	}
	if err := ui.Select(messages.WizardApprovalModeTitle, approvalModeLabels(), &approvalModeLabel); err != nil {
		return err
	}
	approvalModeValue, ok := approvalModeValueForLabel(approvalModeLabel)
	if !ok {
		return fmt.Errorf(messages.WizardUnknownApprovalModeSelectionFmt, approvalModeLabel)
	}
	choices.ApprovalMode = approvalModeValue
	choices.ApprovalModeTouched = true

	// Agents
	enabledAgents := enabledAgentIDs(choices.EnabledAgents)
	if err := ui.MultiSelect(messages.WizardEnableAgentsTitle, SupportedAgents, &enabledAgents); err != nil {
		return err
	}
	// Update map
	choices.EnabledAgents = agentIDSet(enabledAgents)
	choices.EnabledAgentsTouched = true

	// Models (for enabled agents)
	if choices.EnabledAgents[AgentGemini] {
		if hasPreviewModels(GeminiModels) {
			if err := ui.Note(messages.WizardPreviewModelWarningTitle, previewModelWarningText()); err != nil {
				return err
			}
		}
		if err := selectOptionalValue(ui, messages.WizardGeminiModelTitle, GeminiModels, &choices.GeminiModel); err != nil {
			return err
		}
		choices.GeminiModelTouched = true
	}
	if choices.EnabledAgents[AgentClaude] {
		if err := selectOptionalValue(ui, messages.WizardClaudeModelTitle, ClaudeModels, &choices.ClaudeModel); err != nil {
			return err
		}
		choices.ClaudeModelTouched = true
	}
	if choices.EnabledAgents[AgentCodex] {
		if err := selectOptionalValue(ui, messages.WizardCodexModelTitle, CodexModels, &choices.CodexModel); err != nil {
			return err
		}
		choices.CodexModelTouched = true

		if err := selectOptionalValue(ui, messages.WizardCodexReasoningEffortTitle, CodexReasoningEfforts, &choices.CodexReasoning); err != nil {
			return err
		}
		choices.CodexReasoningTouched = true
	}

	// MCP Servers
	missingDefaults := missingDefaultMCPServers(choices.DefaultMCPServers, cfg.Config.MCP.Servers)
	if len(missingDefaults) > 0 {
		choices.MissingDefaultMCPServers = missingDefaults
		restore := true
		if err := ui.Confirm(fmt.Sprintf(messages.WizardMissingDefaultMCPServersPromptFmt, strings.Join(missingDefaults, ", ")), &restore); err != nil {
			return err
		}
		choices.RestoreMissingMCPServers = restore
	}
	var defaultServerIDs []string
	var enabledDefaultServers []string
	for _, s := range choices.DefaultMCPServers {
		defaultServerIDs = append(defaultServerIDs, s.ID)
		if choices.EnabledMCPServers[s.ID] {
			enabledDefaultServers = append(enabledDefaultServers, s.ID)
		}
	}
	if err := ui.MultiSelect(messages.WizardEnableDefaultMCPServersTitle, defaultServerIDs, &enabledDefaultServers); err != nil {
		return err
	}
	// Only update known defaults in the map
	for _, s := range choices.DefaultMCPServers {
		choices.EnabledMCPServers[s.ID] = false // Reset known ones
	}
	for _, id := range enabledDefaultServers {
		choices.EnabledMCPServers[id] = true
	}
	choices.EnabledMCPServersTouched = true

	// Secrets
	// Load existing env to know what's set
	envPath := filepath.Join(root, ".agent-layer", ".env")
	envValues := make(map[string]string)
	if b, err := os.ReadFile(envPath); err == nil {
		parsed, err := envfile.Parse(string(b))
		if err != nil {
			return fmt.Errorf(messages.WizardInvalidEnvFileFmt, envPath, err)
		}
		envValues = parsed
	} else if !os.IsNotExist(err) {
		return err
	}

	for _, srv := range choices.DefaultMCPServers {
		if choices.EnabledMCPServers[srv.ID] {
			if len(srv.RequiredEnv) == 0 {
				continue
			}
			for _, key := range srv.RequiredEnv {
				if key == "" {
					continue
				}

				if existing, ok := choices.Secrets[key]; ok && existing != "" {
					continue
				}
				if val, ok := envValues[key]; ok && val != "" {
					override := false
					if err := ui.Confirm(fmt.Sprintf(messages.WizardSecretAlreadySetPromptFmt, key), &override); err != nil {
						return err
					}
					if !override {
						choices.Secrets[key] = val
						continue
					}
				} else {
					if val := os.Getenv(key); val != "" {
						useEnv := false
						if err := ui.Confirm(fmt.Sprintf(messages.WizardEnvSecretFoundPromptFmt, key), &useEnv); err != nil {
							return err
						}
						if useEnv {
							choices.Secrets[key] = val
							continue
						}
					}
				}

				for {
					var val string
					if err := ui.SecretInput(fmt.Sprintf(messages.WizardSecretInputPromptFmt, key), &val); err != nil {
						return err
					}
					if val != "" {
						choices.Secrets[key] = val
						break
					}
					disable := true
					if err := ui.Confirm(fmt.Sprintf(messages.WizardSecretMissingDisablePromptFmt, key, srv.ID), &disable); err != nil {
						return err
					}
					if disable {
						choices.EnabledMCPServers[srv.ID] = false
						choices.DisabledMCPServers[srv.ID] = true
						break
					}
				}
				if !choices.EnabledMCPServers[srv.ID] {
					break
				}
			}
		}
	}

	// Warnings
	warningsEnabled := choices.WarningsEnabled
	if err := ui.Confirm(messages.WizardEnableWarningsPrompt, &warningsEnabled); err != nil {
		return err
	}
	choices.WarningsEnabled = warningsEnabled
	choices.WarningsEnabledTouched = true
	if choices.WarningsEnabled {
		if err := promptPositiveInt(ui, messages.WizardInstructionTokenThresholdTitle, &choices.InstructionTokenThreshold); err != nil {
			return err
		}
		if err := promptPositiveInt(ui, messages.WizardMCPServerThresholdTitle, &choices.MCPServerThreshold); err != nil {
			return err
		}
		if err := promptPositiveInt(ui, messages.WizardMCPToolsTotalThresholdTitle, &choices.MCPToolsTotalThreshold); err != nil {
			return err
		}
		if err := promptPositiveInt(ui, messages.WizardMCPServerToolsThresholdTitle, &choices.MCPServerToolsThreshold); err != nil {
			return err
		}
		if err := promptPositiveInt(ui, messages.WizardMCPSchemaTokensTotalThresholdTitle, &choices.MCPSchemaTokensTotalThreshold); err != nil {
			return err
		}
		if err := promptPositiveInt(ui, messages.WizardMCPSchemaTokensServerThresholdTitle, &choices.MCPSchemaTokensServerThreshold); err != nil {
			return err
		}
	}

	// 6. Summary
	summary := buildSummary(choices)
	confirmApply := true
	if err := ui.Note(messages.WizardSummaryTitle, summary); err != nil {
		return err
	}
	if err := ui.Confirm(messages.WizardApplyChangesPrompt, &confirmApply); err != nil {
		return err
	}
	if !confirmApply {
		fmt.Println(messages.WizardExitWithoutChanges)
		return nil
	}

	// 7. Apply
	if err := applyChanges(root, configPath, envPath, choices, runSync); err != nil {
		return err
	}

	_, _ = color.New(color.FgGreen).Println(messages.WizardCompleted)
	return nil
}
