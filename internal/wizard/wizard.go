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
	loadCLISkillCatalogFunc   = loadCLISkillCatalog
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
	return runWithWriter(root, ui, runSync, pinVersion, out, false)
}

// RunAfterFreshInitWithWriter runs the wizard immediately after `al init`
// created the bare operational layout. Provider statusline options and the
// workflow-bundle install prompt use fresh-setup defaults in this path.
func RunAfterFreshInitWithWriter(root string, ui UI, runSync syncer, pinVersion string, out io.Writer) error {
	return runWithWriter(root, ui, runSync, pinVersion, out, true)
}

func runWithWriter(root string, ui UI, runSync syncer, pinVersion string, out io.Writer, freshInitDefaults bool) error {
	if out == nil {
		out = os.Stdout
	}
	configPath := filepath.Join(root, ".agent-layer", "config.toml")
	envPath := filepath.Join(root, ".agent-layer", ".env")

	proceed, freshInstall, err := ensureWizardConfig(root, configPath, ui, pinVersion, out)
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
		if errors.Is(err, config.ErrConfigNeedsUpgrade) {
			// The config contains a legacy key that only `al upgrade` can
			// migrate. The wizard's config patch preserves unknown sections
			// verbatim, so proceeding would dead-end at sync. Redirect instead
			// of promising a fix the wizard cannot deliver.
			_, _ = fmt.Fprintf(out, messages.WizardConfigNeedsUpgradeFmt+"\n", err)
			return nil
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

	if freshInstall || freshInitDefaults {
		applyFreshSetupDefaults(choices)
	}

	if err := promptWizardFlow(root, ui, choices); err != nil {
		if errors.Is(err, errWizardCancelled) || errors.Is(err, errWizardBack) {
			if !freshInstall {
				_, _ = fmt.Fprintln(out, messages.WizardExitWithoutChanges)
			}
			return nil
		}
		return err
	}

	if err := confirmAndApply(root, configPath, envPath, ui, choices, runSync, out, !freshInstall); err != nil {
		if errors.Is(err, errWizardCancelled) || errors.Is(err, errWizardBack) {
			if !freshInstall {
				_, _ = fmt.Fprintln(out, messages.WizardExitWithoutChanges)
			}
			return nil
		}
		return err
	}

	return nil
}

func applyFreshSetupDefaults(choices *Choices) {
	choices.InstallWorkflowBundle = true
	// Status lines stay opt-in: leave the fresh-init toggles unchecked so the
	// interactive wizard does not enable them by default. This matches the
	// non-interactive upgrade default (migration value: false) and the bare
	// `al init` default; the user enables a status line by explicitly checking it.
}

func ensureWizardConfig(root, configPath string, ui UI, pinVersion string, out io.Writer) (bool, bool, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		agentLayerPath := filepath.Join(root, ".agent-layer")
		if info, agentLayerErr := os.Stat(agentLayerPath); agentLayerErr == nil {
			if !info.IsDir() {
				return false, false, fmt.Errorf(messages.RootPathNotDirFmt, agentLayerPath)
			}
			return false, false, fmt.Errorf(messages.WizardPartialInstallUpgradeRequired)
		} else if !os.IsNotExist(agentLayerErr) {
			return false, false, agentLayerErr
		}

		confirm := true
		if err := ui.Confirm(messages.WizardInstallPrompt, &confirm); err != nil {
			return false, false, err
		}
		if !confirm {
			_, _ = fmt.Fprintln(out, messages.WizardExitWithoutChanges)
			return false, false, nil
		}

		if err := install.Run(root, install.Options{
			Overwrite:  false,
			PinVersion: pinVersion,
			System:     install.RealSystem{},
		}); err != nil {
			return false, false, fmt.Errorf(messages.WizardInstallFailedFmt, err)
		}
		_, _ = fmt.Fprintln(out, messages.WizardInstallComplete)
		return true, true, nil
	} else if err != nil {
		return false, false, err
	}

	return true, false, nil
}

