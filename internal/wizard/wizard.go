package wizard

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aymanbagabas/go-udiff"
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
	loadConfigLenientFunc     = config.LoadConfigLenient
	errWizardBack             = errors.New("wizard back requested")
	errWizardCancelled        = errors.New("wizard cancelled")
)

// Run starts the interactive wizard.
// pinVersion is written to .agent-layer/al.version when install is needed.
func Run(root string, ui UI, runSync syncer, pinVersion string) error {
	return RunWithWriter(root, ui, runSync, pinVersion, os.Stdout)
}

// RunWithWriter starts the interactive wizard and writes user-facing output to out.
// pinVersion is written to .agent-layer/al.version when install is needed.
func RunWithWriter(root string, ui UI, runSync syncer, pinVersion string, out io.Writer) error {
	if out == nil {
		out = os.Stdout
	}
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	envPath := filepath.Join(root, ".agent-layer", ".env")

	proceed, err := ensureWizardConfig(root, configPath, ui, pinVersion, out)
	if err != nil {
		if errors.Is(err, errWizardBack) || errors.Is(err, errWizardCancelled) {
			_, _ = fmt.Fprintln(out, messages.WizardExitWithoutChanges)
			return nil
		}
		return err
	}
	if !proceed {
		return nil
	}

	cfg, err := loadProjectConfigFunc(root)
	if err != nil {
		if !errors.Is(err, config.ErrConfigValidation) {
			// Non-validation failure (env, instructions, skills, etc.) —
			// lenient config fallback would not help; propagate the real error.
			return fmt.Errorf(messages.WizardLoadConfigFailedFmt, err)
		}
		// Config has validation errors (e.g., missing required fields from a
		// newer version). Fall back to lenient loading so the wizard can still
		// run and help the user fix the config.
		lenientCfg, lenientErr := loadConfigLenientFunc(configPath)
		if lenientErr != nil {
			// TOML syntax error or file unreadable — can't recover.
			return fmt.Errorf(messages.WizardLoadConfigFailedFmt, lenientErr)
		}
		_, _ = fmt.Fprintf(out, messages.ConfigLenientLoadInfoFmt+"\n", "the wizard", err)
		cfg = &config.ProjectConfig{Config: *lenientCfg, Root: root}
	}

	choices, err := initializeChoices(cfg)
	if err != nil {
		return err
	}

	if err := promptWizardFlow(root, ui, cfg, choices); err != nil {
		if errors.Is(err, errWizardCancelled) || errors.Is(err, errWizardBack) {
			_, _ = fmt.Fprintln(out, messages.WizardExitWithoutChanges)
			return nil
		}
		return err
	}

	if err := confirmAndApply(root, configPath, envPath, ui, choices, runSync, out); err != nil {
		if errors.Is(err, errWizardCancelled) || errors.Is(err, errWizardBack) {
			_, _ = fmt.Fprintln(out, messages.WizardExitWithoutChanges)
			return nil
		}
		return err
	}

	return nil
}

func ensureWizardConfig(root, configPath string, ui UI, pinVersion string, out io.Writer) (bool, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		confirm := true
		if err := ui.Confirm(messages.WizardInstallPrompt, &confirm); err != nil {
			return false, err
		}
		if !confirm {
			_, _ = fmt.Fprintln(out, messages.WizardExitWithoutChanges)
			return false, nil
		}

		if err := install.Run(root, install.Options{Overwrite: false, PinVersion: pinVersion, System: install.RealSystem{}}); err != nil {
			return false, fmt.Errorf(messages.WizardInstallFailedFmt, err)
		}
		_, _ = fmt.Fprintln(out, messages.WizardInstallComplete)
		return true, nil
	} else if err != nil {
		return false, err
	}

	return true, nil
}