func initializeChoices(cfg *config.ProjectConfig) (*Choices, error) {
	choices := NewChoices()

	defaultServers, err := loadDefaultMCPServersFunc()
	if err != nil {
		return nil, fmt.Errorf(messages.WizardLoadDefaultMCPServersFailedFmt, err)
	}
	choices.DefaultMCPServers = defaultServers

	cliSkills, err := loadCLISkillCatalogFunc()
	if err != nil {
		return nil, err
	}
	choices.CLISkillsCatalog = cliSkills
	choices.InstallWorkflowBundle = detectAgentLayerEnabledFromDisk(cfg.Root)
	for _, entry := range cliSkills {
		choices.EnabledCLISkills[entry.ID] = catalogSkillExistsOnDisk(cfg.Root, entry.ID)
	}

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
		choices.ApprovalMode = config.ApprovalModeAll
	}

	agentConfigs := []agentEnabledConfig{
		{id: AgentAntigravity, enabled: cfg.Config.Agents.Antigravity.Enabled},
		{id: AgentClaude, enabled: cfg.Config.Agents.Claude.Enabled},
		{id: AgentClaudeVSCode, enabled: cfg.Config.Agents.ClaudeVSCode.Enabled},
		{id: AgentCodex, enabled: cfg.Config.Agents.Codex.Enabled},
		{id: AgentVSCode, enabled: cfg.Config.Agents.VSCode.Enabled},
		{id: AgentCopilotCLI, enabled: cfg.Config.Agents.CopilotCLI.Enabled},
	}
	setEnabledAgentsFromConfig(choices.EnabledAgents, agentConfigs)

	choices.ClaudeModel = cfg.Config.Agents.Claude.Model
	choices.ClaudeReasoning = cfg.Config.Agents.Claude.ReasoningEffort
	if cfg.Config.Agents.Claude.LocalConfigDir != nil {
		choices.ClaudeLocalConfigDir = *cfg.Config.Agents.Claude.LocalConfigDir
	}
	claudeAgentSpecific := cfg.Config.Agents.Claude.AgentSpecific
	choices.ClaudeDisableIDEReading = readClaudeEnvFalse(claudeAgentSpecific, claudeIDEReadingEnvKey)
	choices.ClaudeDisableConnectors = readClaudeEnvFalse(claudeAgentSpecific, claudeConnectorsEnvKey)
	choices.ClaudeDisableMemory = readClaudeAutoMemoryDisabled(claudeAgentSpecific)
	choices.ClaudeStatusline = cfg.Config.Agents.Claude.Statusline != nil && *cfg.Config.Agents.Claude.Statusline
	if cfg.Config.Agents.Claude.DisableQuestionTool != nil {
		choices.ClaudeDisableQuestionTool = *cfg.Config.Agents.Claude.DisableQuestionTool
	} else {
		// Repos predating the typed flag may block the tool via a legacy/manual
		// agent_specific deny or PreToolUse hook (e.g. the pre-0.11 install seed).
		// Detect that so the prompt and summary reflect the effective state; if the
		// user keeps it, the typed flag is written and sync dedups against the
		// lingering legacy entry.
		choices.ClaudeDisableQuestionTool = readClaudeQuestionToolDisabledLegacy(claudeAgentSpecific)
	}
	choices.CodexModel = cfg.Config.Agents.Codex.Model
	choices.CodexReasoning = cfg.Config.Agents.Codex.ReasoningEffort
	choices.CodexApps = readCodexAppsEnabled(cfg.Config.Agents.Codex.AgentSpecific)
	choices.CodexDisableBrowser = readCodexBrowserDisabled(cfg.Config.Agents.Codex.AgentSpecific)
	choices.CodexStatusline = cfg.Config.Agents.Codex.Statusline != nil && *cfg.Config.Agents.Codex.Statusline
	choices.CopilotCLIModel = cfg.Config.Agents.CopilotCLI.Model

	for _, srv := range cfg.Config.MCP.Servers {
		if config.IsAgentEnabled(srv.Enabled) {
			choices.EnabledMCPServers[srv.ID] = true
		}
	}

	for _, srv := range customMCPServers(choices.DefaultMCPServers, cfg.Config.MCP.Servers) {
		choices.CustomMCPServers = append(choices.CustomMCPServers, srv.ID)
		choices.CustomMCPServersEnabled[srv.ID] = config.IsAgentEnabled(srv.Enabled)
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

// readCodexAppsEnabled returns the current value of
// agents.codex.agent_specific.features.apps, defaulting to false when absent
// or not a bool. The wizard's default is to disable the upstream Codex apps
// surface; absence in config is treated as opting in to that default.
func readCodexAppsEnabled(agentSpecific map[string]any) bool {
	apps, ok := readCodexAppsValue(agentSpecific)
	if !ok {
		return false
	}
	return apps
}

func readCodexAppsValue(agentSpecific map[string]any) (bool, bool) {
	features, ok := agentSpecific["features"].(map[string]any)
	if !ok {
		return false, false
	}
	apps, ok := features["apps"].(bool)
	if !ok {
		return false, false
	}
	return apps, true
}

// Agent-specific keys the wizard's Claude disable toggles read and write. They
// live under .claude/settings.json once synced; the wizard stores them as
// agent_specific passthrough in config.toml.
const (
	claudeIDEReadingEnvKey = "CLAUDE_CODE_AUTO_CONNECT_IDE"
	claudeConnectorsEnvKey = "ENABLE_CLAUDEAI_MCP_SERVERS"
)

// readCodexBrowserDisabled reports whether the Codex browser/computer-use
// features are pinned off (features.browser_use == false). Absence is treated
// as "not disabled" so the client keeps its native default.
func readCodexBrowserDisabled(agentSpecific map[string]any) bool {
	features, ok := agentSpecific["features"].(map[string]any)
	if !ok {
		return false
	}
	enabled, ok := features["browser_use"].(bool)
	return ok && !enabled
}

// readClaudeEnvFalse reports whether agent_specific.env[key] is the string
// "false" (settings.json env values are JSON strings, not booleans).
func readClaudeEnvFalse(agentSpecific map[string]any, key string) bool {
	env, ok := agentSpecific["env"].(map[string]any)
	if !ok {
		return false
	}
	value, ok := env[key].(string)
	return ok && value == "false"
}

// readClaudeAutoMemoryDisabled reports whether agent_specific.autoMemoryEnabled
// is the boolean false.
func readClaudeAutoMemoryDisabled(agentSpecific map[string]any) bool {
	enabled, ok := agentSpecific["autoMemoryEnabled"].(bool)
	return ok && !enabled
}

// readClaudeQuestionToolDisabledLegacy detects the pre-typed-flag form of the
// AskUserQuestion block — a permissions.deny entry or a PreToolUse matcher in
// agent_specific — so the wizard prompt defaults to Yes for repos that blocked
// the tool before disable_question_tool existed. Only consulted when the typed
// flag is unset. agent_specific arrays decode as []any (go-toml/v2).
func readClaudeQuestionToolDisabledLegacy(agentSpecific map[string]any) bool {
	const askUserQuestionTool = "AskUserQuestion"
	if permissions, ok := agentSpecific["permissions"].(map[string]any); ok {
		if deny, ok := permissions["deny"].([]any); ok {
			for _, v := range deny {
				if s, ok := v.(string); ok && s == askUserQuestionTool {
					return true
				}
			}
		}
	}
	if hooks, ok := agentSpecific["hooks"].(map[string]any); ok {
		if entries, ok := hooks["PreToolUse"].([]any); ok {
			for _, entry := range entries {
				if m, ok := entry.(map[string]any); ok {
					if matcher, ok := m["matcher"].(string); ok && matcher == askUserQuestionTool {
						return true
					}
				}
			}
		}
	}
	return false
}

type wizardFlowStep int

const (
	wizardFlowStepApproval wizardFlowStep = iota
	wizardFlowStepAgents
	wizardFlowStepModels
	wizardFlowStepEnableLayer
	wizardFlowStepCLISkills
	wizardFlowStepMCPDefaults
	wizardFlowStepCustomMCP
	wizardFlowStepSecrets
	wizardFlowStepWarnings
)

// promptWizardFlow drives the step-by-step prompt loop.
func promptWizardFlow(root string, ui UI, choices *Choices) error {
	// The custom-MCP step has nothing to ask when config.toml has no non-catalog
	// servers. CustomMCPServers is set before the flow and never mutated by it, so
	// skip the step in both directions to avoid trapping back-navigation on a
	// no-op screen (the same pattern as the skippable secrets step).
	skipCustomMCPStep := len(choices.CustomMCPServers) == 0
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
		case wizardFlowStepEnableLayer:
			err = promptEnableAgentLayer(ui, choices)
		case wizardFlowStepCLISkills:
			err = promptCLISkills(ui, choices)
		case wizardFlowStepMCPDefaults:
			err = promptDefaultMCPServers(ui, choices)
		case wizardFlowStepCustomMCP:
			err = promptCustomMCPServers(ui, choices)
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
			if skipCustomMCPStep && step == wizardFlowStepCustomMCP {
				step++
			}
			if step == wizardFlowStepSecrets && !secretsStepHasPrompts(choices) {
				step++
			}
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
		if step == wizardFlowStepSecrets && !secretsStepHasPrompts(choices) {
			step--
		}
		if skipCustomMCPStep && step == wizardFlowStepCustomMCP {
			step--
		}
	}
}

// secretsStepHasPrompts reports whether promptSecrets would render at least one
// prompt for the current choices. The secrets step is a no-op when no enabled
// default MCP server has a required-env key still missing from choices.Secrets;
// in that case it must be skipped in both directions so it does not trap back
// navigation (the same reason wizardFlowStepCustomMCP is skipped). This must
// stay in sync with the gating
// conditions at the top of promptSecrets's loop.
func secretsStepHasPrompts(choices *Choices) bool {
	for _, server := range choices.DefaultMCPServers {
		if !choices.EnabledMCPServers[server.ID] || len(server.RequiredEnv) == 0 {
			continue
		}
		for _, key := range server.RequiredEnv {
			if key == "" {
				continue
			}
			if existing, ok := choices.Secrets[key]; ok && existing != "" {
				continue
			}
			return true
		}
	}
	return false
}

// promptEnableAgentLayer asks whether to install or refresh the workflow bundle.
// A no answer is a no-op for existing files.
func promptEnableAgentLayer(ui UI, choices *Choices) error {
	installWorkflowBundle := choices.InstallWorkflowBundle
	if err := ui.Confirm(messages.WizardEnableAgentLayerPrompt, &installWorkflowBundle); err != nil {
		return err
	}
	choices.InstallWorkflowBundle = installWorkflowBundle
	choices.InstallWorkflowBundleTouched = true
	return nil
}