func initializeChoices(cfg *config.ProjectConfig) (*Choices, error) {
	choices := NewChoices()

	defaultServers, err := loadDefaultMCPServersFunc()
	if err != nil {
		return nil, fmt.Errorf(messages.WizardLoadDefaultMCPServersFailedFmt, err)
	}
	choices.DefaultMCPServers = defaultServers

	warningDefaults, err := loadWarningDefaultsFunc()
	if err != nil {
		return nil, fmt.Errorf(messages.WizardLoadWarningDefaultsFailedFmt, err)
	}
	choices.InstructionTokenThreshold = warningDefaults.InstructionTokenThreshold
	choices.MCPServerThreshold = warningDefaults.MCPServerThreshold
	choices.MCPToolsTotalThreshold = warningDefaults.MCPToolsTotalThreshold
	choices.MCPServerToolsThreshold = warningDefaults.MCPServerToolsThreshold
	choices.MCPSchemaTokensTotalThreshold = warningDefaults.MCPSchemaTokensTotalThreshold
	choices.MCPSchemaTokensServerThreshold = warningDefaults.MCPSchemaTokensServerThreshold

	choices.ApprovalMode = cfg.Config.Approvals.Mode
	if choices.ApprovalMode == "" {
		choices.ApprovalMode = ApprovalAll
	}

	agentConfigs := []agentEnabledConfig{
		{id: AgentGemini, enabled: cfg.Config.Agents.Gemini.Enabled},
		{id: AgentClaude, enabled: cfg.Config.Agents.Claude.Enabled},
		{id: AgentClaudeVSCode, enabled: cfg.Config.Agents.ClaudeVSCode.Enabled},
		{id: AgentCodex, enabled: cfg.Config.Agents.Codex.Enabled},
		{id: AgentVSCode, enabled: cfg.Config.Agents.VSCode.Enabled},
		{id: AgentAntigravity, enabled: cfg.Config.Agents.Antigravity.Enabled},
	}
	setEnabledAgentsFromConfig(choices.EnabledAgents, agentConfigs)

	choices.GeminiModel = cfg.Config.Agents.Gemini.Model
	choices.ClaudeModel = cfg.Config.Agents.Claude.Model
	choices.ClaudeReasoning = cfg.Config.Agents.Claude.ReasoningEffort
	if cfg.Config.Agents.Claude.LocalConfigDir != nil {
		choices.ClaudeLocalConfigDir = *cfg.Config.Agents.Claude.LocalConfigDir
	}
	choices.CodexModel = cfg.Config.Agents.Codex.Model
	choices.CodexReasoning = cfg.Config.Agents.Codex.ReasoningEffort

	for _, srv := range cfg.Config.MCP.Servers {
		if srv.Enabled != nil && *srv.Enabled {
			choices.EnabledMCPServers[srv.ID] = true
		}
	}

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

	return choices, nil
}

type wizardFlowStep int

const (
	wizardFlowStepApproval wizardFlowStep = iota
	wizardFlowStepAgents
	wizardFlowStepModels
	wizardFlowStepMCPDefaults
	wizardFlowStepSecrets
	wizardFlowStepWarnings
)

func promptWizardFlow(root string, ui UI, cfg *config.ProjectConfig, choices *Choices) error {
	step := wizardFlowStepApproval
	for {
		snapshot := choices.Clone()
		var err error

		switch step {
		case wizardFlowStepApproval:
			err = promptApprovalMode(ui, choices)
		case wizardFlowStepAgents:
			err = promptEnabledAgents(ui, choices)
		case wizardFlowStepModels:
			err = promptModels(ui, choices)
		case wizardFlowStepMCPDefaults:
			err = promptDefaultMCPServers(ui, cfg, choices)
		case wizardFlowStepSecrets:
			err = promptSecrets(root, ui, choices)
		case wizardFlowStepWarnings:
			err = promptWarnings(ui, choices)
		default:
			return nil
		}

		if err == nil {
			if step == wizardFlowStepWarnings {
				return nil
			}
			step++
			continue
		}

		if !errors.Is(err, errWizardBack) {
			return err
		}

		if snapshot != nil {
			*choices = *snapshot
		}

		if step == wizardFlowStepApproval {
			exit, confirmErr := confirmWizardExitOnFirstStepEscape(ui)
			if confirmErr != nil {
				return confirmErr
			}
			if exit {
				return errWizardCancelled
			}
			continue
		}

		step--
	}
}

func promptApprovalMode(ui UI, choices *Choices) error {
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
	return nil
}

func promptEnabledAgents(ui UI, choices *Choices) error {
	enabledAgents := enabledAgentIDs(choices.EnabledAgents)
	if err := ui.MultiSelect(messages.WizardEnableAgentsTitle, SupportedAgents(), &enabledAgents); err != nil {
		return err
	}
	choices.EnabledAgents = agentIDSet(enabledAgents)
	choices.EnabledAgentsTouched = true
	return nil
}

func promptWarnings(ui UI, choices *Choices) error {
	warningsEnabled := choices.WarningsEnabled
	if err := ui.Confirm(messages.WizardEnableWarningsPrompt, &warningsEnabled); err != nil {
		return err
	}
	choices.WarningsEnabled = warningsEnabled
	choices.WarningsEnabledTouched = true
	return nil
}