// promptCLISkills presents the CLI skills catalog multiselect. Each row label
// is the catalog entry's user-facing Name, while EnabledCLISkills keys use the
// catalog id. The mapping between names and ids is rebuilt from the catalog so
// renaming a label in the TOML does not corrupt the selection.
func promptCLISkills(ui UI, choices *Choices) error {
	catalog := choices.CLISkillsCatalog
	labels := make([]string, 0, len(catalog))
	labelToID := make(map[string]string, len(catalog))
	enabledLabels := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		labels = append(labels, entry.Name)
		labelToID[entry.Name] = entry.ID
		if choices.EnabledCLISkills[entry.ID] {
			enabledLabels = append(enabledLabels, entry.Name)
		}
	}
	if err := ui.MultiSelect(messages.WizardEnableCLISkillsTitle, labels, &enabledLabels); err != nil {
		return err
	}
	for _, entry := range catalog {
		choices.EnabledCLISkills[entry.ID] = false
	}
	for _, label := range enabledLabels {
		choices.EnabledCLISkills[labelToID[label]] = true
	}
	return nil
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
	if !choices.EnabledAgents[AgentCodex] {
		choices.CodexApps = false
		choices.CodexAppsTouched = false
		choices.CodexDisableBrowser = false
		choices.CodexDisableBrowserTouched = false
		choices.CodexStatusline = false
		choices.CodexStatuslineTouched = false
	}
	if !choices.EnabledAgents[AgentClaude] && !choices.EnabledAgents[AgentClaudeVSCode] {
		choices.ClaudeDisableIDEReading = false
		choices.ClaudeDisableIDEReadingTouched = false
		choices.ClaudeDisableMemory = false
		choices.ClaudeDisableMemoryTouched = false
		choices.ClaudeDisableConnectors = false
		choices.ClaudeDisableConnectorsTouched = false
		choices.ClaudeDisableQuestionTool = false
		choices.ClaudeDisableQuestionToolTouched = false
		choices.ClaudeStatusline = false
		choices.ClaudeStatuslineTouched = false
	}
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
	if choices.EnabledAgents[AgentClaude] {
		if err := selectOptionalValue(ui, messages.WizardClaudeModelTitle, ClaudeModels(), &choices.ClaudeModel); err != nil {
			return err
		}
		choices.ClaudeModelTouched = true
		// Reasoning effort is offered regardless of model. Claude Code is the
		// authority on which model/effort combinations apply, so the wizard does
		// not gate or clear the choice based on the selected model.
		if err := selectOptionalValue(ui, messages.WizardClaudeReasoningEffortTitle, ClaudeReasoningEfforts(), &choices.ClaudeReasoning); err != nil {
			return err
		}
		choices.ClaudeReasoningTouched = true
	}
	if choices.EnabledAgents[AgentClaude] || choices.EnabledAgents[AgentClaudeVSCode] {
		claudeLocalConfigDir := choices.ClaudeLocalConfigDir
		if err := ui.Confirm(messages.WizardClaudeLocalConfigDirPrompt, &claudeLocalConfigDir); err != nil {
			return err
		}
		choices.ClaudeLocalConfigDir = claudeLocalConfigDir
		choices.ClaudeLocalConfigDirTouched = true

		// Per-feature toggles as one multi-select. Checked = keep the feature
		// enabled (Claude Code's native default); unchecking sets the disable-sense
		// field. Both Claude and Claude (VS Code) write .claude/settings.json, so
		// these are offered whenever either is enabled.
		if err := promptFeatureToggles(ui, messages.WizardClaudeFeaturesTitle, []featureToggle{
			{label: messages.WizardClaudeFeatureStatuslineLabel, field: &choices.ClaudeStatusline, touched: &choices.ClaudeStatuslineTouched, enabledSense: true},
			{label: messages.WizardClaudeFeatureIDEReadingLabel, field: &choices.ClaudeDisableIDEReading, touched: &choices.ClaudeDisableIDEReadingTouched},
			{label: messages.WizardClaudeFeatureMemoryLabel, field: &choices.ClaudeDisableMemory, touched: &choices.ClaudeDisableMemoryTouched},
			{label: messages.WizardClaudeFeatureConnectorsLabel, field: &choices.ClaudeDisableConnectors, touched: &choices.ClaudeDisableConnectorsTouched},
			{label: messages.WizardClaudeFeatureQuestionToolLabel, field: &choices.ClaudeDisableQuestionTool, touched: &choices.ClaudeDisableQuestionToolTouched},
		}); err != nil {
			return err
		}
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

		// Codex per-feature toggles as one multi-select. Built-in apps store
		// enabled-sense (true = apps on); browser/computer-use stores disable-sense
		// like the Claude fields. Both are inverted at the prompt boundary so the
		// checkbox always means "keep enabled".
		if err := promptFeatureToggles(ui, messages.WizardCodexFeaturesTitle, []featureToggle{
			{label: messages.WizardCodexFeatureStatuslineLabel, field: &choices.CodexStatusline, touched: &choices.CodexStatuslineTouched, enabledSense: true},
			{label: messages.WizardCodexFeatureAppsLabel, field: &choices.CodexApps, touched: &choices.CodexAppsTouched, enabledSense: true},
			{label: messages.WizardCodexFeatureBrowserLabel, field: &choices.CodexDisableBrowser, touched: &choices.CodexDisableBrowserTouched},
		}); err != nil {
			return err
		}
	}
	if choices.EnabledAgents[AgentCopilotCLI] {
		if err := selectOptionalValue(ui, messages.WizardCopilotCLIModelTitle, CopilotCLIModels(), &choices.CopilotCLIModel); err != nil {
			return err
		}
		choices.CopilotCLIModelTouched = true
	}

	return nil
}