func confirmWizardExitOnFirstStepEscape(ui UI) (bool, error) {
	exit := true
	if err := ui.Confirm(messages.WizardFirstStepEscapeExitPrompt, &exit); err != nil {
		if errors.Is(err, errWizardBack) {
			return false, nil
		}
		return false, err
	}
	return exit, nil
}

func promptModels(ui UI, choices *Choices) error {
	if choices.EnabledAgents[AgentGemini] {
		if hasPreviewModels(GeminiModels()) {
			if err := ui.Note(messages.WizardPreviewModelWarningTitle, previewModelWarningText()); err != nil {
				return err
			}
		}
		if err := selectOptionalValue(ui, messages.WizardGeminiModelTitle, GeminiModels(), &choices.GeminiModel); err != nil {
			return err
		}
		choices.GeminiModelTouched = true
	}
	if choices.EnabledAgents[AgentClaude] {
		if err := selectOptionalValue(ui, messages.WizardClaudeModelTitle, ClaudeModels(), &choices.ClaudeModel); err != nil {
			return err
		}
		choices.ClaudeModelTouched = true
		if config.ClaudeModelSupportsReasoningEffort(choices.ClaudeModel) {
			if err := selectOptionalValue(ui, messages.WizardClaudeReasoningEffortTitle, ClaudeReasoningEfforts(), &choices.ClaudeReasoning); err != nil {
				return err
			}
			choices.ClaudeReasoningTouched = true
		} else if choices.ClaudeReasoning != "" {
			// Clear reasoning effort when the selected model does not support it.
			choices.ClaudeReasoning = ""
			choices.ClaudeReasoningTouched = true
		}
	}
	if choices.EnabledAgents[AgentClaude] || choices.EnabledAgents[AgentClaudeVSCode] {
		claudeLocalConfigDir := choices.ClaudeLocalConfigDir
		if err := ui.Confirm(messages.WizardClaudeLocalConfigDirPrompt, &claudeLocalConfigDir); err != nil {
			return err
		}
		choices.ClaudeLocalConfigDir = claudeLocalConfigDir
		choices.ClaudeLocalConfigDirTouched = true
	}
	if choices.EnabledAgents[AgentCodex] {
		if err := selectOptionalValue(ui, messages.WizardCodexModelTitle, CodexModels(), &choices.CodexModel); err != nil {
			return err
		}
		choices.CodexModelTouched = true

		if err := selectOptionalValue(ui, messages.WizardCodexReasoningEffortTitle, CodexReasoningEfforts(), &choices.CodexReasoning); err != nil {
			return err
		}
		choices.CodexReasoningTouched = true
	}

	return nil
}

func promptDefaultMCPServers(ui UI, cfg *config.ProjectConfig, choices *Choices) error {
	missingDefaults := missingDefaultMCPServers(choices.DefaultMCPServers, cfg.Config.MCP.Servers)
	if len(missingDefaults) > 0 {
		choices.MissingDefaultMCPServers = missingDefaults
		restore := true
		if err := ui.Confirm(fmt.Sprintf(messages.WizardMissingDefaultMCPServersPromptFmt, strings.Join(missingDefaults, ", ")), &restore); err != nil {
			return err
		}
		choices.RestoreMissingMCPServers = restore
	}
	defaultServerIDs := make([]string, 0, len(choices.DefaultMCPServers))
	enabledDefaultServers := make([]string, 0, len(choices.DefaultMCPServers))
	for _, server := range choices.DefaultMCPServers {
		defaultServerIDs = append(defaultServerIDs, server.ID)
		if choices.EnabledMCPServers[server.ID] {
			enabledDefaultServers = append(enabledDefaultServers, server.ID)
		}
	}
	if err := ui.MultiSelect(messages.WizardEnableDefaultMCPServersTitle, defaultServerIDs, &enabledDefaultServers); err != nil {
		return err
	}

	for _, server := range choices.DefaultMCPServers {
		choices.EnabledMCPServers[server.ID] = false
	}
	for _, id := range enabledDefaultServers {
		choices.EnabledMCPServers[id] = true
	}
	choices.EnabledMCPServersTouched = true
	return nil
}