func promptDefaultMCPServers(ui UI, choices *Choices) error {
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

// promptCustomMCPServers asks whether to keep or disable the MCP servers in
// config.toml that are not catalog defaults. Selected servers stay enabled;
// unselected servers are set to enabled = false with their definition preserved
// (a custom server has no catalog template to restore from, so it is never
// deleted). The caller (promptWizardFlow) skips this step when there are no
// custom servers, so CustomMCPServers is non-empty here.
func promptCustomMCPServers(ui UI, choices *Choices) error {
	keptServers := make([]string, 0, len(choices.CustomMCPServers))
	for _, id := range choices.CustomMCPServers {
		if choices.CustomMCPServersEnabled[id] {
			keptServers = append(keptServers, id)
		}
	}
	if err := ui.MultiSelect(messages.WizardKeepCustomMCPServersTitle, choices.CustomMCPServers, &keptServers); err != nil {
		return err
	}
	for _, id := range choices.CustomMCPServers {
		choices.CustomMCPServersEnabled[id] = false
	}
	for _, id := range keptServers {
		choices.CustomMCPServersEnabled[id] = true
	}
	choices.CustomMCPServersTouched = true
	return nil
}

func confirmAndApply(root, configPath, envPath string, ui UI, choices *Choices, runSync syncer, out io.Writer, printExitWithoutChanges bool) error {
	summary := buildSummary(choices)
	confirmApply := true
	if err := ui.Note(messages.WizardSummaryTitle, summary); err != nil {
		return err
	}
	rewritePreview, err := buildRewritePreview(configPath, envPath, choices)
	if err != nil {
		return err
	}
	skillsChangeSet, err := computeSkillsChangeSet(root, choices)
	if err != nil {
		return err
	}
	if skillsPreview := buildSkillsPreview(skillsChangeSet); skillsPreview != "" {
		if rewritePreview == "" || strings.HasPrefix(rewritePreview, "No rewrites needed") {
			rewritePreview = skillsPreview
		} else {
			rewritePreview = rewritePreview + "\n\n" + skillsPreview
		}
	}
	statuslineSourceChangeSet, err := computeStatuslineSourceChangeSet(root, choices)
	if err != nil {
		return err
	}
	if statuslinePreview := buildStatuslineSourcePreview(statuslineSourceChangeSet); statuslinePreview != "" {
		if rewritePreview == "" || strings.HasPrefix(rewritePreview, "No rewrites needed") {
			rewritePreview = statuslinePreview
		} else {
			rewritePreview = rewritePreview + "\n\n" + statuslinePreview
		}
	}
	if err := ui.Note(messages.WizardRewritePreviewTitle, rewritePreview); err != nil {
		return err
	}
	if err := ui.Confirm(messages.WizardApplyChangesPrompt, &confirmApply); err != nil {
		return err
	}
	if !confirmApply {
		if printExitWithoutChanges {
			_, _ = fmt.Fprintln(out, messages.WizardExitWithoutChanges)
		}
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
	if b, err := os.ReadFile(envPath); err == nil { // #nosec G304 -- envPath is the caller-resolved .agent-layer/.env path used by wizard prompts.
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
	currentConfigBytes, err := os.ReadFile(configPath) // #nosec G304 -- configPath is the caller-resolved .agent-layer/config.toml path used for the rewrite preview.
	if err != nil {
		return "", err
	}
	nextConfig, err := PatchConfig(string(currentConfigBytes), choices)
	if err != nil {
		return "", fmt.Errorf(messages.WizardPatchConfigFailedFmt, err)
	}

	currentEnvBytes, err := os.ReadFile(envPath) // #nosec G304 -- envPath is the caller-resolved .agent-layer/.env path used for the rewrite preview.
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	nextEnv := envfile.Patch(string(currentEnvBytes), choices.Secrets)
	redactedCurrentEnv, redactedNextEnv, err := redactEnvPreviewContent(string(currentEnvBytes), nextEnv)
	if err != nil {
		return "", err
	}

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
		redactedCurrentEnv,
		redactedNextEnv,
	))
	if envDiff != "" {
		parts = append(parts, "Secret values are redacted in the .env preview.\n"+envDiff)
	}

	if len(parts) == 0 {
		return "No rewrites needed. Current files already match the selected changes.", nil
	}
	return strings.Join(parts, "\n\n"), nil
}

func redactEnvPreviewContent(currentContent string, nextContent string) (string, string, error) {
	currentValues, err := envfile.Parse(currentContent)
	if err != nil {
		return "", "", fmt.Errorf(messages.WizardInvalidEnvFileFmt, ".agent-layer/.env", err)
	}
	nextValues, err := envfile.Parse(nextContent)
	if err != nil {
		return "", "", fmt.Errorf(messages.WizardInvalidEnvFileFmt, ".agent-layer/.env", err)
	}
	return redactEnvPreviewSide(currentContent, currentValues, nextValues, true),
		redactEnvPreviewSide(nextContent, nextValues, currentValues, false),
		nil
}

func redactEnvPreviewSide(content string, thisValues map[string]string, otherValues map[string]string, currentSide bool) string {
	if content == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		prefix, key, suffix, ok := parseEnvPreviewLine(line)
		if !ok {
			continue
		}
		thisValue, thisOK := thisValues[key]
		otherValue, otherOK := otherValues[key]
		lines[i] = fmt.Sprintf("%s%s=%q%s", prefix, key, redactedEnvPreviewValue(thisValue, thisOK, otherValue, otherOK, currentSide), suffix)
	}
	return strings.Join(lines, "\n")
}

func parseEnvPreviewLine(line string) (string, string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", "", false
	}
	prefix := ""
	if strings.HasPrefix(trimmed, "export ") {
		prefix = "export "
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
	}
	idx := strings.Index(trimmed, "=")
	if idx <= 0 {
		return "", "", "", false
	}
	key := strings.TrimSpace(trimmed[:idx])
	if key == "" {
		return "", "", "", false
	}
	return prefix, key, envPreviewTrailingComment(trimmed[idx+1:]), true
}

func envPreviewTrailingComment(rawValue string) string {
	value := strings.TrimSpace(rawValue)
	if len(value) < 2 {
		return ""
	}

	var closing int
	switch value[0] {
	case '"':
		closing = findEnvPreviewClosingDoubleQuote(value)
	case '\'':
		closingOffset := strings.IndexByte(value[1:], '\'')
		if closingOffset < 0 {
			return ""
		}
		closing = 1 + closingOffset
	default:
		return ""
	}

	if closing < 0 {
		return ""
	}
	suffix := value[closing+1:]
	if strings.HasPrefix(strings.TrimSpace(suffix), "#") {
		return suffix
	}
	return ""
}

func findEnvPreviewClosingDoubleQuote(value string) int {
	escaped := false
	for i := 1; i < len(value); i++ {
		if escaped {
			escaped = false
			continue
		}
		switch value[i] {
		case '\\':
			escaped = true
		case '"':
			return i
		}
	}
	return -1
}

func redactedEnvPreviewValue(thisValue string, thisOK bool, otherValue string, otherOK bool, currentSide bool) string {
	if !thisOK || thisValue == "" {
		return ""
	}
	if otherOK && thisValue == otherValue {
		return "<redacted>"
	}
	if currentSide {
		return "<redacted current>"
	}
	return "<redacted proposed>"
}