func confirmAndApply(root, configPath, envPath string, ui UI, choices *Choices, runSync syncer, out io.Writer) error {
	summary := buildSummary(choices)
	confirmApply := true
	if err := ui.Note(messages.WizardSummaryTitle, summary); err != nil {
		return err
	}
	rewritePreview, err := buildRewritePreview(configPath, envPath, choices)
	if err != nil {
		return err
	}
	if err := ui.Note(messages.WizardRewritePreviewTitle, rewritePreview); err != nil {
		return err
	}
	if err := ui.Confirm(messages.WizardApplyChangesPrompt, &confirmApply); err != nil {
		return err
	}
	if !confirmApply {
		_, _ = fmt.Fprintln(out, messages.WizardExitWithoutChanges)
		return nil
	}

	if err := applyChanges(root, configPath, envPath, choices, runSync, out); err != nil {
		return err
	}

	_, _ = color.New(color.FgGreen).Fprintln(out, messages.WizardCompleted)
	return nil
}

func promptSecrets(root string, ui UI, choices *Choices) error {
	envPath := filepath.Join(root, ".agent-layer", ".env")
	envValues := make(map[string]string)
	if b, err := os.ReadFile(envPath); err == nil {
		parsed, parseErr := envfile.Parse(string(b))
		if parseErr != nil {
			return fmt.Errorf(messages.WizardInvalidEnvFileFmt, envPath, parseErr)
		}
		envValues = parsed
	} else if !os.IsNotExist(err) {
		return err
	}

	for _, server := range choices.DefaultMCPServers {
		if !choices.EnabledMCPServers[server.ID] || len(server.RequiredEnv) == 0 {
			continue
		}
		disableServer := false
		for _, key := range server.RequiredEnv {
			if key == "" {
				continue
			}
			if existing, ok := choices.Secrets[key]; ok && existing != "" {
				continue
			}
			if value, ok := envValues[key]; ok && value != "" {
				override := false
				if err := ui.Confirm(fmt.Sprintf(messages.WizardSecretAlreadySetPromptFmt, key), &override); err != nil {
					return err
				}
				if !override {
					choices.Secrets[key] = value
					continue
				}
			} else if value := os.Getenv(key); value != "" {
				useEnv := false
				if err := ui.Confirm(fmt.Sprintf(messages.WizardEnvSecretFoundPromptFmt, key), &useEnv); err != nil {
					return err
				}
				if useEnv {
					choices.Secrets[key] = value
					continue
				}
			}

			for {
				var value string
				if err := ui.SecretInput(fmt.Sprintf(messages.WizardSecretInputPromptFmt, key), &value); err != nil {
					return err
				}
				normalized := strings.TrimSpace(value)
				switch strings.ToLower(normalized) {
				case "cancel":
					return errWizardCancelled
				case "skip":
					choices.EnabledMCPServers[server.ID] = false
					choices.DisabledMCPServers[server.ID] = true
					disableServer = true
				}
				if disableServer {
					break
				}
				if normalized != "" {
					choices.Secrets[key] = normalized
					break
				}

				disable := true
				if err := ui.Confirm(fmt.Sprintf(messages.WizardSecretMissingDisablePromptFmt, key, server.ID), &disable); err != nil {
					return err
				}
				if disable {
					choices.EnabledMCPServers[server.ID] = false
					choices.DisabledMCPServers[server.ID] = true
					disableServer = true
					break
				}
			}
			if disableServer {
				break
			}
		}
	}

	return nil
}

func buildRewritePreview(configPath, envPath string, choices *Choices) (string, error) {
	currentConfigBytes, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	nextConfig, err := PatchConfig(string(currentConfigBytes), choices)
	if err != nil {
		return "", fmt.Errorf(messages.WizardPatchConfigFailedFmt, err)
	}

	currentEnvBytes, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	nextEnv := envfile.Patch(string(currentEnvBytes), choices.Secrets)

	parts := make([]string, 0, 2)
	configDiff := strings.TrimSpace(udiff.Unified(
		".agent-layer/config.toml (current)",
		".agent-layer/config.toml (proposed)",
		string(currentConfigBytes),
		nextConfig,
	))
	if configDiff != "" {
		parts = append(parts, configDiff)
	}

	envDiff := strings.TrimSpace(udiff.Unified(
		".agent-layer/.env (current)",
		".agent-layer/.env (proposed)",
		string(currentEnvBytes),
		nextEnv,
	))
	if envDiff != "" {
		parts = append(parts, envDiff)
	}

	if len(parts) == 0 {
		return "No rewrites needed. Current files already match the selected changes.", nil
	}
	return strings.Join(parts, "\n\n"), nil
}
